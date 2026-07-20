package protocolruntime

import (
	"testing"

	"github.com/zyf2007/ChatAPI/internal/protocol"
)

func TestRuntimeBuildsAnthropicBlockState(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolAnthropicMessages,
		Model:      "claude-test",
		ResponseID: "msg_test",
	})

	start := runtime.Start()
	if len(start) != 2 || start[0].Event != "message_start" || start[1].Event != "ping" {
		t.Fatalf("unexpected start events: %#v", start)
	}

	first := runtime.Apply(Action{Kind: ActionDelta, DeltaText: "hello"})
	if len(first.StreamEvents) != 2 {
		t.Fatalf("expected content block start plus delta, got %#v", first.StreamEvents)
	}
	if first.StreamEvents[0].Event != "content_block_start" || first.StreamEvents[1].Event != "content_block_delta" {
		t.Fatalf("unexpected first delta events: %#v", first.StreamEvents)
	}

	second := runtime.Apply(Action{Kind: ActionDelta, DeltaText: " world"})
	if len(second.StreamEvents) != 1 || second.StreamEvents[0].Event != "content_block_delta" {
		t.Fatalf("unexpected second delta events: %#v", second.StreamEvents)
	}
}

func TestRuntimeCompletionDoesNotRepeatStreamedText(t *testing.T) {
	tests := []struct {
		name     string
		protocol protocol.Protocol
	}{
		{name: "responses", protocol: protocol.ProtocolResponses},
		{name: "chat_completions", protocol: protocol.ProtocolChatCompletions},
		{name: "anthropic", protocol: protocol.ProtocolAnthropicMessages},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := New(protocol.ConversationMeta{Protocol: test.protocol, Model: "test-model"})
			delta := runtime.Apply(Action{Kind: ActionDelta, DeltaText: "already streamed"})
			completed := runtime.Apply(Action{Kind: ActionComplete})

			for _, event := range completed.StreamEvents {
				if streamEventText(event) == "already streamed" {
					t.Fatalf("completion repeated an earlier delta: %#v; delta events: %#v", completed.StreamEvents, delta.StreamEvents)
				}
			}
		})
	}
}

func TestRuntimePreservesNewlineOnlyResponsesDelta(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test"})
	result := runtime.Apply(Action{Kind: ActionDelta, DeltaText: "\n"})

	var delta string
	for _, event := range result.StreamEvents {
		if event.Event != "response.output_text.delta" {
			continue
		}
		delta, _ = event.Data.(map[string]any)["delta"].(string)
	}
	if delta != "\n" {
		t.Fatalf("newline-only delta changed on the Responses stream: %q", delta)
	}
}

