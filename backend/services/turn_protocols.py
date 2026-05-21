from __future__ import annotations

import json
import re
import uuid
from typing import Any

from ..core.auth import AuthContext
from ..repositories import ConversationStore


_IMAGE_URL_RE = re.compile(r"^(?:https?://[^\s\"']+)?/api/uploads/imgs/[A-Za-z0-9._-]+(?:\?.*)?$", re.IGNORECASE)


def normalize_message_text(value: str) -> str:
    return value.replace("\r\n", "\n").replace("\\r\\n", "\n").replace("\\n", "\n")


def build_protocol_response_id(request_format: str, fallback_request_id: str) -> str:
    if request_format == "chat_completions":
        return f"chatcmpl_{uuid.uuid4().hex}"
    if request_format == "anthropic_messages":
        return f"msg_{uuid.uuid4().hex[:24]}"
    return fallback_request_id


def response_input_payload(data: dict[str, Any]) -> Any:
    if "input" in data:
        return data["input"]
    if "messages" in data:
        return data["messages"]
    return data


def chat_input_payload(data: dict[str, Any]) -> Any:
    return data.get("messages", [])


def anthropic_input_payload(data: dict[str, Any]) -> Any:
    payload: list[Any] = []
    system_prompt = data.get("system")
    if isinstance(system_prompt, str) and system_prompt.strip():
        payload.append({"role": "system", "content": system_prompt})
    elif isinstance(system_prompt, list) and system_prompt:
        payload.append({"role": "system", "content": system_prompt})
    messages = data.get("messages", [])
    if isinstance(messages, list):
        payload.extend(messages)
    return payload


def request_input_payload(data: dict[str, Any], request_format: str) -> Any:
    if request_format == "chat_completions":
        return chat_input_payload(data)
    if request_format == "anthropic_messages":
        return anthropic_input_payload(data)
    return response_input_payload(data)


def _is_image_reference_string(value: str) -> bool:
    return value.startswith("data:image/") or bool(_IMAGE_URL_RE.match(value.strip()))


def serialize_content(value: Any) -> str:
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"))


def canonical_json(value: Any) -> str:
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":"))


def _assistant_request_content(item: dict[str, Any]) -> str:
    tool_calls = item.get("tool_calls")
    if isinstance(tool_calls, list) and tool_calls:
        first_call = tool_calls[0] if isinstance(tool_calls[0], dict) else {}
        function_payload = (
            first_call.get("function")
            if isinstance(first_call, dict) and isinstance(first_call.get("function"), dict)
            else {}
        )
        tool_name = str(function_payload.get("name", "")).strip()
        arguments = serialize_content(function_payload.get("arguments", ""))
        if tool_name:
            return f"{tool_name}({arguments})"
        return serialize_content(tool_calls)
    content = item.get("content")
    if content is None:
        return ""
    return serialize_content(content)


def extract_text_content(node: Any) -> str:
    parts: list[str] = []

    def visit(value: Any) -> None:
        if value is None:
            return
        if isinstance(value, str):
            if value.strip() and not _is_image_reference_string(value):
                parts.append(value.strip())
            return
        if isinstance(value, list):
            for item in value:
                visit(item)
            return
        if isinstance(value, dict):
            item_type = str(value.get("type", "")).strip()
            if item_type in {"input_text", "output_text", "text"} and isinstance(
                value.get("text"), str
            ):
                text = str(value.get("text", "")).strip()
                if text:
                    parts.append(text)
                return
            if item_type == "tool_result":
                visit(value.get("content"))
                return
            if isinstance(value.get("text"), str):
                text = str(value.get("text", "")).strip()
                if text:
                    parts.append(text)
                return
            if "content" in value:
                visit(value.get("content"))

    visit(node)
    return "\n".join(parts).strip()


def normalize_chatbox_history_content(content: str) -> str:
    try:
        parsed = json.loads(content)
    except json.JSONDecodeError:
        parsed = content
    text = extract_text_content(parsed)
    if text:
        return text
    if isinstance(parsed, str):
        return parsed
    return canonical_json(parsed)


def extract_context_text(data: dict[str, Any], request_format: str) -> str:
    input_payload = request_input_payload(data, request_format)
    if isinstance(input_payload, str):
        return "" if _is_image_reference_string(input_payload) else input_payload.strip()
    chunks: list[str] = []

    def visit(node: Any) -> None:
        if node is None:
            return
        if isinstance(node, str):
            if node.strip() and not _is_image_reference_string(node):
                chunks.append(node.strip())
            return
        if isinstance(node, list):
            for item in node:
                visit(item)
            return
        if isinstance(node, dict):
            if node.get("role") in {"user", "assistant", "system", "developer"}:
                visit(node.get("content"))
                return
            if node.get("type") == "tool_result":
                visit(node.get("content"))
                return
            if isinstance(node.get("text"), str):
                chunks.append(str(node["text"]).strip())
                return
            if node.get("type") == "tool_use" and isinstance(node.get("input"), dict):
                raw_input = canonical_json(node.get("input"))
                if raw_input:
                    chunks.append(raw_input)
                return
            if isinstance(node.get("content"), (str, list, dict)):
                visit(node.get("content"))
                return
            for value in node.values():
                visit(value)

    visit(input_payload)
    if not chunks and isinstance(data.get("messages"), list):
        visit(data.get("messages"))
    return "\n".join(chunk for chunk in chunks if chunk).strip()


