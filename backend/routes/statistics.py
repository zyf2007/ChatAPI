from __future__ import annotations

from flask import Flask, request

from ..core import AppDependencies


def register_statistics_routes(app: Flask, *, deps: AppDependencies) -> None:
    auth = deps.auth
    store = deps.store

    @app.get("/api/statistics/summary")
    @auth.require_auth
    def get_statistics_summary():
        owner = auth.owner_id()
        start_at = request.args.get("start") or None
        end_at = request.args.get("end") or None
        summary = store.get_statistics_summary(owner, start_at=start_at, end_at=end_at)
        return {
            "ok": True,
            "summary": summary,
        }
