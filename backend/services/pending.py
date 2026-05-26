from __future__ import annotations

import json
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any


@dataclass
class PendingTurn:
    request_id: str
    conversation_id: str
    owner_id: str
    model: str
    input_text: str
    request_format: str = "responses"
    reasoning_stream_mode: str = ""
    created_at: float = field(default_factory=time.time)
    max_age_seconds: float = 0.0
    auto_abort_message: str = ""
    max_output_chars: int = 0
    output_limit_abort_message: str = ""
    event: threading.Event = field(default_factory=threading.Event)
    stream_event: threading.Event = field(default_factory=threading.Event)
    assistant_text: str = ""
    response_id: str = ""
    draft_chunks: list[Any] = field(default_factory=list)
    draft_text: str = ""
    draft_segments: list[dict[str, str]] = field(default_factory=list)
    draft_answer_text: str = ""
    output_chars: int = 0
    aborted: bool = False
    abort_message: str = ""
    resolved: bool = False
    response_mode: str = "assistant_message"
    response_output_items: list[dict[str, Any]] = field(default_factory=list)
    response_output_text: str = ""
    available_tool_names: set[str] = field(default_factory=set)
    available_tool_schemas: dict[str, dict[str, Any]] = field(default_factory=dict)


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
        request_format: str = "responses",
        reasoning_stream_mode: str = "",
        max_age_seconds: float = 0.0,
        auto_abort_message: str = "",
        max_output_chars: int = 0,
        output_limit_abort_message: str = "",
        available_tool_names: set[str] | None = None,
        available_tool_schemas: dict[str, dict[str, Any]] | None = None,
    ) -> PendingTurn:
        with self._lock:
            if conversation_id in self._by_conversation_id:
                existing_request_id = self._by_conversation_id.get(conversation_id)
                existing = self._by_request_id.get(existing_request_id or "")
                if existing is not None and existing.event.is_set():
                    self._by_conversation_id.pop(conversation_id, None)
                else:
                    raise ValueError("conversation is waiting for a reply")
            pending = PendingTurn(
                request_id=f"resp_{uuid.uuid4().hex}",
                conversation_id=conversation_id,
                owner_id=owner_id,
                model=model,
                input_text=input_text,
                request_format=request_format,
                reasoning_stream_mode=reasoning_stream_mode,
                max_age_seconds=max(0.0, float(max_age_seconds or 0.0)),
                auto_abort_message=str(auto_abort_message or ""),
                max_output_chars=max(0, int(max_output_chars or 0)),
                output_limit_abort_message=str(output_limit_abort_message or ""),
                available_tool_names=available_tool_names or set(),
                available_tool_schemas=available_tool_schemas or {},
            )
            self._by_request_id[pending.request_id] = pending
            self._by_conversation_id[conversation_id] = pending.request_id
            return pending

    def get_by_conversation(self, conversation_id: str) -> PendingTurn | None:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                return None
            pending = self._by_request_id.get(request_id)
            if pending is not None and pending.event.is_set():
                self._by_conversation_id.pop(conversation_id, None)
                return None
            return pending

    def active_count_by_owner(self, owner_id: str) -> int:
        with self._lock:
            return sum(
                1
                for pending in self._by_request_id.values()
                if pending.owner_id == owner_id and not pending.event.is_set()
            )

    def abort_owner_over_limit(
        self,
        *,
        owner_id: str,
        max_active: int,
        error_message: str,
    ) -> list[PendingTurn]:
        if max_active <= 0:
            return []
        aborted: list[PendingTurn] = []
        with self._lock:
            active = sorted(
                (
                    pending
                    for pending in self._by_request_id.values()
                    if pending.owner_id == owner_id and not pending.event.is_set()
                ),
                key=lambda pending: pending.created_at,
            )
            while len(active) >= max_active:
                pending = active.pop(0)
                aborted.append(self._mark_aborted_locked(pending, error_message))
        return aborted

    def abort_expired(self, *, max_age_seconds: float, error_message: str) -> list[PendingTurn]:
        if max_age_seconds <= 0:
            return []
        deadline = time.time() - max_age_seconds
        aborted: list[PendingTurn] = []
        with self._lock:
            for pending in list(self._by_request_id.values()):
                if pending.event.is_set() or pending.created_at > deadline:
                    continue
                aborted.append(self._mark_aborted_locked(pending, error_message))
        return aborted

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
    ) -> PendingTurn:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                raise ValueError("conversation is not waiting for a reply")
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                raise ValueError("conversation is not waiting for a reply")
            if pending.resolved or pending.event.is_set():
                raise ValueError("conversation reply is already completed")
            pending.assistant_text = assistant_text
            pending.response_id = response_id
            pending.response_mode = response_mode
            pending.response_output_items = list(response_output_items or [])
            pending.response_output_text = (
                assistant_text if response_output_text is None else response_output_text
            )
            pending.resolved = True
            pending.stream_event.set()
            pending.event.set()
            return pending

    def consume_draft_chunks(self, request_id: str) -> list[Any]:
        with self._lock:
            pending = self._by_request_id.get(request_id)
            if pending is None:
                return []
            chunks = list(pending.draft_chunks)
            pending.draft_chunks.clear()
            return chunks

    def add_draft(
        self,
        *,
        conversation_id: str,
        owner_id: str,
        chunk: str,
        kind: str = "answer",
    ) -> PendingTurn:
        with self._lock:
            request_id = self._by_conversation_id.get(conversation_id)
            if not request_id:
                raise ValueError("conversation is not waiting for a reply")
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.owner_id != owner_id:
                raise ValueError("conversation is not waiting for a reply")
            if pending.resolved or pending.event.is_set():
                raise ValueError("conversation reply is already completed")
            chunk = str(chunk or "")
            output_limit_abort = self._mark_aborted_if_output_limit_exceeded_locked(
                pending,
                chunk,
            )
            if output_limit_abort is not None:
                return output_limit_abort
            normalized_kind = "thinking" if str(kind).strip() == "thinking" else "answer"
            if normalized_kind == "thinking":
                if not pending.draft_segments and pending.draft_text:
                    pending.draft_segments.append({"type": "answer", "text": pending.draft_text})
                if pending.draft_segments and pending.draft_segments[-1]["type"] == "thinking":
                    pending.draft_segments[-1]["text"] += chunk
                else:
                    pending.draft_segments.append({"type": "thinking", "text": chunk})
                pending.draft_chunks.append({"kind": "thinking", "text": chunk})
                pending.draft_text = _serialize_draft_segments(pending.draft_segments)
            elif pending.draft_segments:
                if pending.draft_segments[-1]["type"] == "answer":
                    pending.draft_segments[-1]["text"] += chunk
                else:
                    pending.draft_segments.append({"type": "answer", "text": chunk})
                pending.draft_chunks.append({"kind": "answer", "text": chunk})
                pending.draft_answer_text += chunk
                pending.draft_text = _serialize_draft_segments(pending.draft_segments)
            else:
                pending.draft_chunks.append(chunk)
                pending.draft_text += chunk
                pending.draft_answer_text += chunk
            pending.output_chars += len(chunk)
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
            if pending.resolved or pending.event.is_set():
                raise ValueError("conversation reply is already completed")
            return self._mark_aborted_locked(pending, error_message)

    def abort_by_request_id(self, *, request_id: str, error_message: str) -> PendingTurn | None:
        with self._lock:
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.resolved or pending.event.is_set():
                return None
            return self._mark_aborted_locked(pending, error_message)

    def abort_if_expired(self, request_id: str) -> PendingTurn | None:
        with self._lock:
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.resolved or pending.event.is_set():
                return None
            if pending.max_age_seconds <= 0:
                return None
            if time.time() - pending.created_at < pending.max_age_seconds:
                return None
            return self._mark_aborted_locked(
                pending,
                pending.auto_abort_message or "本次回复等待超过限制，已自动结束，请重新发送。",
            )

    def abort_if_output_would_exceed(
        self,
        *,
        request_id: str,
        extra_text: str,
    ) -> PendingTurn | None:
        with self._lock:
            pending = self._by_request_id.get(request_id)
            if pending is None or pending.resolved or pending.event.is_set():
                return None
            return self._mark_aborted_if_output_limit_exceeded_locked(
                pending,
                str(extra_text or ""),
            )

    @staticmethod
    def _clear_draft_locked(pending: PendingTurn) -> None:
        pending.draft_chunks.clear()
        pending.draft_text = ""
        pending.draft_segments.clear()
        pending.draft_answer_text = ""
        pending.output_chars = 0

    def _mark_aborted_if_output_limit_exceeded_locked(
        self,
        pending: PendingTurn,
        extra_text: str,
    ) -> PendingTurn | None:
        if pending.max_output_chars <= 0:
            return None
        if not extra_text:
            return None
        if pending.output_chars + len(extra_text) <= pending.max_output_chars:
            return None
        return self._mark_aborted_locked(
            pending,
            pending.output_limit_abort_message or "本次回复超过长度限制，已自动结束，请重新发送。",
        )

    def _mark_aborted_locked(self, pending: PendingTurn, error_message: str) -> PendingTurn:
        pending.aborted = True
        pending.abort_message = error_message
        self._clear_draft_locked(pending)
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


def _serialize_draft_segments(segments: list[dict[str, str]]) -> str:
    payload: list[dict[str, str]] = []
    for segment in segments:
        text = str(segment.get("text") or "")
        if not text:
            continue
        segment_type = "reasoning_text" if segment.get("type") == "thinking" else "output_text"
        payload.append({
            "type": segment_type,
            "text": text,
        })
    return json.dumps(payload, ensure_ascii=False)
