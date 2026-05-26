from __future__ import annotations

import json
import regex
import threading
import time
import uuid
from dataclasses import dataclass
from typing import Any

from .output_controller import TurnOutputController
from .pending import PendingTurn

LEGACY_HEARTBEAT_RULE_ID = "legacy-heartbeat"
REGEX_MATCH_TIMEOUT_SECONDS = 0.1
REGEX_MAX_PATTERN_LENGTH = 512



def _as_string_list(value: Any) -> list[str]:
    if isinstance(value, str):
        text = value.strip()
        return [text] if text else []
    if not isinstance(value, list):
        return []
    result: list[str] = []
    for item in value:
        if not isinstance(item, str):
            continue
        text = item.strip()
        if text:
            result.append(text)
    return result


def _as_non_negative_float(value: Any) -> float:
    try:
        return float(value or 0.0)
    except (TypeError, ValueError):
        raise ValueError("timing values must be numbers")


def _normalize_match_entries(value: Any) -> list[dict[str, str]]:
    if not isinstance(value, list):
        return []
    result: list[dict[str, str]] = []
    for item in value:
        if isinstance(item, str):
            text = item.strip()
            if text:
                result.append({"match_type": "substring", "pattern": text})
            continue
        if not isinstance(item, dict):
            continue
        pattern = str(item.get("pattern") or "").strip()
        if not pattern:
            continue
        match_type = str(item.get("match_type") or "substring").strip() or "substring"
        result.append(
            {
                "match_type": match_type,
                "pattern": pattern,
            }
        )
    return result


@dataclass(frozen=True)
class AutomationRule:
    id: str
    enabled: bool
    contains: list[dict[str, str]]
    excludes: list[dict[str, str]]
    delay_seconds: float
    repeat_interval_seconds: float
    max_output_count: int
    action_type: str
    action_text: str
    error_message: str
    tool_name: str = ""
    tool_arguments: str = ""
    tool_call_id: str = ""

    def matches(self, input_text: str) -> bool:
        if self.contains and not all(_match_pattern(item, input_text) for item in self.contains):
            return False
        if any(_match_pattern(item, input_text) for item in self.excludes):
            return False
        return True


def _match_pattern(item: dict[str, str], input_text: str) -> bool:
    match_type = str(item.get("match_type") or "substring").strip() or "substring"
    pattern = str(item.get("pattern") or "")
    if not pattern:
        return False
    if match_type == "regex":
        if len(pattern) > REGEX_MAX_PATTERN_LENGTH:
            return False
        try:
            return regex.search(pattern, input_text, timeout=REGEX_MATCH_TIMEOUT_SECONDS) is not None
        except (regex.error, TimeoutError):
            return False
    return pattern in input_text


def _validate_tool_arguments(arguments_json: str, schema: dict[str, Any]) -> bool:
    try:
        args = json.loads(arguments_json) if arguments_json.strip() else {}
    except (json.JSONDecodeError, ValueError):
        return False
    if not isinstance(args, dict):
        return False
    properties = schema.get("properties")
    if isinstance(properties, dict) and properties:
        for key in args:
            if key not in properties:
                return False
    required = schema.get("required")
    if isinstance(required, list):
        for field in required:
            if field not in args:
                return False
    return True


def normalize_rule_payload(raw_rule: dict[str, Any]) -> dict[str, Any]:
    conditions = raw_rule.get("conditions")
    timing = raw_rule.get("timing")
    action = raw_rule.get("action")
    if not isinstance(conditions, dict):
        conditions = {}
    if not isinstance(timing, dict):
        timing = {}
    if not isinstance(action, dict):
        action = {}
    delay_seconds = _as_non_negative_float(timing.get("delay_seconds"))
    repeat_interval_seconds = _as_non_negative_float(timing.get("repeat_interval_seconds"))
    try:
        max_output_count = int(timing.get("max_output_count") or 120)
    except (TypeError, ValueError):
        max_output_count = 120
    max_output_count = max(1, max_output_count)
    return {
        "id": str(raw_rule.get("id") or f"rule_{uuid.uuid4().hex[:8]}"),
        "enabled": bool(raw_rule.get("enabled", True)),
        "conditions": {
            "contains": _normalize_match_entries(conditions.get("contains")),
            "excludes": _normalize_match_entries(conditions.get("excludes")),
        },
        "timing": {
            "delay_seconds": delay_seconds,
            "repeat_interval_seconds": repeat_interval_seconds,
            "max_output_count": max_output_count,
        },
        "action": {
            "type": str(action.get("type") or "").strip(),
            "text": str(action.get("text") or ""),
            "error_message": str(action.get("error_message") or ""),
            "tool_name": str(action.get("tool_name") or ""),
            "tool_arguments": str(action.get("tool_arguments") or ""),
            "tool_call_id": str(action.get("tool_call_id") or ""),
        },
    }


