from __future__ import annotations

import json
import sqlite3
import uuid
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat()


def build_title(text: str, max_len: int = 32) -> str:
    normalized = " ".join(text.strip().split())
    if not normalized:
        return "新会话"
    if len(normalized) <= max_len:
        return normalized
    return normalized[:max_len].rstrip() + "..."


def _json_dump(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def _json_load(raw: str | None, default: Any) -> Any:
    if not raw:
        return default
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return default


def _estimate_tokens(text: str) -> int:
    text = text.strip()
    if not text:
        return 0
    ascii_tokens = len(text.split())
    cjk_chars = sum(1 for ch in text if "\u4e00" <= ch <= "\u9fff")
    return max(ascii_tokens, 1) + cjk_chars // 2


def _estimate_usage(input_text: str, output_text: str) -> dict[str, int]:
    input_tokens = _estimate_tokens(input_text)
    output_tokens = _estimate_tokens(output_text)
    return {
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "total_tokens": input_tokens + output_tokens,
    }


def _parse_iso_datetime(value: str) -> datetime | None:
    if not value:
        return None
    try:
        parsed = datetime.fromisoformat(value)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


@dataclass
class Conversation:
    id: str
    owner_id: str
    title: str
    last_user_text: str
    created_at: str
    updated_at: str
    last_message_at: str
    metadata: dict[str, Any]
    message_count: int = 0
    last_message_preview: str = ""

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "owner_id": self.owner_id,
            "title": self.title,
            "last_user_text": self.last_user_text,
            "created_at": self.created_at,
            "updated_at": self.updated_at,
            "last_message_at": self.last_message_at,
            "metadata": self.metadata,
            "message_count": self.message_count,
            "last_message_preview": self.last_message_preview,
        }


@dataclass
class ConversationMessage:
    id: str
    conversation_id: str
    role: str
    content: str
    created_at: str
    status: str
    response_id: str | None
    metadata: dict[str, Any]

    def to_dict(self) -> dict[str, Any]:
        return {
            "id": self.id,
            "conversation_id": self.conversation_id,
            "role": self.role,
            "content": self.content,
            "created_at": self.created_at,
            "status": self.status,
            "response_id": self.response_id,
            "metadata": self.metadata,
        }


