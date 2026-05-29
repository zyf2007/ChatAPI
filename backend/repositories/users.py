from __future__ import annotations

import hashlib
import json
import secrets
import sqlite3
import uuid
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def _json_dump(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def _json_load(raw: str | None, default: Any) -> Any:
    if not raw:
        return default
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return default


@dataclass
class User:
    id: str
    username: str
    password_hash: str
    role: str
    totp_secret: str
    created_at: str
    updated_at: str
    last_login_at: str | None = None

    def to_dict(self, *, include_sensitive: bool = False) -> dict[str, Any]:
        data: dict[str, Any] = {
            "id": self.id,
            "username": self.username,
            "role": self.role,
            "created_at": self.created_at,
            "last_login_at": self.last_login_at or "",
        }
        if include_sensitive:
            data["totp_secret"] = self.totp_secret
        return data


@dataclass
class ApiKey:
    id: str
    user_id: str
    name: str
    api_key: str
    created_at: str

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "name": self.name,
            "api_key": self.api_key,
            "created_at": self.created_at,
        }


def hash_password(password: str) -> str:
    salt = secrets.token_hex(16)
    h = hashlib.sha256((salt + password).encode()).hexdigest()
    return f"{salt}${h}"


def verify_password(password: str, hashed: str) -> bool:
    parts = hashed.split("$", 1)
    if len(parts) != 2:
        return False
    salt, expected = parts
    h = hashlib.sha256((salt + password).encode()).hexdigest()
    return h == expected


