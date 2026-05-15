from __future__ import annotations

import json
import uuid
import threading
import select
import socket
from functools import wraps
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

from flask import Flask, Response, jsonify, request, session, stream_with_context
from flask_cors import CORS

from .assistant import AssistantService, build_openai_error, build_openai_response
from .config import settings
from .store import ConversationStore, build_title


def create_app() -> Flask:
    app = Flask(__name__)
    app.config.update(SECRET_KEY=settings.session_secret)
    CORS(app, supports_credentials=True, origins=settings.cors_origins)

    store = ConversationStore(settings.db_path)
    assistant = AssistantService(settings)
    app.extensions["chat_store"] = store
    app.extensions["assistant_service"] = assistant

    def current_user() -> dict[str, str] | None:
        username = str(session.get("username", "") or "").strip()
        if username:
            return {"username": username}
        return None

    def require_auth(view):
        @wraps(view)
        def wrapped(*args, **kwargs):
            user = current_user()
            if user is None:
                api_key = str(
                    request.headers.get("Authorization", "").removeprefix("Bearer ")
                    or request.headers.get("X-API-Key", "")
                    or ""
                ).strip()
                if settings.api_key and api_key == settings.api_key:
                    return view(*args, **kwargs)
                return jsonify({"error": "unauthorized"}), 401
            return view(*args, **kwargs)

        return wrapped

    def owner_id() -> str:
        api_key = str(
            request.headers.get("Authorization", "").removeprefix("Bearer ")
            or request.headers.get("X-API-Key", "")
            or ""
        ).strip()
        if current_user() or (settings.api_key and api_key == settings.api_key):
            return "workspace:default"
        return "anonymous"

    def conversation_payload(conversation) -> dict[str, Any]:
        return conversation.to_dict()

    @dataclass
    class PendingTurn:
        request_id: str
        conversation_id: str
        owner_id: str
        model: str
        input_text: str
        input_payload: Any
        request_data: dict[str, Any]
        request_context_signature: str
        conversation_title: str
        previous_summary: str
        created_at: str
        event: threading.Event = field(default_factory=threading.Event)
        stream_event: threading.Event = field(default_factory=threading.Event)
        assistant_text: str = ""
        response_id: str = ""
        draft_chunks: list[str] = field(default_factory=list)
        persisted: bool = False

    class PendingTurnRegistry:
        def __init__(self) -> None:
            self._lock = threading.Lock()
            self._by_request_id: dict[str, PendingTurn] = {}
            self._by_conversation_id: dict[str, str] = {}

        def register(
            self,
            *,
            conversation_id: str,
            owner_id: str,
            model: str,
            input_text: str,
            input_payload: Any,
            request_data: dict[str, Any],
            request_context_signature: str,
            conversation_title: str,
            previous_summary: str,
        ) -> PendingTurn:
            with self._lock:
                if conversation_id in self._by_conversation_id:
                    raise ValueError("conversation is waiting for a reply")
                pending = PendingTurn(
                    request_id=f"resp_{uuid.uuid4().hex}",
                    conversation_id=conversation_id,
                    owner_id=owner_id,
                    model=model,
                    input_text=input_text,
                    input_payload=input_payload,
                    request_data=request_data,
                    request_context_signature=request_context_signature,
                    conversation_title=conversation_title,
                    previous_summary=previous_summary,
                    created_at=datetime.now(timezone.utc).isoformat(),
                )
                self._by_request_id[pending.request_id] = pending
                self._by_conversation_id[conversation_id] = pending.request_id
                return pending

        def get_by_conversation(self, conversation_id: str) -> PendingTurn | None:
            with self._lock:
                request_id = self._by_conversation_id.get(conversation_id)
                if not request_id:
                    return None
                return self._by_request_id.get(request_id)

        def resolve(
            self,
            *,
            conversation_id: str,
            owner_id: str,
            assistant_text: str,
            response_id: str,
        ) -> PendingTurn:
            with self._lock:
                request_id = self._by_conversation_id.get(conversation_id)
                if not request_id:
                    raise ValueError("conversation is not waiting for a reply")
                pending = self._by_request_id.get(request_id)
                if pending is None or pending.owner_id != owner_id:
                    raise ValueError("conversation is not waiting for a reply")
                pending.assistant_text = assistant_text
                pending.response_id = response_id
                pending.persisted = True
                pending.stream_event.set()
                pending.event.set()
                return pending

        def add_draft(
            self,
            *,
            conversation_id: str,
            owner_id: str,
            chunk: str,
        ) -> PendingTurn:
            with self._lock:
                request_id = self._by_conversation_id.get(conversation_id)
                if not request_id:
                    raise ValueError("conversation is not waiting for a reply")
                pending = self._by_request_id.get(request_id)
                if pending is None or pending.owner_id != owner_id:
                    raise ValueError("conversation is not waiting for a reply")
                pending.draft_chunks.append(chunk)
                pending.stream_event.set()
                return pending

        def discard(
            self,
            *,
            conversation_id: str,
            owner_id: str,
        ) -> PendingTurn | None:
            with self._lock:
                request_id = self._by_conversation_id.get(conversation_id)
                if not request_id:
                    return None
                pending = self._by_request_id.get(request_id)
                if pending is None or pending.owner_id != owner_id:
                    return None
                self._by_request_id.pop(request_id, None)
                self._by_conversation_id.pop(conversation_id, None)
                return pending

        def wait(self, request_id: str) -> PendingTurn:
            with self._lock:
                pending = self._by_request_id.get(request_id)
            if pending is None:
                raise ValueError("response not found")
            pending.event.wait()
            with self._lock:
                self._by_request_id.pop(request_id, None)
                self._by_conversation_id.pop(pending.conversation_id, None)
            return pending

    pending_turns = PendingTurnRegistry()

    def _client_disconnected(client_socket: Any) -> bool:
        if client_socket is None:
            return False
        try:
            readable, _, _ = select.select([client_socket], [], [], 0)
            if not readable:
                return False
            data = client_socket.recv(1, socket.MSG_PEEK)
            return data == b""
        except (OSError, ValueError):
            return True

    def _discard_pending_turn(pending: PendingTurn) -> None:
        discarded = pending_turns.discard(
            conversation_id=pending.conversation_id,
            owner_id=pending.owner_id,
        )
        if discarded is None:
            return
        conversation = store.get_conversation(discarded.conversation_id, discarded.owner_id)
        store.update_conversation(
            discarded.conversation_id,
            discarded.owner_id,
            metadata={
                **(conversation.metadata if conversation else {}),
                "realtime_status": "aborted",
            },
        )

    def _build_stream_response_base(
        *,
        pending: PendingTurn,
        conversation_id: str,
        status: str,
        assistant_text: str = "",
        usage: dict[str, int] | None = None,
    ) -> dict[str, Any]:
        payload = build_openai_response(
            response_id=pending.response_id or pending.request_id,
            model=pending.model,
            conversation_id=conversation_id,
            assistant_text=assistant_text,
            usage=usage or assistant._usage_from_texts(pending.input_text, assistant_text),
            status=status,
        )
        if status != "completed":
            payload["output"] = []
            payload["output_text"] = assistant_text
            payload["usage"] = None
        return payload

    def _sse_event(event: str, data: dict[str, Any]) -> str:
        return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False, separators=(',', ':'))}\n\n"

    def _stream_pending_turn(pending: PendingTurn) -> Response:
        client_socket = request.environ.get("werkzeug.socket")
        message_id = f"msg_{uuid.uuid4().hex[:24]}"

        def generate():
            sequence = 0
            sent_text = ""

            def emit(event: str, data: dict[str, Any]) -> str:
                nonlocal sequence
                payload = dict(data)
                payload["sequence_number"] = sequence
                sequence += 1
                return _sse_event(event, payload)

            try:
                yield emit(
                    "response.created",
                    {
                        "type": "response.created",
                        "response": _build_stream_response_base(
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
                        "response": _build_stream_response_base(
                            pending=pending,
                            conversation_id=pending.conversation_id,
                            status="in_progress",
                        ),
                    },
                )
                yield emit(
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
                )
                yield emit(
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
                )

                while True:
                    if _client_disconnected(client_socket):
                        _discard_pending_turn(pending)
                        return

                    while pending.draft_chunks:
                        chunk = pending.draft_chunks.pop(0)
                        sent_text += chunk
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
                        final_text = finalized.assistant_text
                        if final_text.startswith(sent_text):
                            remaining = final_text[len(sent_text):]
                        else:
                            remaining = final_text
                            sent_text = ""
                        if remaining:
                            sent_text += remaining
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
                        usage = assistant._usage_from_texts(finalized.input_text, final_text)
                        yield emit(
                            "response.completed",
                            {
                                "type": "response.completed",
                                "response": _build_stream_response_base(
                                    pending=finalized,
                                    conversation_id=finalized.conversation_id,
                                    status="completed",
                                    assistant_text=final_text,
                                    usage=usage,
                                ),
                            },
                        )
                        return

                    pending.stream_event.wait(0.5)
                    pending.stream_event.clear()
            except GeneratorExit:
                _discard_pending_turn(pending)
                raise

        return Response(
            stream_with_context(generate()),
            mimetype="text/event-stream",
            headers={
                "Cache-Control": "no-cache",
                "X-Accel-Buffering": "no",
                "Connection": "keep-alive",
            },
        )

    @app.get("/api/health")
    def health():
        return {"ok": True, "title": settings.title}

    @app.post("/api/auth/login")
    def login():
        data = request.get_json(silent=True) or {}
        username = str(data.get("username", "")).strip()
        password = str(data.get("password", ""))
        if username != settings.username or password != settings.password:
            return jsonify({"error": "账号或密码不正确"}), 401
        session["username"] = username
        return {"ok": True, "user": {"username": username}}

    @app.post("/api/auth/logout")
    def logout():
        session.clear()
        return {"ok": True}

    @app.get("/api/auth/session")
    def auth_session():
        user = current_user()
        return {"authenticated": bool(user), "user": user}

    @app.get("/api/conversations")
    @require_auth
    def list_conversations():
        conversations = store.list_conversations(owner_id())
        return {
            "ok": True,
            "items": [conversation_payload(item) for item in conversations],
        }

    @app.post("/api/conversations")
    @require_auth
    def create_conversation():
        data = request.get_json(silent=True) or {}
        title = build_title(str(data.get("title", "")).strip()) or "新会话"
        conversation = store.create_conversation(owner_id(), title=title)
        return {"ok": True, "conversation": conversation_payload(conversation)}

    @app.get("/api/conversations/<conversation_id>")
    @require_auth
    def get_conversation(conversation_id: str):
        conversation = store.get_conversation(conversation_id, owner_id())
        if conversation is None:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True, "conversation": conversation_payload(conversation)}

    @app.get("/api/conversations/<conversation_id>/messages")
    @require_auth
    def get_messages(conversation_id: str):
        try:
            messages = store.get_messages(conversation_id, owner_id())
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True, "items": [message.to_dict() for message in messages]}

    @app.delete("/api/conversations/<conversation_id>")
    @require_auth
    def delete_conversation(conversation_id: str):
        try:
            store.delete_conversation(conversation_id, owner_id())
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True}

    @app.post("/api/conversations/<conversation_id>/rename")
    @require_auth
    def rename_conversation(conversation_id: str):
        data = request.get_json(silent=True) or {}
        title = build_title(str(data.get("title", "")).strip())
        if not title:
            return jsonify({"error": "title is required"}), 400
        try:
            conversation = store.update_conversation(conversation_id, owner_id(), title=title)
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True, "conversation": conversation_payload(conversation)}

    def _extract_context_text(data: dict[str, Any]) -> str:
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

    def _response_input_payload(data: dict[str, Any]) -> Any:
        if "input" in data:
            return data["input"]
        if "messages" in data:
            return data["messages"]
        return data

    def _canonical_json(value: Any) -> str:
        return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))

    def _build_request_context_signature(data: dict[str, Any]) -> str:
        messages = data.get("messages")
        if isinstance(messages, list) and messages:
            last_user_index = -1
            for index, item in enumerate(messages):
                if isinstance(item, dict) and str(item.get("role", "")).strip() == "user":
                    last_user_index = index
            if last_user_index >= 0:
                return _canonical_json(messages[:last_user_index])
            if len(messages) > 1:
                return _canonical_json(messages[:-1])
            return _canonical_json(messages)

        input_payload = data.get("input")
        if isinstance(input_payload, str):
            return input_payload.strip()
        if input_payload is not None:
            return _canonical_json(input_payload)
        return ""

    def _build_next_context_signature(data: dict[str, Any], assistant_text: str) -> str:
        messages = data.get("messages")
        if isinstance(messages, list) and messages:
            last_user_index = -1
            for index, item in enumerate(messages):
                if isinstance(item, dict) and str(item.get("role", "")).strip() == "user":
                    last_user_index = index
            if last_user_index >= 0:
                prefix = messages[:last_user_index]
                current_user = messages[last_user_index]
                next_messages = list(prefix) + [current_user, {"role": "assistant", "content": assistant_text}]
                return _canonical_json(next_messages)
            next_messages = list(messages) + [{"role": "assistant", "content": assistant_text}]
            return _canonical_json(next_messages)

        input_payload = data.get("input")
        if isinstance(input_payload, str):
            return input_payload.strip()
        if input_payload is not None:
            return _canonical_json(input_payload)
        return assistant_text.strip()

    def _prepare_pending_turn(data: dict[str, Any]):
        if not isinstance(data, dict):
            return build_openai_error("request body must be a JSON object")

        context_text = _extract_context_text(data)
        if not context_text:
            return build_openai_error("input is required")

        request_context_signature = _build_request_context_signature(data)
        model = str(data.get("model") or settings.upstream_model or "local-fallback")
        owner = owner_id()

        explicit_conversation_id = str(data.get("conversation_id", "")).strip()
        if explicit_conversation_id:
            conversation = store.get_conversation(explicit_conversation_id, owner)
            if conversation is None:
                return build_openai_error("conversation not found", code="not_found", status=404)
        else:
            conversation = store.match_or_create_conversation(
                owner,
                request_context_signature,
                title_hint=context_text,
            )

        existing_pending = pending_turns.get_by_conversation(conversation.id)
        if existing_pending is not None:
            return build_openai_error(
                "conversation is waiting for a reply",
                code="conflict",
                status=409,
            )

        store.add_message(
            conversation.id,
            "user",
            context_text,
            metadata={
                "turn": "user",
                "status": "pending",
                "source": "responses",
            },
        )
        updated_conversation = store.update_conversation(
            conversation.id,
            owner,
            title=conversation.title if conversation.title not in {"新会话", "New conversation", ""} else build_title(context_text),
            last_user_text=context_text[:1000],
            context_signature=request_context_signature,
        )
        store.update_conversation(
            conversation.id,
            owner,
            metadata={
                **updated_conversation.metadata,
                "realtime_status": "waiting",
            },
        )
        pending = pending_turns.register(
            conversation_id=conversation.id,
            owner_id=owner,
            model=model,
            input_text=context_text,
            input_payload=_response_input_payload(data),
            request_data=data,
            request_context_signature=request_context_signature,
            conversation_title=updated_conversation.title,
            previous_summary=updated_conversation.summary,
        )
        return pending, updated_conversation

    def _finalize_pending_turn(pending: PendingTurn) -> dict[str, Any]:
        if pending.persisted:
            updated_conversation = store.get_conversation(
                pending.conversation_id,
                pending.owner_id,
            )
            if updated_conversation is None:
                raise ValueError("conversation not found")
        else:
            next_context_signature = _build_next_context_signature(
                pending.request_data,
                pending.assistant_text,
            )
            updated_conversation = store.record_assistant_reply(
                pending.conversation_id,
                pending.owner_id,
                pending.input_text,
                pending.assistant_text,
                response_id=pending.response_id,
                context_signature=next_context_signature,
                assistant_metadata={
                    "provider": "human",
                    "model": pending.model,
                },
            )
            store.update_conversation(
                pending.conversation_id,
                pending.owner_id,
                metadata={
                    **updated_conversation.metadata,
                    "realtime_status": "closed",
                },
            )
        usage = assistant._usage_from_texts(pending.input_text, pending.assistant_text)
        payload = build_openai_response(
            response_id=pending.response_id,
            model=pending.model,
            conversation_id=updated_conversation.id,
            assistant_text=pending.assistant_text,
            usage=usage,
        )
        payload["conversation"] = updated_conversation.to_dict()
        payload["input_text"] = pending.input_text
        return payload

    def _handle_responses_request(data: dict[str, Any]):
        prepared = _prepare_pending_turn(data)
        if isinstance(prepared, tuple):
            pending, _conversation = prepared
        else:
            return prepared

        if bool(data.get("stream")):
            return _stream_pending_turn(pending)

        client_socket = request.environ.get("werkzeug.socket")
        while True:
            if pending.event.is_set():
                waited = pending_turns.wait(pending.request_id)
                return jsonify(_finalize_pending_turn(waited))
            if _client_disconnected(client_socket):
                _discard_pending_turn(pending)
                return jsonify(
                    build_openai_error(
                        "client disconnected",
                        code="client_disconnected",
                        status=499,
                    )[0]
                ), 499
            pending.event.wait(0.5)

    @app.post("/v1/responses")
    @require_auth
    def responses():
        data = request.get_json(silent=True) or {}
        result = _handle_responses_request(data)
        if isinstance(result, tuple):
            body, status = result
            return jsonify(body), status
        return result

    @app.post("/api/chat/send")
    @require_auth
    def chat_send():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        text = str(data.get("text", "")).strip()
        if not text:
            return {"error": "text is required"}, 400
        conversation_id = str(data.get("conversation_id", "")).strip()
        owner = owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        pending = pending_turns.get_by_conversation(conversation_id)
        if pending is None:
            return {"error": "conversation is not waiting for a reply"}, 409
        if pending.owner_id != owner:
            return {"error": "conversation not found"}, 404

        try:
            response_id = pending.request_id
            next_context_signature = _build_next_context_signature(pending.request_data, text)
            updated_conversation = store.record_assistant_reply(
                conversation_id,
                owner,
                pending.input_text,
                text,
                response_id=response_id,
                context_signature=next_context_signature,
                assistant_metadata={
                    "provider": "human",
                    "model": str(data.get("model") or pending.model or settings.upstream_model),
                },
            )
            store.update_conversation(
                conversation_id,
                owner,
                metadata={
                    **updated_conversation.metadata,
                    "realtime_status": "closed",
                },
            )
            pending = pending_turns.resolve(
                conversation_id=conversation_id,
                owner_id=owner,
                assistant_text=text,
                response_id=response_id,
            )
        except ValueError as error:
            return {"error": str(error)}, 409

        conversation = store.get_conversation(conversation_id, owner)
        return {
            "ok": True,
            "conversation": conversation_payload(conversation) if conversation else None,
            "message": {
                "role": "assistant",
                "content": text,
                "response_id": pending.response_id,
            },
        }

    @app.post("/api/chat/draft")
    @require_auth
    def chat_draft():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        text = str(data.get("text", "")).strip()
        if not text:
            return {"error": "text is required"}, 400
        conversation_id = str(data.get("conversation_id", "")).strip()
        owner = owner_id()
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

        return {
            "ok": True,
            "conversation_id": pending.conversation_id,
            "request_id": pending.request_id,
            "draft_length": sum(len(chunk) for chunk in pending.draft_chunks),
        }

    return app
