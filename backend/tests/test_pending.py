from __future__ import annotations

import threading

import pytest

from backend.services.pending import PendingTurnRegistry


def make_registry() -> PendingTurnRegistry:
    return PendingTurnRegistry()


# ---------------------------------------------------------------------------
# register
# ---------------------------------------------------------------------------

class TestRegister:
    def test_register_returns_pending_turn(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        assert p.conversation_id == "c1"
        assert p.owner_id == "u1"
        assert p.model == "m"
        assert p.input_text == "hi"

    def test_request_id_is_unique(self):
        reg = make_registry()
        p1 = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="a")
        reg.discard(conversation_id="c1", owner_id="u1")
        p2 = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="b")
        assert p1.request_id != p2.request_id

    def test_duplicate_conversation_raises(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        with pytest.raises(ValueError, match="waiting for a reply"):
            reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi again")

    def test_heartbeat_interval_negative_clamped_to_zero(self):
        reg = make_registry()
        p = reg.register(
            conversation_id="c1", owner_id="u1", model="m", input_text="hi",
            heartbeat_interval_seconds=-5.0,
        )
        assert p.heartbeat_interval_seconds == 0.0


# ---------------------------------------------------------------------------
# get_by_conversation
# ---------------------------------------------------------------------------

class TestGetByConversation:
    def test_found(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        assert reg.get_by_conversation("c1") is p

    def test_not_found(self):
        reg = make_registry()
        assert reg.get_by_conversation("nonexistent") is None


# ---------------------------------------------------------------------------
# resolve
# ---------------------------------------------------------------------------

class TestResolve:
    def test_resolve_sets_event(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="Hello!", response_id="resp_1",
        )
        assert p.event.is_set()
        assert p.assistant_text == "Hello!"
        assert p.resolved is True

    def test_resolve_unknown_conversation_raises(self):
        reg = make_registry()
        with pytest.raises(ValueError):
            reg.resolve(
                conversation_id="nope", owner_id="u1",
                assistant_text="x", response_id="r",
            )

    def test_resolve_wrong_owner_raises(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        with pytest.raises(ValueError):
            reg.resolve(
                conversation_id="c1", owner_id="other",
                assistant_text="x", response_id="r",
            )

    def test_resolve_tool_call_mode(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        item = {"call_id": "call_abc", "name": "get_weather", "arguments": '{"city":"Beijing"}'}
        p = reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="",
            response_id="resp_1",
            response_mode="tool_call",
            response_output_items=[item],
        )
        assert p.response_mode == "tool_call"
        assert p.response_output_items == [item]

    def test_resolve_tool_call_items_are_copied(self):
        """resolve 后修改原列表不影响已存储的 items。"""
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        src = [{"call_id": "call_1", "name": "fn", "arguments": "{}"}]
        reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="", response_id="r",
            response_mode="tool_call",
            response_output_items=src,
        )
        src.clear()
        assert p.response_output_items == [{"call_id": "call_1", "name": "fn", "arguments": "{}"}]

    def test_resolve_default_mode_is_assistant_message(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="Hello", response_id="r",
        )
        assert p.response_mode == "assistant_message"
        assert p.response_output_items == []

    def test_double_resolve_raises(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="x", response_id="r",
        )
        with pytest.raises(ValueError):
            reg.resolve(
                conversation_id="c1", owner_id="u1",
                assistant_text="y", response_id="r2",
            )


# ---------------------------------------------------------------------------
# add_draft / consume_draft_chunks
# ---------------------------------------------------------------------------

class TestDraft:
    def test_draft_chunks_accumulated_and_consumed(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.add_draft(conversation_id="c1", owner_id="u1", chunk="Hello")
        reg.add_draft(conversation_id="c1", owner_id="u1", chunk=" world")
        chunks = reg.consume_draft_chunks(p.request_id)
        assert chunks == ["Hello", " world"]
        assert reg.consume_draft_chunks(p.request_id) == []

    def test_draft_sets_stream_event(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.add_draft(conversation_id="c1", owner_id="u1", chunk="x")
        assert p.stream_event.is_set()

    def test_draft_after_resolve_raises(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.resolve(
            conversation_id="c1", owner_id="u1",
            assistant_text="done", response_id="r",
        )
        with pytest.raises(ValueError):
            reg.add_draft(conversation_id="c1", owner_id="u1", chunk="late")


# ---------------------------------------------------------------------------
# abort
# ---------------------------------------------------------------------------

class TestAbort:
    def test_abort_sets_aborted_flag(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.abort(conversation_id="c1", owner_id="u1", error_message="overloaded")
        assert p.aborted is True
        assert p.abort_message == "overloaded"
        assert p.event.is_set()

    def test_abort_unknown_conversation_raises(self):
        reg = make_registry()
        with pytest.raises(ValueError):
            reg.abort(conversation_id="nope", owner_id="u1", error_message="err")


# ---------------------------------------------------------------------------
# discard
# ---------------------------------------------------------------------------

class TestDiscard:
    def test_discard_removes_pending(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        reg.discard(conversation_id="c1", owner_id="u1")
        assert reg.get_by_conversation("c1") is None

    def test_discard_nonexistent_returns_none(self):
        reg = make_registry()
        assert reg.discard(conversation_id="nope", owner_id="u1") is None

    def test_discard_wrong_owner_returns_none(self):
        reg = make_registry()
        reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")
        result = reg.discard(conversation_id="c1", owner_id="other")
        assert result is None
        assert reg.get_by_conversation("c1") is not None


# ---------------------------------------------------------------------------
# wait
# ---------------------------------------------------------------------------

class TestWait:
    def test_wait_returns_resolved_turn(self):
        reg = make_registry()
        p = reg.register(conversation_id="c1", owner_id="u1", model="m", input_text="hi")

        def resolve_after_delay():
            reg.resolve(
                conversation_id="c1", owner_id="u1",
                assistant_text="Answer", response_id="resp_1",
            )

        t = threading.Thread(target=resolve_after_delay)
        t.start()
        waited = reg.wait(p.request_id)
        t.join()

        assert waited.assistant_text == "Answer"
        assert reg.get_by_conversation("c1") is None  # 已从注册表移除

    def test_wait_unknown_request_raises(self):
        reg = make_registry()
        with pytest.raises(ValueError, match="not found"):
            reg.wait("nonexistent_request_id")
