from __future__ import annotations

import json
import queue
import time
from typing import Any

from flask import Flask
from flask_sock import Sock

from ..core import AppDependencies
from ..services.realtime import RealtimeBroker


def register_realtime_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    sock = Sock(app)

    @sock.route("/api/ws")
    def websocket_sync(ws: Any) -> None:
        if auth.current_user() is None and not auth.is_request_authorized_by_api_key():
            ws.close()
            return

        owner = auth.owner_id()
        reconcile_waiting = app.extensions.get("chat_reconcile_waiting")
        realtime = app.extensions.get("chat_realtime")
        if not callable(reconcile_waiting) or not isinstance(realtime, RealtimeBroker):
            ws.close()
            return

        subscription = realtime.subscribe(owner)

        try:
            reconcile_waiting(owner)
            ws.send(
                json.dumps(
                    realtime.build_snapshot(owner),
                    ensure_ascii=False,
                    separators=(",", ":"),
                )
            )

            while True:
                try:
                    event = subscription.events.get(timeout=20)
                    ws.send(json.dumps(event, ensure_ascii=False, separators=(",", ":")))
                except queue.Empty:
                    ws.send('{"type":"ping"}')
        finally:
            realtime.unsubscribe(subscription)
