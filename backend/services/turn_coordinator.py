from __future__ import annotations

import uuid
from dataclasses import dataclass
from typing import Any, Callable

from ..core import AppDependencies
from ..repositories import build_title
from .automation_rules import AutomationRuleEngine
from .ntfy import notify_new_message
from .output_controller import TurnOutputController
from .payload_anthropic import build_anthropic_message_response
from .payload_chat_completions import build_chat_completion_response
from .payload_openai import (
    build_openai_error,
    build_openai_response,
    estimate_usage,
)
from .pending import PendingTurn
from .turn_request_tools import (
    extract_tool_names,
    extract_tool_schemas,
    find_response_message_metadata,
)
from .turn_protocols import (
    build_message_debug_metadata,
    extract_chatbox_comparable_request_messages,
    extract_context_text,
    extract_request_messages,
    normalize_chatbox_history_content,
    normalize_message_text,
    request_input_payload,
    resolve_conversation_for_request,
)


def _normalize_reasoning_stream_mode(value: Any) -> str:
    mode = str(value or "").strip().lower().replace("-", "_")
    if mode == "summery":
        mode = "summary"
    elif mode == "reasoning":
        mode = "reasoning_text"
    if mode in {"summary", "reasoning_text"}:
        return mode
    return ""


def _normalize_request_format(value: Any) -> str:
    request_format = str(value or "").strip().lower().replace("-", "_")
    if request_format in {"responses", "chat_completions", "anthropic_messages"}:
        return request_format
    return ""


def _conversation_request_format(conversation: Any) -> str:
    metadata = dict(getattr(conversation, "metadata", {}) or {})
    return _normalize_request_format(metadata.get("request_format"))


@dataclass(frozen=True)
class PreparedTurn:
    pending: PendingTurn
    conversation_id: str


