package protocol

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
)

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

func outputSegments(result TurnResult) []OutputSegment {
	if len(result.OutputSegments) > 0 {
		return result.OutputSegments
	}
	if result.OutputText == "" {
		return nil
	}
	return []OutputSegment{{Mode: result.Mode, Text: result.OutputText, ReasoningStreamMode: result.ReasoningStreamMode}}
}

func segmentMode(segment OutputSegment) string {
	if strings.TrimSpace(segment.Mode) == "thinking" {
		return "thinking"
	}
	return "answer"
}

// BuildResponsesOutput is the shared segment-to-Responses-output builder used by
// non-stream responses and stream response.completed.output.
func BuildResponsesOutput(result TurnResult) []map[string]any {
	return buildResponsesOutput(result)
}

func buildResponsesOutput(result TurnResult) []map[string]any {
	switch result.Mode {
	case "tool_call":
		return []map[string]any{{
			"type": "function_call", "name": result.ToolName,
			"call_id": responsesCallID(result), "arguments": result.OutputText,
		}}
	case "tool_result":
		return []map[string]any{{
			"type": "function_call_output", "call_id": responsesCallID(result),
			"output": stringValue(result.ToolOutput, result.OutputText),
		}}
	}
	output := make([]map[string]any, 0, len(outputSegments(result)))
	for _, segment := range outputSegments(result) {
		if segment.Text == "" {
			continue
		}
		if segmentMode(segment) == "thinking" {
			item := map[string]any{
				"id": "rs_" + uuid.NewString(), "type": "reasoning", "status": "completed",
				"summary": []map[string]any{}, "content": []map[string]any{},
			}
			if normalizedReasoningMode(segment.ReasoningStreamMode) == "reasoning" {
				item["content"] = []map[string]any{{"type": "reasoning_text", "text": segment.Text}}
			} else {
				item["summary"] = []map[string]any{{"type": "summary_text", "text": segment.Text}}
			}
			output = append(output, item)
			continue
		}
		output = append(output, map[string]any{
			"type": "message", "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": segment.Text}},
		})
	}
	return output
}

func buildChatCompletionMessage(result TurnResult) map[string]any {
	if result.Mode == "tool_call" {
		return map[string]any{"role": "assistant", "content": "", "tool_calls": []map[string]any{buildChatCompletionToolCall(result)}}
	}
	var answer, thinking strings.Builder
	for _, segment := range outputSegments(result) {
		if segmentMode(segment) == "thinking" {
			thinking.WriteString(segment.Text)
		} else {
			answer.WriteString(segment.Text)
		}
	}
	message := map[string]any{"role": "assistant", "content": answer.String()}
	if thinking.Len() > 0 {
		// Chat Completions has no ordered mixed-content representation. Keep the
		// answer lossless and expose reasoning through the common extension.
		message["reasoning_content"] = thinking.String()
	}
	return message
}

func buildChatCompletionToolCall(result TurnResult) map[string]any {
	return map[string]any{"id": openAIToolCallID(result), "type": "function", "function": map[string]any{
		"name": result.ToolName, "arguments": result.OutputText,
	}}
}

func buildAnthropicContent(result TurnResult) []map[string]any {
	if result.Mode == "tool_call" {
		return []map[string]any{buildAnthropicToolUseBlock(result)}
	}
	// Human-authored/Mock thinking has no provider signature. Emit it as text
	// rather than claiming it is an official Anthropic thinking block.
	content := make([]map[string]any, 0, len(outputSegments(result)))
	for _, segment := range outputSegments(result) {
		if segment.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": segment.Text})
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "text", "text": ""})
	}
	return content
}

func normalizedReasoningMode(mode string) string {
	if strings.TrimSpace(mode) == "reasoning" || strings.TrimSpace(mode) == "reasoning_text" {
		return "reasoning"
	}
	return "summary"
}

func buildAnthropicToolUseBlock(result TurnResult) map[string]any {
	return map[string]any{"type": "tool_use", "id": anthropicToolUseID(result), "name": result.ToolName, "input": parseJSONValue(result.OutputText)}
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
