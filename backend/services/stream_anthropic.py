from __future__ import annotations

import time
import uuid
from typing import Any, Callable

from flask import stream_with_context

from .pending import PendingTurn, PendingTurnRegistry
from .response_payloads import estimate_usage
from .stream_common import (
    build_stream_response,
    client_disconnected,
    discard_pending_turn,
    sse_data,
    sse_event,
)


def stream_anthropic_turn(
    pending: PendingTurn,
    *,
    pending_turns: PendingTurnRegistry,
    store: Any,
    build_abort_error: Callable[[str], tuple[dict[str, Any], int]],
    client_socket: Any,
    publish_sync: Callable[[str, str | None], None] | None = None,
):
    message_id = pending.response_id or f"msg_{uuid.uuid4().hex[:24]}"

    def generate():
        sent_text = ""
        block_started = False
        def emit(payload: dict[str, Any]) -> str:
            event_name = str(payload.get("type") or "message")
            return sse_event(event_name, payload)

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
            yield emit(
                {
                    "type": "content_block_start",
                    "index": 0,
                    "content_block": {"type": "text", "text": ""},
                }
            )
            block_started = True

            while True:
                if client_disconnected(client_socket):
                    discard_pending_turn(
                        pending,
                        pending_turns=pending_turns,
                        store=store,
                        publish_sync=publish_sync,
                    )
                    return

                for piece in pending_turns.consume_draft_chunks(pending.request_id):
                    sent_text += piece
                    yield emit(
                        {
                            "type": "content_block_delta",
                            "index": 0,
                            "delta": {
                                "type": "text_delta",
                                "text": piece,
                            },
                        }
                    )

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
                        if block_started:
                            yield emit({"type": "content_block_stop", "index": 0})
                        metadata = finalized.response_output_items[0] if finalized.response_output_items else {}
                        yield emit(
                            {
                                "type": "content_block_start",
                                "index": 1,
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
                                "index": 1,
                                "delta": {
                                    "type": "input_json_delta",
                                    "partial_json": str(metadata.get("arguments", "")),
                                },
                            }
                        )
                        yield emit({"type": "content_block_stop", "index": 1})
                        stop_reason = "tool_use"
                    else:
                        remaining = finalized.assistant_text
                        if remaining.startswith(sent_text):
                            remaining = remaining[len(sent_text):]
                        if remaining:
                            yield emit(
                                {
                                    "type": "content_block_delta",
                                    "index": 0,
                                    "delta": {
                                        "type": "text_delta",
                                        "text": remaining,
                                    },
                                }
                            )
                        yield emit({"type": "content_block_stop", "index": 0})
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

    return build_stream_response(stream_with_context(generate()))
