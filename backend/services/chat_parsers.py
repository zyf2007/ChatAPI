from __future__ import annotations

from typing import Any


def extract_chat_context_text(messages: list[dict[str, Any]]) -> str:
    for msg in reversed(messages):
        role = str(msg.get("role", "")).strip()
        content = msg.get("content")
        if role == "tool":
            if isinstance(content, str) and content.strip():
                return content.strip()
            continue
        if role != "user":
            continue
        if isinstance(content, str):
            return content.strip()
        if isinstance(content, list):
            parts = [
                str(block.get("text", "")).strip()
                for block in content
                if isinstance(block, dict) and str(block.get("type", "")).strip() == "text"
                and str(block.get("text", "")).strip()
            ]
            result = "\n".join(parts).strip()
            if result:
                return result
    return ""


def extract_chat_tool_result_call_ids(messages: list[dict[str, Any]]) -> list[str]:
    return [
        str(msg.get("tool_call_id", "")).strip()
        for msg in messages
        if str(msg.get("role", "")).strip() == "tool"
        and str(msg.get("tool_call_id", "")).strip()
    ]


def extract_anthropic_context_text(data: dict[str, Any]) -> str:
    messages = data.get("messages", [])
    if not isinstance(messages, list):
        return ""
    for msg in reversed(messages):
        if str(msg.get("role", "")).strip() != "user":
            continue
        content = msg.get("content")
        if isinstance(content, str):
            return content.strip()
        if isinstance(content, list):
            parts = [
                str(block.get("text", "")).strip()
                for block in content
                if isinstance(block, dict) and str(block.get("type", "")).strip() == "text"
                and str(block.get("text", "")).strip()
            ]
            if parts:
                return "\n".join(parts).strip()
            tool_parts: list[str] = []
            for block in content:
                if not isinstance(block, dict) or str(block.get("type", "")).strip() != "tool_result":
                    continue
                tr_content = block.get("content")
                if isinstance(tr_content, str) and tr_content.strip():
                    tool_parts.append(tr_content.strip())
                elif isinstance(tr_content, list):
                    for tb in tr_content:
                        if isinstance(tb, dict) and tb.get("type") == "text" and str(tb.get("text", "")).strip():
                            tool_parts.append(str(tb["text"]).strip())
            if tool_parts:
                return "\n".join(tool_parts).strip()
    return ""


def extract_anthropic_tool_result_use_ids(data: dict[str, Any]) -> list[str]:
    messages = data.get("messages", [])
    if not isinstance(messages, list):
        return []
    use_ids: list[str] = []
    for msg in messages:
        if str(msg.get("role", "")).strip() != "user":
            continue
        content = msg.get("content")
        if not isinstance(content, list):
            continue
        for block in content:
            if not isinstance(block, dict):
                continue
            if str(block.get("type", "")).strip() == "tool_result":
                uid = str(block.get("tool_use_id", "")).strip()
                if uid:
                    use_ids.append(uid)
    return use_ids
