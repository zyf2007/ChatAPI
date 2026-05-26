from __future__ import annotations

from flask import Flask, abort, jsonify, request

from ..core import AppDependencies
from ..services.email import get_available_email_providers, resolve_email_provider
from ..services.realtime import ConnectionLimitExceeded, ConnectionLease, RealtimeBroker
from ..services.response_stream import (
    abort_pending_if_expired,
    client_disconnected,
    discard_pending_turn,
    stream_anthropic_turn,
    stream_chat_completion_turn,
    stream_pending_turn,
)
from ..services.turn_coordinator import PreparedTurn, TurnCoordinator


def register_response_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    store = deps.store
    system_config_store = deps.system_config_store
    realtime = app.extensions.get("chat_realtime")

    def publish_sync(owner_id: str, conversation_id: str | None = None) -> None:
        if realtime is None or not conversation_id:
            return
        realtime.publish_conversation_upsert(owner_id, conversation_id)

    coordinator = TurnCoordinator(
        deps,
        extensions=app.extensions,
        logger=app.logger,
        publish_sync=publish_sync,
    )

    def stream_limit_error():
        return (
            jsonify(
                {
                    "error": {
                        "message": "connection limit exceeded",
                        "type": "connection_limit_exceeded",
                        "code": "connection_limit_exceeded",
                    }
                }
            ),
            429,
        )

    def handle_protocol_request(data: dict[str, object], request_format: str):
        prepared = coordinator.prepare_pending_turn(data, request_format)
        if isinstance(prepared, tuple):
            body, status = prepared
            return jsonify(body), status

        assert isinstance(prepared, PreparedTurn)
        pending = prepared.pending
        if bool(data.get("stream")):
            lease: ConnectionLease | None = None
            if isinstance(realtime, RealtimeBroker):
                try:
                    lease = realtime.acquire_connection(
                        pending.owner_id,
                        kind=f"sse:{request_format}",
                        max_connections=system_config_store.get_system_config(
                            "value.realtime_max_connections",
                            "0",
                        ),
                        max_connections_per_user=system_config_store.get_system_config(
                            "value.realtime_max_connections_per_user",
                            "0",
                        ),
                    )
                except ConnectionLimitExceeded:
                    discard_pending_turn(
                        pending,
                        pending_turns=deps.pending_turns,
                        store=deps.store,
                        publish_sync=publish_sync,
                    )
                    return stream_limit_error()
            stream_kwargs = {
                "pending": pending,
                "pending_turns": deps.pending_turns,
                "store": deps.store,
                "build_abort_error": coordinator.build_abort_error,
                "client_socket": request.environ.get("werkzeug.socket"),
                "publish_sync": publish_sync,
                "connection_lease": lease,
                "realtime": realtime if isinstance(realtime, RealtimeBroker) else None,
            }
            if request_format == "chat_completions":
                return stream_chat_completion_turn(**stream_kwargs)
            if request_format == "anthropic_messages":
                return stream_anthropic_turn(**stream_kwargs)
            return stream_pending_turn(**stream_kwargs)

        client_socket = request.environ.get("werkzeug.socket")
        while True:
            abort_pending_if_expired(
                pending,
                pending_turns=deps.pending_turns,
                store=deps.store,
                publish_sync=publish_sync,
            )
            if pending.event.is_set():
                waited = deps.pending_turns.wait(pending.request_id)
                if waited.aborted:
                    body, status = coordinator.build_abort_error(
                        waited.abort_message or "request aborted"
                    )
                    return jsonify(body), status
                return jsonify(coordinator.finalize_pending_turn(waited))
            if client_disconnected(client_socket):
                discard_pending_turn(
                    pending,
                    pending_turns=deps.pending_turns,
                    store=deps.store,
                    publish_sync=publish_sync,
                )
                body, status = coordinator.build_not_found_error(
                    "client disconnected",
                    code="client_disconnected",
                    status=499,
                )
                return jsonify(body), status
            pending.event.wait(0.5)

    @app.get("/models")
    @app.get("/v1/models")
    @auth.require_auth
    def list_models():
        model_ids = system_config_store.get_model_ids()
        if not model_ids:
            abort(404)
        return {
            "object": "list",
            "data": [
                {
                    "id": model_id,
                    "object": "model",
                    "created": 0,
                    "owned_by": "chatapi",
                }
                for model_id in model_ids
            ],
        }

    @app.post("/responses")
    @app.post("/v1/responses")
    @auth.require_auth
    def responses():
        data = request.get_json(silent=True) or {}
        return handle_protocol_request(data, "responses")

    @app.post("/chat/completions")
    @app.post("/v1/chat/completions")
    @auth.require_auth
    def chat_completions():
        data = request.get_json(silent=True) or {}
        return handle_protocol_request(data, "chat_completions")

    @app.post("/messages")
    @app.post("/v1/messages")
    @auth.require_auth
    def anthropic_messages():
        data = request.get_json(silent=True) or {}
        return handle_protocol_request(data, "anthropic_messages")

    @app.post("/api/chat/output/complete")
    @auth.require_session_auth
    def chat_output_complete():
        result = coordinator.complete_manual_output(request.get_json(silent=True) or {})
        if isinstance(result, tuple):
            body, status = result
            return jsonify(body), status
        return jsonify(result)

    @app.post("/api/chat/output/delta")
    @auth.require_session_auth
    def chat_output_delta():
        result = coordinator.add_manual_output_delta(request.get_json(silent=True) or {})
        if isinstance(result, tuple):
            body, status = result
            return jsonify(body), status
        return jsonify(result)

    @app.get("/api/config/models")
    @auth.require_session_auth
    @auth.require_admin
    def get_config_models():
        return {"ok": True, "models": system_config_store.get_model_ids()}

    @app.post("/api/config/models")
    @auth.require_session_auth
    @auth.require_admin
    def add_config_model():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        model_id = str(data.get("id", "")).strip()
        if not model_id:
            return {"error": "model id is required"}, 400
        existing = system_config_store.get_model_ids()
        if model_id not in existing:
            existing.append(model_id)
            system_config_store.set_system_config("value.model_ids", "\n".join(existing))
        return {"ok": True, "models": system_config_store.get_model_ids()}

    @app.delete("/api/config/models/<path:model_id>")
    @auth.require_session_auth
    @auth.require_admin
    def delete_config_model(model_id: str):
        normalized_model_id = str(model_id or "").strip()
        existing = [item for item in system_config_store.get_model_ids() if item != normalized_model_id]
        system_config_store.set_system_config("value.model_ids", "\n".join(existing))
        return {"ok": True, "models": system_config_store.get_model_ids()}

    @app.get("/api/config/stream-heartbeat")
    @auth.require_session_auth
    def get_stream_heartbeat_config():
        owner_id = auth.owner_id()
        return {"ok": True, **coordinator.get_stream_heartbeat_settings(owner_id)}

    @app.post("/api/config/stream-heartbeat")
    @auth.require_session_auth
    def update_stream_heartbeat_config():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400

        owner_id = auth.owner_id()
        heartbeat_text = str(data.get("heartbeat_text", ""))
        raw_interval = data.get("heartbeat_interval_seconds", 0)
        try:
            interval_seconds = float(raw_interval or 0)
        except (TypeError, ValueError):
            return {"error": "heartbeat_interval_seconds must be a number"}, 400
        if interval_seconds < 0:
            return {"error": "heartbeat_interval_seconds must be greater than or equal to 0"}, 400

        return {
            "ok": True,
            **coordinator.update_stream_heartbeat_settings(
                owner_id,
                heartbeat_text=heartbeat_text,
                heartbeat_interval_seconds=interval_seconds,
            ),
        }

    @app.get("/api/config/automation-rules")
    @auth.require_session_auth
    def get_automation_rules():
        owner_id = auth.owner_id()
        return {"ok": True, "rules": coordinator.get_automation_rules(owner_id)}

    @app.post("/api/config/automation-rules")
    @auth.require_session_auth
    def update_automation_rules():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        rules = data.get("rules", [])
        if not isinstance(rules, list):
            return {"error": "rules must be an array"}, 400
        owner_id = auth.owner_id()
        try:
            normalized = coordinator.update_automation_rules(owner_id, rules)
        except ValueError as error:
            return {"error": str(error)}, 400
        return {"ok": True, "rules": normalized}

    @app.get("/api/config/system")
    @auth.require_session_auth
    @auth.require_admin
    def get_system_config():
        available_email_providers = get_available_email_providers()
        snapshot = system_config_store.get_system_config_snapshot()
        snapshot["email_provider"] = resolve_email_provider(
            str(snapshot.get("email_provider", "")),
            available_email_providers,
        )
        return {
            "ok": True,
            **snapshot,
            "image_usage": deps.image_store.storage_usage(deps.store.iter_messages()),
            "email_provider_options": available_email_providers,
        }

    @app.post("/api/config/system")
    @auth.require_session_auth
    @auth.require_admin
    def update_system_config():
        data = request.get_json(silent=True) or {}
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400

        available_email_providers = get_available_email_providers()
        data = dict(data)
        data["email_provider"] = resolve_email_provider(str(data.get("email_provider", "")), available_email_providers)

        try:
            system_config_store.update_system_config_snapshot(data)
        except ValueError as error:
            return {"error": str(error)}, 400

        return {
            "ok": True,
            **system_config_store.get_system_config_snapshot(),
            "image_usage": deps.image_store.storage_usage(deps.store.iter_messages()),
            "email_provider_options": available_email_providers,
        }

    @app.get("/api/config/app-info")
    @auth.require_session_auth
    def get_app_info():
        return {
            "ok": True,
        }