class TurnCoordinator:
    def __init__(
        self,
        deps: AppDependencies,
        *,
        extensions: dict[str, Any],
        logger: Any,
        publish_sync: Callable[[str, str | None], None] | None = None,
    ):
        self._deps = deps
        self._extensions = extensions
        self._logger = logger
        self._publish_sync = publish_sync
        self._output_controller = TurnOutputController(
            store=deps.store,
            pending_turns=deps.pending_turns,
            publish_sync=publish_sync,
        )
        self._automation_rules = AutomationRuleEngine(
            user_store=deps.user_store,
            output_controller=self._output_controller,
        )

    @property
    def auth(self):
        return self._deps.auth

    @property
    def pending_turns(self):
        return self._deps.pending_turns

    @property
    def store(self):
        return self._deps.store

    @property
    def user_store(self):
        return self._deps.user_store

    @property
    def settings(self):
        return self._deps.settings

    @property
    def message_rate_limiter(self):
        return self._deps.message_rate_limiter

    def get_stream_heartbeat_settings(self, owner_id: str) -> dict[str, Any]:
        return self._automation_rules.get_heartbeat_rule_settings(owner_id)

    def update_stream_heartbeat_settings(
        self,
        owner_id: str,
        *,
        heartbeat_text: str,
        heartbeat_interval_seconds: float,
    ) -> dict[str, Any]:
        return self._automation_rules.update_heartbeat_rule_settings(
            owner_id,
            heartbeat_text=heartbeat_text,
            interval_seconds=heartbeat_interval_seconds,
        )

    def get_automation_rules(self, owner_id: str) -> list[dict[str, Any]]:
        return self._automation_rules.load_rule_payloads(owner_id)

    def update_automation_rules(self, owner_id: str, rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
        return self._automation_rules.save_rule_payloads(owner_id, rules)

    def build_abort_error(self, message_text: str) -> tuple[dict[str, Any], int]:
        return build_openai_error(
            message_text or "request aborted",
            code="request_aborted",
            status=400,
        )

    def build_not_found_error(self, message: str, *, code: str = "not_found", status: int = 404):
        return build_openai_error(message, code=code, status=status)

    def _pending_limits(self) -> dict[str, Any]:
        return self._deps.system_config_store.get_pending_limits()

    def _mark_aborted_pending_turns(self, pending_turns: list[PendingTurn]) -> None:
        for pending in pending_turns:
            conversation = self.store.get_conversation(pending.conversation_id, pending.owner_id)
            self.store.update_conversation(
                pending.conversation_id,
                pending.owner_id,
                metadata={
                    **(conversation.metadata if conversation else {}),
                    "realtime_status": "aborted",
                    "realtime_draft_text": "",
                },
            )
            self._notify(pending.owner_id, pending.conversation_id)

    def enforce_pending_limits(self, owner_id: str) -> dict[str, Any]:
        limits = self._pending_limits()
        abort_message = str(limits.get("abort_message") or "本次回复等待超过限制，已自动结束，请重新发送。")
        aborted = self.pending_turns.abort_expired(
            max_age_seconds=float(limits.get("max_age_seconds") or 0),
            error_message=abort_message,
        )
        aborted.extend(
            self.pending_turns.abort_owner_over_limit(
                owner_id=owner_id,
                max_active=int(limits.get("max_per_user") or 10),
                error_message=abort_message,
            )
        )
        if aborted:
            self._mark_aborted_pending_turns(aborted)
        return limits

    def _resolve_reasoning_stream_mode(
        self,
        data: dict[str, Any],
        *,
        request_format: str,
        conversation: Any | None,
    ) -> str:
        if request_format != "responses":
            return ""

        requested_mode = _normalize_reasoning_stream_mode(
            data.get("reasoning_stream_mode")
            or data.get("responses_reasoning_stream_mode")
            or data.get("reasoning_mode")
        )
        return requested_mode

    def _resolve_conversation_protocol(
        self,
        data: dict[str, Any],
        *,
        request_format: str,
        conversation: Any | None,
    ) -> str:
        if conversation is None:
            return request_format
        metadata = dict(conversation.metadata or {})
        locked_format = _normalize_request_format(metadata.get("request_format"))
        if locked_format and locked_format != request_format:
            raise ValueError("conversation protocol is already locked")
        if request_format == "responses":
            return "responses"
        if locked_format:
            return locked_format
        return request_format

    def prepare_pending_turn(self, data: dict[str, Any], request_format: str):
        if not isinstance(data, dict):
            return build_openai_error("request body must be a JSON object")

        owner = self.auth.owner_id()
        normalized_data = self._deps.image_store.normalize_request_data(data, owner_id=owner)
        context_text = extract_context_text(normalized_data, request_format)
        if not context_text:
            return build_openai_error("input is required")

        model = str(normalized_data.get("model") or "mock-gpt-4.1-mini")
        rate_limit = self.user_store.get_effective_messages_per_minute_limit(owner, 0)
        if not self.message_rate_limiter.allow(owner, rate_limit):
            return build_openai_error(
                f"rate limit exceeded: max {rate_limit} messages per minute",
                code="rate_limit_exceeded",
                status=429,
            )

        resolved_conversation, conversation_error = resolve_conversation_for_request(
            self.store,
            normalized_data,
            owner,
            request_format,
        )
        if conversation_error is not None:
            message, status = conversation_error
            error_code = "conflict" if status == 409 else "not_found"
            return build_openai_error(message, code=error_code, status=status)
        if (
            resolved_conversation is not None
            and resolved_conversation.source in {"history", "tool_call_id"}
            and _conversation_request_format(resolved_conversation.conversation)
            and _conversation_request_format(resolved_conversation.conversation) != request_format
        ):
            resolved_conversation = None
        conversation = (
            resolved_conversation.conversation
            if resolved_conversation is not None
            else None
        )
        if conversation is None:
            conversation = self.store.create_conversation(owner, title=build_title(context_text))

        existing_pending = self.pending_turns.get_by_conversation(conversation.id)
        if existing_pending is not None:
            if resolved_conversation is not None and resolved_conversation.source == "history":
                conversation = self.store.create_conversation(owner, title=build_title(context_text))
            else:
                return build_openai_error(
                    "conversation is waiting for a reply",
                    code="conflict",
                    status=409,
                )

        try:
            request_format = self._resolve_conversation_protocol(
                normalized_data,
                request_format=request_format,
                conversation=conversation,
            )
            reasoning_stream_mode = self._resolve_reasoning_stream_mode(
                normalized_data,
                request_format=request_format,
                conversation=conversation,
            )
        except ValueError as error:
            return build_openai_error(str(error), code="conflict", status=409)

        conversation_metadata = {
            **conversation.metadata,
            "request_format": request_format,
        }
        conversation = self.store.update_conversation(
            conversation.id,
            owner,
            metadata=conversation_metadata,
        )

        extracted_messages = extract_request_messages(
            self.store,
            normalized_data,
            conversation_id=conversation.id,
            owner=owner,
            request_format=request_format,
        )
        comparable_messages = extract_chatbox_comparable_request_messages(
            self.store,
            normalized_data,
            owner=owner,
            request_format=request_format,
        )
        history_prefix_length = self.store.get_request_history_prefix_length(
            conversation.id,
            owner,
            comparable_messages,
            normalize_stored_content=normalize_chatbox_history_content,
        )
        if history_prefix_length > 0 and len(extracted_messages) >= history_prefix_length:
            extracted_messages = extracted_messages[history_prefix_length:]
        has_tool_results = any(
            str(message_payload.get("metadata", {}).get("response_mode", "")).strip()
            == "tool_result"
            for message_payload in extracted_messages
            if isinstance(message_payload, dict)
        )
        if has_tool_results:
            extracted_messages = [
                message_payload
                for message_payload in extracted_messages
                if str(message_payload.get("role") or "").strip() != "user"
            ]
        updated_conversation = self.store.update_conversation(
            conversation.id,
            owner,
            title=conversation.title
            if conversation.title not in {"新会话", "New conversation", ""}
            else build_title(context_text),
            last_user_text=context_text[:1000],
        )
        pending_limits = self.enforce_pending_limits(owner)
        pending = self.pending_turns.register(
            conversation_id=conversation.id,
            owner_id=owner,
            model=model,
            input_text=context_text,
            request_format=request_format,
            reasoning_stream_mode=reasoning_stream_mode,
            max_age_seconds=float(pending_limits.get("max_age_seconds") or 0),
            auto_abort_message=str(pending_limits.get("abort_message") or ""),
            max_output_chars=int(
                300
                if pending_limits.get("max_output_chars") is None
                else pending_limits.get("max_output_chars")
            ),
            output_limit_abort_message=str(pending_limits.get("output_limit_abort_message") or ""),
            available_tool_names=extract_tool_names(normalized_data),
            available_tool_schemas=extract_tool_schemas(normalized_data),
        )
        try:
            request_debug_metadata = build_message_debug_metadata(
                auth=self.auth,
                request_format=request_format,
                request_data=normalized_data,
                input_text=context_text,
                input_payload=request_input_payload(normalized_data, request_format),
                request_id=pending.request_id,
                resolved_model=model,
            )
            if extracted_messages:
                for index, message_payload in enumerate(extracted_messages):
                    metadata = dict(message_payload.get("metadata") or {})
                    if index == len(extracted_messages) - 1:
                        metadata = {**metadata, **request_debug_metadata}
                    self.store.add_message(
                        conversation.id,
                        str(message_payload.get("role") or "user"),
                        str(message_payload.get("content") or ""),
                        metadata=metadata,
                    )
            else:
                self.store.add_message(
                    conversation.id,
                    "user",
                    context_text,
                    metadata={
                        "turn": "user",
                        "status": "pending",
                        "source": request_format,
                        **request_debug_metadata,
                    },
                )
            notify_new_message(
                self._deps.system_config_store,
                self.user_store,
                owner,
                conversation_title=updated_conversation.title or build_title(context_text),
                message_text=context_text,
                logger=self._logger,
            )
            self.store.update_conversation(
                conversation.id,
                owner,
                metadata={
                    **updated_conversation.metadata,
                    "realtime_status": "waiting",
                    "realtime_draft_text": "",
                    "request_format": request_format,
                },
            )
            self._notify(owner, conversation.id)
            self._automation_rules.start_for_pending(pending)
        except Exception:
            self.pending_turns.discard(
                conversation_id=conversation.id,
                owner_id=owner,
            )
            raise
        return PreparedTurn(pending=pending, conversation_id=updated_conversation.id)

    def finalize_pending_turn(self, pending: PendingTurn) -> dict[str, Any]:
        updated_conversation = self.store.get_conversation(pending.conversation_id, pending.owner_id)
        if updated_conversation is None:
            raise ValueError("conversation not found")
        usage = estimate_usage(pending.input_text, pending.assistant_text)
        try:
            messages = self.store.get_messages(pending.conversation_id, pending.owner_id)
        except ValueError:
            messages = []
        message_metadata = find_response_message_metadata(messages, pending.response_id)
        tool_name = str(message_metadata.get("tool_name", "")).strip()
        tool_call_id = str(message_metadata.get("tool_call_id", "")).strip()
        arguments = str(message_metadata.get("arguments", "")).strip()

        if pending.request_format == "chat_completions":
            return build_chat_completion_response(
                response_id=pending.response_id,
                model=pending.model,
                assistant_text=pending.assistant_text,
                usage=usage,
                response_mode=pending.response_mode,
                tool_name=tool_name,
                tool_call_id=tool_call_id,
                arguments=arguments,
            )
        if pending.request_format == "anthropic_messages":
            return build_anthropic_message_response(
                response_id=pending.response_id,
                model=pending.model,
                assistant_text=pending.assistant_text,
                usage=usage,
                response_mode=pending.response_mode,
                tool_name=tool_name,
                tool_call_id=tool_call_id,
                arguments=arguments,
            )
        payload = build_openai_response(
            response_id=pending.response_id,
            model=pending.model,
            conversation_id=updated_conversation.id,
            assistant_text=pending.assistant_text,
            usage=usage,
            output_items=pending.response_output_items or None,
            output_text=pending.response_output_text,
        )
        payload["conversation"] = updated_conversation.to_dict()
        payload["input_text"] = pending.input_text
        return payload

    def complete_manual_output(self, data: dict[str, Any]):
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        mode = str(data.get("mode", "assistant_message")).strip() or "assistant_message"
        if mode not in {"assistant_message", "thinking", "tool_call"}:
            return {"error": "unsupported mode"}, 400
        text = str(data.get("text", "")).strip()
        tool_name = str(data.get("tool_name", "")).strip()
        tool_call_id = str(data.get("tool_call_id", "")).strip()
        reasoning_stream_mode = str(data.get("reasoning_stream_mode", "")).strip()
        if mode == "tool_call":
            if not tool_name:
                return {"error": "tool_name is required"}, 400
            if not text:
                return {"error": "tool arguments are required"}, 400
            if not tool_call_id:
                tool_call_id = f"call_{uuid.uuid4().hex[:24]}"
        conversation_id = str(data.get("conversation_id", "")).strip()
        owner = self.auth.owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        pending = self.pending_turns.get_by_conversation(conversation_id)
        if pending is None:
            return {"error": "conversation is not waiting for a reply"}, 409
        if pending.owner_id != owner:
            return {"error": "conversation not found"}, 404

        try:
            if mode == "tool_call":
                pending, assistant_metadata = self._output_controller.complete_tool_call(
                    conversation_id=conversation_id,
                    owner_id=owner,
                    tool_name=tool_name,
                    arguments=text,
                    provider="human",
                    model=str(data.get("model") or pending.model or "mock-gpt-4.1-mini"),
                    tool_call_id=tool_call_id,
                    reasoning_stream_mode=reasoning_stream_mode,
                )
                assistant_text = pending.assistant_text
            else:
                pending, assistant_metadata = self._output_controller.complete_assistant_message(
                    conversation_id=conversation_id,
                    owner_id=owner,
                    provider="human",
                    model=str(data.get("model") or pending.model or "mock-gpt-4.1-mini"),
                    reasoning_stream_mode=reasoning_stream_mode,
                )
                assistant_text = pending.assistant_text
        except ValueError as error:
            return {"error": str(error)}, 409

        conversation = self.store.get_conversation(conversation_id, owner)
        return {
            "ok": True,
            "conversation": conversation.to_dict() if conversation else None,
            "message": {
                "role": "assistant" if mode == "assistant_message" else "tool_call",
                "content": assistant_text,
                "response_id": pending.response_id,
                "metadata": assistant_metadata,
            },
        }

    def add_manual_output_delta(self, data: dict[str, Any]):
        if not isinstance(data, dict):
            return {"error": "request body must be a JSON object"}, 400
        text = normalize_message_text(str(data.get("text", "")).strip())
        if not text:
            return {"error": "text is required"}, 400
        conversation_id = str(data.get("conversation_id", "")).strip()
        reasoning_stream_mode = str(data.get("reasoning_stream_mode", "")).strip()
        chunk_kind = "thinking" if str(data.get("kind", "")).strip() == "thinking" else "answer"
        owner = self.auth.owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        try:
            pending = self._output_controller.add_text_delta(
                conversation_id=conversation_id,
                owner_id=owner,
                text=text,
                reasoning_stream_mode=reasoning_stream_mode,
                kind=chunk_kind,
            )
        except ValueError as error:
            return {"error": str(error)}, 409

        return {
            "ok": True,
            "conversation_id": pending.conversation_id,
            "request_id": pending.request_id,
            "draft_text": pending.draft_text,
            "draft_length": len(pending.draft_text),
        }

    def _notify(self, owner_id: str, conversation_id: str) -> None:
        if self._publish_sync is None:
            return
        self._publish_sync(owner_id, conversation_id)
