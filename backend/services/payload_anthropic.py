from __future__ import annotations

import json
from typing import Any

from .thinking import has_thinking, split_thinking_parts


def parse_json_object(text: str) -> Any:
    try:
        return json.loads(text)
    except (TypeError, json.JSONDecodeError):
        return text


def _build_anthropic_text_content(assistant_text: str) -> list[dict[str, Any]]:
    if not has_thinking(assistant_text):
        return [{"type": "text", "text": assistant_text}]
    parts = split_thinking_parts(assistant_text)
    if not parts:
        return [{"type": "text", "text": ""}]

    content: list[dict[str, Any]] = []
    for part in parts:
        if part["type"] == "thinking":
            content.append(
                {
                    "type": "thinking",
                    "thinking": part["text"],
                    "signature": "mock-thinking",
                }
            )
        else:
            content.append({"type": "text", "text": part["text"]})
    return content or [{"type": "text", "text": assistant_text}]


def build_anthropic_message_response(
    *,
    response_id: str,
    model: str,
    assistant_text: str,
    usage: dict[str, int] | None,
    response_mode: str = "assistant_message",
    tool_name: str = "",
    tool_call_id: str = "",
    arguments: str = "",
) -> dict[str, Any]:
    stop_reason = "end_turn"
    content: list[dict[str, Any]]
    if response_mode == "tool_call":
        stop_reason = "tool_use"
        content = [
            {
                "type": "tool_use",
                "id": tool_call_id,
                "name": tool_name,
                "input": parse_json_object(arguments),
            }
        ]
    else:
        content = _build_anthropic_text_content(assistant_text)
    return {
        "id": response_id,
        "type": "message",
        "role": "assistant",
        "model": model,
        "content": content,
        "stop_reason": stop_reason,
        "stop_sequence": None,
        "usage": {
            "input_tokens": usage["input_tokens"] if usage else 0,
            "output_tokens": usage["output_tokens"] if usage else 0,
            "cache_creation_input_tokens": 0,
            "cache_read_input_tokens": 0,
        },
    }
