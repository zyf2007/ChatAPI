from __future__ import annotations

import time
import uuid
from typing import Any

from .thinking import answer_text, has_thinking, split_thinking_parts


def _estimate_tokens(text: str) -> int:
    text = text.strip()
    if not text:
        return 0
    ascii_tokens = len(text.split())
    cjk_chars = sum(1 for ch in text if "\u4e00" <= ch <= "\u9fff")
    return max(ascii_tokens, 1) + cjk_chars // 2


def estimate_usage(input_text: str, output_text: str) -> dict[str, int]:
    input_tokens = _estimate_tokens(input_text)
    output_tokens = _estimate_tokens(output_text)
    return {
        "input_tokens": input_tokens,
        "output_tokens": output_tokens,
        "total_tokens": input_tokens + output_tokens,
    }


def build_openai_response(
    *,
    response_id: str,
    model: str,
    conversation_id: str,
    assistant_text: str,
    usage: dict[str, int] | None,
    status: str = "completed",
    output_items: list[dict[str, Any]] | None = None,
    output_text: str | None = None,
) -> dict[str, Any]:
    created_at = int(time.time())
    normalized_output_items = output_items
    if normalized_output_items is None:
        normalized_output_items = _build_default_output_items(assistant_text)
        output_text_source = assistant_text if output_text is None else output_text
        normalized_output_text = (
            answer_text(output_text_source)
            if has_thinking(output_text_source)
            else output_text_source
        )
    else:
        normalized_output_text = assistant_text if output_text is None else output_text
    return {
        "id": response_id,
        "object": "response",
        "created_at": created_at,
        "status": status,
        "model": model,
        "conversation_id": conversation_id,
        "output": normalized_output_items,
        "output_text": normalized_output_text,
        "usage": usage,
    }


def _build_default_output_items(assistant_text: str) -> list[dict[str, Any]]:
    parts = split_thinking_parts(assistant_text)
    if not any(part["type"] == "thinking" for part in parts):
        return [
            {
                "id": f"msg_{uuid.uuid4().hex[:24]}",
                "type": "message",
                "role": "assistant",
                "content": [
                    {
                        "type": "output_text",
                        "text": assistant_text,
                    }
                ],
            }
        ]
    if not parts:
        parts = [{"type": "answer", "text": ""}]

    output_items: list[dict[str, Any]] = []
    answer_parts: list[str] = []
    for part in parts:
        text = part["text"]
        if part["type"] == "thinking":
            output_items.append(
                {
                    "id": f"rs_{uuid.uuid4().hex[:24]}",
                    "type": "reasoning",
                    "status": "completed",
                    "content": [
                        {
                            "type": "reasoning_text",
                            "text": text,
                        }
                    ],
                    "summary": [
                        {
                            "type": "summary_text",
                            "text": text,
                        }
                    ],
                }
            )
        else:
            answer_parts.append(text)

    normalized_answer_text = "\n\n".join(part for part in answer_parts if part).strip()
    if normalized_answer_text or not output_items:
        output_items.append(
            {
                "id": f"msg_{uuid.uuid4().hex[:24]}",
                "type": "message",
                "status": "completed",
                "role": "assistant",
                "content": [
                    {
                        "type": "output_text",
                        "annotations": [],
                        "text": normalized_answer_text,
                    }
                ],
            }
        )
    return output_items


def build_openai_error(
    message: str,
    code: str = "bad_request",
    status: int = 400,
) -> tuple[dict[str, Any], int]:
    return (
        {
            "error": {
                "message": message,
                "type": code,
                "code": code,
            }
        },
        status,
    )
