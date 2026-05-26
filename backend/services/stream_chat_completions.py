from __future__ import annotations

import time
import uuid
from typing import Any, Callable

from flask import stream_with_context

from .pending import PendingTurn, PendingTurnRegistry
from .realtime import ConnectionLease, RealtimeBroker
from .response_payloads import estimate_usage
from .stream_common import (
    abort_pending_if_expired,
    build_stream_response,
    client_disconnected,
    discard_pending_turn,
    sse_data,
)


def stream_chat_completion_turn(
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
    completion_id = pending.response_id or f"chatcmpl_{uuid.uuid4().hex}"
    created_at = int(time.time())

    def chunk(delta: dict[str, Any], *, finish_reason: str | None = None, usage: dict[str, Any] | None = None):
        return {
            "id": completion_id,
            "object": "chat.completion.chunk",
            "created": created_at,
            "model": pending.model,
            "choices": [
                {
                    "index": 0,
                    "delta": delta,
                    "finish_reason": finish_reason,
                }
            ],
            "usage": usage,
        }

    def generate():
        sent_text = ""
        sent_role = False
        thinking_open = False

        def emit_content_delta(content: str) -> dict[str, Any]:
            nonlocal sent_text
            sent_text += content
            return {"content": content}

        def ensure_thinking_open() -> dict[str, Any] | None:
            nonlocal thinking_open
            if thinking_open:
                return None
            thinking_open = True
            return emit_content_delta("<think>")

        def ensure_thinking_closed() -> dict[str, Any] | None:
            nonlocal thinking_open
            if not thinking_open:
                return None
            thinking_open = False
            return emit_content_delta("</think>")
        try:
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
                        piece_kind = str(piece.get("kind") or "").strip()
                        deltas: list[dict[str, Any]] = []
                        if piece_kind == "thinking":
                            open_delta = ensure_thinking_open()
                            if open_delta is not None:
                                deltas.append(open_delta)
                            if piece_text:
                                deltas.append(emit_content_delta(piece_text))
                        else:
                            close_delta = ensure_thinking_closed()
                            if close_delta is not None:
                                deltas.append(close_delta)
                            if piece_text:
                                deltas.append(emit_content_delta(piece_text))
                    else:
                        deltas = [emit_content_delta(piece)]
                    for index, delta in enumerate(deltas):
                        if not sent_role and index == 0:
                            delta["role"] = "assistant"
                            sent_role = True
                        yield sse_data(chunk(delta))

                if pending.event.is_set():
                    finalized = pending_turns.wait(pending.request_id)
                    if finalized.aborted:
                        error_body, _ = build_abort_error(
                            finalized.abort_message or "request aborted"
                        )
                        yield sse_data({"error": error_body["error"]})
                        yield sse_data("[DONE]")
                        return
                    usage = estimate_usage(finalized.input_text, finalized.assistant_text)
                    if finalized.response_mode == "tool_call":
                        metadata = finalized.response_output_items[0] if finalized.response_output_items else {}
                        delta = {
                            "role": "assistant",
                            "tool_calls": [
                                {
                                    "index": 0,
                                    "id": str(metadata.get("call_id", "")),
                                    "type": "function",
                                    "function": {
                                        "name": str(metadata.get("name", "")),
                                        "arguments": str(metadata.get("arguments", "")),
                                    },
                                }
                            ],
                        }
                        yield sse_data(chunk(delta, finish_reason="tool_calls"))
                    else:
                        close_delta = ensure_thinking_closed()
                        if close_delta is not None:
                            if not sent_role:
                                close_delta["role"] = "assistant"
                                sent_role = True
                            yield sse_data(chunk(close_delta))
                        remaining = finalized.assistant_text
                        if remaining.startswith(sent_text):
                            remaining = remaining[len(sent_text):]
                        if remaining or not sent_role:
                            delta = {"content": remaining}
                            if not sent_role:
                                delta["role"] = "assistant"
                            yield sse_data(chunk(delta))
                        yield sse_data(chunk({}, finish_reason="stop"))
                    yield sse_data(
                        {
                            "id": completion_id,
                            "object": "chat.completion.chunk",
                            "created": created_at,
                            "model": pending.model,
                            "choices": [],
                            "usage": {
                                "prompt_tokens": usage["input_tokens"],
                                "completion_tokens": usage["output_tokens"],
                                "total_tokens": usage["total_tokens"],
                            },
                        }
                    )
                    yield sse_data("[DONE]")
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
