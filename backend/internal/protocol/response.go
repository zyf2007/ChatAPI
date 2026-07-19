package protocol

import (
	"strings"

	"github.com/google/uuid"
)

func BuildResponseForMeta(meta ConversationMeta, result TurnResult) map[string]any {
	usage := normalizeUsage(result.Usage)
	outcome := ResolveCompletionOutcome(result.FinishReason, result.Mode)
	switch meta.Protocol {
	case ProtocolChatCompletions:
		return map[string]any{
			"id":     chatCompletionID(result),
			"object": "chat.completion",
			"model":  meta.Model,
			"choices": []map[string]any{{
				"index": 0, "message": buildChatCompletionMessage(result),
				"finish_reason": outcome.ChatFinishReason(),
			}},
			"usage": map[string]any{
				"prompt_tokens": usage.InputTokens, "completion_tokens": usage.OutputTokens,
				"total_tokens": usage.TotalTokens,
			},
		}
	case ProtocolAnthropicMessages:
		return map[string]any{
			"id": anthropicMessageID(result), "type": "message", "role": "assistant",
			"stop_reason":   outcome.AnthropicStopReason(),
			"stop_sequence": nullableStopSequence(result.StopSequence),
			"content":       buildAnthropicContent(result),
			"usage": map[string]any{
				"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
			},
		}
	default:
		body := map[string]any{
			"id":     responseIDWithFallback(result, "resp_"+uuid.NewString()),
			"object": "response", "status": "completed",
			"output_text": responseOutputText(result),
			"usage": map[string]any{
				"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
				"total_tokens": usage.TotalTokens,
			},
			"output": buildResponsesOutput(result),
		}
		if outcome.ResponsesIncomplete() {
			body["status"] = "incomplete"
			body["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
		}
		return body
	}
}

func responseOutputText(result TurnResult) string {
	var output strings.Builder
	for _, segment := range outputSegments(result) {
		if segmentMode(segment) == "answer" {
			output.WriteString(segment.Text)
		}
	}
	return output.String()
}

func nullableStopSequence(value string) any {
	if value == "" {
		return nil
	}
	return value
}