func TestRuntimeMapsOutputGuardFinishReasons(t *testing.T) {
	t.Run("responses_length", func(t *testing.T) {
		runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test", ResponseID: "resp_test"})
		result := runtime.Apply(Action{Kind: ActionComplete, FinishReason: "length", OutputTokens: 7})
		last := result.StreamEvents[len(result.StreamEvents)-1]
		if last.Event != "response.incomplete" {
			t.Fatalf("unexpected responses terminal event: %#v", last)
		}
		response := last.Data.(map[string]any)["response"].(map[string]any)
		if response["status"] != "incomplete" || response["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
			t.Fatalf("unexpected incomplete response: %#v", response)
		}
	})

	t.Run("chat_length", func(t *testing.T) {
		runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolChatCompletions, Model: "gpt-test"})
		result := runtime.Apply(Action{Kind: ActionComplete, FinishReason: "length", OutputTokens: 7})
		terminal := result.StreamEvents[len(result.StreamEvents)-2].Data.(map[string]any)
		choices := terminal["choices"].([]map[string]any)
		if choices[0]["finish_reason"] != "length" {
			t.Fatalf("unexpected chat finish reason: %#v", terminal)
		}
	})

	t.Run("truncated_tool_call_uses_length", func(t *testing.T) {
		chatRuntime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolChatCompletions, Model: "gpt-test"})
		chatResult := chatRuntime.Apply(Action{Kind: ActionComplete, Mode: "tool_call", ToolName: "lookup", OutputText: `{`, FinishReason: "length", OutputTokens: 7})
		chatTerminal := chatResult.StreamEvents[len(chatResult.StreamEvents)-2].Data.(map[string]any)
		if chatTerminal["choices"].([]map[string]any)[0]["finish_reason"] != "length" {
			t.Fatalf("truncated chat tool call lost length outcome: %#v", chatTerminal)
		}

		anthropicRuntime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolAnthropicMessages, Model: "claude-test"})
		anthropicResult := anthropicRuntime.Apply(Action{Kind: ActionComplete, Mode: "tool_call", ToolName: "lookup", OutputText: `{`, FinishReason: "length", OutputTokens: 7})
		messageDelta := anthropicResult.StreamEvents[len(anthropicResult.StreamEvents)-2].Data.(map[string]any)
		if messageDelta["delta"].(map[string]any)["stop_reason"] != "max_tokens" {
			t.Fatalf("truncated anthropic tool call lost length outcome: %#v", messageDelta)
		}
	})

	t.Run("anthropic_stop_sequence", func(t *testing.T) {
		runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolAnthropicMessages, Model: "claude-test"})
		result := runtime.Apply(Action{Kind: ActionComplete, FinishReason: "stop_sequence", StopSequence: "END", OutputTokens: 7})
		messageDelta := result.StreamEvents[len(result.StreamEvents)-2].Data.(map[string]any)
		delta := messageDelta["delta"].(map[string]any)
		if delta["stop_reason"] != "stop_sequence" || delta["stop_sequence"] != "END" {
			t.Fatalf("unexpected anthropic stop delta: %#v", messageDelta)
		}
	})
}

func TestRuntimeResponsesCompletionIncludesRequiredUsageFields(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test"})
	result := runtime.Apply(Action{Kind: ActionComplete, OutputTokens: 7})
	terminal := result.StreamEvents[len(result.StreamEvents)-1]
	response := terminal.Data.(map[string]any)["response"].(map[string]any)
	usage := response["usage"].(map[string]any)

	for _, field := range []string{"input_tokens", "output_tokens", "total_tokens", "input_tokens_details", "output_tokens_details"} {
		if _, ok := usage[field]; !ok {
			t.Fatalf("response.completed usage is missing %q: %#v", field, usage)
		}
	}
	if usage["input_tokens"] != 0 || usage["output_tokens"] != 7 || usage["total_tokens"] != 7 {
		t.Fatalf("unexpected response.completed usage: %#v", usage)
	}
}

func streamEventText(event protocol.StreamEvent) string {
	data, ok := event.Data.(map[string]any)
	if !ok {
		return ""
	}
	if delta, ok := data["delta"].(string); ok {
		return delta
	}
	choices, ok := data["choices"].([]map[string]any)
	if !ok || len(choices) == 0 {
		return ""
	}
	delta, _ := choices[0]["delta"].(map[string]any)
	text, _ := delta["content"].(string)
	return text
}

func TestRuntimeBuildsResponsesAbort(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	result := runtime.Apply(Action{
		Kind: ActionAbort,
		ErrorBody: map[string]any{
			"error": map[string]any{
				"message": "request disconnected",
				"type":    "request_disconnected",
				"code":    "request_disconnected",
			},
		},
	})
	if len(result.StreamEvents) != 1 || result.StreamEvents[0].Event != "response.failed" {
		t.Fatalf("unexpected abort events: %#v", result.StreamEvents)
	}
}

func TestRuntimeBuildsResponsesToolCallLifecycle(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	requireEventOrder(t, result.StreamEvents,
		"response.output_item.added",
		"response.function_call_arguments.delta",
		"response.function_call_arguments.done",
		"response.output_item.done",
		"response.completed",
	)
}

