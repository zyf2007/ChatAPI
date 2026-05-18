from __future__ import annotations

import json
import time
import uuid
from typing import Any

from flask import Flask, Response, jsonify, request, stream_with_context

from ..core import AppDependencies
from ..repositories import build_title
from ..services.chat_parsers import extract_chat_context_text, extract_chat_tool_result_call_ids
from ..services.ntfy import notify_new_message
from ..services.pending import PendingTurn
from ..services.response_payloads import estimate_usage
from ..services.response_stream import client_disconnected, discard_pending_turn


def register_chat_completions_routes(app: Flask, *, deps: AppDependencies) -> None:
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

    def build_error(message: str, code: str = "bad_request", status: int = 400) -> tuple[dict[str, Any], int]:
        return {"error": {"message": message, "type": code, "code": code}}, status

    def _chunk_json(completion_id: str, model: str, delta: dict[str, Any], finish_reason: str | None) -> str:
        payload = {
            "id": completion_id,
            "object": "chat.completion.chunk",
            "created": int(time.time()),
            "model": model,
            "choices": [{"index": 0, "delta": delta, "finish_reason": finish_reason}],
        }
        return f"data: {json.dumps(payload, ensure_ascii=False, separators=(',', ':'))}\n\n"

    def _build_response(
        completion_id: str,
        model: str,
        assistant_text: str,
        usage: dict[str, int],
        finish_reason: str = "stop",
        tool_calls: list[dict[str, Any]] | None = None,
    ) -> dict[str, Any]:
        message: dict[str, Any] = {"role": "assistant"}
        if tool_calls:
            message["content"] = None
            message["tool_calls"] = tool_calls
        else:
            message["content"] = assistant_text
        return {
            "id": completion_id,
            "object": "chat.completion",
            "created": int(time.time()),
            "model": model,
            "choices": [{"index": 0, "message": message, "finish_reason": finish_reason}],
            "usage": {
                "prompt_tokens": usage["input_tokens"],
                "completion_tokens": usage["output_tokens"],
                "total_tokens": usage["total_tokens"],
            },
        }

    def _make_tool_calls_from_item(item: dict[str, Any]) -> list[dict[str, Any]]:
        return [
            {
                "id": str(item.get("call_id", f"call_{uuid.uuid4().hex[:24]}")),
                "type": "function",
                "function": {
                    "name": str(item.get("name", "")),
                    "arguments": str(item.get("arguments", "")),
                },
            }
        ]

    def _stream_response(
        pending: PendingTurn,
        completion_id: str,
        client_socket: Any,
    ) -> Response:
        def generate():
            sent_text = ""
            last_heartbeat_at = time.monotonic()

            yield _chunk_json(completion_id, pending.model, {"role": "assistant", "content": ""}, None)

            try:
                while True:
                    if client_disconnected(client_socket):
                        discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                        return

                    for chunk in pending_turns.consume_draft_chunks(pending.request_id):
                        sent_text += chunk
                        yield _chunk_json(completion_id, pending.model, {"content": chunk}, None)

                    heartbeat_interval = max(0.0, float(pending.heartbeat_interval_seconds or 0.0))
                    if heartbeat_interval > 0 and pending.heartbeat_text:
                        now = time.monotonic()
                        if now - last_heartbeat_at >= heartbeat_interval:
                            yield _chunk_json(completion_id, pending.model, {"content": pending.heartbeat_text}, None)
                            last_heartbeat_at = now

                    if pending.event.is_set():
                        finalized = pending_turns.wait(pending.request_id)
                        if finalized.aborted:
                            err_body, _ = build_error(
                                finalized.abort_message or "request aborted",
                                code="request_aborted",
                            )
                            yield f"data: {json.dumps(err_body, ensure_ascii=False, separators=(',', ':'))}\n\n"
                            yield "data: [DONE]\n\n"
                            return

                        if finalized.response_mode == "tool_call":
                            items = finalized.response_output_items or []
                            item = items[0] if items else {}
                            tc_delta = [
                                {
                                    "index": 0,
                                    "id": str(item.get("call_id", "")),
                                    "type": "function",
                                    "function": {
                                        "name": str(item.get("name", "")),
                                        "arguments": str(item.get("arguments", "")),
                                    },
                                }
                            ]
                            yield _chunk_json(
                                completion_id, finalized.model,
                                {"tool_calls": tc_delta},
                                "tool_calls",
                            )
                        else:
                            final_text = finalized.assistant_text
                            remaining = final_text[len(sent_text):] if final_text.startswith(sent_text) else final_text
                            if remaining:
                                yield _chunk_json(completion_id, finalized.model, {"content": remaining}, None)
                            yield _chunk_json(completion_id, finalized.model, {}, "stop")

                        yield "data: [DONE]\n\n"
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

    @app.post("/v1/chat/completions")
    @auth.require_auth
    def chat_completions():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            body, status = build_error("request body must be a JSON object")
            return jsonify(body), status

        messages = data.get("messages", [])
        if not isinstance(messages, list):
            body, status = build_error("messages must be an array")
            return jsonify(body), status

        context_text = extract_chat_context_text(messages)
        if not context_text:
            body, status = build_error("messages must contain at least one user message with text content")
            return jsonify(body), status

        model = str(data.get("model") or "mock-gpt-4.1-mini")
        owner = auth.owner_id()

        if not message_rate_limiter.allow(owner):
            body, status = build_error(
                f"rate limit exceeded: max {settings.messages_per_minute_limit} messages per minute",
                code="rate_limit_exceeded",
                status=429,
            )
            return jsonify(body), status

        conversation = None
        for call_id in extract_chat_tool_result_call_ids(messages):
            conversation = store.find_conversation_by_tool_call_id(owner, call_id)
            if conversation is not None:
                break

        explicit_conv_id = str(data.get("conversation_id", "")).strip()
        if explicit_conv_id and conversation is None:
            conversation = store.get_conversation(explicit_conv_id, owner)
            if conversation is None:
                body, status = build_error("conversation not found", code="not_found", status=404)
                return jsonify(body), status

        if conversation is None:
            conversation = store.create_conversation(owner, title=build_title(context_text))

        if pending_turns.get_by_conversation(conversation.id) is not None:
            body, status = build_error("conversation is waiting for a reply", code="conflict", status=409)
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
            store.add_message(
                conversation.id,
                "user",
                context_text,
                metadata={
                    "turn": "user",
                    "status": "pending",
                    "source": "chat_completions",
                    "provider": "chat_completions",
                    "model": model,
                    "request_debug": {
                        "request_id": pending.request_id,
                        "model": model,
                        "input_text": context_text,
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

        completion_id = f"chatcmpl-{uuid.uuid4().hex}"

        if bool(data.get("stream")):
            return _stream_response(pending, completion_id, request.environ.get("werkzeug.socket"))

        client_socket = request.environ.get("werkzeug.socket")
        while True:
            if pending.event.is_set():
                waited = pending_turns.wait(pending.request_id)
                if waited.aborted:
                    body, status = build_error(
                        waited.abort_message or "request aborted",
                        code="request_aborted",
                        status=400,
                    )
                    return jsonify(body), status

                usage = estimate_usage(waited.input_text, waited.assistant_text)
                if waited.response_mode == "tool_call":
                    items = waited.response_output_items or []
                    item = items[0] if items else {}
                    payload = _build_response(
                        completion_id, model, "", usage,
                        finish_reason="tool_calls",
                        tool_calls=_make_tool_calls_from_item(item),
                    )
                else:
                    payload = _build_response(completion_id, model, waited.assistant_text, usage)
                return jsonify(payload)

            if client_disconnected(client_socket):
                discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                body, status = build_error("client disconnected", code="client_disconnected", status=499)
                return jsonify(body), status

            pending.event.wait(0.5)