class UserStore:
    def __init__(self, db_path: Path):
        self.db_path = db_path
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init_db()

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.db_path, timeout=30, check_same_thread=False)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL")
        conn.execute("PRAGMA foreign_keys = ON")
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
                CREATE TABLE IF NOT EXISTS users (
                    id TEXT PRIMARY KEY,
                    username TEXT UNIQUE NOT NULL,
                    password_hash TEXT NOT NULL,
                    role TEXT NOT NULL DEFAULT 'user',
                    totp_secret TEXT NOT NULL DEFAULT '',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    last_login_at TEXT
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS user_api_keys (
                    id TEXT PRIMARY KEY,
                    user_id TEXT NOT NULL,
                    name TEXT NOT NULL DEFAULT '',
                    api_key TEXT UNIQUE NOT NULL,
                    created_at TEXT NOT NULL,
                    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
                )
                """
            )
            conn.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_user_api_keys_user_id
                ON user_api_keys(user_id)
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS user_configs (
                    user_id TEXT NOT NULL,
                    key TEXT NOT NULL,
                    value TEXT NOT NULL DEFAULT '',
                    PRIMARY KEY (user_id, key),
                    FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS uploaded_images (
                    filename TEXT PRIMARY KEY,
                    owner_id TEXT NOT NULL,
                    mime_type TEXT NOT NULL DEFAULT '',
                    created_at TEXT NOT NULL,
                    FOREIGN KEY(owner_id) REFERENCES users(id) ON DELETE CASCADE
                )
                """
            )
            conn.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_uploaded_images_owner_id
                ON uploaded_images(owner_id, created_at DESC)
                """
            )
    # --- User CRUD ---

    def create_user(self, username: str, password: str, role: str = "user") -> User:
        now = utc_now_iso()
        user = User(
            id=str(uuid.uuid4()),
            username=username,
            password_hash=hash_password(password),
            role=role,
            totp_secret="",
            created_at=now,
            updated_at=now,
        )
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO users (id, username, password_hash, role, totp_secret, created_at, updated_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
                """,
                (user.id, user.username, user.password_hash, user.role, user.totp_secret, user.created_at, user.updated_at),
            )
        return user

    def delete_user(self, user_id: str) -> bool:
        with self._connection() as conn:
            conn.execute("DELETE FROM user_api_keys WHERE user_id = ?", (user_id,))
            conn.execute("DELETE FROM user_configs WHERE user_id = ?", (user_id,))
            cursor = conn.execute("DELETE FROM users WHERE id = ?", (user_id,))
            return cursor.rowcount > 0

    def get_user(self, user_id: str) -> User | None:
        with self._connection() as conn:
            row = conn.execute("SELECT * FROM users WHERE id = ?", (user_id,)).fetchone()
        if row is None:
            return None
        return self._row_to_user(row)

    def get_user_by_username(self, username: str) -> User | None:
        with self._connection() as conn:
            row = conn.execute("SELECT * FROM users WHERE username = ?", (username,)).fetchone()
        if row is None:
            return None
        return self._row_to_user(row)

    def list_users(self) -> list[User]:
        with self._connection() as conn:
            rows = conn.execute("SELECT * FROM users ORDER BY created_at ASC").fetchall()
        return [self._row_to_user(row) for row in rows]

    def verify_user_password(self, username: str, password: str) -> User | None:
        user = self.get_user_by_username(username)
        if user is None:
            return None
        if not verify_password(password, user.password_hash):
            return None
        return user

    def update_user_totp_secret(self, user_id: str, secret: str) -> None:
        with self._connection() as conn:
            conn.execute(
                "UPDATE users SET totp_secret = ?, updated_at = ? WHERE id = ?",
                (secret, utc_now_iso(), user_id),
            )

    def update_user_password(self, user_id: str, password: str) -> bool:
        with self._connection() as conn:
            cursor = conn.execute(
                "UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?",
                (hash_password(password), utc_now_iso(), user_id),
            )
            return cursor.rowcount > 0

    def update_last_login_at(self, user_id: str) -> None:
        with self._connection() as conn:
            conn.execute(
                "UPDATE users SET last_login_at = ? WHERE id = ?",
                (utc_now_iso(), user_id),
            )

    # --- API Key management ---

    def create_api_key(self, user_id: str, name: str, api_key: str | None = None) -> tuple[ApiKey, str]:
        """Returns (ApiKey, raw_key). If api_key is None, generates a strong one."""
        if api_key is None:
            api_key = f"sk-{secrets.token_urlsafe(32)}"
        elif len(api_key) < 4:
            raise ValueError("API Key 至少需要 4 个字符")

        with self._connection() as conn:
            existing_user = conn.execute(
                "SELECT id FROM users WHERE id = ?",
                (user_id,),
            ).fetchone()
            if existing_user is None:
                raise ValueError("用户不存在")
            existing = conn.execute("SELECT id FROM user_api_keys WHERE api_key = ?", (api_key,)).fetchone()
            if existing is not None:
                raise ValueError("API Key 已被占用")

        key_obj = ApiKey(
            id=str(uuid.uuid4()),
            user_id=user_id,
            name=name,
            api_key=api_key,
            created_at=utc_now_iso(),
        )
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO user_api_keys (id, user_id, name, api_key, created_at)
                VALUES (?, ?, ?, ?, ?)
                """,
                (key_obj.id, key_obj.user_id, key_obj.name, key_obj.api_key, key_obj.created_at),
            )
        return key_obj, api_key

    def list_api_keys(self, user_id: str) -> list[ApiKey]:
        with self._connection() as conn:
            rows = conn.execute(
                "SELECT * FROM user_api_keys WHERE user_id = ? ORDER BY created_at ASC",
                (user_id,),
            ).fetchall()
        return [
            ApiKey(
                id=str(row["id"]),
                user_id=str(row["user_id"]),
                name=str(row["name"]),
                api_key=str(row["api_key"]),
                created_at=str(row["created_at"]),
            )
            for row in rows
        ]

    def count_api_keys(self, user_id: str) -> int:
        with self._connection() as conn:
            row = conn.execute(
                "SELECT COUNT(*) AS cnt FROM user_api_keys WHERE user_id = ?",
                (user_id,),
            ).fetchone()
        if row is None:
            return 0
        return int(row["cnt"])

    def get_api_key_counts(self) -> dict[str, int]:
        with self._connection() as conn:
            rows = conn.execute(
                "SELECT user_id, COUNT(*) as cnt FROM user_api_keys GROUP BY user_id"
            ).fetchall()
        return {str(row["user_id"]): int(row["cnt"]) for row in rows}

    def delete_api_key(self, user_id: str, key_id: str) -> bool:
        with self._connection() as conn:
            cursor = conn.execute(
                "DELETE FROM user_api_keys WHERE id = ? AND user_id = ?",
                (key_id, user_id),
            )
            return cursor.rowcount > 0

    def resolve_api_key_owner(self, raw_key: str) -> str | None:
        if not raw_key:
            return None
        with self._connection() as conn:
            row = conn.execute(
                """SELECT uak.user_id
                   FROM user_api_keys uak
                   JOIN users u ON u.id = uak.user_id
                   WHERE uak.api_key = ?""",
                (raw_key,),
            ).fetchone()
        if row is None:
            return None
        return str(row["user_id"])

    def resolve_api_key_name(self, raw_key: str) -> str | None:
        if not raw_key:
            return None
        with self._connection() as conn:
            row = conn.execute(
                "SELECT name FROM user_api_keys WHERE api_key = ?",
                (raw_key,),
            ).fetchone()
        if row is None:
            return None
        return str(row["name"])

    # --- Uploaded image ownership ---

    def set_uploaded_image_owner(self, filename: str, owner_id: str, mime_type: str = "") -> None:
        clean_filename = str(filename).strip()
        clean_owner_id = str(owner_id).strip()
        if not clean_filename or not clean_owner_id:
            return
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO uploaded_images (filename, owner_id, mime_type, created_at)
                VALUES (?, ?, ?, ?)
                ON CONFLICT(filename) DO UPDATE SET
                    owner_id = excluded.owner_id,
                    mime_type = excluded.mime_type
                """,
                (clean_filename, clean_owner_id, str(mime_type).strip(), utc_now_iso()),
            )

    def get_uploaded_image_owner(self, filename: str) -> str | None:
        clean_filename = str(filename).strip()
        if not clean_filename:
            return None
        with self._connection() as conn:
            row = conn.execute(
                "SELECT owner_id FROM uploaded_images WHERE filename = ?",
                (clean_filename,),
            ).fetchone()
        if row is None:
            return None
        return str(row["owner_id"])

    # --- User config ---

    def get_user_config(self, user_id: str, key: str, default: str = "") -> str:
        with self._connection() as conn:
            row = conn.execute(
                "SELECT value FROM user_configs WHERE user_id = ? AND key = ?",
                (user_id, key),
            ).fetchone()
        if row is None:
            return default
        return str(row["value"] or "")

    def set_user_config(self, user_id: str, key: str, value: str) -> None:
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO user_configs (user_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
                """,
                (user_id, key, value),
            )

    def get_user_config_flag(self, user_id: str, name: str, default: bool = False) -> bool:
        return self.get_user_config(user_id, f"flag.{name}", "1" if default else "0") == "1"

    def set_user_config_flag(self, user_id: str, name: str, enabled: bool) -> None:
        self.set_user_config(user_id, f"flag.{name}", "1" if enabled else "0")

    def _get_user_config_row(self, user_id: str, key: str) -> str | None:
        with self._connection() as conn:
            row = conn.execute(
                "SELECT value FROM user_configs WHERE user_id = ? AND key = ?",
                (user_id, key),
            ).fetchone()
        if row is None:
            return None
        return str(row["value"] or "")

    @staticmethod
    def _normalize_model_ids(raw: str) -> str:
        model_ids: list[str] = []
        for item in str(raw or "").replace("\n", ",").split(","):
            model_id = item.strip()
            if not model_id:
                continue
            model_ids.append(model_id)
        return "\n".join(dict.fromkeys(model_ids))

    def get_user_model_ids(self, user_id: str, default_model_ids: list[str] | None = None) -> list[str]:
        raw = self._get_user_config_row(user_id, "value.model_ids")
        if raw is None and default_model_ids is not None:
            raw = "\n".join(default_model_ids)
        normalized = self._normalize_model_ids(raw or "")
        if not normalized:
            return []
        return [item for item in normalized.split("\n") if item]

    def set_user_model_ids(self, user_id: str, model_ids: list[str]) -> None:
        self.set_user_config(user_id, "value.model_ids", self._normalize_model_ids("\n".join(model_ids)))

    def get_user_config_snapshot(self, user_id: str) -> dict[str, Any]:
        return {
            "ntfy_url_enabled": self.get_user_config_flag(user_id, "ntfy_url", False),
            "ntfy_url": self.get_user_config(user_id, "value.ntfy_url", ""),
            "messages_per_minute_limit_enabled": self.get_user_config_flag(user_id, "messages_per_minute_limit", False),
            "messages_per_minute_limit": int(self.get_user_config(user_id, "value.messages_per_minute_limit", "0") or "0"),
        }

    def update_user_config_snapshot(self, user_id: str, data: dict[str, Any]) -> None:
        with self._connection() as conn:
            ntfy_enabled = bool(data.get("ntfy_url_enabled"))
            ntfy_url = str(data.get("ntfy_url", ""))
            conn.execute(
                """
                INSERT INTO user_configs (user_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
                """,
                (user_id, "flag.ntfy_url", "1" if ntfy_enabled else "0"),
            )
            conn.execute(
                """
                INSERT INTO user_configs (user_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
                """,
                (user_id, "value.ntfy_url", ntfy_url),
            )

            limit_enabled = bool(data.get("messages_per_minute_limit_enabled"))
            try:
                limit_value = str(int(data.get("messages_per_minute_limit", 0)))
            except (TypeError, ValueError):
                raise ValueError("messages_per_minute_limit 必须是数字")
            conn.execute(
                """
                INSERT INTO user_configs (user_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
                """,
                (user_id, "flag.messages_per_minute_limit", "1" if limit_enabled else "0"),
            )
            conn.execute(
                """
                INSERT INTO user_configs (user_id, key, value)
                VALUES (?, ?, ?)
                ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value
                """,
                (user_id, "value.messages_per_minute_limit", limit_value),
            )

    def get_effective_ntfy_url(self, user_id: str) -> str:
        if not self.get_user_config_flag(user_id, "ntfy_url", False):
            return ""
        return self.get_user_config(user_id, "value.ntfy_url", "").strip()

    def get_effective_messages_per_minute_limit(self, user_id: str, fallback: int = 0) -> int:
        if not self.get_user_config_flag(user_id, "messages_per_minute_limit", False):
            return fallback
        raw = self.get_user_config(user_id, "value.messages_per_minute_limit", str(fallback)).strip()
        try:
            return int(raw)
        except ValueError:
            return fallback

    # --- Automation rules (per user) ---

    def get_automation_rules(self, user_id: str) -> str:
        return self.get_user_config(user_id, "automation_rules_json", "[]")

    def set_automation_rules(self, user_id: str, rules_json: str) -> None:
        self.set_user_config(user_id, "automation_rules_json", rules_json)

    @staticmethod
    def _row_to_user(row: sqlite3.Row) -> User:
        return User(
            id=str(row["id"]),
            username=str(row["username"]),
            password_hash=str(row["password_hash"]),
            role=str(row["role"]),
            totp_secret=str(row["totp_secret"] or ""),
            created_at=str(row["created_at"]),
            updated_at=str(row["updated_at"]),
            last_login_at=row["last_login_at"] if row["last_login_at"] else None,
        )
