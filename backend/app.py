from __future__ import annotations

from flask import Flask
from flask_cors import CORS

from .core import AppDependencies, AuthContext, settings
from .repositories import ConversationStore
from .services import MessageRateLimiter, PendingTurnRegistry
from .services.realtime import RealtimeBroker
from .routes import (
    register_auth_routes,
    register_conversation_routes,
    register_realtime_routes,
    register_response_routes,
    register_statistics_routes,
)


def create_app() -> Flask:
    app = Flask(__name__)
    app.config.update(SECRET_KEY=settings.session_secret)
    CORS(app, supports_credentials=True, origins=settings.cors_origins)

    store = ConversationStore(settings.db_path)
    auth = AuthContext(settings)
    pending_turns = PendingTurnRegistry()
    message_rate_limiter = MessageRateLimiter(
        limit=settings.messages_per_minute_limit
    )
    realtime = RealtimeBroker(store)
    deps = AppDependencies(
        settings=settings,
        auth=auth,
        store=store,
        pending_turns=pending_turns,
        message_rate_limiter=message_rate_limiter,
    )
    app.extensions["chat_store"] = store
    app.extensions["chat_realtime"] = realtime

    @app.get("/api/health")
    def health():
        return {"ok": True, "title": settings.title}

    register_auth_routes(app, auth=auth, settings=settings)
    register_conversation_routes(app, deps=deps)
    register_realtime_routes(app, deps=deps)
    register_response_routes(app, deps=deps)
    register_statistics_routes(app, deps=deps)

    return app
