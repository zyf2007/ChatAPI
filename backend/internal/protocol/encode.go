package protocol

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

var thinkTagPattern = regexp.MustCompile(`(?s)<think>(.*?)</think>`)

func responseIDWithFallback(result TurnResult, fallback string) string {
	return stringValue(result.ResponseID, fallback)
}

func chatCompletionID(result TurnResult) string {
	return responseIDWithFallback(result, "chatcmpl_"+uuid.NewString())
}

func anthropicMessageID(result TurnResult) string {
	return responseIDWithFallback(result, "msg_"+uuid.NewString())
}

func responsesCallID(result TurnResult) string {
	return stringValue(result.ToolCallID, "call_"+uuid.NewString())
}

func openAIToolCallID(result TurnResult) string {
	return stringValue(result.ToolCallID, "toolcall_"+uuid.NewString())
}

func anthropicToolUseID(result TurnResult) string {
	return stringValue(result.ToolCallID, "toolu_"+uuid.NewString())
}

func buildResponsesOutput(result TurnResult) []map[string]any {
	switch result.Mode {
	case "tool_call":
		return []map[string]any{{
			"type":      "function_call",
			"name":      result.ToolName,
			"call_id":   responsesCallID(result),
			"arguments": result.OutputText,
		}}
	case "tool_result":
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": responsesCallID(result),
			"output":  stringValue(result.ToolOutput, result.OutputText),
		}}
	default:
		thinking, answer := splitThinkingContent(result.OutputText)
		output := make([]map[string]any, 0, 2)
		if thinking != "" {
			output = append(output, map[string]any{
				"id":     "rs_" + uuid.NewString(),
				"type":   "reasoning",
				"status": "completed",
				"summary": []map[string]any{{
					"type": "summary_text",
					"text": thinking,
				}},
				"content": []map[string]any{{
					"type": "reasoning_text",
					"text": thinking,
				}},
			})
		}
		output = append(output, map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []map[string]any{
				{"type": "output_text", "text": answer},
			},
		})
		return output
	}
}

func buildChatCompletionMessage(result TurnResult) map[string]any {
	if result.Mode == "tool_call" {
		return map[string]any{
			"role":       "assistant",
			"content":    "",
			"tool_calls": []map[string]any{buildChatCompletionToolCall(result)},
		}
	}
	thinking, answer := splitThinkingContent(result.OutputText)
	message := map[string]any{
		"role":    "assistant",
		"content": answer,
	}
	if thinking != "" {
		// Common OpenAI-compatible extension used by many clients for reasoning models.
		message["reasoning_content"] = thinking
	}
	return message
}

func buildChatCompletionToolCall(result TurnResult) map[string]any {
	return map[string]any{
		"id":   openAIToolCallID(result),
		"type": "function",
		"function": map[string]any{
			"name":      result.ToolName,
			"arguments": result.OutputText,
		},
	}
}

func buildAnthropicContent(result TurnResult) []map[string]any {
	if result.Mode == "tool_call" {
		return []map[string]any{buildAnthropicToolUseBlock(result)}
	}
	thinking, answer := splitThinkingContent(result.OutputText)
	content := make([]map[string]any, 0, 2)
	if thinking != "" {
		content = append(content, map[string]any{
			"type":     "thinking",
			"thinking": thinking,
		})
	}
	if answer != "" || thinking == "" {
		content = append(content, map[string]any{"type": "text", "text": answer})
	}
	return content
}

// splitThinkingContent extracts <think>...</think> segments written by the
// workspace draft path and returns (thinking, answer).
func splitThinkingContent(raw string) (thinking string, answer string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	matches := thinkTagPattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return "", raw
	}
	var thinkingParts []string
	var answerParts []string
	last := 0
	for _, match := range matches {
		if match[0] > last {
			answerParts = append(answerParts, raw[last:match[0]])
		}
		if match[2] >= 0 && match[3] >= 0 {
			part := strings.TrimSpace(raw[match[2]:match[3]])
			if part != "" {
				thinkingParts = append(thinkingParts, part)
			}
		}
		last = match[1]
	}
	if last < len(raw) {
		answerParts = append(answerParts, raw[last:])
	}
	return strings.TrimSpace(strings.Join(thinkingParts, "\n")), strings.Join(answerParts, "")
}

func buildAnthropicToolUseBlock(result TurnResult) map[string]any {
	return map[string]any{
		"type":  "tool_use",
		"id":    anthropicToolUseID(result),
		"name":  result.ToolName,
		"input": parseJSONValue(result.OutputText),
	}
}

func parseJSONValue(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}

func normalizeUsage(usage Usage) Usage {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.TotalTokens <= 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}