class ConversationStore:
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
                CREATE TABLE IF NOT EXISTS conversations (
                    id TEXT PRIMARY KEY,
                    owner_id TEXT NOT NULL,
                    title TEXT NOT NULL,
                    last_user_text TEXT NOT NULL DEFAULT '',
                    metadata TEXT NOT NULL DEFAULT '{}',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    last_message_at TEXT NOT NULL
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS messages (
                    id TEXT PRIMARY KEY,
                    conversation_id TEXT NOT NULL,
                    role TEXT NOT NULL,
                    content TEXT NOT NULL,
                    status TEXT NOT NULL DEFAULT 'final',
                    response_id TEXT,
                    metadata TEXT NOT NULL DEFAULT '{}',
                    created_at TEXT NOT NULL,
                    FOREIGN KEY(conversation_id) REFERENCES conversations(id)
                )
                """
            )
            conn.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_conversations_owner_updated
                ON conversations(owner_id, updated_at DESC)
                """
            )
            conn.execute(
                """
                CREATE INDEX IF NOT EXISTS idx_messages_conversation_created
                ON messages(conversation_id, created_at ASC)
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS config (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL DEFAULT ''
                )
                """
            )

    def create_conversation(
        self,
        owner_id: str,
        title: str = "新会话",
        last_user_text: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        now = utc_now_iso()
        conv = Conversation(
            id=str(uuid.uuid4()),
            owner_id=owner_id,
            title=title or "新会话",
            last_user_text=last_user_text,
            created_at=now,
            updated_at=now,
            last_message_at=now,
            metadata=metadata or {},
            message_count=0,
            last_message_preview="",
        )
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO conversations
                (id, owner_id, title, last_user_text, metadata, created_at, updated_at, last_message_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    conv.id,
                    conv.owner_id,
                    conv.title,
                    conv.last_user_text,
                    _json_dump(conv.metadata),
                    conv.created_at,
                    conv.updated_at,
                    conv.last_message_at,
                ),
            )
        return conv

    def get_conversation(self, conversation_id: str, owner_id: str) -> Conversation | None:
        with self._connection() as conn:
            row = conn.execute(
                """
                SELECT
                    c.*,
                    COUNT(m.id) AS message_count,
                    COALESCE((
                        SELECT substr(m2.content, 1, 120)
                        FROM messages m2
                        WHERE m2.conversation_id = c.id
                        ORDER BY datetime(m2.created_at) DESC
                        LIMIT 1
                    ), '') AS last_message_preview
                FROM conversations c
                LEFT JOIN messages m ON m.conversation_id = c.id
                WHERE c.id = ? AND c.owner_id = ?
                GROUP BY c.id
                """,
                (conversation_id, owner_id),
            ).fetchone()
        if row is None:
            return None
        return self._row_to_conversation(row)

    def list_conversations(self, owner_id: str) -> list[Conversation]:
        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT
                    c.*,
                    COUNT(m.id) AS message_count,
                    COALESCE((
                        SELECT substr(m2.content, 1, 120)
                        FROM messages m2
                        WHERE m2.conversation_id = c.id
                        ORDER BY datetime(m2.created_at) DESC
                        LIMIT 1
                    ), '') AS last_message_preview
                FROM conversations c
                LEFT JOIN messages m ON m.conversation_id = c.id
                WHERE c.owner_id = ?
                GROUP BY c.id
                ORDER BY datetime(c.updated_at) DESC
                """,
                (owner_id,),
            ).fetchall()
        return [self._row_to_conversation(row) for row in rows]

    def delete_conversation(self, conversation_id: str, owner_id: str) -> None:
        if self.get_conversation(conversation_id, owner_id) is None:
            raise ValueError("conversation not found")
        with self._connection() as conn:
            conn.execute(
                """
                DELETE FROM messages
                WHERE conversation_id = ?
                """,
                (conversation_id,),
            )
            conn.execute(
                """
                DELETE FROM conversations
                WHERE id = ? AND owner_id = ?
                """,
                (conversation_id, owner_id),
            )

    def delete_conversations_except_latest(
        self,
        owner_id: str,
        keep_count: int,
    ) -> tuple[list[str], int]:
        keep_count = max(0, int(keep_count))
        conversations = self.list_conversations(owner_id)
        stale_conversations = conversations[keep_count:]
        deletable_ids = [
            item.id
            for item in stale_conversations
            if item.metadata.get("realtime_status") != "waiting"
        ]
        skipped_count = len(stale_conversations) - len(deletable_ids)

        if not deletable_ids:
            return [], skipped_count

        placeholders = ", ".join("?" for _ in deletable_ids)
        with self._connection() as conn:
            conn.execute(
                f"""
                DELETE FROM messages
                WHERE conversation_id IN ({placeholders})
                """,
                deletable_ids,
            )
            conn.execute(
                f"""
                DELETE FROM conversations
                WHERE owner_id = ? AND id IN ({placeholders})
                """,
                (owner_id, *deletable_ids),
            )
        return deletable_ids, skipped_count

    def get_messages(self, conversation_id: str, owner_id: str) -> list[ConversationMessage]:
        if self.get_conversation(conversation_id, owner_id) is None:
            raise ValueError("conversation not found")
        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT id, conversation_id, role, content, status, response_id, metadata, created_at
                FROM messages
                WHERE conversation_id = ?
                ORDER BY datetime(created_at) ASC
                """,
                (conversation_id,),
            ).fetchall()
        return [
            ConversationMessage(
                id=str(row["id"]),
                conversation_id=str(row["conversation_id"]),
                role=str(row["role"]),
                content=str(row["content"]),
                created_at=str(row["created_at"]),
                status=str(row["status"]),
                response_id=str(row["response_id"]) if row["response_id"] else None,
                metadata=_json_load(row["metadata"], {}),
            )
            for row in rows
        ]

    def get_statistics_summary(
        self,
        owner_id: str,
        *,
        start_at: str | None = None,
        end_at: str | None = None,
    ) -> dict[str, Any]:
        start_dt = _parse_iso_datetime(start_at or "")
        end_dt = _parse_iso_datetime(end_at or "")

        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT
                    m.conversation_id,
                    m.role,
                    m.content,
                    m.created_at,
                    m.response_id
                FROM messages m
                JOIN conversations c ON c.id = m.conversation_id
                WHERE c.owner_id = ?
                ORDER BY
                    m.conversation_id ASC,
                    datetime(m.created_at) ASC,
                    CASE
                        WHEN m.role = 'user' THEN 0
                        WHEN m.role = 'assistant' THEN 1
                        ELSE 2
                    END ASC,
                    m.id ASC
                """,
                (owner_id,),
            ).fetchall()

        latest_user_messages: dict[str, dict[str, Any]] = {}
        total_requests = 0
        total_input_tokens = 0
        total_output_tokens = 0
        total_tokens = 0
        total_request_seconds = 0.0

        for row in rows:
            conversation_id = str(row["conversation_id"])
            created_at = _parse_iso_datetime(str(row["created_at"] or ""))
            if created_at is None:
                continue

            role = str(row["role"] or "")
            content = str(row["content"] or "")

            if role == "user":
                latest_user_messages[conversation_id] = {
                    "content": content,
                    "created_at": created_at,
                }
                continue

            if role != "assistant":
                continue

            if not str(row["response_id"] or "").strip():
                continue
            if start_dt is not None and created_at < start_dt:
                continue
            if end_dt is not None and created_at > end_dt:
                continue

            user_message = latest_user_messages.get(conversation_id)
            if user_message is None:
                continue

            usage = _estimate_usage(str(user_message["content"]), content)
            duration_seconds = max(
                0.0,
                (created_at - user_message["created_at"]).total_seconds(),
            )

            total_requests += 1
            total_input_tokens += usage["input_tokens"]
            total_output_tokens += usage["output_tokens"]
            total_tokens += usage["total_tokens"]
            total_request_seconds += duration_seconds

        average_request_time_seconds = (
            total_request_seconds / total_requests if total_requests else 0.0
        )
        average_tpm = (
            total_output_tokens / (total_request_seconds / 60.0)
            if total_request_seconds > 0
            else 0.0
        )
        return {
            "total_requests": total_requests,
            "average_request_time_seconds": average_request_time_seconds,
            "average_tpm": average_tpm,
            "total_tokens": total_tokens,
            "input_tokens": total_input_tokens,
            "output_tokens": total_output_tokens,
            "start_at": start_at,
            "end_at": end_at,
        }

    def find_conversation_by_tool_call_id(
        self, owner_id: str, tool_call_id: str
    ) -> Conversation | None:
        if not tool_call_id:
            return None
        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT
                    c.*,
                    COUNT(all_messages.id) AS message_count,
                    COALESCE((
                        SELECT substr(m2.content, 1, 120)
                        FROM messages m2
                        WHERE m2.conversation_id = c.id
                        ORDER BY datetime(m2.created_at) DESC
                        LIMIT 1
                    ), '') AS last_message_preview,
                    matched.metadata AS matched_metadata
                FROM conversations c
                JOIN messages matched ON matched.conversation_id = c.id
                LEFT JOIN messages all_messages ON all_messages.conversation_id = c.id
                WHERE c.owner_id = ?
                GROUP BY c.id, matched.id
                ORDER BY datetime(matched.created_at) DESC
                """,
                (owner_id,),
            ).fetchall()
        for row in rows:
            metadata = _json_load(row["matched_metadata"], {})
            if str(metadata.get("tool_call_id", "")).strip() != tool_call_id:
                continue
            return self._row_to_conversation(row)
        return None

    def add_message(
        self,
        conversation_id: str,
        role: str,
        content: str,
        status: str = "final",
        response_id: str | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> ConversationMessage:
        message = ConversationMessage(
            id=str(uuid.uuid4()),
            conversation_id=conversation_id,
            role=role,
            content=content,
            created_at=utc_now_iso(),
            status=status,
            response_id=response_id,
            metadata=metadata or {},
        )
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO messages
                (id, conversation_id, role, content, status, response_id, metadata, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    message.id,
                    message.conversation_id,
                    message.role,
                    message.content,
                    message.status,
                    message.response_id,
                    _json_dump(message.metadata),
                    message.created_at,
                ),
            )
            conn.execute(
                """
                UPDATE conversations
                SET updated_at = ?, last_message_at = ?
                WHERE id = ?
                """,
                (
                    message.created_at,
                    message.created_at,
                    conversation_id,
                ),
            )
        return message

    def update_conversation(
        self,
        conversation_id: str,
        owner_id: str,
        *,
        title: str | None = None,
        last_user_text: str | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        current = self.get_conversation(conversation_id, owner_id)
        if current is None:
            raise ValueError("conversation not found")
        new_title = title if title is not None else current.title
        new_last_user_text = last_user_text if last_user_text is not None else current.last_user_text
        new_metadata = metadata if metadata is not None else current.metadata
        with self._connection() as conn:
            conn.execute(
                """
                UPDATE conversations
                SET title = ?, last_user_text = ?, metadata = ?, updated_at = ?
                WHERE id = ? AND owner_id = ?
                """,
                (
                    new_title,
                    new_last_user_text,
                    _json_dump(new_metadata),
                    utc_now_iso(),
                    conversation_id,
                    owner_id,
                ),
            )
        refreshed = self.get_conversation(conversation_id, owner_id)
        if refreshed is None:
            raise ValueError("conversation not found")
        return refreshed

    def get_config(self, key: str, default: str = "") -> str:
        with self._connection() as conn:
            row = conn.execute(
                """
                SELECT value
                FROM config
                WHERE key = ?
                """,
                (key,),
            ).fetchone()
        if row is None:
            return default
        return str(row["value"] or "")

    def set_config(self, key: str, value: str) -> None:
        with self._connection() as conn:
            conn.execute(
                """
                INSERT INTO config (key, value)
                VALUES (?, ?)
                ON CONFLICT(key) DO UPDATE SET value = excluded.value
                """,
                (key, value),
            )

    def record_assistant_reply(
        self,
        conversation_id: str,
        owner_id: str,
        user_text: str,
        assistant_text: str,
        response_id: str,
        assistant_metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        if self.get_conversation(conversation_id, owner_id) is None:
            raise ValueError("conversation not found")
        self.add_message(
            conversation_id,
            "assistant",
            assistant_text,
            response_id=response_id,
            metadata=assistant_metadata or {},
        )
        current = self.get_conversation(conversation_id, owner_id)
        if current is None:
            raise ValueError("conversation not found")
        if current.title in {"新会话", "New conversation", ""}:
            self.update_conversation(
                conversation_id,
                owner_id,
                title=build_title(user_text),
                last_user_text=user_text[:1000],
            )
        else:
            self.update_conversation(
                conversation_id,
                owner_id,
                last_user_text=user_text[:1000],
            )
        refreshed = self.get_conversation(conversation_id, owner_id)
        if refreshed is None:
            raise ValueError("conversation not found")
        return refreshed

    @staticmethod
    def _row_to_conversation(row: sqlite3.Row) -> Conversation:
        return Conversation(
            id=str(row["id"]),
            owner_id=str(row["owner_id"]),
            title=str(row["title"]),
            last_user_text=str(row["last_user_text"] or ""),
            created_at=str(row["created_at"]),
            updated_at=str(row["updated_at"]),
            last_message_at=str(row["last_message_at"]),
            metadata=_json_load(row["metadata"], {}),
            message_count=int(row["message_count"] or 0),
            last_message_preview=str(row["last_message_preview"] or ""),
        )
