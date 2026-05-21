from __future__ import annotations

import uuid
from typing import Any, Callable

from .models import ConversationMessage, json_dump, json_load, utc_now_iso


class MessageCrudMixin:
    _COMPARABLE_ROLES = {"user", "assistant", "tool"}

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
        return [self._row_to_message(row) for row in rows]

    def get_recent_messages(self, owner_id: str, limit: int = 20) -> list[dict[str, Any]]:
        limit = max(1, min(int(limit), 100))
        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT
                    m.id,
                    m.conversation_id,
                    c.title AS conversation_title,
                    m.role,
                    m.content,
                    m.status,
                    m.response_id,
                    m.metadata,
                    m.created_at
                FROM messages m
                JOIN conversations c ON c.id = m.conversation_id
                WHERE c.owner_id = ?
                ORDER BY datetime(m.created_at) DESC, m.id DESC
                LIMIT ?
                """,
                (owner_id, limit),
            ).fetchall()
        return [
            {
                "id": str(row["id"]),
                "conversation_id": str(row["conversation_id"]),
                "conversation_title": str(row["conversation_title"] or ""),
                "role": str(row["role"]),
                "content": str(row["content"]),
                "status": str(row["status"]),
                "response_id": str(row["response_id"]) if row["response_id"] else None,
                "metadata": json_load(row["metadata"], {}),
                "created_at": str(row["created_at"]),
            }
            for row in rows
        ]

    def iter_messages(self) -> list[ConversationMessage]:
        with self._connection() as conn:
            rows = conn.execute(
                """
                SELECT id, conversation_id, role, content, status, response_id, metadata, created_at
                FROM messages
                ORDER BY datetime(created_at) ASC
                """
            ).fetchall()
        return [self._row_to_message(row) for row in rows]

    def find_conversation_by_tool_call_id(self, owner_id: str, tool_call_id: str):
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
            metadata = json_load(row["matched_metadata"], {})
            if str(metadata.get("tool_call_id", "")).strip() != tool_call_id:
                continue
            return self._row_to_conversation(row)
        return None

    def find_conversation_by_message_history(
        self,
        owner_id: str,
        messages: list[dict[str, str]],
        *,
        normalize_stored_content: Callable[[str], str] | None = None,
    ):
        if not messages:
            return None

        candidates = self.list_conversations(owner_id)
        best_match = None
        best_length = -1
        normalize_content = normalize_stored_content or (lambda value: value)

        for conversation in candidates:
            stored_messages = self._comparable_messages_for_conversation(
                conversation.id,
                owner_id,
                normalize_content=normalize_content,
            )
            if not stored_messages or len(stored_messages) > len(messages):
                continue
            request_prefix = [
                {"role": item["role"], "content": item["content"]}
                for item in messages[: len(stored_messages)]
            ]
            if stored_messages != request_prefix:
                continue
            if len(stored_messages) > best_length:
                best_match = conversation
                best_length = len(stored_messages)

        return best_match

    def get_request_history_prefix_length(
        self,
        conversation_id: str,
        owner_id: str,
        messages: list[dict[str, str]],
        *,
        normalize_stored_content: Callable[[str], str] | None = None,
    ) -> int:
        stored_messages = self._comparable_messages_for_conversation(
            conversation_id,
            owner_id,
            normalize_content=normalize_stored_content or (lambda value: value),
        )
        if not stored_messages or len(stored_messages) > len(messages):
            return 0
        request_prefix = [
            {"role": item["role"], "content": item["content"]}
            for item in messages[: len(stored_messages)]
        ]
        if stored_messages != request_prefix:
            return 0
        return len(stored_messages)

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
                    json_dump(message.metadata),
                    message.created_at,
                ),
            )
            conn.execute(
                """
                UPDATE conversations
                SET updated_at = ?, last_message_at = ?
                WHERE id = ?
                """,
                (message.created_at, message.created_at, conversation_id),
            )
        return message

    @staticmethod
    def _row_to_message(row) -> ConversationMessage:
        return ConversationMessage(
            id=str(row["id"]),
            conversation_id=str(row["conversation_id"]),
            role=str(row["role"]),
            content=str(row["content"]),
            created_at=str(row["created_at"]),
            status=str(row["status"]),
            response_id=str(row["response_id"]) if row["response_id"] else None,
            metadata=json_load(row["metadata"], {}),
        )

    def _comparable_messages_for_conversation(
        self,
        conversation_id: str,
        owner_id: str,
        *,
        normalize_content: Callable[[str], str],
    ) -> list[dict[str, str]]:
        return [
            {"role": message.role, "content": normalize_content(message.content)}
            for message in self.get_messages(conversation_id, owner_id)
            if message.role in self._COMPARABLE_ROLES
        ]
