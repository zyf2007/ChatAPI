from __future__ import annotations

import json
import uuid
from typing import Any

from flask import Flask, jsonify, request

from ..core import AppDependencies
from ..repositories import build_title
from ..services.response_payloads import (
    build_openai_error,
    build_openai_response,
    estimate_usage,
)
from ..services.ntfy import notify_new_message
from ..services.pending import PendingTurn
from ..services.response_stream import (
    client_disconnected,
    discard_pending_turn,
    stream_pending_turn,
)


def register_response_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    store = deps.store
    pending_turns = deps.pending_turns
    settings = deps.settings
    message_rate_limiter = deps.message_rate_limiter
    config_store = deps.store

    heartbeat_text_key = "stream_heartbeat_text"
    heartbeat_interval_key = "stream_heartbeat_interval_seconds"

    def get_stream_heartbeat_settings() -> dict[str, Any]:
        raw_text = config_store.get_config(heartbeat_text_key, "")
        raw_interval = config_store.get_config(heartbeat_interval_key, "0")
        try:
            interval_seconds = float(raw_interval)
        except (TypeError, ValueError):
            interval_seconds = 0.0
        return {
            "heartbeat_text": raw_text,
            "heartbeat_interval_seconds": max(0.0, interval_seconds),
        }

    def reconcile_waiting_conversations(owner: str) -> None:
        reconciler = app.extensions.get("chat_reconcile_waiting")
        if callable(reconciler):
            reconciler(owner)

    def build_abort_error(message_text: str) -> tuple[dict[str, Any], int]:
        return build_openai_error(
            message_text or "request aborted",
            code="request_aborted",
            status=400,
        )

    def normalize_message_text(value: str) -> str:
        return (
            value.replace("\r\n", "\n")
            .replace("\\r\\n", "\n")
            .replace("\\n", "\n")
        )

    def response_input_payload(data: dict[str, Any]) -> Any:
        if "input" in data:
            return data["input"]
        if "messages" in data:
            return data["messages"]
        return data

    def canonical_json(value: Any) -> str:
        return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))

    def extract_context_text(data: dict[str, Any]) -> str:
        input_payload = data.get("input")
        if isinstance(input_payload, str):
            return input_payload.strip()
        chunks: list[str] = []

        def visit(node: Any) -> None:
            if node is None:
                return
            if isinstance(node, str):
                if node.strip():
                    chunks.append(node.strip())
                return
            if isinstance(node, list):
                for item in node:
                    visit(item)
                return
            if isinstance(node, dict):
                if node.get("role") in {"user", "assistant", "system", "developer"}:
                    visit(node.get("content"))
                    return
                if isinstance(node.get("text"), str):
                    chunks.append(str(node["text"]).strip())
                    return
                if isinstance(node.get("content"), (str, list, dict)):
                    visit(node.get("content"))
                    return
                for value in node.values():
                    visit(value)

        visit(input_payload)
        if not chunks and isinstance(data.get("messages"), list):
            visit(data.get("messages"))
        return "\n".join(chunk for chunk in chunks if chunk).strip()

    def extract_text_content(node: Any) -> str:
        parts: list[str] = []

        def visit(value: Any) -> None:
            if value is None:
                return
            if isinstance(value, str):
                if value.strip():
                    parts.append(value.strip())
                return
            if isinstance(value, list):
                for item in value:
                    visit(item)
                return
            if isinstance(value, dict):
                item_type = str(value.get("type", "")).strip()
                if item_type in {"input_text", "output_text", "text"} and isinstance(
                    value.get("text"), str
                ):
                    text = str(value.get("text", "")).strip()
                    if text:
                        parts.append(text)
                    return
                if isinstance(value.get("text"), str):
                    text = str(value.get("text", "")).strip()
                    if text:
                        parts.append(text)
                    return
                if "content" in value:
                    visit(value.get("content"))
                    return

        visit(node)
        return "\n".join(parts).strip()

    def resolve_tool_name_for_call(
        conversation_id: str | None, owner: str, call_id: str
    ) -> str:
        if not conversation_id or not call_id:
            return ""
        try:
            messages = store.get_messages(conversation_id, owner)
        except ValueError:
            return ""
        for message in reversed(messages):
            if str(message.metadata.get("tool_call_id", "")).strip() == call_id:
                return str(message.metadata.get("tool_name", "")).strip()
        return ""

    def extract_request_messages(
        data: dict[str, Any],
        *,
        conversation_id: str | None,
        owner: str,
    ) -> list[dict[str, Any]]:
        payload = response_input_payload(data)
        items = payload if isinstance(payload, list) else [payload]
        extracted: list[dict[str, Any]] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            role = str(item.get("role", "")).strip()
            item_type = str(item.get("type", "")).strip()
            if role == "user":
                content = extract_text_content(item.get("content"))
                if content:
                    extracted.append(
                        {
                            "role": "user",
                            "content": content,
                            "metadata": {
                                "turn": "user",
                                "status": "pending",
                                "source": "responses",
                            },
                        }
                    )
                continue
            if item_type == "function_call_output":
                output = str(item.get("output", "")).strip()
                call_id = str(item.get("call_id", "")).strip()
                if output:
                    extracted.append(
                        {
                            "role": "tool",
                            "content": output,
                            "metadata": {
                                "source": "responses",
                                "response_mode": "tool_result",
                                "tool_call_id": call_id,
                                "tool_name": resolve_tool_name_for_call(
                                    conversation_id,
                                    owner,
                                    call_id,
                                ),
                                "output": output,
                            },
                        }
                    )
        return extracted

    def extract_tool_result_call_ids(data: dict[str, Any]) -> list[str]:
        payload = response_input_payload(data)
        items = payload if isinstance(payload, list) else [payload]
        call_ids: list[str] = []
        for item in items:
            if not isinstance(item, dict):
                continue
            if str(item.get("type", "")).strip() != "function_call_output":
                continue
            call_id = str(item.get("call_id", "")).strip()
            if call_id:
                call_ids.append(call_id)
        return call_ids

    def resolve_conversation_for_request(data: dict[str, Any], owner: str):
        explicit_conversation_id = str(data.get("conversation_id", "")).strip()
        if explicit_conversation_id:
            conversation = store.get_conversation(explicit_conversation_id, owner)
            if conversation is None:
                return None, build_openai_error("conversation not found", code="not_found", status=404)
            return conversation, None

        for call_id in extract_tool_result_call_ids(data):
            conversation = store.find_conversation_by_tool_call_id(owner, call_id)
            if conversation is not None:
                return conversation, None

        return None, None

    def build_message_debug_metadata(
        *,
        request_data: dict[str, Any],
        input_text: str,
        input_payload: Any,
        request_id: str,
        resolved_model: str,
        response_id: str | None = None,
    ) -> dict[str, Any]:
        tool_schemas = request_data.get("tools")
        return {
            "provider": "responses",
            "model": resolved_model,
            "request_debug": {
                "request_id": request_id,
                "response_id": response_id or "",
                "model": resolved_model,
                "request_keys": sorted(request_data.keys()),
                "input_text": input_text,
                "input_payload": input_payload,
                "tool_schemas": tool_schemas if isinstance(tool_schemas, list) else [],
                "request_body": request_data,
                "headers": auth.request_headers_snapshot(),
            },
        }

    def prepare_pending_turn(data: dict[str, Any]):
        if not isinstance(data, dict):
            return build_openai_error("request body must be a JSON object")

        context_text = extract_context_text(data)
        if not context_text:
            return build_openai_error("input is required")

        model = str(data.get("model") or "mock-gpt-4.1-mini")
        owner = auth.owner_id()
        if not message_rate_limiter.allow(owner):
            return build_openai_error(
                f"rate limit exceeded: max {settings.messages_per_minute_limit} messages per minute",
                code="rate_limit_exceeded",
                status=429,
            )

        conversation, conversation_error = resolve_conversation_for_request(data, owner)
        if conversation_error is not None:
            return conversation_error
        if conversation is None:
            conversation = store.create_conversation(
                owner,
                title=build_title(context_text),
            )

        existing_pending = pending_turns.get_by_conversation(conversation.id)
        if existing_pending is not None:
            return build_openai_error(
                "conversation is waiting for a reply",
                code="conflict",
                status=409,
            )

        extracted_messages = extract_request_messages(
            data,
            conversation_id=conversation.id,
            owner=owner,
        )
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
            request_debug_metadata = build_message_debug_metadata(
                request_data=data,
                input_text=context_text,
                input_payload=response_input_payload(data),
                request_id=pending.request_id,
                resolved_model=model,
            )
            if extracted_messages:
                for index, message_payload in enumerate(extracted_messages):
                    metadata = dict(message_payload.get("metadata") or {})
                    if message_payload.get("role") == "user" and index == len(extracted_messages) - 1:
                        metadata = {**metadata, **request_debug_metadata}
                    store.add_message(
                        conversation.id,
                        str(message_payload.get("role") or "user"),
                        str(message_payload.get("content") or ""),
                        metadata=metadata,
                    )
            else:
                store.add_message(
                    conversation.id,
                    "user",
                    context_text,
                    metadata={
                        "turn": "user",
                        "status": "pending",
                        "source": "responses",
                        **request_debug_metadata,
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
            pending_turns.discard(
                conversation_id=conversation.id,
                owner_id=owner,
            )
            raise
        return pending, updated_conversation

    def finalize_pending_turn(pending: PendingTurn) -> dict[str, Any]:
        updated_conversation = store.get_conversation(
            pending.conversation_id,
            pending.owner_id,
        )
        if updated_conversation is None:
            raise ValueError("conversation not found")
        usage = estimate_usage(pending.input_text, pending.assistant_text)
        payload = build_openai_response(
            response_id=pending.response_id,
            model=pending.model,
            conversation_id=updated_conversation.id,
            assistant_text=pending.assistant_text,
            usage=usage,
            output_items=pending.response_output_items or None,
            output_text=pending.response_output_text,
        )
        payload["conversation"] = updated_conversation.to_dict()
        payload["input_text"] = pending.input_text
        return payload

    def handle_responses_request(data: dict[str, Any]):
        prepared = prepare_pending_turn(data)
        if isinstance(prepared, tuple):
            pending, _conversation = prepared
        else:
            return prepared

        if bool(data.get("stream")):
            return stream_pending_turn(
                pending,
                pending_turns=pending_turns,
                store=store,
                build_abort_error=build_abort_error,
                client_socket=request.environ.get("werkzeug.socket"),
            )

        client_socket = request.environ.get("werkzeug.socket")
        while True:
            if pending.event.is_set():
                waited = pending_turns.wait(pending.request_id)
                if waited.aborted:
                    body, status = build_abort_error(waited.abort_message or "request aborted")
                    return jsonify(body), status
                return jsonify(finalize_pending_turn(waited))
            if client_disconnected(client_socket):
                discard_pending_turn(pending, pending_turns=pending_turns, store=store)
                return jsonify(
                    build_openai_error(
                        "client disconnected",
                        code="client_disconnected",
                        status=499,
                    )[0]
                ), 499
            pending.event.wait(0.5)

    @app.post("/v1/responses")
    @auth.require_auth
    def responses():
        data = request.get_json(silent=True) or {}
        result = handle_responses_request(data)
        if isinstance(result, tuple):
            body, status = result
            return jsonify(body), status
        return result

    @app.post("/api/chat/send")
    @auth.require_auth
    def chat_send():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        mode = str(data.get("mode", "assistant_message")).strip() or "assistant_message"
        if mode not in {"assistant_message", "tool_call"}:
            return {"error": "unsupported mode"}, 400
        text = str(data.get("text", "")).strip()
        tool_name = str(data.get("tool_name", "")).strip()
        tool_call_id = str(data.get("tool_call_id", "")).strip()
        if mode == "assistant_message" and not text:
            return {"error": "text is required"}, 400
        if mode == "tool_call":
            if not tool_name:
                return {"error": "tool_name is required"}, 400
            if not text:
                return {"error": "tool arguments are required"}, 400
            if not tool_call_id:
                tool_call_id = f"call_{uuid.uuid4().hex[:24]}"
        conversation_id = str(data.get("conversation_id", "")).strip()
        owner = auth.owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        pending = pending_turns.get_by_conversation(conversation_id)
        if pending is None:
            return {"error": "conversation is not waiting for a reply"}, 409
        if pending.owner_id != owner:
            return {"error": "conversation not found"}, 404

        try:
            response_id = pending.request_id
            if mode == "tool_call":
                assistant_text = f"{tool_name}({text})"
                output_items = [
                    {
                        "id": f"fc_{uuid.uuid4().hex[:24]}",
                        "type": "function_call",
                        "status": "completed",
                        "call_id": tool_call_id,
                        "name": tool_name,
                        "arguments": text,
                    }
                ]
                output_text = ""
                assistant_metadata = {
                    "provider": "human",
                    "model": str(data.get("model") or pending.model or "mock-gpt-4.1-mini"),
                    "response_mode": "tool_call",
                    "tool_name": tool_name,
                    "tool_call_id": tool_call_id,
                    "arguments": text,
                }
            else:
                submitted_text = normalize_message_text(text)
                assistant_text = (
                    submitted_text
                    if not pending.draft_text or submitted_text.startswith(pending.draft_text)
                    else f"{pending.draft_text}{submitted_text}"
                )
                output_items = []
                output_text = assistant_text
                assistant_metadata = {
                    "provider": "human",
                    "model": str(data.get("model") or pending.model or "mock-gpt-4.1-mini"),
                    "response_mode": "assistant_message",
                }
            updated_conversation = store.record_assistant_reply(
                conversation_id,
                owner,
                pending.input_text,
                assistant_text,
                response_id=response_id,
                assistant_metadata=assistant_metadata,
            )
            store.update_conversation(
                conversation_id,
                owner,
                metadata={
                    **updated_conversation.metadata,
                    "realtime_status": "closed",
                    "realtime_draft_text": "",
                },
            )
            pending = pending_turns.resolve(
                conversation_id=conversation_id,
                owner_id=owner,
                assistant_text=assistant_text,
                response_id=response_id,
                response_mode=mode,
                response_output_items=output_items,
                response_output_text=output_text,
            )
        except ValueError as error:
            return {"error": str(error)}, 409

        conversation = store.get_conversation(conversation_id, owner)
        return {
            "ok": True,
            "conversation": conversation.to_dict() if conversation else None,
            "message": {
                "role": "assistant" if mode == "assistant_message" else "tool_call",
                "content": assistant_text,
                "response_id": pending.response_id,
                "metadata": assistant_metadata,
            },
        }

    @app.post("/api/chat/draft")
    @auth.require_auth
    def chat_draft():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        text = normalize_message_text(str(data.get("text", "")).strip())
        if not text:
            return {"error": "text is required"}, 400
        conversation_id = str(data.get("conversation_id", "")).strip()
        owner = auth.owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        try:
            pending = pending_turns.add_draft(
                conversation_id=conversation_id,
                owner_id=owner,
                chunk=text,
            )
        except ValueError as error:
            return {"error": str(error)}, 409
        conversation = store.get_conversation(conversation_id, owner)
        if conversation is not None:
            store.update_conversation(
                conversation_id,
                owner,
                metadata={
                    **conversation.metadata,
                    "realtime_status": "waiting",
                    "realtime_draft_text": pending.draft_text,
                },
            )

        return {
            "ok": True,
            "conversation_id": pending.conversation_id,
            "request_id": pending.request_id,
            "draft_text": pending.draft_text,
            "draft_length": len(pending.draft_text),
        }

    @app.get("/api/config/stream-heartbeat")
    @auth.require_auth
    def get_stream_heartbeat_config():
        return {"ok": True, **get_stream_heartbeat_settings()}

    @app.post("/api/config/stream-heartbeat")
    @auth.require_auth
    def update_stream_heartbeat_config():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400

        heartbeat_text = str(data.get("heartbeat_text", ""))
        raw_interval = data.get("heartbeat_interval_seconds", 0)
        try:
            interval_seconds = float(raw_interval or 0)
        except (TypeError, ValueError):
            return {"error": "heartbeat_interval_seconds must be a number"}, 400
        if interval_seconds < 0:
            return {"error": "heartbeat_interval_seconds must be greater than or equal to 0"}, 400

        config_store.set_config(heartbeat_text_key, heartbeat_text)
        config_store.set_config(heartbeat_interval_key, str(interval_seconds))
        return {
            "ok": True,
            "heartbeat_text": heartbeat_text,
            "heartbeat_interval_seconds": interval_seconds,
        }