def validate_rule_payload(raw_rule: dict[str, Any]) -> tuple[dict[str, Any] | None, str | None]:
    if not isinstance(raw_rule, dict):
        return None, "rule must be an object"
    try:
        normalized = normalize_rule_payload(raw_rule)
    except ValueError as error:
        return None, str(error)
    if normalized["timing"]["delay_seconds"] < 0:
        return None, "delay_seconds must be greater than or equal to 0"
    if normalized["timing"]["repeat_interval_seconds"] < 0:
        return None, "repeat_interval_seconds must be greater than or equal to 0"
    if normalized["timing"].get("max_output_count", 120) < 1:
        return None, "max_output_count must be greater than 0"
    for group_name in ("contains", "excludes"):
        for item in normalized["conditions"][group_name]:
            match_type = str(item.get("match_type") or "")
            pattern = str(item.get("pattern") or "")
            if match_type not in {"substring", "regex"}:
                return None, "condition match_type must be substring or regex"
            if not pattern:
                return None, "condition pattern is required"
            if match_type == "regex":
                if len(pattern) > REGEX_MAX_PATTERN_LENGTH:
                    return None, f"condition regex pattern is too long, max {REGEX_MAX_PATTERN_LENGTH} characters"
                try:
                    regex.compile(pattern)
                except regex.error as error:
                    return None, f"invalid regex: {error}"
    action_type = normalized["action"]["type"]
    if action_type not in {"output_text", "complete", "error", "tool_call"}:
        return None, "action.type must be one of output_text, complete, error, tool_call"
    if action_type == "output_text" and not normalized["action"]["text"]:
        return None, "output_text rule requires action.text"
    if action_type == "error" and not normalized["action"]["error_message"]:
        return None, "error rule requires action.error_message"
    if action_type == "tool_call" and not normalized["action"].get("tool_name"):
        return None, "tool_call rule requires action.tool_name"
    return normalized, None


def materialize_rule(payload: dict[str, Any]) -> AutomationRule:
    action = payload.get("action", {})
    return AutomationRule(
        id=str(payload["id"]),
        enabled=bool(payload["enabled"]),
        contains=list(payload["conditions"]["contains"]),
        excludes=list(payload["conditions"]["excludes"]),
        delay_seconds=float(payload["timing"]["delay_seconds"]),
        repeat_interval_seconds=float(payload["timing"]["repeat_interval_seconds"]),
        max_output_count=max(1, int(payload["timing"].get("max_output_count", 120) or 120)),
        action_type=str(action.get("type") or ""),
        action_text=str(action.get("text") or ""),
        error_message=str(action.get("error_message") or ""),
        tool_name=str(action.get("tool_name") or ""),
        tool_arguments=str(action.get("tool_arguments") or ""),
        tool_call_id=str(action.get("tool_call_id") or ""),
    )


