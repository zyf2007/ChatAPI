from __future__ import annotations

import time
import uuid
from typing import Any, Callable, Generator

from flask import stream_with_context

from .pending import PendingTurn, PendingTurnRegistry
from .response_payloads import build_openai_response, estimate_usage
from .realtime import ConnectionLease, RealtimeBroker
from .stream_common import (
    abort_pending_if_expired,
    build_stream_response,
    client_disconnected,
    discard_pending_turn,
    sse_event,
)
from .thinking import ThinkingStreamParser, answer_text, has_thinking


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
    connection_lease: ConnectionLease | None = None,
    realtime: RealtimeBroker | None = None,
):
    def generate():
        sequence = 0
        sent_raw_text = ""
        sent_answer_text = ""
        sent_thinking_text = ""
        output_index = 0
        parser = ThinkingStreamParser()
        completed_output_items: list[dict[str, Any]] = []
        open_text_item: dict[str, Any] | None = None

        def emit(event: str, data: dict[str, Any]) -> str:
            nonlocal sequence
            payload = dict(data)
            payload["sequence_number"] = sequence
            sequence += 1
            return sse_event(event, payload)

        def next_output_index() -> int:
            nonlocal output_index
            current = output_index
            output_index += 1
            return current

        def start_text_item() -> Generator[str, None, None]:
            nonlocal open_text_item
            item_id = f"msg_{uuid.uuid4().hex[:24]}"
            item_index = next_output_index()
            open_text_item = {"id": item_id, "index": item_index, "text": ""}
            yield emit(
                "response.output_item.added",
                {
                    "type": "response.output_item.added",
                    "item": {
                        "id": item_id,
                        "type": "message",
                        "status": "in_progress",
                        "content": [],
                        "role": "assistant",
                    },
                    "output_index": item_index,
                },
            )
            yield emit(
                "response.content_part.added",
                {
                    "type": "response.content_part.added",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "part": {
                        "type": "output_text",
                        "annotations": [],
                        "text": "",
                    },
                },
            )

        def close_text_item() -> Generator[str, None, None]:
            nonlocal open_text_item
            if open_text_item is None:
                return
            item_id = str(open_text_item["id"])
            item_index = int(open_text_item["index"])
            text = str(open_text_item["text"])
            item = {
                "id": item_id,
                "type": "message",
                "status": "completed",
                "role": "assistant",
                "content": [
                    {
                        "type": "output_text",
                        "annotations": [],
                        "text": text,
                    }
                ],
            }
            yield emit(
                "response.output_text.done",
                {
                    "type": "response.output_text.done",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "text": text,
                },
            )
            yield emit(
                "response.content_part.done",
                {
                    "type": "response.content_part.done",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "part": item["content"][0],
                },
            )
            yield emit(
                "response.output_item.done",
                {
                    "type": "response.output_item.done",
                    "output_index": item_index,
                    "item": item,
                },
            )
            completed_output_items.append(item)
            open_text_item = None

        def emit_answer_delta(delta: str) -> Generator[str, None, None]:
            nonlocal sent_answer_text, open_text_item
            if not delta:
                return
            if open_text_item is None:
                yield from start_text_item()
            assert open_text_item is not None
            item_id = str(open_text_item["id"])
            item_index = int(open_text_item["index"])
            open_text_item["text"] = str(open_text_item["text"]) + delta
            sent_answer_text += delta
            yield emit(
                "response.output_text.delta",
                {
                    "type": "response.output_text.delta",
                    "content_index": 0,
                    "delta": delta,
                    "item_id": item_id,
                    "output_index": item_index,
                },
            )

        def emit_reasoning_summary_events(
            *,
            item_id: str,
            item_index: int,
            text: str,
        ) -> Generator[str, None, None]:
            yield emit(
                "response.reasoning_summary_part.added",
                {
                    "type": "response.reasoning_summary_part.added",
                    "item_id": item_id,
                    "output_index": item_index,
                    "summary_index": 0,
                    "part": {
                        "type": "summary_text",
                        "text": "",
                    },
                },
            )
            yield emit(
                "response.reasoning_summary_text.delta",
                {
                    "type": "response.reasoning_summary_text.delta",
                    "delta": text,
                    "item_id": item_id,
                    "output_index": item_index,
                    "summary_index": 0,
                },
            )
            yield emit(
                "response.reasoning_summary_text.done",
                {
                    "type": "response.reasoning_summary_text.done",
                    "item_id": item_id,
                    "output_index": item_index,
                    "summary_index": 0,
                    "text": text,
                },
            )
            yield emit(
                "response.reasoning_summary_part.done",
                {
                    "type": "response.reasoning_summary_part.done",
                    "item_id": item_id,
                    "output_index": item_index,
                    "summary_index": 0,
                    "part": {
                        "type": "summary_text",
                        "text": text,
                    },
                },
            )

        def emit_reasoning_text_events(
            *,
            item_id: str,
            item_index: int,
            text: str,
        ) -> Generator[str, None, None]:
            yield emit(
                "response.content_part.added",
                {
                    "type": "response.content_part.added",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "part": {
                        "type": "reasoning_text",
                        "text": "",
                    },
                },
            )
            yield emit(
                "response.reasoning_text.delta",
                {
                    "type": "response.reasoning_text.delta",
                    "content_index": 0,
                    "delta": text,
                    "item_id": item_id,
                    "output_index": item_index,
                },
            )
            yield emit(
                "response.reasoning_text.done",
                {
                    "type": "response.reasoning_text.done",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "text": text,
                },
            )
            yield emit(
                "response.content_part.done",
                {
                    "type": "response.content_part.done",
                    "content_index": 0,
                    "item_id": item_id,
                    "output_index": item_index,
                    "part": {
                        "type": "reasoning_text",
                        "text": text,
                    },
                },
            )

        def emit_reasoning_block(text: str) -> Generator[str, None, None]:
            nonlocal sent_thinking_text
            if not text:
                return
            yield from close_text_item()
            item_id = f"rs_{uuid.uuid4().hex[:24]}"
            item_index = next_output_index()
            item = {
                "id": item_id,
                "type": "reasoning",
                "status": "completed",
                "summary": [
                    {
                        "type": "summary_text",
                        "text": text,
                    }
                ],
                "content": [
                    {
                        "type": "reasoning_text",
                        "text": text,
                    }
                ],
            }
            sent_thinking_text += ("\n\n" if sent_thinking_text else "") + text
            yield emit(
                "response.output_item.added",
                {
                    "type": "response.output_item.added",
                    "item": {
                        "id": item_id,
                        "type": "reasoning",
                        "status": "in_progress",
                        "summary": [],
                        "content": [],
                    },
                    "output_index": item_index,
                },
            )
            if pending.reasoning_stream_mode == "reasoning_text":
                yield from emit_reasoning_text_events(
                    item_id=item_id,
                    item_index=item_index,
                    text=text,
                )
            else:
                yield from emit_reasoning_summary_events(
                    item_id=item_id,
                    item_index=item_index,
                    text=text,
                )
            yield emit(
                "response.output_item.done",
                {
                    "type": "response.output_item.done",
                    "output_index": item_index,
                    "item": item,
                },
            )
            completed_output_items.append(item)

        def emit_parsed_text(text: str = "", *, flush: bool = False) -> Generator[str, None, None]:
            parts = parser.flush() if flush else parser.feed(text)
            for part in parts:
                if part["type"] == "thinking":
                    yield from emit_reasoning_block(part["text"])
                else:
                    yield from emit_answer_delta(part["text"])

        def completed_output_text(final_text: str) -> str:
            if has_thinking(final_text):
                return answer_text(final_text)
            return sent_answer_text

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

                abort_pending_if_expired(
                    pending,
                    pending_turns=pending_turns,
                    store=store,
                    publish_sync=publish_sync,
                )

                for chunk in pending_turns.consume_draft_chunks(pending.request_id):
                    if isinstance(chunk, dict):
                        chunk_text = str(chunk.get("text") or "")
                        chunk_kind = str(chunk.get("kind") or "").strip()
                        if not chunk_text:
                            continue
                        if chunk_kind == "thinking":
                            yield from emit_reasoning_block(chunk_text)
                        else:
                            sent_raw_text += chunk_text
                            yield from emit_answer_delta(chunk_text)
                        continue
                    sent_raw_text += chunk
                    yield from emit_parsed_text(chunk)

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
                    if finalized.response_mode == "assistant_message":
                        if final_text.startswith(sent_raw_text):
                            remaining = final_text[len(sent_raw_text):]
                        else:
                            remaining = final_text
                        if remaining:
                            sent_raw_text += remaining
                            yield from emit_parsed_text(remaining)
                    yield from emit_parsed_text(flush=True)
                    yield from close_text_item()

                    output_items = list(completed_output_items)
                    if finalized.response_mode != "assistant_message":
                        structured_item = (
                            finalized.response_output_items[0]
                            if finalized.response_output_items
                            else None
                        )
                        if structured_item is not None:
                            structured_index = next_output_index()
                            yield emit(
                                "response.output_item.added",
                                {
                                    "type": "response.output_item.added",
                                    "item": structured_item,
                                    "output_index": structured_index,
                                },
                            )
                            yield emit(
                                "response.output_item.done",
                                {
                                    "type": "response.output_item.done",
                                    "output_index": structured_index,
                                    "item": structured_item,
                                },
                            )
                            output_items.append(structured_item)

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
                                output_items=output_items or None,
                                output_text=completed_output_text(final_text),
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
        finally:
            if connection_lease is not None and realtime is not None:
                realtime.release_connection(connection_lease)

    return build_stream_response(stream_with_context(generate()))
