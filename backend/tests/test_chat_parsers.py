from __future__ import annotations

import pytest

from backend.services.chat_parsers import (
    extract_anthropic_context_text,
    extract_anthropic_tool_result_use_ids,
    extract_chat_context_text,
    extract_chat_tool_result_call_ids,
)


# ---------------------------------------------------------------------------
# extract_chat_context_text
# ---------------------------------------------------------------------------

class TestExtractChatContextText:
    def test_simple_user_string(self):
        messages = [{"role": "user", "content": "Hello"}]
        assert extract_chat_context_text(messages) == "Hello"

    def test_picks_last_user_message(self):
        messages = [
            {"role": "user", "content": "First"},
            {"role": "assistant", "content": "Reply"},
            {"role": "user", "content": "Second"},
        ]
        assert extract_chat_context_text(messages) == "Second"

    def test_skips_system_role(self):
        messages = [
            {"role": "system", "content": "You are helpful"},
            {"role": "user", "content": "Hi"},
        ]
        assert extract_chat_context_text(messages) == "Hi"

    def test_content_as_text_blocks(self):
        messages = [
            {"role": "user", "content": [
                {"type": "text", "text": "Block one"},
                {"type": "text", "text": "Block two"},
            ]}
        ]
        assert extract_chat_context_text(messages) == "Block one\nBlock two"

    def test_content_blocks_skips_non_text(self):
        messages = [
            {"role": "user", "content": [
                {"type": "image_url", "image_url": {"url": "http://example.com/img.png"}},
                {"type": "text", "text": "Describe this"},
            ]}
        ]
        assert extract_chat_context_text(messages) == "Describe this"

    def test_tool_role_string_content(self):
        """tool 消息的字符串 content 应被当作上下文文本返回。"""
        messages = [
            {"role": "user", "content": "What's the weather?"},
            {"role": "assistant", "content": None, "tool_calls": [{"id": "call_1"}]},
            {"role": "tool", "tool_call_id": "call_1", "content": "Sunny, 25°C"},
        ]
        assert extract_chat_context_text(messages) == "Sunny, 25°C"

    def test_tool_role_empty_content_falls_back_to_user(self):
        """tool 消息 content 为空时，继续往前找 user 消息。"""
        messages = [
            {"role": "user", "content": "What's the weather?"},
            {"role": "assistant", "content": None},
            {"role": "tool", "tool_call_id": "call_1", "content": ""},
        ]
        assert extract_chat_context_text(messages) == "What's the weather?"

    def test_empty_messages(self):
        assert extract_chat_context_text([]) == ""

    def test_no_user_message(self):
        messages = [{"role": "assistant", "content": "Hi"}]
        assert extract_chat_context_text(messages) == ""

    def test_whitespace_is_stripped(self):
        messages = [{"role": "user", "content": "  hello  "}]
        assert extract_chat_context_text(messages) == "hello"


# ---------------------------------------------------------------------------
# extract_chat_tool_result_call_ids
# ---------------------------------------------------------------------------

class TestExtractChatToolResultCallIds:
    def test_single_tool_message(self):
        messages = [
            {"role": "user", "content": "ping"},
            {"role": "tool", "tool_call_id": "call_abc", "content": "pong"},
        ]
        assert extract_chat_tool_result_call_ids(messages) == ["call_abc"]

    def test_multiple_tool_messages(self):
        messages = [
            {"role": "tool", "tool_call_id": "call_1", "content": "result1"},
            {"role": "tool", "tool_call_id": "call_2", "content": "result2"},
        ]
        assert extract_chat_tool_result_call_ids(messages) == ["call_1", "call_2"]

    def test_ignores_non_tool_roles(self):
        messages = [
            {"role": "user", "content": "hi", "tool_call_id": "should_be_ignored"},
        ]
        assert extract_chat_tool_result_call_ids(messages) == []

    def test_skips_empty_tool_call_id(self):
        messages = [{"role": "tool", "tool_call_id": "", "content": "result"}]
        assert extract_chat_tool_result_call_ids(messages) == []

    def test_empty_messages(self):
        assert extract_chat_tool_result_call_ids([]) == []