def resolve_tool_name_for_call(
    store: ConversationStore,
    conversation_id: str | None,
    owner: str,
    call_id: str,
) -> str:
    if not conversation_id or not call_id:
        return ""
    try:
        messages = store.get_messages(conversation_id, owner)
    except ValueError:
        return ""
    for message in reversed(messages):
        if str(message.metadata.get("tool_call_id", "")).strip() == call_id:
            return str(message.metadata.get("tool_name", "")).strip()
    return ""


def extract_request_messages(
    store: ConversationStore,
    data: dict[str, Any],
    *,
    conversation_id: str | None,
    owner: str,
    request_format: str,
) -> list[dict[str, Any]]:
    payload = request_input_payload(data, request_format)
    items = payload if isinstance(payload, list) else [payload]
    extracted: list[dict[str, Any]] = []
    for item in items:
        if not isinstance(item, dict):
            continue
        role = str(item.get("role", "")).strip()
        item_type = str(item.get("type", "")).strip()
        if request_format == "anthropic_messages" and role == "user":
            content_blocks = item.get("content")
            if isinstance(content_blocks, list):
                content = serialize_content(content_blocks)
                if content:
                    extracted.append(
                        {
                            "role": "user",
                            "content": content,
                            "metadata": {
                                "turn": "user",
                                "status": "pending",
                                "source": request_format,
                            },
                        }
                    )
                for content_block in content_blocks:
                    if not isinstance(content_block, dict):
                        continue
                    if str(content_block.get("type", "")).strip() != "tool_result":
                        continue
                    output = serialize_content(content_block.get("content"))
                    call_id = str(content_block.get("tool_use_id", "")).strip()
                    if output:
                        extracted.append(
                            {
                                "role": "tool",
                                "content": output,
                                "metadata": {
                                    "source": request_format,
                                    "response_mode": "tool_result",
                                    "tool_call_id": call_id,
                                    "tool_name": resolve_tool_name_for_call(
                                        store,
                                        conversation_id,
                                        owner,
                                        call_id,
                                    ),
                                    "output": output,
                                },
                            }
                        )
            continue
        if role == "user":
            content = serialize_content(item.get("content"))
            if content:
                extracted.append(
                    {
                        "role": "user",
                        "content": content,
                        "metadata": {
                            "turn": "user",
                            "status": "pending",
                            "source": request_format,
                        },
                    }
                )
            continue
        if role == "assistant":
            content = _assistant_request_content(item)
            if content:
                metadata: dict[str, Any] = {
                    "source": request_format,
                    "turn": "assistant",
                    "history_imported": True,
                }
                tool_calls = item.get("tool_calls")
                if isinstance(tool_calls, list) and tool_calls:
                    metadata["response_mode"] = "tool_call"
                    first_call = tool_calls[0] if isinstance(tool_calls[0], dict) else {}
                    function_payload = (
                        first_call.get("function")
                        if isinstance(first_call, dict) and isinstance(first_call.get("function"), dict)
                        else {}
                    )
                    metadata["tool_call_id"] = str(first_call.get("id", "")).strip()
                    metadata["tool_name"] = str(function_payload.get("name", "")).strip()
                    metadata["arguments"] = serialize_content(function_payload.get("arguments", ""))
                extracted.append(
                    {
                        "role": "assistant",
                        "content": content,
                        "metadata": metadata,
                    }
                )
            continue
        if request_format == "chat_completions" and role == "tool":
            output = serialize_content(item.get("content"))
            call_id = str(item.get("tool_call_id", "")).strip()
            if output:
                extracted.append(
                    {
                        "role": "tool",
                        "content": output,
                        "metadata": {
                            "source": request_format,
                            "response_mode": "tool_result",
                            "tool_call_id": call_id,
                            "tool_name": resolve_tool_name_for_call(
                                store,
                                conversation_id,
                                owner,
                                call_id,
                            ),
                            "output": output,
                        },
                    }
                )
            continue
        if item_type == "function_call_output":
            output = serialize_content(item.get("output", ""))
            call_id = str(item.get("call_id", "")).strip()
            if output:
                extracted.append(
                    {
                        "role": "tool",
                        "content": output,
                        "metadata": {
                            "source": request_format,
                            "response_mode": "tool_result",
                            "tool_call_id": call_id,
                            "tool_name": resolve_tool_name_for_call(
                                store,
                                conversation_id,
                                owner,
                                call_id,
                            ),
                            "output": output,
                        },
                    }
                )
    return extracted


