from __future__ import annotations

import uuid
from typing import Any, Callable

from ..repositories import ConversationStore
from .pending import PendingTurnRegistry
from .turn_protocols import build_protocol_response_id, normalize_message_text


class TurnOutputController:
    def __init__(
        self,
        *,
        store: ConversationStore,
        pending_turns: PendingTurnRegistry,
        publish_sync: Callable[[str, str | None], None] | None = None,
    ):
        self._store = store
        self._pending_turns = pending_turns
        self._publish_sync = publish_sync

    def add_text_delta(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        text: str,
    ):
        pending = self._pending_turns.add_draft(
            conversation_id=conversation_id,
            owner_id=owner_id,
            chunk=normalize_message_text(text),
        )
        conversation = self._store.get_conversation(conversation_id, owner_id)
        if conversation is not None:
            self._store.update_conversation(
                conversation_id,
                owner_id,
                metadata={
                    **conversation.metadata,
                    "realtime_status": "waiting",
                    "realtime_draft_text": pending.draft_text,
                },
            )
            self._notify(owner_id, conversation_id)
        return pending

    def complete_assistant_message(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        text: str,
        provider: str,
        model: str | None = None,
    ):
        pending = self._require_pending(conversation_id, owner_id)
        submitted_text = normalize_message_text(text)
        assistant_text = (
            submitted_text
            if not pending.draft_text or submitted_text.startswith(pending.draft_text)
            else f"{pending.draft_text}{submitted_text}"
        )
        if not assistant_text.strip():
            raise ValueError("assistant message text is required")
        response_id = build_protocol_response_id(pending.request_format, pending.request_id)
        assistant_metadata = {
            "provider": provider,
            "model": str(model or pending.model or "mock-gpt-4.1-mini"),
            "response_mode": "assistant_message",
        }
        updated_conversation = self._store.record_assistant_reply(
            conversation_id,
            owner_id,
            pending.input_text,
            assistant_text,
            response_id=response_id,
            assistant_metadata=assistant_metadata,
        )
        self._store.update_conversation(
            conversation_id,
            owner_id,
            metadata={
                **updated_conversation.metadata,
                "realtime_status": "closed",
                "realtime_draft_text": "",
            },
        )
        self._notify(owner_id, conversation_id)
        resolved = self._pending_turns.resolve(
            conversation_id=conversation_id,
            owner_id=owner_id,
            assistant_text=assistant_text,
            response_id=response_id,
            response_mode="assistant_message",
            response_output_items=[],
            response_output_text=assistant_text,
        )
        return resolved, assistant_metadata

    def complete_tool_call(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        tool_name: str,
        arguments: str,
        provider: str,
        model: str | None = None,
        tool_call_id: str | None = None,
    ):
        pending = self._require_pending(conversation_id, owner_id)
        call_id = tool_call_id or f"call_{uuid.uuid4().hex[:24]}"
        assistant_text = f"{tool_name}({arguments})"
        response_id = build_protocol_response_id(pending.request_format, pending.request_id)
        output_items = [
            {
                "id": f"fc_{uuid.uuid4().hex[:24]}",
                "type": "function_call",
                "status": "completed",
                "call_id": call_id,
                "name": tool_name,
                "arguments": arguments,
            }
        ]
        assistant_metadata = {
            "provider": provider,
            "model": str(model or pending.model or "mock-gpt-4.1-mini"),
            "response_mode": "tool_call",
            "tool_name": tool_name,
            "tool_call_id": call_id,
            "arguments": arguments,
        }
        updated_conversation = self._store.record_assistant_reply(
            conversation_id,
            owner_id,
            pending.input_text,
            assistant_text,
            response_id=response_id,
            assistant_metadata=assistant_metadata,
        )
        self._store.update_conversation(
            conversation_id,
            owner_id,
            metadata={
                **updated_conversation.metadata,
                "realtime_status": "closed",
                "realtime_draft_text": "",
            },
        )
        self._notify(owner_id, conversation_id)
        resolved = self._pending_turns.resolve(
            conversation_id=conversation_id,
            owner_id=owner_id,
            assistant_text=assistant_text,
            response_id=response_id,
            response_mode="tool_call",
            response_output_items=output_items,
            response_output_text="",
        )
        return resolved, assistant_metadata

    def abort(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        error_message: str,
    ):
        return self._pending_turns.abort(
            conversation_id=conversation_id,
            owner_id=owner_id,
            error_message=error_message,
        )

    def _require_pending(self, conversation_id: str, owner_id: str):
        pending = self._pending_turns.get_by_conversation(conversation_id)
        if pending is None or pending.owner_id != owner_id:
            raise ValueError("conversation is not waiting for a reply")
        return pending

    def _notify(self, owner_id: str, conversation_id: str) -> None:
        if self._publish_sync is None:
            return
        self._publish_sync(owner_id, conversation_id)
