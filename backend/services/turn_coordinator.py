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
from .turn_protocols import (
    build_message_debug_metadata,
    extract_context_text,
    extract_request_messages,
    normalize_message_text,
    request_input_payload,
    resolve_conversation_for_request,
)


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
            store=deps.store,
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
    def settings(self):
        return self._deps.settings

    @property
    def message_rate_limiter(self):
        return self._deps.message_rate_limiter

    def get_stream_heartbeat_settings(self) -> dict[str, Any]:
        return self._automation_rules.get_heartbeat_rule_settings()

    def update_stream_heartbeat_settings(
        self,
        *,
        heartbeat_text: str,
        heartbeat_interval_seconds: float,
    ) -> dict[str, Any]:
        return self._automation_rules.update_heartbeat_rule_settings(
            heartbeat_text=heartbeat_text,
            interval_seconds=heartbeat_interval_seconds,
        )

    def get_automation_rules(self) -> list[dict[str, Any]]:
        return self._automation_rules.load_rule_payloads()

    def update_automation_rules(self, rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
        return self._automation_rules.save_rule_payloads(rules)

    def build_abort_error(self, message_text: str) -> tuple[dict[str, Any], int]:
        return build_openai_error(
            message_text or "request aborted",
            code="request_aborted",
            status=400,
        )

    def build_not_found_error(self, message: str, *, code: str = "not_found", status: int = 404):
        return build_openai_error(message, code=code, status=status)

    def prepare_pending_turn(self, data: dict[str, Any], request_format: str):
        if not isinstance(data, dict):
            return build_openai_error("request body must be a JSON object")

        context_text = extract_context_text(data, request_format)
        if not context_text:
            return build_openai_error("input is required")

        model = str(data.get("model") or "mock-gpt-4.1-mini")
        owner = self.auth.owner_id()
        if not self.message_rate_limiter.allow(owner):
            return build_openai_error(
                f"rate limit exceeded: max {self.settings.messages_per_minute_limit} messages per minute",
                code="rate_limit_exceeded",
                status=429,
            )

        conversation, conversation_error = resolve_conversation_for_request(
            self.store,
            data,
            owner,
            request_format,
        )
        if conversation_error is not None:
            message, status = conversation_error
            return build_openai_error(message, code="not_found", status=status)
        if conversation is None:
            conversation = self.store.create_conversation(owner, title=build_title(context_text))

        existing_pending = self.pending_turns.get_by_conversation(conversation.id)
        if existing_pending is not None:
            return build_openai_error(
                "conversation is waiting for a reply",
                code="conflict",
                status=409,
            )

        extracted_messages = extract_request_messages(
            self.store,
            data,
            conversation_id=conversation.id,
            owner=owner,
            request_format=request_format,
        )
        updated_conversation = self.store.update_conversation(
            conversation.id,
            owner,
            title=conversation.title
            if conversation.title not in {"新会话", "New conversation", ""}
            else build_title(context_text),
            last_user_text=context_text[:1000],
        )
        pending = self.pending_turns.register(
            conversation_id=conversation.id,
            owner_id=owner,
            model=model,
            input_text=context_text,
            request_format=request_format,
        )
        try:
            request_debug_metadata = build_message_debug_metadata(
                auth=self.auth,
                request_format=request_format,
                request_data=data,
                input_text=context_text,
                input_payload=request_input_payload(data, request_format),
                request_id=pending.request_id,
                resolved_model=model,
            )
            if extracted_messages:
                for index, message_payload in enumerate(extracted_messages):
                    metadata = dict(message_payload.get("metadata") or {})
                    if message_payload.get("role") == "user" and index == len(extracted_messages) - 1:
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
                self.settings,
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
        message_metadata: dict[str, Any] = {}
        try:
            messages = self.store.get_messages(pending.conversation_id, pending.owner_id)
        except ValueError:
            messages = []
        for message in reversed(messages):
            if message.response_id == pending.response_id and message.role == "assistant":
                message_metadata = message.metadata
                break
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
        if mode not in {"assistant_message", "tool_call"}:
            return {"error": "unsupported mode"}, 400
        text = str(data.get("text", "")).strip()
        tool_name = str(data.get("tool_name", "")).strip()
        tool_call_id = str(data.get("tool_call_id", "")).strip()
        if mode == "assistant_message" and not text:
            return {"error": "text is required"}, 400
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
                )
                assistant_text = pending.assistant_text
            else:
                pending, assistant_metadata = self._output_controller.complete_assistant_message(
                    conversation_id=conversation_id,
                    owner_id=owner,
                    text=text,
                    provider="human",
                    model=str(data.get("model") or pending.model or "mock-gpt-4.1-mini"),
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
        owner = self.auth.owner_id()
        if not conversation_id:
            return {"error": "conversation_id is required"}, 400

        try:
            pending = self._output_controller.add_text_delta(
                conversation_id=conversation_id,
                owner_id=owner,
                text=text,
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
