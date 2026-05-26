from __future__ import annotations

import uuid
from typing import Any, Callable

from ..repositories import ConversationStore
from .pending import PendingTurnRegistry
from .thinking import compose_thinking_text
from .turn_protocols import build_protocol_response_id, normalize_message_text


def _normalize_reasoning_stream_mode(value: str) -> str:
    mode = str(value or "").strip().lower().replace("-", "_")
    if mode == "summery":
        mode = "summary"
    elif mode == "reasoning":
        mode = "reasoning_text"
    if mode in {"summary", "reasoning_text"}:
        return mode
    return ""


class TurnOutputController:
    def __init__(
        self,
        *,
        store: ConversationStore,
        pending_turns: PendingTurnRegistry,
        publish_sync: Callable[[str, str | None], None] | None = None,
    ):
        self._store = store
        self._pending_turns = pending_turns
        self._publish_sync = publish_sync

    def add_text_delta(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        text: str,
        reasoning_stream_mode: str = "",
        kind: str = "answer",
    ):
        pending = self._pending_turns.add_draft(
            conversation_id=conversation_id,
            owner_id=owner_id,
            chunk=normalize_message_text(text),
            kind=kind,
        )
        if pending.aborted:
            self._mark_aborted_conversation(pending)
            raise ValueError(pending.abort_message or "request aborted")
        self._apply_reasoning_stream_mode(
            pending,
            conversation_id=conversation_id,
            owner_id=owner_id,
            reasoning_stream_mode=reasoning_stream_mode,
        )
        conversation = self._store.get_conversation(conversation_id, owner_id)
        if conversation is not None:
            self._store.update_conversation(
                conversation_id,
                owner_id,
                metadata={
                    **conversation.metadata,
                    "realtime_status": "waiting",
                    "realtime_draft_text": pending.draft_text,
                    **(
                        {"request_format": pending.request_format}
                        if pending.request_format
                        else {}
                    ),
                    **(
                        {"reasoning_stream_mode": pending.reasoning_stream_mode}
                        if pending.request_format == "responses" and pending.reasoning_stream_mode
                        else {}
                    ),
                },
            )
            self._notify(owner_id, conversation_id)
        return pending

    def complete_assistant_message(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        provider: str,
        model: str | None = None,
        reasoning_stream_mode: str = "",
    ):
        pending = self._require_pending(conversation_id, owner_id)
        self._apply_reasoning_stream_mode(
            pending,
            conversation_id=conversation_id,
            owner_id=owner_id,
            reasoning_stream_mode=reasoning_stream_mode,
        )
        assistant_text = (
            compose_thinking_text(pending.draft_segments)
            if pending.request_format != "responses" and pending.draft_segments
            else pending.draft_answer_text or pending.draft_text
        )
        if not assistant_text.strip():
            raise ValueError("assistant message text is required")
        response_id = build_protocol_response_id(pending.request_format, pending.request_id)
        assistant_metadata = {
            "provider": provider,
            "model": str(model or pending.model or "mock-gpt-4.1-mini"),
            "response_mode": "assistant_message",
        }
        updated_conversation = self._store.record_assistant_reply(
            conversation_id,
            owner_id,
            pending.input_text,
            assistant_text,
            response_id=response_id,
            assistant_metadata=assistant_metadata,
        )
        self._store.update_conversation(
            conversation_id,
            owner_id,
            metadata={
                **updated_conversation.metadata,
                "realtime_status": "closed",
                "realtime_draft_text": "",
                "request_format": pending.request_format,
                **(
                    {"reasoning_stream_mode": pending.reasoning_stream_mode}
                    if pending.request_format == "responses" and pending.reasoning_stream_mode
                    else {}
                ),
            },
        )
        self._notify(owner_id, conversation_id)
        resolved = self._pending_turns.resolve(
            conversation_id=conversation_id,
            owner_id=owner_id,
            assistant_text=assistant_text,
            response_id=response_id,
            response_mode="assistant_message",
            response_output_items=[],
            response_output_text=assistant_text,
        )
        return resolved, assistant_metadata

    def complete_tool_call(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        tool_name: str,
        arguments: str,
        provider: str,
        model: str | None = None,
        tool_call_id: str | None = None,
        reasoning_stream_mode: str = "",
    ):
        pending = self._require_pending(conversation_id, owner_id)
        self._apply_reasoning_stream_mode(
            pending,
            conversation_id=conversation_id,
            owner_id=owner_id,
            reasoning_stream_mode=reasoning_stream_mode,
        )
        call_id = tool_call_id or f"call_{uuid.uuid4().hex[:24]}"
        assistant_text = f"{tool_name}({arguments})"
        aborted = self._pending_turns.abort_if_output_would_exceed(
            request_id=pending.request_id,
            extra_text=assistant_text,
        )
        if aborted is not None:
            self._mark_aborted_conversation(aborted)
            raise ValueError(aborted.abort_message or "request aborted")
        response_id = build_protocol_response_id(pending.request_format, pending.request_id)
        output_items = [
            {
                "id": f"fc_{uuid.uuid4().hex[:24]}",
                "type": "function_call",
                "status": "completed",
                "call_id": call_id,
                "name": tool_name,
                "arguments": arguments,
            }
        ]
        assistant_metadata = {
            "provider": provider,
            "model": str(model or pending.model or "mock-gpt-4.1-mini"),
            "response_mode": "tool_call",
            "tool_name": tool_name,
            "tool_call_id": call_id,
            "arguments": arguments,
        }
        updated_conversation = self._store.record_assistant_reply(
            conversation_id,
            owner_id,
            pending.input_text,
            assistant_text,
            response_id=response_id,
            assistant_metadata=assistant_metadata,
        )
        self._store.update_conversation(
            conversation_id,
            owner_id,
            metadata={
                **updated_conversation.metadata,
                "realtime_status": "closed",
                "realtime_draft_text": "",
                "request_format": pending.request_format,
                **(
                    {"reasoning_stream_mode": pending.reasoning_stream_mode}
                    if pending.request_format == "responses" and pending.reasoning_stream_mode
                    else {}
                ),
            },
        )
        self._notify(owner_id, conversation_id)
        resolved = self._pending_turns.resolve(
            conversation_id=conversation_id,
            owner_id=owner_id,
            assistant_text=assistant_text,
            response_id=response_id,
            response_mode="tool_call",
            response_output_items=output_items,
            response_output_text="",
        )
        return resolved, assistant_metadata

    def abort(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        error_message: str,
    ):
        pending = self._pending_turns.abort(
            conversation_id=conversation_id,
            owner_id=owner_id,
            error_message=error_message,
        )
        self._mark_aborted_conversation(pending)
        return pending

    def _mark_aborted_conversation(self, pending):
        conversation = self._store.get_conversation(pending.conversation_id, pending.owner_id)
        if conversation is None:
            return
        self._store.update_conversation(
            pending.conversation_id,
            pending.owner_id,
            metadata={
                **conversation.metadata,
                "realtime_status": "aborted",
                "realtime_draft_text": "",
            },
        )
        self._notify(pending.owner_id, pending.conversation_id)

    def _require_pending(self, conversation_id: str, owner_id: str):
        pending = self._pending_turns.get_by_conversation(conversation_id)
        if pending is None or pending.owner_id != owner_id:
            raise ValueError("conversation is not waiting for a reply")
        return pending

    def _apply_reasoning_stream_mode(
        self,
        pending,
        *,
        conversation_id: str,
        owner_id: str,
        reasoning_stream_mode: str,
    ) -> None:
        if pending.request_format != "responses":
            return

        requested_mode = _normalize_reasoning_stream_mode(reasoning_stream_mode)
        if requested_mode:
            pending.reasoning_stream_mode = requested_mode

    def _notify(self, owner_id: str, conversation_id: str) -> None:
        if self._publish_sync is None:
            return
        self._publish_sync(owner_id, conversation_id)
