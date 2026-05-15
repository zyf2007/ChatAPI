from __future__ import annotations

import json
import sqlite3
import uuid
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


@dataclass
class Conversation:
    id: str
    owner_id: str
    title: str
    summary: str
    last_user_text: str
    context_signature: str
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
            "summary": self.summary,
            "last_user_text": self.last_user_text,
            "context_signature": self.context_signature,
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

    def _init_db(self) -> None:
        with self._connect() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS conversations (
                    id TEXT PRIMARY KEY,
                    owner_id TEXT NOT NULL,
                    title TEXT NOT NULL,
                    summary TEXT NOT NULL DEFAULT '',
                    last_user_text TEXT NOT NULL DEFAULT '',
                    context_signature TEXT NOT NULL DEFAULT '',
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
            self._ensure_column(
                conn,
                "conversations",
                "last_user_text",
                "TEXT NOT NULL DEFAULT ''",
            )
            self._ensure_column(
                conn,
                "conversations",
                "context_signature",
                "TEXT NOT NULL DEFAULT ''",
            )

    def _ensure_column(
        self, conn: sqlite3.Connection, table: str, column: str, ddl: str
    ) -> None:
        cols = conn.execute(f"PRAGMA table_info({table})").fetchall()
        names = {str(col["name"]) for col in cols}
        if column not in names:
            conn.execute(f"ALTER TABLE {table} ADD COLUMN {column} {ddl}")

    def create_conversation(
        self,
        owner_id: str,
        title: str = "新会话",
        summary: str = "",
        last_user_text: str = "",
        context_signature: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        now = utc_now_iso()
        conv = Conversation(
            id=str(uuid.uuid4()),
            owner_id=owner_id,
            title=title or "新会话",
            summary=summary,
            last_user_text=last_user_text,
            context_signature=context_signature,
            created_at=now,
            updated_at=now,
            last_message_at=now,
            metadata=metadata or {},
            message_count=0,
            last_message_preview="",
        )
        with self._connect() as conn:
            conn.execute(
                """
                INSERT INTO conversations
                (id, owner_id, title, summary, last_user_text, context_signature, metadata, created_at, updated_at, last_message_at)
                VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                """,
                (
                    conv.id,
                    conv.owner_id,
                    conv.title,
                    conv.summary,
                    conv.last_user_text,
                    conv.context_signature,
                    _json_dump(conv.metadata),
                    conv.created_at,
                    conv.updated_at,
                    conv.last_message_at,
                ),
            )
        return conv

    def get_conversation(self, conversation_id: str, owner_id: str) -> Conversation | None:
        with self._connect() as conn:
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
        with self._connect() as conn:
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
        with self._connect() as conn:
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

    def get_messages(self, conversation_id: str, owner_id: str) -> list[ConversationMessage]:
        if self.get_conversation(conversation_id, owner_id) is None:
            raise ValueError("conversation not found")
        with self._connect() as conn:
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
        with self._connect() as conn:
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
                SET updated_at = ?, last_message_at = ?, summary = CASE
                    WHEN summary = '' THEN ?
                    ELSE substr(summary || '\n' || ?, 1, 1000)
                END
                WHERE id = ?
                """,
                (
                    message.created_at,
                    message.created_at,
                    content[:240],
                    content[:240],
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
        summary: str | None = None,
        last_user_text: str | None = None,
        context_signature: str | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        current = self.get_conversation(conversation_id, owner_id)
        if current is None:
            raise ValueError("conversation not found")
        new_title = title if title is not None else current.title
        new_summary = summary if summary is not None else current.summary
        new_last_user_text = last_user_text if last_user_text is not None else current.last_user_text
        new_context_signature = (
            context_signature if context_signature is not None else current.context_signature
        )
        new_metadata = metadata if metadata is not None else current.metadata
        with self._connect() as conn:
            conn.execute(
                """
                UPDATE conversations
                SET title = ?, summary = ?, last_user_text = ?, context_signature = ?, metadata = ?, updated_at = ?
                WHERE id = ? AND owner_id = ?
                """,
                (
                    new_title,
                    new_summary,
                    new_last_user_text,
                    new_context_signature,
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

    def record_turn(
        self,
        conversation_id: str,
        owner_id: str,
        user_text: str,
        assistant_text: str,
        response_id: str,
        context_signature: str | None = None,
        assistant_metadata: dict[str, Any] | None = None,
    ) -> Conversation:
        if self.get_conversation(conversation_id, owner_id) is None:
            raise ValueError("conversation not found")
        self.add_message(
            conversation_id,
            "user",
            user_text,
            metadata={"turn": "user"},
        )
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
                context_signature=context_signature or current.context_signature,
            )
        else:
            self.update_conversation(
                conversation_id,
                owner_id,
                last_user_text=user_text[:1000],
                context_signature=context_signature or current.context_signature,
            )
        refreshed = self.get_conversation(conversation_id, owner_id)
        if refreshed is None:
            raise ValueError("conversation not found")
        return refreshed

    def record_assistant_reply(
        self,
        conversation_id: str,
        owner_id: str,
        user_text: str,
        assistant_text: str,
        response_id: str,
        context_signature: str | None = None,
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
                context_signature=context_signature or current.context_signature,
            )
        else:
            self.update_conversation(
                conversation_id,
                owner_id,
                last_user_text=user_text[:1000],
                context_signature=context_signature or current.context_signature,
            )
        refreshed = self.get_conversation(conversation_id, owner_id)
        if refreshed is None:
            raise ValueError("conversation not found")
        return refreshed

    def match_or_create_conversation(
        self, owner_id: str, context_signature: str, title_hint: str = ""
    ) -> Conversation:
        candidates = self.list_conversations(owner_id)
        if not candidates:
            return self.create_conversation(
                owner_id,
                title=build_title(title_hint),
                context_signature=context_signature,
            )
        for candidate in candidates:
            if candidate.context_signature == context_signature:
                return candidate
        return self.create_conversation(
            owner_id,
            title=build_title(title_hint),
            context_signature=context_signature,
        )

    @staticmethod
    def _row_to_conversation(row: sqlite3.Row) -> Conversation:
        return Conversation(
            id=str(row["id"]),
            owner_id=str(row["owner_id"]),
            title=str(row["title"]),
            summary=str(row["summary"]),
            last_user_text=str(row["last_user_text"] or ""),
            context_signature=str(row["context_signature"] or ""),
            created_at=str(row["created_at"]),
            updated_at=str(row["updated_at"]),
            last_message_at=str(row["last_message_at"]),
            metadata=_json_load(row["metadata"], {}),
            message_count=int(row["message_count"] or 0),
            last_message_preview=str(row["last_message_preview"] or ""),
        )
