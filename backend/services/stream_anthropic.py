from __future__ import annotations

import time
import uuid
from typing import Any, Callable, Generator

from flask import stream_with_context

from .pending import PendingTurn, PendingTurnRegistry
from .realtime import ConnectionLease, RealtimeBroker
from .response_payloads import estimate_usage
from .stream_common import (
    abort_pending_if_expired,
    build_stream_response,
    client_disconnected,
    discard_pending_turn,
    sse_event,
)
from .thinking import ThinkingStreamParser


def stream_anthropic_turn(
    pending: PendingTurn,
    *,
    pending_turns: PendingTurnRegistry,
    store: Any,
    build_abort_error: Callable[[str], tuple[dict[str, Any], int]],
    client_socket: Any,
    publish_sync: Callable[[str, str | None], None] | None = None,
    connection_lease: ConnectionLease | None = None,
    realtime: RealtimeBroker | None = None,
):
    message_id = pending.response_id or f"msg_{uuid.uuid4().hex[:24]}"

    def generate():
        sent_raw_text = ""
        block_index = 0
        open_text_block: dict[str, Any] | None = None
        parser = ThinkingStreamParser()

        def emit(payload: dict[str, Any]) -> str:
            event_name = str(payload.get("type") or "message")
            return sse_event(event_name, payload)

        def next_block_index() -> int:
            nonlocal block_index
            current = block_index
            block_index += 1
            return current

        def start_text_block() -> Generator[str, None, None]:
            nonlocal open_text_block
            index = next_block_index()
            open_text_block = {"index": index, "text": ""}
            yield emit(
                {
                    "type": "content_block_start",
                    "index": index,
                    "content_block": {"type": "text", "text": ""},
                }
            )

        def close_text_block() -> Generator[str, None, None]:
            nonlocal open_text_block
            if open_text_block is None:
                return
            index = int(open_text_block["index"])
            yield emit({"type": "content_block_stop", "index": index})
            open_text_block = None

        def emit_text_delta(text: str) -> Generator[str, None, None]:
            nonlocal open_text_block
            if not text:
                return
            if open_text_block is None:
                yield from start_text_block()
            assert open_text_block is not None
            index = int(open_text_block["index"])
            open_text_block["text"] = str(open_text_block["text"]) + text
            yield emit(
                {
                    "type": "content_block_delta",
                    "index": index,
                    "delta": {
                        "type": "text_delta",
                        "text": text,
                    },
                }
            )

        def emit_thinking_block(text: str) -> Generator[str, None, None]:
            if not text:
                return
            yield from close_text_block()
            index = next_block_index()
            yield emit(
                {
                    "type": "content_block_start",
                    "index": index,
                    "content_block": {
                        "type": "thinking",
                        "thinking": "",
                        "signature": "mock-thinking",
                    },
                }
            )
            yield emit(
                {
                    "type": "content_block_delta",
                    "index": index,
                    "delta": {
                        "type": "thinking_delta",
                        "thinking": text,
                    },
                }
            )
            yield emit(
                {
                    "type": "content_block_delta",
                    "index": index,
                    "delta": {
                        "type": "signature_delta",
                        "signature": "mock-thinking",
                    },
                }
            )
            yield emit({"type": "content_block_stop", "index": index})

        def emit_parsed_text(text: str = "", *, flush: bool = False) -> Generator[str, None, None]:
            parts = parser.flush() if flush else parser.feed(text)
            for part in parts:
                if part["type"] == "thinking":
                    yield from emit_thinking_block(part["text"])
                else:
                    yield from emit_text_delta(part["text"])

        try:
            yield emit(
                {
                    "type": "message_start",
                    "message": {
                        "id": message_id,
                        "type": "message",
                        "role": "assistant",
                        "model": pending.model,
                        "content": [],
                        "usage": {
                            "input_tokens": estimate_usage(pending.input_text, "")["input_tokens"],
                            "output_tokens": 0,
                        },
                    },
                }
            )

            while True:
                if client_disconnected(client_socket):
                    discard_pending_turn(
                        pending,
                        pending_turns=pending_turns,
                        store=store,
                        publish_sync=publish_sync,
                    )
                    return

                abort_pending_if_expired(
                    pending,
                    pending_turns=pending_turns,
                    store=store,
                    publish_sync=publish_sync,
                )

                for piece in pending_turns.consume_draft_chunks(pending.request_id):
                    if isinstance(piece, dict):
                        piece_text = str(piece.get("text") or "")
                        if str(piece.get("kind") or "").strip() == "thinking":
                            yield from emit_thinking_block(piece_text)
                        else:
                            sent_raw_text += piece_text
                            yield from emit_text_delta(piece_text)
                        continue
                    sent_raw_text += piece
                    yield from emit_parsed_text(piece)

                if pending.event.is_set():
                    finalized = pending_turns.wait(pending.request_id)
                    if finalized.aborted:
                        error_body, _ = build_abort_error(
                            finalized.abort_message or "request aborted"
                        )
                        yield emit({"type": "error", "error": error_body["error"]})
                        return
                    usage = estimate_usage(finalized.input_text, finalized.assistant_text)
                    if finalized.response_mode == "tool_call":
                        yield from emit_parsed_text(flush=True)
                        yield from close_text_block()
                        metadata = finalized.response_output_items[0] if finalized.response_output_items else {}
                        index = next_block_index()
                        yield emit(
                            {
                                "type": "content_block_start",
                                "index": index,
                                "content_block": {
                                    "type": "tool_use",
                                    "id": str(metadata.get("call_id", "")),
                                    "name": str(metadata.get("name", "")),
                                    "input": {},
                                },
                            }
                        )
                        yield emit(
                            {
                                "type": "content_block_delta",
                                "index": index,
                                "delta": {
                                    "type": "input_json_delta",
                                    "partial_json": str(metadata.get("arguments", "")),
                                },
                            }
                        )
                        yield emit({"type": "content_block_stop", "index": index})
                        stop_reason = "tool_use"
                    else:
                        remaining = finalized.assistant_text
                        if remaining.startswith(sent_raw_text):
                            remaining = remaining[len(sent_raw_text):]
                        if remaining:
                            sent_raw_text += remaining
                            yield from emit_parsed_text(remaining)
                        yield from emit_parsed_text(flush=True)
                        yield from close_text_block()
                        stop_reason = "end_turn"
                    yield emit(
                        {
                            "type": "message_delta",
                            "delta": {
                                "stop_reason": stop_reason,
                                "stop_sequence": None,
                            },
                            "usage": {
                                "input_tokens": usage["input_tokens"],
                                "output_tokens": usage["output_tokens"],
                                "cache_creation_input_tokens": 0,
                                "cache_read_input_tokens": 0,
                            },
                        }
                    )
                    yield emit({"type": "message_stop"})
                    return

                pending.stream_event.wait(0.5)
                pending.stream_event.clear()
        except GeneratorExit:
            discard_pending_turn(
                pending,
                pending_turns=pending_turns,
                store=store,
                publish_sync=publish_sync,
            )
            raise
        finally:
            if connection_lease is not None and realtime is not None:
                realtime.release_connection(connection_lease)

    return build_stream_response(stream_with_context(generate()))
