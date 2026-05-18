from __future__ import annotations

import time
import uuid
from typing import Any, Callable

from flask import stream_with_context

from .pending import PendingTurn, PendingTurnRegistry
from .response_payloads import build_openai_response, estimate_usage
from .stream_common import (
    build_stream_response,
    client_disconnected,
    discard_pending_turn,
    sse_event,
)


def build_stream_response_base(
    *,
    pending: PendingTurn,
    conversation_id: str,
    status: str,
    assistant_text: str = "",
    usage: dict[str, int] | None = None,
    output_items: list[dict[str, Any]] | None = None,
    output_text: str | None = None,
) -> dict[str, Any]:
    payload = build_openai_response(
        response_id=pending.response_id or pending.request_id,
        model=pending.model,
        conversation_id=conversation_id,
        assistant_text=assistant_text,
        usage=usage or estimate_usage(pending.input_text, assistant_text),
        status=status,
        output_items=output_items,
        output_text=output_text,
    )
    if status != "completed":
        payload["output"] = []
        payload["output_text"] = output_text if output_text is not None else assistant_text
        payload["usage"] = None
    return payload


def stream_pending_turn(
    pending: PendingTurn,
    *,
    pending_turns: PendingTurnRegistry,
    store: Any,
    build_abort_error: Callable[[str], tuple[dict[str, Any], int]],
    client_socket: Any,
    publish_sync: Callable[[str, str | None], None] | None = None,
):
    message_id = f"msg_{uuid.uuid4().hex[:24]}"

    def generate():
        sequence = 0
        sent_final_text = ""
        emitted_text_item = False
        def emit(event: str, data: dict[str, Any]) -> str:
            nonlocal sequence
            payload = dict(data)
            payload["sequence_number"] = sequence
            sequence += 1
            return sse_event(event, payload)

        def ensure_text_item() -> list[str]:
            nonlocal emitted_text_item
            if emitted_text_item:
                return []
            emitted_text_item = True
            return [
                emit(
                    "response.output_item.added",
                    {
                        "type": "response.output_item.added",
                        "item": {
                            "id": message_id,
                            "type": "message",
                            "status": "in_progress",
                            "content": [],
                            "role": "assistant",
                        },
                        "output_index": 0,
                    },
                ),
                emit(
                    "response.content_part.added",
                    {
                        "type": "response.content_part.added",
                        "content_index": 0,
                        "item_id": message_id,
                        "output_index": 0,
                        "part": {
                            "type": "output_text",
                            "annotations": [],
                            "text": "",
                        },
                    },
                ),
            ]

        try:
            yield emit(
                "response.created",
                {
                    "type": "response.created",
                    "response": build_stream_response_base(
                        pending=pending,
                        conversation_id=pending.conversation_id,
                        status="in_progress",
                    ),
                },
            )
            yield emit(
                "response.in_progress",
                {
                    "type": "response.in_progress",
                    "response": build_stream_response_base(
                        pending=pending,
                        conversation_id=pending.conversation_id,
                        status="in_progress",
                    ),
                },
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

                for chunk in pending_turns.consume_draft_chunks(pending.request_id):
                    for event_chunk in ensure_text_item():
                        yield event_chunk
                    sent_final_text += chunk
                    yield emit(
                        "response.output_text.delta",
                        {
                            "type": "response.output_text.delta",
                            "content_index": 0,
                            "delta": chunk,
                            "item_id": message_id,
                            "output_index": 0,
                        },
                    )

                if pending.event.is_set():
                    finalized = pending_turns.wait(pending.request_id)
                    if finalized.aborted:
                        error_body, _ = build_abort_error(
                            finalized.abort_message or "request aborted"
                        )
                        failed_response = build_stream_response_base(
                            pending=finalized,
                            conversation_id=finalized.conversation_id,
                            status="failed",
                            assistant_text="",
                            usage=None,
                            output_items=[],
                            output_text="",
                        )
                        failed_response["error"] = error_body["error"]
                        yield emit(
                            "response.failed",
                            {
                                "type": "response.failed",
                                "response": failed_response,
                            },
                        )
                        return
                    final_text = finalized.assistant_text
                    output_items = finalized.response_output_items
                    output_text = finalized.response_output_text
                    if finalized.response_mode != "assistant_message":
                        structured_item = output_items[0] if output_items else None
                        if structured_item is not None:
                            yield emit(
                                "response.output_item.added",
                                {
                                    "type": "response.output_item.added",
                                    "item": structured_item,
                                    "output_index": 0,
                                },
                            )
                            yield emit(
                                "response.output_item.done",
                                {
                                    "type": "response.output_item.done",
                                    "output_index": 0,
                                    "item": structured_item,
                                },
                            )
                    else:
                        for event_chunk in ensure_text_item():
                            yield event_chunk
                        if final_text.startswith(sent_final_text):
                            remaining = final_text[len(sent_final_text):]
                        else:
                            remaining = final_text
                            sent_final_text = ""
                        if remaining:
                            sent_final_text += remaining
                            yield emit(
                                "response.output_text.delta",
                                {
                                    "type": "response.output_text.delta",
                                    "content_index": 0,
                                    "delta": remaining,
                                    "item_id": message_id,
                                    "output_index": 0,
                                },
                            )
                        yield emit(
                            "response.output_text.done",
                            {
                                "type": "response.output_text.done",
                                "content_index": 0,
                                "item_id": message_id,
                                "output_index": 0,
                                "text": final_text,
                            },
                        )
                        yield emit(
                            "response.content_part.done",
                            {
                                "type": "response.content_part.done",
                                "content_index": 0,
                                "item_id": message_id,
                                "output_index": 0,
                                "part": {
                                    "type": "output_text",
                                    "annotations": [],
                                    "text": final_text,
                                },
                            },
                        )
                        yield emit(
                            "response.output_item.done",
                            {
                                "type": "response.output_item.done",
                                "output_index": 0,
                                "item": {
                                    "id": message_id,
                                    "type": "message",
                                    "status": "completed",
                                    "role": "assistant",
                                    "content": [
                                        {
                                            "type": "output_text",
                                            "annotations": [],
                                            "text": final_text,
                                        }
                                    ],
                                },
                            },
                        )
                    usage = estimate_usage(finalized.input_text, final_text)
                    yield emit(
                        "response.completed",
                        {
                            "type": "response.completed",
                            "response": build_stream_response_base(
                                pending=finalized,
                                conversation_id=finalized.conversation_id,
                                status="completed",
                                assistant_text=final_text,
                                usage=usage,
                                output_items=output_items,
                                output_text=output_text,
                            ),
                        },
                    )
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