func TestRuntimeBuildsResponsesBuiltinToolEvents(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol:   protocol.ProtocolResponses,
		Model:      "gpt-test",
		ResponseID: "resp_test",
	})

	search := runtime.Apply(Action{
		Kind:             ActionBuiltin,
		BuiltinToolKind:  "web_search",
		BuiltinToolQuery: "latest go release",
	})
	requireEventOrder(t, search.StreamEvents,
		"response.output_item.added",
		"response.web_search_call.in_progress",
		"response.web_search_call.searching",
		"response.web_search_call.completed",
		"response.output_item.done",
	)

	image := runtime.Apply(Action{
		Kind:              ActionBuiltin,
		BuiltinToolKind:   "image_generation",
		BuiltinToolResult: "aW1hZ2U=",
	})
	requireEventOrder(t, image.StreamEvents,
		"response.output_item.added",
		"response.image_generation_call.in_progress",
		"response.image_generation_call.generating",
		"response.image_generation_call.completed",
		"response.output_item.done",
	)
	done := image.StreamEvents[len(image.StreamEvents)-1].Data.(map[string]any)
	item := done["item"].(map[string]any)
	if item["type"] != "image_generation_call" || item["result"] != "aW1hZ2U=" {
		t.Fatalf("unexpected image generation output item: %#v", item)
	}
}

func TestRuntimeBuildsChatCompletionToolCallChunks(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolChatCompletions,
		Model:    "gpt-test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "call_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	if len(result.StreamEvents) != 3 {
		t.Fatalf("unexpected chat tool events: %#v", result.StreamEvents)
	}
	first := result.StreamEvents[0].Data.(map[string]any)
	choices := first["choices"].([]map[string]any)
	delta := choices[0]["delta"].(map[string]any)
	if _, ok := delta["tool_calls"]; !ok {
		t.Fatalf("missing chat tool_calls delta: %#v", first)
	}
	stop := result.StreamEvents[1].Data.(map[string]any)
	stopChoices := stop["choices"].([]map[string]any)
	if stopChoices[0]["finish_reason"] != "tool_calls" {
		t.Fatalf("unexpected chat finish reason: %#v", stop)
	}
	if result.StreamEvents[2].Data != "[DONE]" {
		t.Fatalf("missing chat done marker: %#v", result.StreamEvents)
	}
}

func TestRuntimeDowngradesAnthropicThinkingToText(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolAnthropicMessages, Model: "claude-test"})
	thinking := runtime.Apply(Action{Kind: ActionDelta, Mode: "thinking", DeltaText: "alpha"})
	if len(thinking.StreamEvents) != 2 {
		t.Fatalf("expected text block start and delta: %#v", thinking.StreamEvents)
	}
	start := thinking.StreamEvents[0].Data.(map[string]any)["content_block"].(map[string]any)
	if start["type"] != "text" {
		t.Fatalf("thinking became official Anthropic block: %#v", start)
	}
	delta := thinking.StreamEvents[1].Data.(map[string]any)["delta"].(map[string]any)
	if delta["type"] != "text_delta" || delta["text"] != "alpha" {
		t.Fatalf("thinking was not downgraded to text delta: %#v", delta)
	}
	for _, event := range thinking.StreamEvents {
		data, _ := event.Data.(map[string]any)
		if data["signature"] != nil || data["type"] == "thinking_delta" || data["type"] == "signature_delta" {
			t.Fatalf("unsigned Anthropic thinking event emitted: %#v", event)
		}
	}
}

