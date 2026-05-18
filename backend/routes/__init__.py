from .auth import register_auth_routes
from .conversations import register_conversation_routes
from .realtime import register_realtime_routes
from .statistics import register_statistics_routes
from .responses import register_response_routes

__all__ = [
    "register_auth_routes",
    "register_conversation_routes",
    "register_realtime_routes",
    "register_statistics_routes",
    "register_response_routes",
]
