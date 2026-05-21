from __future__ import annotations

from flask import Flask, jsonify, request, session

from ...core.auth import verify_totp_code
from ...services.realtime import RealtimeBroker
from .common import AuthRouteDeps, get_logger, verify_geetest


def register_session_routes(app: Flask, deps: AuthRouteDeps) -> None:
    @app.post("/api/auth/login")
    def login():
        data = request.get_json(silent=True) or {}
        username = str(data.get("username", "")).strip()
        password = str(data.get("password", ""))
        totp = str(data.get("totp", "")).strip()
        geetest_params = data.get("geetest_params")

        geetest_error = verify_geetest(deps.settings, geetest_params, get_logger())
        if geetest_error is not None:
            return jsonify({"error": geetest_error}), 400

        user = deps.user_store.verify_user_password(username, password)
        if user is None:
            return jsonify({"error": "账号或密码不正确"}), 401

        if user.totp_secret:
            if not totp or not verify_totp_code(user.totp_secret, totp):
                return jsonify({"error": "验证码不正确", "totp_required": True}), 401

        session["user_id"] = user.id
        session["username"] = user.username
        session["role"] = user.role
        deps.user_store.update_last_login_at(user.id)
        return {"ok": True, "user": user.to_dict()}

    @app.post("/api/auth/logout")
    def logout():
        session.clear()
        return {"ok": True}

    @app.get("/api/auth/session")
    def auth_session():
        user = deps.auth.current_user()
        ext_reg = deps.system_config_store.get_system_config("flag.external_registration", "0") == "1"
        geetest_enabled = bool(deps.settings.geetest_captcha_id)
        realtime = app.extensions.get("chat_realtime")
        current_connection_count = 0
        if user is not None and isinstance(realtime, RealtimeBroker):
            current_connection_count = realtime.count_owner_connections(user["id"])
        try:
            max_connection_per_user = int(
                deps.system_config_store.get_system_config("value.realtime_max_connections_per_user", "0") or 0,
            )
        except ValueError:
            max_connection_per_user = 0
        max_connection_per_user = max(0, max_connection_per_user)
        if user is None:
            return {
                "authenticated": False,
                "user": None,
                "registration_enabled": ext_reg,
                "geetest_enabled": geetest_enabled,
                "geetest_captcha_id": deps.settings.geetest_captcha_id if geetest_enabled else "",
                "current_connection_count": 0,
                "realtime_max_connections_per_user": max_connection_per_user,
            }

        db_user = deps.user_store.get_user(user["id"])
        totp_enabled = bool(db_user and db_user.totp_secret)
        return {
            "authenticated": True,
            "user": user,
            "totp_enabled": totp_enabled,
            "registration_enabled": ext_reg,
            "geetest_enabled": geetest_enabled,
            "geetest_captcha_id": deps.settings.geetest_captcha_id if geetest_enabled else "",
            "current_connection_count": current_connection_count,
            "realtime_max_connections_per_user": max_connection_per_user,
        }
