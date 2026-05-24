from __future__ import annotations

import secrets
import sqlite3
import re
from contextlib import contextmanager
from pathlib import Path
from typing import Any


class SystemConfigStore:
    _DOMAIN_PATTERN = re.compile(r"^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")

    def __init__(self, db_path: Path):
        self.db_path = db_path
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init_db()

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.db_path, timeout=30, check_same_thread=False)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL")
        return conn

    @contextmanager
    def _connection(self):
        conn = self._connect()
        try:
            yield conn
            conn.commit()
        except Exception:
            conn.rollback()
            raise
        finally:
            conn.close()

    def _init_db(self) -> None:
        with self._connection() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS config (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL DEFAULT ''
                )
                """
            )

    def get_system_config(self, key: str, default: str = "") -> str:
        with self._connection() as conn:
            row = conn.execute("SELECT value FROM config WHERE key = ?", (key,)).fetchone()
        if row is None:
            return default
        return str(row["value"] or "")

    def set_system_config(self, key: str, value: str) -> None:
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO config (key, value)
                VALUES (?, ?)
                ON CONFLICT(key) DO UPDATE SET value = excluded.value
                """,
                (key, value),
            )

    def get_system_config_flag(self, key: str, default: bool = False) -> bool:
        return self.get_system_config(key, "1" if default else "0") == "1"

    def _normalize_registration_email_domains(self, raw: str) -> str:
        domains: list[str] = []
        for item in raw.replace("\n", ",").split(","):
            domain = item.strip().lower()
            if not domain:
                continue
            if domain.startswith("@"):
                domain = domain[1:]
            if not self._DOMAIN_PATTERN.fullmatch(domain):
                raise ValueError(f"无效的邮箱域名：{domain}")
            domains.append(domain)
        return ",".join(dict.fromkeys(domains))

    def get_registration_email_domains(self) -> list[str]:
        raw = self.get_system_config("value.registration_email_domains", "")
        if not raw.strip():
            return []
        return [item for item in self._normalize_registration_email_domains(raw).split(",") if item]

    def _normalize_model_ids(self, raw: str) -> str:
        model_ids: list[str] = []
        for item in raw.replace("\n", ",").split(","):
            model_id = item.strip()
            if not model_id:
                continue
            model_ids.append(model_id)
        return "\n".join(dict.fromkeys(model_ids))

    def get_model_ids(self) -> list[str]:
        raw = self.get_system_config("value.model_ids", "")
        normalized = self._normalize_model_ids(raw)
        if not normalized:
            return []
        return [item for item in normalized.split("\n") if item]

    def is_registration_email_allowed(self, email: str) -> bool:
        if not self.get_system_config_flag("flag.registration_email_domain_restriction", False):
            return True
        domains = set(self.get_registration_email_domains())
        if not domains or "@" not in email:
            return False
        _, domain = email.rsplit("@", 1)
        return domain.strip().lower() in domains

    def _normalize_ntfy_private_url_policy(self, raw: str) -> str:
        value = str(raw or "").strip().lower()
        if value in {"admin", "all"}:
            return value
        return "disabled"

    def is_ntfy_private_url_allowed_for_role(self, role: str) -> bool:
        policy = self._normalize_ntfy_private_url_policy(
            self.get_system_config("value.ntfy_private_url_policy", "disabled"),
        )
        if policy == "all":
            return True
        if policy == "admin":
            return str(role or "").strip() == "admin"
        return False

    def get_system_config_snapshot(self) -> dict[str, Any]:
        api_key_limit_raw = self.get_system_config("value.api_key_limit_per_user", "0").strip()
        try:
            api_key_limit_per_user = max(0, int(api_key_limit_raw or "0"))
        except ValueError:
            api_key_limit_per_user = 0
        realtime_max_connections = self._get_non_negative_int("value.realtime_max_connections", 0)
        realtime_max_connections_per_user = self._get_non_negative_int("value.realtime_max_connections_per_user", 0)
        realtime_queue_size = self._get_positive_int("value.realtime_queue_size", 100)
        image_max_single_bytes = self._get_non_negative_int("value.image_max_single_bytes", 0)
        image_max_request_bytes = self._get_non_negative_int("value.image_max_request_bytes", 0)
        image_max_total_bytes = self._get_non_negative_int("value.image_max_total_bytes", 0)
        return {
            "public_statistics": self.get_system_config_flag("public_statistics", False),
            "title_enabled": self.get_system_config_flag("flag.title", False),
            "title": self.get_system_config("value.title", ""),
            "external_registration_enabled": self.get_system_config_flag(
                "flag.external_registration",
                False,
            ),
            "email_verification_enabled": self.get_system_config_flag(
                "flag.email_verification",
                False,
            ),
            "email_provider": self.get_system_config("value.email_provider", ""),
            "registration_email_domain_restriction_enabled": self.get_system_config_flag(
                "flag.registration_email_domain_restriction",
                False,
            ),
            "registration_email_domains": self.get_system_config("value.registration_email_domains", ""),
            "ntfy_private_url_policy": self._normalize_ntfy_private_url_policy(
                self.get_system_config("value.ntfy_private_url_policy", "disabled"),
            ),
            "api_key_limit_per_user": api_key_limit_per_user,
            "realtime_max_connections": realtime_max_connections,
            "realtime_max_connections_per_user": realtime_max_connections_per_user,
            "realtime_queue_size": realtime_queue_size,
            "image_max_single_bytes": image_max_single_bytes,
            "image_max_request_bytes": image_max_request_bytes,
            "image_max_total_bytes": image_max_total_bytes,
        }

    def _get_non_negative_int(self, key: str, default: int) -> int:
        try:
            return max(0, int(self.get_system_config(key, str(default)) or default))
        except ValueError:
            return default

    def _get_positive_int(self, key: str, default: int) -> int:
        try:
            return max(1, int(self.get_system_config(key, str(default)) or default))
        except ValueError:
            return default

    def update_system_config_snapshot(self, data: dict[str, Any]) -> None:
        registration_email_domain_restriction_enabled = bool(
            data.get("registration_email_domain_restriction_enabled"),
        )
        try:
            api_key_limit_per_user = int(data.get("api_key_limit_per_user", 0) or 0)
        except (TypeError, ValueError) as exc:
            raise ValueError("每个账号 API Key 数量上限必须是大于等于 0 的整数") from exc
        if api_key_limit_per_user < 0:
            raise ValueError("每个账号 API Key 数量上限必须是大于等于 0 的整数")
        realtime_max_connections = self._normalize_non_negative_config_int(
            data,
            "realtime_max_connections",
            "全局最大实时连接数必须是大于等于 0 的整数",
        )
        realtime_max_connections_per_user = self._normalize_non_negative_config_int(
            data,
            "realtime_max_connections_per_user",
            "单用户最大实时连接数必须是大于等于 0 的整数",
        )
        realtime_queue_size = self._normalize_non_negative_config_int(
            data,
            "realtime_queue_size",
            "单连接事件队列上限必须是大于 0 的整数",
        )
        if realtime_queue_size <= 0:
            raise ValueError("单连接事件队列上限必须是大于 0 的整数")
        image_max_single_bytes = self._normalize_non_negative_config_int(
            data,
            "image_max_single_bytes",
            "单张图片大小上限必须是大于等于 0 的整数",
        )
        image_max_request_bytes = self._normalize_non_negative_config_int(
            data,
            "image_max_request_bytes",
            "单次请求图片大小上限必须是大于等于 0 的整数",
        )
        image_max_total_bytes = self._normalize_non_negative_config_int(
            data,
            "image_max_total_bytes",
            "图片总容量上限必须是大于等于 0 的整数",
        )
        if registration_email_domain_restriction_enabled:
            registration_email_domains = self._normalize_registration_email_domains(
                str(data.get("registration_email_domains", "")),
            )
        else:
            registration_email_domains = ""
        self.set_system_config(
            "public_statistics",
            "1" if bool(data.get("public_statistics")) else "0",
        )
        self.set_system_config(
            "flag.title",
            "1" if bool(data.get("title_enabled")) else "0",
        )
        self.set_system_config("value.title", str(data.get("title", "")))
        self.set_system_config(
            "flag.external_registration",
            "1" if bool(data.get("external_registration_enabled")) else "0",
        )
        self.set_system_config(
            "flag.email_verification",
            "1" if bool(data.get("email_verification_enabled")) else "0",
        )
        self.set_system_config("value.email_provider", str(data.get("email_provider", "")))
        self.set_system_config(
            "flag.registration_email_domain_restriction",
            "1" if registration_email_domain_restriction_enabled else "0",
        )
        self.set_system_config(
            "value.registration_email_domains",
            registration_email_domains,
        )
        self.set_system_config(
            "value.ntfy_private_url_policy",
            self._normalize_ntfy_private_url_policy(str(data.get("ntfy_private_url_policy", "disabled"))),
        )
        self.set_system_config(
            "value.api_key_limit_per_user",
            str(api_key_limit_per_user),
        )
        self.set_system_config("value.realtime_max_connections", str(realtime_max_connections))
        self.set_system_config("value.realtime_max_connections_per_user", str(realtime_max_connections_per_user))
        self.set_system_config("value.realtime_queue_size", str(realtime_queue_size))
        self.set_system_config("value.image_max_single_bytes", str(image_max_single_bytes))
        self.set_system_config("value.image_max_request_bytes", str(image_max_request_bytes))
        self.set_system_config("value.image_max_total_bytes", str(image_max_total_bytes))

    def _normalize_non_negative_config_int(
        self,
        data: dict[str, Any],
        key: str,
        message: str,
    ) -> int:
        try:
            value = int(data.get(key, 0) or 0)
        except (TypeError, ValueError) as exc:
            raise ValueError(message) from exc
        if value < 0:
            raise ValueError(message)
        return value

    def get_effective_title(self, fallback: str = "") -> str:
        if not self.get_system_config_flag("flag.title", False):
            return fallback
        value = self.get_system_config("value.title", "").strip()
        return value or fallback

    def get_or_create_session_secret(self, fallback_secret: str = "") -> str:
        if fallback_secret.strip() == "change-this-session-secret":
            fallback_secret = ""
        with self._connection() as conn:
            conn.execute("BEGIN IMMEDIATE")
            row = conn.execute(
                "SELECT value FROM config WHERE key = ?",
                ("system.session_secret",),
            ).fetchone()
            if row is not None:
                current = str(row["value"] or "")
                if current:
                    return current

            value = fallback_secret or secrets.token_urlsafe(48)
            conn.execute(
                """
                INSERT INTO config (key, value)
                VALUES (?, ?)
                ON CONFLICT(key) DO UPDATE SET value = excluded.value
                """,
                ("system.session_secret", value),
            )
            return value