# ---------------------------------------------------------------------------
# extract_anthropic_context_text
# ---------------------------------------------------------------------------

class TestExtractAnthropicContextText:
    def test_simple_string_content(self):
        data = {"messages": [{"role": "user", "content": "Hello"}]}
        assert extract_anthropic_context_text(data) == "Hello"

    def test_content_as_text_blocks(self):
        data = {"messages": [{"role": "user", "content": [
            {"type": "text", "text": "Part A"},
            {"type": "text", "text": "Part B"},
        ]}]}
        assert extract_anthropic_context_text(data) == "Part A\nPart B"

    def test_picks_last_user_message(self):
        data = {"messages": [
            {"role": "user", "content": "First"},
            {"role": "assistant", "content": [{"type": "text", "text": "Reply"}]},
            {"role": "user", "content": "Second"},
        ]}
        assert extract_anthropic_context_text(data) == "Second"

    def test_tool_result_string_content(self):
        """tool_result block 的字符串 content 应作为上下文文本返回。"""
        data = {"messages": [
            {"role": "user", "content": "What's the weather?"},
            {"role": "assistant", "content": [
                {"type": "tool_use", "id": "toolu_1", "name": "get_weather", "input": {}}
            ]},
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_1", "content": "Sunny, 25°C"}
            ]},
        ]}
        assert extract_anthropic_context_text(data) == "Sunny, 25°C"

    def test_tool_result_block_content(self):
        """tool_result content 为 block 数组时正确提取文本。"""
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_1", "content": [
                    {"type": "text", "text": "Result text"}
                ]}
            ]},
        ]}
        assert extract_anthropic_context_text(data) == "Result text"

    def test_tool_result_multiple_blocks(self):
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_1", "content": "First"},
                {"type": "tool_result", "tool_use_id": "toolu_2", "content": "Second"},
            ]},
        ]}
        assert extract_anthropic_context_text(data) == "First\nSecond"

    def test_empty_messages_key(self):
        assert extract_anthropic_context_text({"messages": []}) == ""

    def test_missing_messages_key(self):
        assert extract_anthropic_context_text({}) == ""

    def test_messages_not_list(self):
        assert extract_anthropic_context_text({"messages": "bad"}) == ""

    def test_skips_assistant_role(self):
        data = {"messages": [{"role": "assistant", "content": "I am the assistant"}]}
        assert extract_anthropic_context_text(data) == ""


# ---------------------------------------------------------------------------
# extract_anthropic_tool_result_use_ids
# ---------------------------------------------------------------------------

class TestExtractAnthropicToolResultUseIds:
    def test_single_tool_result(self):
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_abc"}
            ]}
        ]}
        assert extract_anthropic_tool_result_use_ids(data) == ["toolu_abc"]

    def test_multiple_tool_results(self):
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": "toolu_1"},
                {"type": "tool_result", "tool_use_id": "toolu_2"},
            ]}
        ]}
        assert extract_anthropic_tool_result_use_ids(data) == ["toolu_1", "toolu_2"]

    def test_ignores_non_tool_result_blocks(self):
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "text", "text": "hi"},
                {"type": "tool_result", "tool_use_id": "toolu_1"},
            ]}
        ]}
        assert extract_anthropic_tool_result_use_ids(data) == ["toolu_1"]

    def test_ignores_non_user_roles(self):
        data = {"messages": [
            {"role": "assistant", "content": [
                {"type": "tool_result", "tool_use_id": "should_be_ignored"}
            ]}
        ]}
        assert extract_anthropic_tool_result_use_ids(data) == []

    def test_skips_empty_tool_use_id(self):
        data = {"messages": [
            {"role": "user", "content": [
                {"type": "tool_result", "tool_use_id": ""}
            ]}
        ]}
        assert extract_anthropic_tool_result_use_ids(data) == []

    def test_string_content_ignored(self):
        """用户消息 content 为字符串时不报错，返回空列表。"""
        data = {"messages": [{"role": "user", "content": "plain text"}]}
        assert extract_anthropic_tool_result_use_ids(data) == []

    def test_empty(self):
        assert extract_anthropic_tool_result_use_ids({}) == []