func TestRuntimeAnthropicAlternatingModesOpenSeparateTextBlocks(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolAnthropicMessages, Model: "claude-test"})
	steps := []Action{
		{Kind: ActionDelta, Mode: "thinking", DeltaText: "alpha"},
		{Kind: ActionDelta, Mode: "answer", DeltaText: "beta"},
		{Kind: ActionDelta, Mode: "thinking", DeltaText: "gamma"},
		{Kind: ActionDelta, Mode: "answer", DeltaText: "delta"},
	}
	var starts, stops, deltas int
	for _, step := range steps {
		result := runtime.Apply(step)
		for _, event := range result.StreamEvents {
			switch event.Event {
			case "content_block_start":
				starts++
				block := event.Data.(map[string]any)["content_block"].(map[string]any)
				if block["type"] != "text" {
					t.Fatalf("expected text block, got %#v", block)
				}
				if _, ok := block["signature"]; ok {
					t.Fatalf("signature fabricated: %#v", block)
				}
			case "content_block_stop":
				stops++
			case "content_block_delta":
				deltas++
				delta := event.Data.(map[string]any)["delta"].(map[string]any)
				if delta["type"] != "text_delta" {
					t.Fatalf("unexpected delta type: %#v", delta)
				}
			}
			data, _ := event.Data.(map[string]any)
			if data["type"] == "thinking_delta" || data["type"] == "signature_delta" {
				t.Fatalf("forbidden anthropic thinking event: %#v", event)
			}
		}
	}
	// Four logical segments => four text blocks opened; three switches close previous blocks.
	if starts != 4 || stops != 3 || deltas != 4 {
		t.Fatalf("unexpected lifecycle starts=%d stops=%d deltas=%d", starts, stops, deltas)
	}
	completed := runtime.Apply(Action{Kind: ActionComplete})
	finalStops := 0
	for _, event := range completed.StreamEvents {
		if event.Event == "content_block_stop" {
			finalStops++
		}
	}
	if finalStops != 1 {
		t.Fatalf("complete should close the final open block once: %#v", completed.StreamEvents)
	}
	// Non-stream content should also be 4 text blocks in the same order.
	body := protocol.BuildResponseForMeta(protocol.ConversationMeta{Protocol: protocol.ProtocolAnthropicMessages, Model: "claude-test"}, protocol.TurnResult{
		OutputSegments: []protocol.OutputSegment{
			{Mode: "thinking", Text: "alpha"},
			{Mode: "answer", Text: "beta"},
			{Mode: "thinking", Text: "gamma"},
			{Mode: "answer", Text: "delta"},
		},
	})
	content := body["content"].([]map[string]any)
	if len(content) != 4 {
		t.Fatalf("non-stream block count diverged: %#v", content)
	}
}

func TestRuntimeResponsesAlternatingAnswerSegmentsUseDistinctIDs(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test", ResponseID: "resp_test"})
	steps := []Action{
		{Kind: ActionDelta, Mode: "answer", DeltaText: "beta"},
		{Kind: ActionDelta, Mode: "thinking", DeltaText: "alpha", ReasoningStreamMode: "summary"},
		{Kind: ActionDelta, Mode: "answer", DeltaText: "delta"},
	}
	var messageIDs []string
	var textDones []string
	for _, step := range steps {
		result := runtime.Apply(step)
		for _, event := range result.StreamEvents {
			switch event.Event {
			case "response.output_item.added":
				item := event.Data.(map[string]any)["item"].(map[string]any)
				if item["type"] == "message" {
					messageIDs = append(messageIDs, item["id"].(string))
				}
			case "response.output_text.done":
				textDones = append(textDones, event.Data.(map[string]any)["text"].(string))
			}
		}
	}
	if len(messageIDs) != 2 || messageIDs[0] == "" || messageIDs[0] == messageIDs[1] {
		t.Fatalf("expected two distinct message item ids, got %#v", messageIDs)
	}
	if len(textDones) != 1 || textDones[0] != "beta" {
		t.Fatalf("first answer done should close only beta before thinking: %#v", textDones)
	}
	completed := runtime.Apply(Action{Kind: ActionComplete})
	for _, event := range completed.StreamEvents {
		if event.Event == "response.output_text.done" {
			textDones = append(textDones, event.Data.(map[string]any)["text"].(string))
		}
	}
	if len(textDones) != 2 || textDones[0] != "beta" || textDones[1] != "delta" {
		t.Fatalf("done texts must be per-segment, got %#v", textDones)
	}
	last := completed.StreamEvents[len(completed.StreamEvents)-1]
	response := last.Data.(map[string]any)["response"].(map[string]any)
	if response["output_text"] != "betadelta" {
		t.Fatalf("response.output_text must aggregate all answer segments, got %#v", response["output_text"])
	}
	// Completion must not re-emit answer deltas already streamed.
	for _, event := range completed.StreamEvents {
		if event.Event == "response.output_text.delta" {
			t.Fatalf("complete repeated a text delta: %#v", event)
		}
	}
}