def chatbox_comparable_messages_from_internal_messages(
    messages: list[dict[str, Any]],
) -> list[dict[str, str]]:
    comparable: list[dict[str, str]] = []
    for message in messages:
        if not isinstance(message, dict):
            continue
        role = str(message.get("role", "")).strip()
        if role not in {"user", "assistant", "tool"}:
            continue
        content = normalize_chatbox_history_content(str(message.get("content") or ""))
        if content:
            comparable.append(
                {
                    "role": role,
                    "content": content,
                }
            )
    return comparable


def extract_chatbox_comparable_request_messages(
    store: ConversationStore,
    data: dict[str, Any],
    *,
    owner: str,
    request_format: str,
) -> list[dict[str, str]]:
    return chatbox_comparable_messages_from_internal_messages(
        extract_request_messages(
            store,
            data,
            conversation_id=None,
            owner=owner,
            request_format=request_format,
        )
    )


def resolve_conversation_by_history_strategies(
    store: ConversationStore,
    data: dict[str, Any],
    owner: str,
    request_format: str,
):
    chatbox_messages = extract_chatbox_comparable_request_messages(
        store,
        data,
        owner=owner,
        request_format=request_format,
    )
    return store.find_conversation_by_message_history(
        owner,
        chatbox_messages,
        normalize_stored_content=normalize_chatbox_history_content,
    )


def extract_tool_result_call_ids(data: dict[str, Any], request_format: str) -> list[str]:
    payload = request_input_payload(data, request_format)
    items = payload if isinstance(payload, list) else [payload]
    call_ids: list[str] = []
    for item in items:
        if not isinstance(item, dict):
            continue
        if request_format == "chat_completions":
            if str(item.get("role", "")).strip() != "tool":
                continue
            call_id = str(item.get("tool_call_id", "")).strip()
            if call_id:
                call_ids.append(call_id)
            continue
        if request_format == "anthropic_messages":
            content_blocks = item.get("content")
            if not isinstance(content_blocks, list):
                continue
            for content_block in content_blocks:
                if not isinstance(content_block, dict):
                    continue
                if str(content_block.get("type", "")).strip() != "tool_result":
                    continue
                call_id = str(content_block.get("tool_use_id", "")).strip()
                if call_id:
                    call_ids.append(call_id)
            continue
        if str(item.get("type", "")).strip() != "function_call_output":
            continue
        call_id = str(item.get("call_id", "")).strip()
        if call_id:
            call_ids.append(call_id)
    return call_ids


def resolve_conversation_for_request(
    store: ConversationStore,
    data: dict[str, Any],
    owner: str,
    request_format: str,
):
    explicit_conversation_id = str(data.get("conversation_id", "")).strip()
    if explicit_conversation_id:
        conversation = store.get_conversation(explicit_conversation_id, owner)
        if conversation is None:
            return None, ("conversation not found", 404)
        return conversation, None

    for call_id in extract_tool_result_call_ids(data, request_format):
        conversation = store.find_conversation_by_tool_call_id(owner, call_id)
        if conversation is not None:
            return conversation, None

    conversation = resolve_conversation_by_history_strategies(
        store,
        data,
        owner,
        request_format,
    )
    if conversation is not None:
        return conversation, None

    return None, None


def build_message_debug_metadata(
    *,
    auth: AuthContext,
    request_format: str,
    request_data: dict[str, Any],
    input_text: str,
    input_payload: Any,
    request_id: str,
    resolved_model: str,
    response_id: str | None = None,
) -> dict[str, Any]:
    tool_schemas = request_data.get("tools")
    if request_format == "anthropic_messages" and isinstance(tool_schemas, list):
        tool_schemas = [
            {
                "type": "function",
                "function": {
                    "name": item.get("name"),
                    "description": item.get("description", ""),
                    "parameters": item.get("input_schema", {}),
                },
            }
            if isinstance(item, dict)
            else item
            for item in tool_schemas
        ]
    return {
        "provider": request_format,
        "model": resolved_model,
        "request_format": request_format,
        "request_debug": {
            "request_id": request_id,
            "response_id": response_id or "",
            "model": resolved_model,
            "request_format": request_format,
            "api_key_name": auth.request_api_key_name(),
            "request_keys": sorted(request_data.keys()),
            "input_text": input_text,
            "input_payload": input_payload,
            "tool_schemas": tool_schemas if isinstance(tool_schemas, list) else [],
            "request_body": request_data,
            "headers": auth.request_headers_snapshot(),
        },
    }