class AutomationRuleEngine:
    def __init__(self, *, user_store: Any, output_controller: TurnOutputController):
        self._user_store = user_store
        self._output_controller = output_controller

    def load_rule_payloads(self, owner_id: str) -> list[dict[str, Any]]:
        raw = self._user_store.get_automation_rules(owner_id)
        try:
            data = json.loads(raw)
        except json.JSONDecodeError:
            data = []
        if not isinstance(data, list):
            return []
        result: list[dict[str, Any]] = []
        for item in data:
            normalized, error = validate_rule_payload(item)
            if normalized is None or error is not None:
                continue
            result.append(normalized)
        return result

    def save_rule_payloads(self, owner_id: str, rules: list[dict[str, Any]]) -> list[dict[str, Any]]:
        validated: list[dict[str, Any]] = []
        for item in rules:
            normalized, error = validate_rule_payload(item)
            if normalized is None:
                raise ValueError(error or "invalid rule")
            validated.append(normalized)
        self._user_store.set_automation_rules(owner_id, json.dumps(validated, ensure_ascii=False))
        return validated

    def get_heartbeat_rule_settings(self, owner_id: str) -> dict[str, Any]:
        for rule in self.load_rule_payloads(owner_id):
            if str(rule.get("id")) != LEGACY_HEARTBEAT_RULE_ID:
                continue
            text = str(rule["action"]["text"])
            interval = float(rule["timing"]["repeat_interval_seconds"] or 0.0)
            return {
                "heartbeat_text": text,
                "heartbeat_interval_seconds": interval,
            }
        return {
            "heartbeat_text": "",
            "heartbeat_interval_seconds": 0.0,
        }

    def update_heartbeat_rule_settings(self, owner_id: str, *, heartbeat_text: str, interval_seconds: float) -> dict[str, Any]:
        rules = [rule for rule in self.load_rule_payloads(owner_id) if str(rule.get("id")) != LEGACY_HEARTBEAT_RULE_ID]
        if heartbeat_text and interval_seconds > 0:
            rules.append(
                {
                    "id": LEGACY_HEARTBEAT_RULE_ID,
                    "enabled": True,
                    "conditions": {"contains": [], "excludes": []},
                    "timing": {
                        "delay_seconds": float(interval_seconds),
                        "repeat_interval_seconds": float(interval_seconds),
                        "max_output_count": 120,
                    },
                    "action": {
                        "type": "output_text",
                        "text": heartbeat_text,
                        "error_message": "",
                    },
                }
            )
        self.save_rule_payloads(owner_id, rules)
        return {
            "heartbeat_text": heartbeat_text,
            "heartbeat_interval_seconds": float(interval_seconds),
        }

    def start_for_pending(self, pending: PendingTurn) -> None:
        input_text = pending.input_text
        owner_id = pending.owner_id
        rules = [
            materialize_rule(payload)
            for payload in self.load_rule_payloads(owner_id)
            if bool(payload.get("enabled", True))
        ]
        for rule in rules:
            if not rule.matches(input_text):
                continue
            worker = threading.Thread(
                target=self._run_rule,
                args=(pending, rule),
                daemon=True,
                name=f"automation-rule-{rule.id}",
            )
            worker.start()

    def _run_rule(self, pending: PendingTurn, rule: AutomationRule) -> None:
        owner_id = pending.owner_id
        conversation_id = pending.conversation_id
        if self._sleep_until_ready(pending, rule.delay_seconds):
            return
        output_count = 0
        while True:
            if pending.event.is_set():
                return
            try:
                if rule.action_type == "output_text":
                    self._output_controller.add_text_delta(
                        conversation_id=conversation_id,
                        owner_id=owner_id,
                        text=rule.action_text,
                    )
                    output_count += 1
                    if output_count >= rule.max_output_count:
                        return
                elif rule.action_type == "complete":
                    self._output_controller.complete_assistant_message(
                        conversation_id=conversation_id,
                        owner_id=owner_id,
                        provider="rule",
                    )
                    return
                elif rule.action_type == "error":
                    self._output_controller.abort(
                        conversation_id=conversation_id,
                        owner_id=owner_id,
                        error_message=rule.error_message,
                    )
                    return
                elif rule.action_type == "tool_call":
                    tool_name = str(rule.tool_name or "").strip()
                    tool_arguments = str(rule.tool_arguments or "")
                    tool_call_id = str(rule.tool_call_id or "") or None
                    if not tool_name:
                        return
                    if pending.available_tool_names and tool_name not in pending.available_tool_names:
                        return
                    tool_schema = pending.available_tool_schemas.get(tool_name)
                    if tool_schema is not None:
                        if not _validate_tool_arguments(tool_arguments, tool_schema):
                            return
                    self._output_controller.complete_tool_call(
                        conversation_id=conversation_id,
                        owner_id=owner_id,
                        tool_name=tool_name,
                        arguments=tool_arguments,
                        provider="rule",
                        tool_call_id=tool_call_id,
                    )
                    return
            except ValueError:
                return

            if rule.repeat_interval_seconds <= 0:
                return
            if self._sleep_until_ready(pending, rule.repeat_interval_seconds):
                return

    @staticmethod
    def _sleep_until_ready(pending: PendingTurn, duration_seconds: float) -> bool:
        remaining = max(0.0, float(duration_seconds))
        while remaining > 0:
            if pending.event.wait(min(0.25, remaining)):
                return True
            remaining -= min(0.25, remaining)
        return pending.event.is_set()