func TestRuntimeResponsesAlternatingReasoningSegmentsUseDistinctIDs(t *testing.T) {
	for _, mode := range []string{"summary", "reasoning"} {
		t.Run(mode, func(t *testing.T) {
			runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test", ResponseID: "resp_test"})
			steps := []Action{
				{Kind: ActionDelta, Mode: "thinking", DeltaText: "alpha", ReasoningStreamMode: mode},
				{Kind: ActionDelta, Mode: "answer", DeltaText: "beta"},
				{Kind: ActionDelta, Mode: "thinking", DeltaText: "gamma", ReasoningStreamMode: mode},
			}
			var reasoningIDs []string
			var doneTexts []string
			doneEvent := "response.reasoning_summary_text.done"
			if mode == "reasoning" {
				doneEvent = "response.reasoning_text.done"
			}
			for _, step := range steps {
				result := runtime.Apply(step)
				for _, event := range result.StreamEvents {
					switch event.Event {
					case "response.output_item.added":
						item := event.Data.(map[string]any)["item"].(map[string]any)
						if item["type"] == "reasoning" {
							reasoningIDs = append(reasoningIDs, item["id"].(string))
						}
					case doneEvent:
						doneTexts = append(doneTexts, event.Data.(map[string]any)["text"].(string))
					}
				}
			}
			if len(reasoningIDs) != 2 || reasoningIDs[0] == "" || reasoningIDs[0] == reasoningIDs[1] {
				t.Fatalf("expected two distinct reasoning item ids, got %#v", reasoningIDs)
			}
			if len(doneTexts) != 1 || doneTexts[0] != "alpha" {
				t.Fatalf("first reasoning done should close only alpha: %#v", doneTexts)
			}
			completed := runtime.Apply(Action{Kind: ActionComplete})
			for _, event := range completed.StreamEvents {
				if event.Event == doneEvent {
					doneTexts = append(doneTexts, event.Data.(map[string]any)["text"].(string))
				}
			}
			if len(doneTexts) != 2 || doneTexts[0] != "alpha" || doneTexts[1] != "gamma" {
				t.Fatalf("reasoning done texts must not accumulate across segments: %#v", doneTexts)
			}
		})
	}
}

func TestRuntimeResponsesSameSegmentAccumulatesIntoSingleDone(t *testing.T) {
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test", ResponseID: "resp_test"})
	first := runtime.Apply(Action{Kind: ActionDelta, Mode: "answer", DeltaText: "be"})
	second := runtime.Apply(Action{Kind: ActionDelta, Mode: "answer", DeltaText: "ta"})
	var messageIDs []string
	for _, event := range append(first.StreamEvents, second.StreamEvents...) {
		if event.Event == "response.output_item.added" {
			item := event.Data.(map[string]any)["item"].(map[string]any)
			if item["type"] == "message" {
				messageIDs = append(messageIDs, item["id"].(string))
			}
		}
	}
	completed := runtime.Apply(Action{Kind: ActionComplete})
	var textDones []string
	for _, event := range completed.StreamEvents {
		if event.Event == "response.output_text.done" {
			textDones = append(textDones, event.Data.(map[string]any)["text"].(string))
		}
		if event.Event == "response.output_text.delta" {
			t.Fatalf("complete repeated a text delta: %#v", event)
		}
	}
	if len(messageIDs) != 1 {
		t.Fatalf("same answer segment must keep one message item: %#v", messageIDs)
	}
	if len(textDones) != 1 || textDones[0] != "beta" {
		t.Fatalf("same-segment done must be full segment text, got %#v", textDones)
	}
	response := completed.StreamEvents[len(completed.StreamEvents)-1].Data.(map[string]any)["response"].(map[string]any)
	if response["output_text"] != "beta" {
		t.Fatalf("unexpected aggregate output_text: %#v", response["output_text"])
	}
}

