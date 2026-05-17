from __future__ import annotations

import threading
import uuid
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any


@dataclass
class PendingTurn:
    request_id: str
    conversation_id: str
    owner_id: str
    model: str
    input_text: str
    input_payload: Any
    request_data: dict[str, Any]
    request_context_signature: str
    conversation_title: str
    previous_summary: str
    created_at: str
    event: threading.Event = field(default_factory=threading.Event)
    stream_event: threading.Event = field(default_factory=threading.Event)
    assistant_text: str = ""
    response_id: str = ""
    draft_chunks: list[str] = field(default_factory=list)
    persisted: bool = False
    aborted: bool = False
    abort_message: str = ""
    response_mode: str = "assistant_message"
    response_output_items: list[dict[str, Any]] = field(default_factory=list)
    response_output_text: str = ""
    response_metadata: dict[str, Any] = field(default_factory=dict)
    heartbeat_text: str = ""
    heartbeat_interval_seconds: float = 0.0


class PendingTurnRegistry:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._by_request_id: dict[str, PendingTurn] = {}
        self._by_conversation_id: dict[str, str] = {}

    def register(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        model: str,
        input_text: str,
        input_payload: Any,
        request_data: dict[str, Any],
        request_context_signature: str,
        conversation_title: str,
        previous_summary: str,
        heartbeat_text: str = "",
        heartbeat_interval_seconds: float = 0.0,
    ) -> PendingTurn:
        with self._lock:
            if conversation_id in self._by_conversation_id:
                raise ValueError("conversation is waiting for a reply")
            pending = PendingTurn(
                request_id=f"resp_{uuid.uuid4().hex}",
                conversation_id=conversation_id,
                owner_id=owner_id,
                model=model,
                input_text=input_text,
                input_payload=input_payload,
                request_data=request_data,
                request_context_signature=request_context_signature,
                conversation_title=conversation_title,
                previous_summary=previous_summary,
                created_at=datetime.now(timezone.utc).isoformat(),
                heartbeat_text=heartbeat_text,
                heartbeat_interval_seconds=max(0.0, float(heartbeat_interval_seconds or 0.0)),
            )
            self._by_request_id[pending.request_id] = pending
            self._by_conversation_id[conversation_id] = pending.request_id
            return pending

    def get_by_conversation(self, conversation_id: str) -> PendingTurn | None:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                return None
            return self._by_request_id.get(request_id)

    def resolve(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        assistant_text: str,
        response_id: str,
        response_mode: str = "assistant_message",
        response_output_items: list[dict[str, Any]] | None = None,
        response_output_text: str | None = None,
        response_metadata: dict[str, Any] | None = None,
    ) -> PendingTurn:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                raise ValueError("conversation is not waiting for a reply")
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                raise ValueError("conversation is not waiting for a reply")
            pending.assistant_text = assistant_text
            pending.response_id = response_id
            pending.response_mode = response_mode
            pending.response_output_items = list(response_output_items or [])
            pending.response_output_text = (
                assistant_text if response_output_text is None else response_output_text
            )
            pending.response_metadata = dict(response_metadata or {})
            pending.persisted = True
            pending.stream_event.set()
            pending.event.set()
            return pending

    def add_draft(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        chunk: str,
    ) -> PendingTurn:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                raise ValueError("conversation is not waiting for a reply")
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                raise ValueError("conversation is not waiting for a reply")
            pending.draft_chunks.append(chunk)
            pending.stream_event.set()
            return pending

    def abort(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        error_message: str,
    ) -> PendingTurn:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                raise ValueError("conversation is not waiting for a reply")
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                raise ValueError("conversation is not waiting for a reply")
            pending.aborted = True
            pending.abort_message = error_message
            pending.stream_event.set()
            pending.event.set()
            return pending

    def discard(
        self,
        *,
        conversation_id: str,
        owner_id: str,
    ) -> PendingTurn | None:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                return None
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                return None
            self._by_request_id.pop(request_id, None)
            self._by_conversation_id.pop(conversation_id, None)
            return pending

    def wait(self, request_id: str) -> PendingTurn:
        with self._lock:
            pending = self._by_request_id.get(request_id)
        if pending is None:
            raise ValueError("response not found")
        pending.event.wait()
        with self._lock:
            self._by_request_id.pop(request_id, None)
            self._by_conversation_id.pop(pending.conversation_id, None)
        return pending
