from __future__ import annotations

from pathlib import Path

from flask import Flask, abort, send_from_directory
from flask_cors import CORS

from .core import AppDependencies, AuthContext, settings
from .repositories import ConversationStore, SystemConfigStore, UserStore, utc_now_iso
from .services import ImageAssetStore, MessageRateLimiter, PendingTurnRegistry
from .services.realtime import RealtimeBroker
from .routes import (
    register_admin_routes,
    register_auth_routes,
    register_conversation_routes,
    register_realtime_routes,
    register_response_routes,
    register_statistics_routes,
    register_upload_routes,
    register_user_api_key_routes,
    register_user_config_routes,
)


def create_app() -> Flask:
    store = ConversationStore(settings.db_path)
    system_config_store = SystemConfigStore(settings.db_path)
    user_store = UserStore(settings.db_path)

    # Ensure admin user exists
    admin = user_store.get_user_by_username(settings.admin_username)
    if admin is None:
        user_store.create_user(settings.admin_username, settings.admin_password, role="admin")
    elif admin.role != "admin":
        # Promote to admin if somehow not admin
        with user_store._connection() as conn:
            conn.execute(
                "UPDATE users SET role = 'admin', updated_at = ? WHERE id = ?",
                (utc_now_iso(), admin.id),
            )

    session_secret = system_config_store.get_or_create_session_secret(settings.session_secret)

    app = Flask(__name__)
    app.config.update(SECRET_KEY=session_secret)
    CORS(app, supports_credentials=True, origins=settings.cors_origins)

    auth = AuthContext(store, user_store)
    pending_turns = PendingTurnRegistry()
    message_rate_limiter = MessageRateLimiter()
    image_store = ImageAssetStore(
        settings.uploads_img_dir,
        system_config_store=system_config_store,
        user_store=user_store,
    )
    realtime = RealtimeBroker(store, user_store)
    deps = AppDependencies(
        settings=settings,
        auth=auth,
        store=store,
        system_config_store=system_config_store,
        user_store=user_store,
        pending_turns=pending_turns,
        message_rate_limiter=message_rate_limiter,
        image_store=image_store,
    )
    app.extensions["chat_store"] = store
    app.extensions["chat_system_config_store"] = system_config_store
    app.extensions["chat_user_store"] = user_store
    app.extensions["chat_realtime"] = realtime
    app.extensions["chat_image_store"] = image_store

    messages = store.iter_messages()
    owner_lookup = {conversation.id: conversation.owner_id for conversation in store.list_conversations_all()}
    image_store.backfill_owners_from_messages(messages, owner_lookup)
    image_store.cleanup_orphans(messages)

    @app.after_request
    def apply_security_headers(response):
        response.headers.setdefault("X-Frame-Options", "DENY")
        response.headers.setdefault("X-Content-Type-Options", "nosniff")
        response.headers.setdefault("Referrer-Policy", "strict-origin-when-cross-origin")
        response.headers.setdefault("Cross-Origin-Opener-Policy", "same-origin")
        return response

    @app.get("/api/health")
    def health():
        return {"ok": True, "title": system_config_store.get_effective_title("ChatAPI")}

    register_auth_routes(
        app,
        auth=auth,
        settings=settings,
        system_config_store=system_config_store,
        user_store=user_store,
    )
    register_admin_routes(app, auth=auth, store=store, user_store=user_store)
    register_user_config_routes(
        app,
        auth=auth,
        user_store=user_store,
        system_config_store=system_config_store,
    )
    register_user_api_key_routes(app, auth=auth, user_store=user_store)
    register_conversation_routes(app, deps=deps)
    register_realtime_routes(app, deps=deps)
    register_response_routes(app, deps=deps)
    register_statistics_routes(app, deps=deps)
    register_upload_routes(app, deps=deps)

    if settings.web_dist_dir:
        web_dist_dir = settings.web_dist_dir
        index_file = web_dist_dir / "index.html"
        if not web_dist_dir.exists():
            raise FileNotFoundError(f"WEB_DIST_DIR not found: {web_dist_dir}")
        if not web_dist_dir.is_dir():
            raise NotADirectoryError(f"WEB_DIST_DIR is not a directory: {web_dist_dir}")

        def _send_dist_file(request_path: str):
            candidate = (web_dist_dir / request_path).resolve()
            try:
                candidate.relative_to(web_dist_dir.resolve())
            except ValueError as exc:
                raise FileNotFoundError(request_path) from exc
            if candidate.is_file():
                relative_path = candidate.relative_to(web_dist_dir).as_posix()
                return send_from_directory(web_dist_dir, relative_path)
            raise FileNotFoundError(request_path)

        @app.get("/", defaults={"request_path": ""})
        @app.get("/<path:request_path>")
        def serve_web_dist(request_path: str):
            if request_path.startswith("api/") or request_path.startswith("v1/"):
                abort(404)
            if not request_path:
                if not index_file.exists():
                    abort(404)
                return send_from_directory(web_dist_dir, "index.html")
            try:
                return _send_dist_file(request_path)
            except FileNotFoundError:
                if index_file.exists():
                    return send_from_directory(web_dist_dir, "index.html")
                abort(404)

    return app