func TestRuntimeResponsesCompletedOutputMatchesSharedBuilder(t *testing.T) {
	segments := []protocol.OutputSegment{
		{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
		{Mode: "answer", Text: "beta"},
		{Mode: "thinking", Text: "gamma", ReasoningStreamMode: "reasoning"},
		{Mode: "answer", Text: "delta"},
	}
	runtime := New(protocol.ConversationMeta{Protocol: protocol.ProtocolResponses, Model: "gpt-test", ResponseID: "resp_test"})
	for _, segment := range segments {
		runtime.Apply(Action{
			Kind: ActionDelta, Mode: segment.Mode, DeltaText: segment.Text, ReasoningStreamMode: segment.ReasoningStreamMode,
		})
	}
	completed := runtime.Apply(Action{Kind: ActionComplete, OutputSegments: segments})
	last := completed.StreamEvents[len(completed.StreamEvents)-1]
	response := last.Data.(map[string]any)["response"].(map[string]any)
	streamOutput := response["output"].([]map[string]any)
	nonStream := protocol.BuildResponsesOutput(protocol.TurnResult{OutputSegments: segments})
	if len(streamOutput) != len(nonStream) {
		t.Fatalf("stream/non-stream output length mismatch stream=%d non=%d", len(streamOutput), len(nonStream))
	}
	for index := range nonStream {
		if streamOutput[index]["type"] != nonStream[index]["type"] {
			t.Fatalf("type mismatch at %d: stream=%v non=%v", index, streamOutput[index]["type"], nonStream[index]["type"])
		}
		if nonStream[index]["type"] != "reasoning" {
			continue
		}
		streamSummary := len(asMapSlice(streamOutput[index]["summary"]))
		streamContent := len(asMapSlice(streamOutput[index]["content"]))
		nonSummary := len(asMapSlice(nonStream[index]["summary"]))
		nonContent := len(asMapSlice(nonStream[index]["content"]))
		if streamSummary != nonSummary || streamContent != nonContent {
			t.Fatalf("reasoning field drift at %d stream=%#v non=%#v", index, streamOutput[index], nonStream[index])
		}
		// summary-mode must not populate content; reasoning-mode must not populate summary.
		if nonSummary > 0 && nonContent > 0 {
			t.Fatalf("both summary and content populated: %#v", nonStream[index])
		}
	}
}

func asMapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if entry, ok := item.(map[string]any); ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}
func TestRuntimeEmitsChatReasoningContent(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolChatCompletions,
		Model:    "gpt-test",
	})

	thinking := runtime.Apply(Action{Kind: ActionDelta, Mode: "thinking", DeltaText: "reason"})
	if len(thinking.StreamEvents) != 1 {
		t.Fatalf("unexpected thinking events: %#v", thinking.StreamEvents)
	}
	first := thinking.StreamEvents[0].Data.(map[string]any)
	choices := first["choices"].([]map[string]any)
	delta := choices[0]["delta"].(map[string]any)
	if delta["reasoning_content"] != "reason" {
		t.Fatalf("missing chat reasoning_content: %#v", first)
	}

	answer := runtime.Apply(Action{Kind: ActionDelta, Mode: "answer", DeltaText: "hello"})
	answerData := answer.StreamEvents[0].Data.(map[string]any)
	answerChoices := answerData["choices"].([]map[string]any)
	answerDelta := answerChoices[0]["delta"].(map[string]any)
	if answerDelta["content"] != "hello" {
		t.Fatalf("unexpected answer delta: %#v", answerData)
	}
}

func TestRuntimeBuildsAnthropicToolUseLifecycle(t *testing.T) {
	runtime := New(protocol.ConversationMeta{
		Protocol: protocol.ProtocolAnthropicMessages,
		Model:    "claude-test",
	})

	result := runtime.Apply(Action{
		Kind:       ActionComplete,
		Mode:       "tool_call",
		ToolName:   "lookup_weather",
		ToolCallID: "toolu_1",
		OutputText: `{"city":"Hangzhou"}`,
	})

	requireEventOrder(t, result.StreamEvents,
		"content_block_start",
		"content_block_delta",
		"content_block_stop",
		"message_delta",
		"message_stop",
	)
	delta := result.StreamEvents[1].Data.(map[string]any)["delta"].(map[string]any)
	if delta["type"] != "input_json_delta" {
		t.Fatalf("unexpected anthropic tool delta: %#v", delta)
	}
}

func requireEventOrder(t *testing.T, events []protocol.StreamEvent, expected ...string) {
	t.Helper()
	if len(events) != len(expected) {
		t.Fatalf("unexpected event count got=%d want=%d events=%#v", len(events), len(expected), events)
	}
	for index, want := range expected {
		if events[index].Event != want {
			t.Fatalf("unexpected event at %d got=%q want=%q events=%#v", index, events[index].Event, want, events)
		}
	}
}
