from __future__ import annotations

from typing import Any

from flask import Flask, jsonify, request

from ..core import AppDependencies
from ..repositories import build_title


def register_conversation_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    store = deps.store
    pending_turns = deps.pending_turns
    realtime = app.extensions.get("chat_realtime")

    def publish_sync(owner_id: str, conversation_id: str | None = None) -> None:
        if realtime is None or not conversation_id:
            return
        realtime.publish_conversation_upsert(owner_id, conversation_id)

    def publish_delete(owner_id: str, conversation_id: str) -> None:
        if realtime is None:
            return
        realtime.publish_conversation_delete(owner_id, conversation_id)

    def conversation_payload(conversation) -> dict[str, Any]:
        return conversation.to_dict()

    def reconcile_waiting_conversations(owner: str) -> None:
        conversations = store.list_conversations(owner)
        for conversation in conversations:
            if conversation.metadata.get("realtime_status") != "waiting":
                continue
            if pending_turns.get_by_conversation(conversation.id) is not None:
                continue
            store.update_conversation(
                conversation.id,
                owner,
                metadata={
                    **conversation.metadata,
                    "realtime_status": "aborted",
                },
            )
            publish_sync(owner, conversation.id)

    app.extensions["chat_reconcile_waiting"] = reconcile_waiting_conversations

    @app.get("/api/conversations")
    @auth.require_auth
    def list_conversations():
        owner = auth.owner_id()
        reconcile_waiting_conversations(owner)
        conversations = store.list_conversations(owner)
        return {
            "ok": True,
            "items": [conversation_payload(item) for item in conversations],
        }

    @app.post("/api/conversations")
    @auth.require_auth
    def create_conversation():
        data = request.get_json(silent=True) or {}
        title = build_title(str(data.get("title", "")).strip()) or "新会话"
        conversation = store.create_conversation(auth.owner_id(), title=title)
        publish_sync(auth.owner_id(), conversation.id)
        return {"ok": True, "conversation": conversation_payload(conversation)}

    @app.get("/api/conversations/<conversation_id>")
    @auth.require_auth
    def get_conversation(conversation_id: str):
        owner = auth.owner_id()
        reconcile_waiting_conversations(owner)
        conversation = store.get_conversation(conversation_id, owner)
        if conversation is None:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True, "conversation": conversation_payload(conversation)}

    @app.get("/api/conversations/<conversation_id>/messages")
    @auth.require_auth
    def get_messages(conversation_id: str):
        reconcile_waiting_conversations(auth.owner_id())
        try:
            messages = store.get_messages(conversation_id, auth.owner_id())
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        return {"ok": True, "items": [message.to_dict() for message in messages]}

    @app.delete("/api/conversations/<conversation_id>")
    @auth.require_auth
    def delete_conversation(conversation_id: str):
        owner = auth.owner_id()
        try:
            store.delete_conversation(conversation_id, owner)
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        publish_delete(owner, conversation_id)
        return {"ok": True}

    @app.post("/api/conversations/prune")
    @auth.require_auth
    def prune_conversations():
        data = request.get_json(silent=True) or {}
        keep_count = data.get("keep_count")
        try:
            keep_count = int(keep_count)
        except (TypeError, ValueError):
            return jsonify({"error": "keep_count must be an integer"}), 400
        if keep_count < 0:
            return jsonify({"error": "keep_count must be greater than or equal to 0"}), 400

        deleted_ids, skipped_count = store.delete_conversations_except_latest(
            auth.owner_id(),
            keep_count,
        )
        for conversation_id in deleted_ids:
            publish_delete(auth.owner_id(), conversation_id)
        return {
            "ok": True,
            "deleted_count": len(deleted_ids),
            "skipped_count": skipped_count,
            "keep_count": keep_count,
        }

    @app.post("/api/conversations/<conversation_id>/abort")
    @auth.require_auth
    def abort_conversation(conversation_id: str):
        data = request.get_json(silent=True) or {}
        error_message = str(data.get("error", "")).strip() or "request aborted"
        owner = auth.owner_id()
        try:
            pending_turns.abort(
                conversation_id=conversation_id,
                owner_id=owner,
                error_message=error_message,
            )
        except ValueError as error:
            return {"error": str(error)}, 409

        conversation = store.get_conversation(conversation_id, owner)
        if conversation is None:
            return jsonify({"error": "conversation not found"}), 404
        updated = store.update_conversation(
            conversation_id,
            owner,
            metadata={
                **conversation.metadata,
                "realtime_status": "aborted",
            },
        )
        publish_sync(owner, conversation_id)
        return {"ok": True, "conversation": conversation_payload(updated)}

    @app.post("/api/conversations/<conversation_id>/rename")
    @auth.require_auth
    def rename_conversation(conversation_id: str):
        data = request.get_json(silent=True) or {}
        title = build_title(str(data.get("title", "")).strip())
        if not title:
            return jsonify({"error": "title is required"}), 400
        try:
            conversation = store.update_conversation(conversation_id, auth.owner_id(), title=title)
        except ValueError:
            return jsonify({"error": "conversation not found"}), 404
        publish_sync(auth.owner_id(), conversation_id)
        return {"ok": True, "conversation": conversation_payload(conversation)}
