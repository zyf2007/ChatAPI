from __future__ import annotations

import json
import time
import uuid
from typing import Any

from flask import Flask, Response, jsonify, request, stream_with_context

from ..core import AppDependencies
from ..repositories import build_title
from ..services.chat_parsers import extract_anthropic_context_text, extract_anthropic_tool_result_use_ids
from ..services.ntfy import notify_new_message
from ..services.pending import PendingTurn
from ..services.response_payloads import estimate_usage
from ..services.response_stream import client_disconnected, discard_pending_turn, sse_event


def register_messages_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    store = deps.store
    pending_turns = deps.pending_turns
    settings = deps.settings
    message_rate_limiter = deps.message_rate_limiter

    heartbeat_text_key = "stream_heartbeat_text"
    heartbeat_interval_key = "stream_heartbeat_interval_seconds"

    def get_stream_heartbeat_settings() -> dict[str, Any]:
        raw_text = store.get_config(heartbeat_text_key, "")
        raw_interval = store.get_config(heartbeat_interval_key, "0")
        try:
            interval_seconds = float(raw_interval)
        except (TypeError, ValueError):
            interval_seconds = 0.0
        return {
            "heartbeat_text": raw_text,
            "heartbeat_interval_seconds": max(0.0, interval_seconds),
        }

    def build_error(error_type: str, message: str, status: int = 400) -> tuple[dict[str, Any], int]:
        return {"type": "error", "error": {"type": error_type, "message": message}}, status

    def _build_response(
        message_id: str,
        model: str,
        content_blocks: list[dict[str, Any]],
        stop_reason: str,
        usage: dict[str, int],
    ) -> dict[str, Any]:
        return {
            "id": message_id,
            "type": "message",
            "role": "assistant",
            "content": content_blocks,
            "model": model,
            "stop_reason": stop_reason,
            "stop_sequence": None,
            "usage": {
                "input_tokens": usage["input_tokens"],
                "output_tokens": usage["output_tokens"],
            },
        }

    def _parse_tool_arguments(arguments: str) -> dict[str, Any]:
        try:
            parsed = json.loads(arguments)
            if isinstance(parsed, dict):
                return parsed
        except (json.JSONDecodeError, ValueError):
            pass
        return {}

    def _stream_response(
        pending: PendingTurn,
        message_id: str,
        input_tokens: int,
        client_socket: Any,
    ) -> Response:
        def generate():
            emitted_text_block = False
            sent_text = ""
            last_heartbeat_at = time.monotonic()

            yield sse_event(
                "message_start",
                {
                    "type": "message_start",
                    "message": {
                        "id": message_id,
                        "type": "message",
                        "role": "assistant",
                        "content": [],
                        "model": pending.model,
                        "stop_reason": None,
                        "stop_sequence": None,
                        "usage": {"input_tokens": input_tokens, "output_tokens": 0},
                    },
                },
            )
            yield sse_event("ping", {"type": "ping"})

            def ensure_text_block() -> list[str]:
                nonlocal emitted_text_block
                if emitted_text_block:
                    return []
                emitted_text_block = True
                return [
                    sse_event(
                        "content_block_start",
                        {
                            "type": "content_block_start",
                            "index": 0,
                            "content_block": {"type": "text", "text": ""},
                        },
                    )
                ]

            try:
                while True:
                    if client_disconnected(client_socket):
                        discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                        return

                    for chunk in pending_turns.consume_draft_chunks(pending.request_id):
                        for event_chunk in ensure_text_block():
                            yield event_chunk
                        sent_text += chunk
                        yield sse_event(
                            "content_block_delta",
                            {
                                "type": "content_block_delta",
                                "index": 0,
                                "delta": {"type": "text_delta", "text": chunk},
                            },
                        )

                    heartbeat_interval = max(0.0, float(pending.heartbeat_interval_seconds or 0.0))
                    if heartbeat_interval > 0 and pending.heartbeat_text:
                        now = time.monotonic()
                        if now - last_heartbeat_at >= heartbeat_interval:
                            for event_chunk in ensure_text_block():
                                yield event_chunk
                            yield sse_event(
                                "content_block_delta",
                                {
                                    "type": "content_block_delta",
                                    "index": 0,
                                    "delta": {"type": "text_delta", "text": pending.heartbeat_text},
                                },
                            )
                            last_heartbeat_at = now

                    if pending.event.is_set():
                        finalized = pending_turns.wait(pending.request_id)
                        if finalized.aborted:
                            err_body, _ = build_error(
                                "overloaded_error",
                                finalized.abort_message or "request aborted",
                                status=529,
                            )
                            yield sse_event("error", err_body)
                            return

                        output_tokens = estimate_usage("", finalized.assistant_text)["output_tokens"]

                        if finalized.response_mode == "tool_call":
                            items = finalized.response_output_items or []
                            item = items[0] if items else {}
                            call_id = str(item.get("call_id", f"toolu_{uuid.uuid4().hex[:24]}"))
                            tool_name = str(item.get("name", ""))
                            arguments = str(item.get("arguments", ""))
                            yield sse_event(
                                "content_block_start",
                                {
                                    "type": "content_block_start",
                                    "index": 0,
                                    "content_block": {
                                        "type": "tool_use",
                                        "id": call_id,
                                        "name": tool_name,
                                        "input": {},
                                    },
                                },
                            )
                            yield sse_event(
                                "content_block_delta",
                                {
                                    "type": "content_block_delta",
                                    "index": 0,
                                    "delta": {"type": "input_json_delta", "partial_json": arguments},
                                },
                            )
                            yield sse_event("content_block_stop", {"type": "content_block_stop", "index": 0})
                            yield sse_event(
                                "message_delta",
                                {
                                    "type": "message_delta",
                                    "delta": {"stop_reason": "tool_use", "stop_sequence": None},
                                    "usage": {"output_tokens": output_tokens},
                                },
                            )
                        else:
                            for event_chunk in ensure_text_block():
                                yield event_chunk
                            final_text = finalized.assistant_text
                            remaining = final_text[len(sent_text):] if final_text.startswith(sent_text) else final_text
                            if remaining:
                                yield sse_event(
                                    "content_block_delta",
                                    {
                                        "type": "content_block_delta",
                                        "index": 0,
                                        "delta": {"type": "text_delta", "text": remaining},
                                    },
                                )
                            yield sse_event("content_block_stop", {"type": "content_block_stop", "index": 0})
                            yield sse_event(
                                "message_delta",
                                {
                                    "type": "message_delta",
                                    "delta": {"stop_reason": "end_turn", "stop_sequence": None},
                                    "usage": {"output_tokens": output_tokens},
                                },
                            )

                        yield sse_event("message_stop", {"type": "message_stop"})
                        return

                    wait_timeout = 0.5
                    if heartbeat_interval > 0 and pending.heartbeat_text:
                        elapsed = time.monotonic() - last_heartbeat_at
                        wait_timeout = max(0.05, min(0.5, heartbeat_interval - elapsed))
                    pending.stream_event.wait(wait_timeout)
                    pending.stream_event.clear()
            except GeneratorExit:
                discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                raise

        return Response(
            stream_with_context(generate()),
            mimetype="text/event-stream",
            headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no", "Connection": "keep-alive"},
        )

    @app.post("/v1/messages")
    def messages():
        if auth.current_user() is None and not auth.is_request_authorized_by_api_key():
            body, status = build_error("authentication_error", "invalid api key", status=401)
            return jsonify(body), status

        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            body, status = build_error("invalid_request_error", "request body must be a JSON object")
            return jsonify(body), status

        context_text = extract_anthropic_context_text(data)
        if not context_text:
            body, status = build_error(
                "invalid_request_error",
                "messages must contain at least one user message with text content",
            )
            return jsonify(body), status

        model = str(data.get("model") or "claude-3-5-sonnet-20241022")
        owner = auth.owner_id()

        if not message_rate_limiter.allow(owner):
            body, status = build_error(
                "rate_limit_error",
                f"rate limit exceeded: max {settings.messages_per_minute_limit} messages per minute",
                status=429,
            )
            return jsonify(body), status

        conversation = None
        for use_id in extract_anthropic_tool_result_use_ids(data):
            conversation = store.find_conversation_by_tool_call_id(owner, use_id)
            if conversation is not None:
                break

        explicit_conv_id = str(data.get("conversation_id", "")).strip()
        if explicit_conv_id and conversation is None:
            conversation = store.get_conversation(explicit_conv_id, owner)
            if conversation is None:
                body, status = build_error("invalid_request_error", "conversation not found", status=404)
                return jsonify(body), status

        if conversation is None:
            conversation = store.create_conversation(owner, title=build_title(context_text))

        if pending_turns.get_by_conversation(conversation.id) is not None:
            body, status = build_error("invalid_request_error", "conversation is waiting for a reply", status=409)
            return jsonify(body), status

        updated_conversation = store.update_conversation(
            conversation.id,
            owner,
            title=conversation.title if conversation.title not in {"新会话", "New conversation", ""} else build_title(context_text),
            last_user_text=context_text[:1000],
        )

        pending = pending_turns.register(
            conversation_id=conversation.id,
            owner_id=owner,
            model=model,
            input_text=context_text,
            **get_stream_heartbeat_settings(),
        )

        try:
            raw_tools = data.get("tools")
            store.add_message(
                conversation.id,
                "user",
                context_text,
                metadata={
                    "turn": "user",
                    "status": "pending",
                    "source": "anthropic",
                    "provider": "anthropic",
                    "model": model,
                    "request_debug": {
                        "request_id": pending.request_id,
                        "model": model,
                        "input_text": context_text,
                        "tool_schemas": raw_tools if isinstance(raw_tools, list) else [],
                        "headers": auth.request_headers_snapshot(),
                    },
                },
            )
            notify_new_message(
                settings,
                conversation_title=updated_conversation.title or build_title(context_text),
                message_text=context_text,
                logger=app.logger,
            )
            store.update_conversation(
                conversation.id,
                owner,
                metadata={
                    **updated_conversation.metadata,
                    "realtime_status": "waiting",
                    "realtime_draft_text": "",
                },
            )
        except Exception:
            pending_turns.discard(conversation_id=conversation.id, owner_id=owner)
            raise

        input_usage = estimate_usage(context_text, "")
        input_tokens = input_usage["input_tokens"]
        message_id = f"msg_{uuid.uuid4().hex[:24]}"

        if bool(data.get("stream")):
            return _stream_response(
                pending,
                message_id,
                input_tokens,
                request.environ.get("werkzeug.socket"),
            )

        client_socket = request.environ.get("werkzeug.socket")
        while True:
            if pending.event.is_set():
                waited = pending_turns.wait(pending.request_id)
                if waited.aborted:
                    body, status = build_error(
                        "overloaded_error",
                        waited.abort_message or "request aborted",
                        status=529,
                    )
                    return jsonify(body), status

                usage = estimate_usage(waited.input_text, waited.assistant_text)

                if waited.response_mode == "tool_call":
                    items = waited.response_output_items or []
                    item = items[0] if items else {}
                    call_id = str(item.get("call_id", f"toolu_{uuid.uuid4().hex[:24]}"))
                    content_blocks: list[dict[str, Any]] = [
                        {
                            "type": "tool_use",
                            "id": call_id,
                            "name": str(item.get("name", "")),
                            "input": _parse_tool_arguments(str(item.get("arguments", ""))),
                        }
                    ]
                    payload = _build_response(message_id, model, content_blocks, "tool_use", usage)
                else:
                    content_blocks = [{"type": "text", "text": waited.assistant_text}]
                    payload = _build_response(message_id, model, content_blocks, "end_turn", usage)
                return jsonify(payload)

            if client_disconnected(client_socket):
                discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                body, status = build_error("api_error", "client disconnected", status=499)
                return jsonify(body), status

            pending.event.wait(0.5)
