from __future__ import annotations

import json
import select
import socket
from typing import Any, Callable

from flask import Response

from .pending import PendingTurn, PendingTurnRegistry


STREAM_HEADERS = {
    "Cache-Control": "no-cache",
    "X-Accel-Buffering": "no",
    "Connection": "keep-alive",
}


def client_disconnected(client_socket: Any) -> bool:
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


def discard_pending_turn(
    pending: PendingTurn,
    *,
    pending_turns: PendingTurnRegistry,
    store: Any,
    publish_sync: Callable[[str, str | None], None] | None = None,
) -> None:
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
    if publish_sync is not None:
        publish_sync(discarded.owner_id, discarded.conversation_id)


def sse_event(event: str, data: dict[str, Any]) -> str:
    return f"event: {event}\ndata: {json.dumps(data, ensure_ascii=False, separators=(',', ':'))}\n\n"


def sse_data(data: dict[str, Any] | str) -> str:
    serialized = (
        data
        if isinstance(data, str)
        else json.dumps(data, ensure_ascii=False, separators=(",", ":"))
    )
    return f"data: {serialized}\n\n"


def build_stream_response(response_iter) -> Response:
    return Response(
        response_iter,
        mimetype="text/event-stream",
        headers=STREAM_HEADERS,
    )
