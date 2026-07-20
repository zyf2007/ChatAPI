package protocolruntime

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/zyf2007/ChatAPI/internal/protocol"
)

type ActionKind string

const (
	ActionDelta    ActionKind = "delta"
	ActionComplete ActionKind = "complete"
	ActionAbort    ActionKind = "abort"
	ActionBuiltin  ActionKind = "builtin_tool"
)

type Action struct {
	Kind                ActionKind
	DeltaText           string
	OutputText          string
	OutputSegments      []protocol.OutputSegment
	Mode                string
	ReasoningStreamMode string
	ToolName            string
	ToolCallID          string
	ToolOutput          string
	BuiltinToolKind     string
	BuiltinToolQuery    string
	BuiltinToolResult   string
	FinishReason        string
	StopSequence        string
	OutputTokens        int
	ErrorBody           map[string]any
}

type Result struct {
	StreamEvents []protocol.StreamEvent
}

type Runtime struct {
	meta protocol.ConversationMeta

	responseID string
	chatID     string
	messageID  string

	responsesOutputIndex      int
	responsesMessageItemID    string
	responsesTextPartOpen     bool
	responsesText             strings.Builder // current open answer segment only
	responsesAnswerAggregate  strings.Builder // response-level concatenation of all answer segments
	responsesClosedOutput     []map[string]any
	responsesReasoningItemID  string
	responsesReasoningMode    string
	responsesReasoningOpen    bool
	responsesReasoningText    strings.Builder // current open reasoning segment only
	responsesReasoningSummary strings.Builder // current open reasoning segment only
	responsesLastToolCallID   string
	responsesLastToolName     string
	responsesLastToolArgs     string

	chatReasoningText strings.Builder

	anthropicBlockIndex int
	anthropicBlockType  string
	anthropicText       strings.Builder
	anthropicThinking   strings.Builder
}

func New(meta protocol.ConversationMeta) *Runtime {
	responseID := strings.TrimSpace(meta.ResponseID)
	if responseID == "" {
		responseID = "resp_" + uuid.NewString()
	}
	return &Runtime{
		meta:       meta,
		responseID: responseID,
		chatID:     "chatcmpl_" + uuid.NewString(),
		messageID:  "msg_" + uuid.NewString(),
	}
}

func (r *Runtime) Start() []protocol.StreamEvent {
	if r == nil {
		return nil
	}
	switch r.meta.Protocol {
	case protocol.ProtocolChatCompletions:
		return []protocol.StreamEvent{r.chatChunk(map[string]any{"role": "assistant"}, nil)}
	case protocol.ProtocolAnthropicMessages:
		return []protocol.StreamEvent{
			{
				Event: "message_start",
				Data: map[string]any{
					"type": "message_start",
					"message": map[string]any{
						"id":      r.messageID,
						"type":    "message",
						"role":    "assistant",
						"model":   r.meta.Model,
						"content": []any{},
					},
				},
			},
			{
				Event: "ping",
				Data:  map[string]any{"type": "ping"},
			},
		}
	default:
		return []protocol.StreamEvent{
			{
				Event: "response.created",
				Data: map[string]any{
					"type": "response.created",
					"response": map[string]any{
						"id":     r.responseID,
						"object": "response",
						"status": "in_progress",
						"model":  r.meta.Model,
					},
				},
			},
			{
				Event: "response.in_progress",
				Data: map[string]any{
					"type": "response.in_progress",
					"response": map[string]any{
						"id":     r.responseID,
						"object": "response",
						"status": "in_progress",
						"model":  r.meta.Model,
					},
				},
			},
		}
	}
}

func (r *Runtime) Apply(action Action) Result {
	if r == nil {
		return Result{}
	}
	switch action.Kind {
	case ActionDelta:
		return Result{StreamEvents: r.delta(action)}
	case ActionComplete:
		return Result{StreamEvents: r.complete(action)}
	case ActionAbort:
		return Result{StreamEvents: r.abort(action)}
	case ActionBuiltin:
		return Result{StreamEvents: r.builtinTool(action)}
	default:
		return Result{}
	}
}

func (r *Runtime) builtinTool(action Action) []protocol.StreamEvent {
	if r.meta.Protocol != protocol.ProtocolResponses {
		return nil
	}
	switch strings.TrimSpace(action.BuiltinToolKind) {
	case "web_search":
		return r.responsesWebSearch(action)
	case "image_generation":
		return r.responsesImageGeneration(action)
	default:
		return nil
	}
}

func (r *Runtime) delta(action Action) []protocol.StreamEvent {
	switch r.meta.Protocol {
	case protocol.ProtocolChatCompletions:
		if normalizedMode(action.Mode) == "thinking" {
			return r.chatReasoningDelta(action.DeltaText)
		}
		return []protocol.StreamEvent{r.chatChunk(map[string]any{"content": action.DeltaText}, nil)}
	case protocol.ProtocolAnthropicMessages:
		// Human thinking is still text (no signature), but mode/segment switches must
		// close the current block so stream block count matches non-stream content.
		return r.anthropicLogicalTextDelta(action)
	default:
		if normalizedMode(action.Mode) == "thinking" {
			return r.responsesReasoningDelta(action)
		}
		return r.responsesTextDelta(action.DeltaText)
	}
}

func (r *Runtime) complete(action Action) []protocol.StreamEvent {
	switch r.meta.Protocol {
	case protocol.ProtocolChatCompletions:
		return r.completeChat(action)
	case protocol.ProtocolAnthropicMessages:
		return r.completeAnthropic(action)
	default:
		return r.completeResponses(action)
	}
}

func (r *Runtime) abort(action Action) []protocol.StreamEvent {
	switch r.meta.Protocol {
	case protocol.ProtocolChatCompletions:
		return nil
	case protocol.ProtocolAnthropicMessages:
		return []protocol.StreamEvent{{Event: "error", Data: action.ErrorBody}}
	default:
		errorPayload, _ := action.ErrorBody["error"].(map[string]any)
		return []protocol.StreamEvent{{
			Event: "response.failed",
			Data: map[string]any{
				"type": "response.failed",
				"response": map[string]any{
					"id":          r.responseID,
					"object":      "response",
					"status":      "failed",
					"model":       r.meta.Model,
					"output":      []any{},
					"output_text": "",
					"error":       errorPayload,
				},
			},
		}}
	}
}

func (r *Runtime) responsesTextDelta(delta string) []protocol.StreamEvent {
	if delta == "" {
		return nil
	}
	events := r.closeResponsesReasoning()
	if !r.responsesTextPartOpen {
		if r.responsesMessageItemID == "" {
			r.responsesMessageItemID = "msg_" + uuid.NewString()
		}
		events = append(events,
			protocol.StreamEvent{
				Event: "response.output_item.added",
				Data: map[string]any{
					"type":         "response.output_item.added",
					"output_index": r.responsesOutputIndex,
					"item": map[string]any{
						"id":      r.responsesMessageItemID,
						"type":    "message",
						"status":  "in_progress",
						"role":    "assistant",
						"content": []any{},
					},
				},
			},
			protocol.StreamEvent{
				Event: "response.content_part.added",
				Data: map[string]any{
					"type":          "response.content_part.added",
					"item_id":       r.responsesMessageItemID,
					"output_index":  r.responsesOutputIndex,
					"content_index": 0,
					"part":          map[string]any{"type": "output_text", "text": ""},
				},
			},
		)
		r.responsesTextPartOpen = true
	}
	r.responsesText.WriteString(delta)
	events = append(events, protocol.StreamEvent{
		Event: "response.output_text.delta",
		Data: map[string]any{
			"type":          "response.output_text.delta",
			"item_id":       r.responsesMessageItemID,
			"output_index":  r.responsesOutputIndex,
			"content_index": 0,
			"delta":         delta,
		},
	})
	return events
}

func (r *Runtime) responsesReasoningDelta(action Action) []protocol.StreamEvent {
	delta := action.DeltaText
	if delta == "" {
		return nil
	}
	events := r.closeResponsesTextPart()
	if !r.responsesReasoningOpen {
		r.responsesReasoningItemID = "rs_" + uuid.NewString()
		r.responsesReasoningMode = normalizedReasoningMode(action.ReasoningStreamMode)
		r.responsesReasoningOpen = true
		events = append(events, protocol.StreamEvent{
			Event: "response.output_item.added",
			Data: map[string]any{
				"type":         "response.output_item.added",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":      r.responsesReasoningItemID,
					"type":    "reasoning",
					"status":  "in_progress",
					"summary": []any{},
					"content": []any{},
				},
			},
		})
		if r.responsesReasoningMode == "reasoning" {
			events = append(events, protocol.StreamEvent{
				Event: "response.content_part.added",
				Data: map[string]any{
					"type":          "response.content_part.added",
					"item_id":       r.responsesReasoningItemID,
					"output_index":  r.responsesOutputIndex,
					"content_index": 0,
					"part":          map[string]any{"type": "reasoning_text", "text": ""},
				},
			})
		} else {
			events = append(events, protocol.StreamEvent{
				Event: "response.reasoning_summary_part.added",
				Data: map[string]any{
					"type":          "response.reasoning_summary_part.added",
					"item_id":       r.responsesReasoningItemID,
					"output_index":  r.responsesOutputIndex,
					"summary_index": 0,
					"part":          map[string]any{"type": "summary_text", "text": ""},
				},
			})
		}
	}
	if r.responsesReasoningMode == "reasoning" {
		r.responsesReasoningText.WriteString(delta)
		events = append(events, protocol.StreamEvent{
			Event: "response.reasoning_text.delta",
			Data: map[string]any{
				"type":          "response.reasoning_text.delta",
				"item_id":       r.responsesReasoningItemID,
				"output_index":  r.responsesOutputIndex,
				"content_index": 0,
				"delta":         delta,
			},
		})
		return events
	}
	r.responsesReasoningSummary.WriteString(delta)
	events = append(events, protocol.StreamEvent{
		Event: "response.reasoning_summary_text.delta",
		Data: map[string]any{
			"type":          "response.reasoning_summary_text.delta",
			"item_id":       r.responsesReasoningItemID,
			"output_index":  r.responsesOutputIndex,
			"summary_index": 0,
			"delta":         delta,
		},
	})
	return events
}

func (r *Runtime) completeResponses(action Action) []protocol.StreamEvent {
	events := make([]protocol.StreamEvent, 0)
	mode := normalizedMode(action.Mode)
	switch mode {
	case "tool_call":
		events = append(events, r.closeResponsesReasoning()...)
		events = append(events, r.closeResponsesTextPart()...)
		events = append(events, r.responsesToolCall(action)...)
	case "tool_result":
		events = append(events, r.responsesToolResult(action)...)
	case "thinking":
		if action.OutputText != "" {
			events = append(events, r.responsesReasoningDelta(Action{
				Kind:                ActionDelta,
				DeltaText:           action.OutputText,
				Mode:                action.Mode,
				ReasoningStreamMode: action.ReasoningStreamMode,
			})...)
		}
	default:
		if action.OutputText != "" {
			events = append(events, r.responsesTextDelta(action.OutputText)...)
		}
	}
	events = append(events, r.closeResponsesReasoning()...)
	events = append(events, r.closeResponsesTextPart()...)
	eventName := "response.completed"
	status := "completed"
	response := map[string]any{
		"id":          r.responseID,
		"object":      "response",
		"status":      status,
		"model":       r.meta.Model,
		"output_text": r.responsesAnswerAggregate.String(),
		"output":      r.responsesCompletedOutput(action),
		"usage":       responsesUsage(action.OutputTokens),
	}
	outcome := protocol.ResolveCompletionOutcome(action.FinishReason, action.Mode)
	if outcome.ResponsesIncomplete() {
		eventName = "response.incomplete"
		status = "incomplete"
		response["status"] = status
		response["incomplete_details"] = map[string]any{"reason": "max_output_tokens"}
	}
	events = append(events, protocol.StreamEvent{
		Event: eventName,
		Data: map[string]any{
			"type":     eventName,
			"response": response,
		},
	})
	return events
}

func responsesUsage(outputTokens int) map[string]any {
	if outputTokens < 0 {
		outputTokens = 0
	}
	// Interactive output does not currently tokenize the submitted prompt. A
	// zero count is still required because Responses clients treat these fields
	// as mandatory in response.completed.
	return map[string]any{
		"input_tokens":  0,
		"output_tokens": outputTokens,
		"total_tokens":  outputTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens": 0,
		},
		"output_tokens_details": map[string]any{
			"reasoning_tokens": 0,
		},
	}
}

func (r *Runtime) responsesToolCall(action Action) []protocol.StreamEvent {
	callID := strings.TrimSpace(action.ToolCallID)
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	arguments := strings.TrimSpace(action.OutputText)
	r.responsesLastToolCallID = callID
	r.responsesLastToolName = strings.TrimSpace(action.ToolName)
	r.responsesLastToolArgs = arguments
	itemID := "fc_" + uuid.NewString()
	completedItem := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"status":    "completed",
		"name":      action.ToolName,
		"call_id":   callID,
		"arguments": arguments,
	}
	events := []protocol.StreamEvent{
		{
			Event: "response.output_item.added",
			Data: map[string]any{
				"type":         "response.output_item.added",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":        itemID,
					"type":      "function_call",
					"status":    "in_progress",
					"name":      action.ToolName,
					"call_id":   callID,
					"arguments": "",
				},
			},
		},
	}
	if arguments != "" {
		events = append(events, protocol.StreamEvent{
			Event: "response.function_call_arguments.delta",
			Data: map[string]any{
				"type":         "response.function_call_arguments.delta",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
				"delta":        arguments,
			},
		})
	}
	events = append(events,
		protocol.StreamEvent{
			Event: "response.function_call_arguments.done",
			Data: map[string]any{
				"type":         "response.function_call_arguments.done",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
				"arguments":    arguments,
			},
		},
		protocol.StreamEvent{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": r.responsesOutputIndex,
				"item":         completedItem,
			},
		},
	)
	r.responsesClosedOutput = append(r.responsesClosedOutput, completedItem)
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) responsesToolResult(action Action) []protocol.StreamEvent {
	events := make([]protocol.StreamEvent, 0, 4)
	events = append(events, r.closeResponsesReasoning()...)
	events = append(events, r.closeResponsesTextPart()...)

	callID := strings.TrimSpace(action.ToolCallID)
	if callID == "" {
		callID = strings.TrimSpace(r.responsesLastToolCallID)
	}
	if callID == "" {
		callID = "call_" + uuid.NewString()
	}
	r.responsesLastToolCallID = callID

	// Match protocol.BuildResponsesOutput: ToolOutput wins over OutputText.
	output := strings.TrimSpace(action.ToolOutput)
	if output == "" {
		output = strings.TrimSpace(action.OutputText)
	}
	itemID := "fco_" + uuid.NewString()
	completedItem := map[string]any{
		"id":      itemID,
		"type":    "function_call_output",
		"status":  "completed",
		"call_id": callID,
		"output":  output,
	}
	events = append(events,
		protocol.StreamEvent{
			Event: "response.output_item.added",
			Data: map[string]any{
				"type":         "response.output_item.added",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":      itemID,
					"type":    "function_call_output",
					"status":  "in_progress",
					"call_id": callID,
					"output":  "",
				},
			},
		},
		protocol.StreamEvent{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": r.responsesOutputIndex,
				"item":         completedItem,
			},
		},
	)
	r.responsesClosedOutput = append(r.responsesClosedOutput, completedItem)
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) responsesWebSearch(action Action) []protocol.StreamEvent {
	events := make([]protocol.StreamEvent, 0, 5)
	events = append(events, r.closeResponsesReasoning()...)
	events = append(events, r.closeResponsesTextPart()...)
	itemID := "ws_" + uuid.NewString()
	events = append(events,
		protocol.StreamEvent{
			Event: "response.output_item.added",
			Data: map[string]any{
				"type":         "response.output_item.added",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":     itemID,
					"type":   "web_search_call",
					"status": "in_progress",
				},
			},
		},
		protocol.StreamEvent{
			Event: "response.web_search_call.in_progress",
			Data: map[string]any{
				"type":         "response.web_search_call.in_progress",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.web_search_call.searching",
			Data: map[string]any{
				"type":         "response.web_search_call.searching",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.web_search_call.completed",
			Data: map[string]any{
				"type":         "response.web_search_call.completed",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":     itemID,
					"type":   "web_search_call",
					"status": "completed",
				},
			},
		},
	)
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) responsesImageGeneration(action Action) []protocol.StreamEvent {
	events := make([]protocol.StreamEvent, 0, 5)
	events = append(events, r.closeResponsesReasoning()...)
	events = append(events, r.closeResponsesTextPart()...)
	itemID := "ig_" + uuid.NewString()
	result := strings.TrimSpace(action.BuiltinToolResult)
	events = append(events,
		protocol.StreamEvent{
			Event: "response.output_item.added",
			Data: map[string]any{
				"type":         "response.output_item.added",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":     itemID,
					"type":   "image_generation_call",
					"status": "in_progress",
				},
			},
		},
		protocol.StreamEvent{
			Event: "response.image_generation_call.in_progress",
			Data: map[string]any{
				"type":         "response.image_generation_call.in_progress",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.image_generation_call.generating",
			Data: map[string]any{
				"type":         "response.image_generation_call.generating",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.image_generation_call.completed",
			Data: map[string]any{
				"type":         "response.image_generation_call.completed",
				"item_id":      itemID,
				"output_index": r.responsesOutputIndex,
			},
		},
		protocol.StreamEvent{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": r.responsesOutputIndex,
				"item": map[string]any{
					"id":     itemID,
					"type":   "image_generation_call",
					"status": "completed",
					"result": result,
				},
			},
		},
	)
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) closeResponsesTextPart() []protocol.StreamEvent {
	if !r.responsesTextPartOpen {
		return nil
	}
	// Segment-level close: done text is this segment only. Response-level aggregate
	// keeps every answer segment so response.output_text stays the full join.
	text := r.responsesText.String()
	itemID := r.responsesMessageItemID
	outputIndex := r.responsesOutputIndex
	r.responsesTextPartOpen = false
	r.responsesText.Reset()
	r.responsesMessageItemID = ""
	r.responsesAnswerAggregate.WriteString(text)
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"status": "completed",
		"role":   "assistant",
		"content": []map[string]any{{
			"type": "output_text",
			"text": text,
		}},
	}
	r.responsesClosedOutput = append(r.responsesClosedOutput, item)
	events := []protocol.StreamEvent{
		{
			Event: "response.output_text.done",
			Data: map[string]any{
				"type":          "response.output_text.done",
				"item_id":       itemID,
				"output_index":  outputIndex,
				"content_index": 0,
				"text":          text,
			},
		},
		{
			Event: "response.content_part.done",
			Data: map[string]any{
				"type":          "response.content_part.done",
				"item_id":       itemID,
				"output_index":  outputIndex,
				"content_index": 0,
				"part":          map[string]any{"type": "output_text", "text": text},
			},
		},
		{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": outputIndex,
				"item":         item,
			},
		},
	}
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) closeResponsesReasoning() []protocol.StreamEvent {
	if !r.responsesReasoningOpen {
		return nil
	}
	// Segment-level close: each reasoning item is independent. Reset builders so the
	// next thinking segment cannot accumulate prior text or reuse the item id.
	itemID := r.responsesReasoningItemID
	mode := r.responsesReasoningMode
	outputIndex := r.responsesOutputIndex
	r.responsesReasoningOpen = false
	r.responsesReasoningItemID = ""
	r.responsesReasoningMode = ""
	if mode == "reasoning" {
		text := r.responsesReasoningText.String()
		r.responsesReasoningText.Reset()
		r.responsesReasoningSummary.Reset()
		item := map[string]any{
			"id":      itemID,
			"type":    "reasoning",
			"status":  "completed",
			"summary": []any{},
			"content": []map[string]any{{"type": "reasoning_text", "text": text}},
		}
		r.responsesClosedOutput = append(r.responsesClosedOutput, item)
		events := []protocol.StreamEvent{
			{
				Event: "response.reasoning_text.done",
				Data: map[string]any{
					"type":          "response.reasoning_text.done",
					"item_id":       itemID,
					"output_index":  outputIndex,
					"content_index": 0,
					"text":          text,
				},
			},
			{
				Event: "response.content_part.done",
				Data: map[string]any{
					"type":          "response.content_part.done",
					"item_id":       itemID,
					"output_index":  outputIndex,
					"content_index": 0,
					"part":          map[string]any{"type": "reasoning_text", "text": text},
				},
			},
			{
				Event: "response.output_item.done",
				Data: map[string]any{
					"type":         "response.output_item.done",
					"output_index": outputIndex,
					"item":         item,
				},
			},
		}
		r.responsesOutputIndex++
		return events
	}
	text := r.responsesReasoningSummary.String()
	r.responsesReasoningText.Reset()
	r.responsesReasoningSummary.Reset()
	item := map[string]any{
		"id":      itemID,
		"type":    "reasoning",
		"status":  "completed",
		"summary": []map[string]any{{"type": "summary_text", "text": text}},
		"content": []any{},
	}
	r.responsesClosedOutput = append(r.responsesClosedOutput, item)
	events := []protocol.StreamEvent{
		{
			Event: "response.reasoning_summary_text.done",
			Data: map[string]any{
				"type":          "response.reasoning_summary_text.done",
				"item_id":       itemID,
				"output_index":  outputIndex,
				"summary_index": 0,
				"text":          text,
			},
		},
		{
			Event: "response.reasoning_summary_part.done",
			Data: map[string]any{
				"type":          "response.reasoning_summary_part.done",
				"item_id":       itemID,
				"output_index":  outputIndex,
				"summary_index": 0,
				"part":          map[string]any{"type": "summary_text", "text": text},
			},
		},
		{
			Event: "response.output_item.done",
			Data: map[string]any{
				"type":         "response.output_item.done",
				"output_index": outputIndex,
				"item":         item,
			},
		},
	}
	r.responsesOutputIndex++
	return events
}

func (r *Runtime) responsesCompletedOutput(action Action) []map[string]any {
	// Prefer durable ordered segments when present so stream completed.output matches
	// non-stream BuildResponseForMeta / buildResponsesOutput field-for-field.
	// Tool modes without OutputSegments fall through to the live ledger below so
	// previously closed answer/reasoning items stay ordered with function_call(s).
	if len(action.OutputSegments) > 0 {
		return protocol.BuildResponsesOutput(protocol.TurnResult{
			Mode:           action.Mode,
			OutputText:     action.OutputText,
			ToolName:       action.ToolName,
			ToolCallID:     action.ToolCallID,
			ToolOutput:     action.ToolOutput,
			OutputSegments: action.OutputSegments,
		})
	}
	// Live stream ledger: closed text/reasoning/tool items in output_index order.
	if len(r.responsesClosedOutput) > 0 {
		output := make([]map[string]any, len(r.responsesClosedOutput))
		copy(output, r.responsesClosedOutput)
		return output
	}
	// Last-resort rebuild when complete arrived without any prior streamed items.
	switch normalizedMode(action.Mode) {
	case "tool_call":
		callID := strings.TrimSpace(action.ToolCallID)
		if callID == "" {
			callID = r.responsesLastToolCallID
		}
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		toolName := nonEmpty(strings.TrimSpace(action.ToolName), r.responsesLastToolName)
		arguments := nonEmpty(strings.TrimSpace(action.OutputText), r.responsesLastToolArgs)
		return []map[string]any{{
			"type":      "function_call",
			"name":      toolName,
			"call_id":   callID,
			"arguments": arguments,
		}}
	case "tool_result":
		callID := strings.TrimSpace(action.ToolCallID)
		if callID == "" {
			callID = r.responsesLastToolCallID
		}
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		output := strings.TrimSpace(action.ToolOutput)
		if output == "" {
			output = strings.TrimSpace(action.OutputText)
		}
		return []map[string]any{{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  output,
		}}
	}
	return []map[string]any{}
}

func (r *Runtime) completeChat(action Action) []protocol.StreamEvent {
	outcome := protocol.ResolveCompletionOutcome(action.FinishReason, action.Mode)
	if normalizedMode(action.Mode) == "tool_call" {
		callID := strings.TrimSpace(action.ToolCallID)
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		toolCall := map[string]any{
			"index": 0,
			"id":    callID,
			"type":  "function",
			"function": map[string]any{
				"name":      strings.TrimSpace(action.ToolName),
				"arguments": strings.TrimSpace(action.OutputText),
			},
		}
		return []protocol.StreamEvent{
			r.chatChunk(map[string]any{"tool_calls": []map[string]any{toolCall}}, nil),
			r.chatChunk(map[string]any{}, stringPtr(outcome.ChatFinishReason())),
			{Data: "[DONE]", Done: true},
		}
	}
	events := make([]protocol.StreamEvent, 0, 4)
	mode := normalizedMode(action.Mode)
	// Only emit unsent complete-time text. Thinking/answer deltas already streamed
	// are tracked in builders and must not be repeated here.
	if mode == "thinking" && action.OutputText != "" {
		events = append(events, r.chatReasoningDelta(action.OutputText)...)
	} else if mode != "thinking" && action.OutputText != "" {
		events = append(events, r.chatChunk(map[string]any{"content": action.OutputText}, nil))
	}
	events = append(events, r.chatChunk(map[string]any{}, stringPtr(outcome.ChatFinishReason())), protocol.StreamEvent{Data: "[DONE]", Done: true})
	return events
}

func (r *Runtime) chatReasoningDelta(delta string) []protocol.StreamEvent {
	if delta == "" {
		return nil
	}
	r.chatReasoningText.WriteString(delta)
	return []protocol.StreamEvent{r.chatChunk(map[string]any{"reasoning_content": delta}, nil)}
}

func (r *Runtime) chatChunk(delta map[string]any, finishReason *string) protocol.StreamEvent {
	choice := map[string]any{
		"index": 0,
		"delta": delta,
	}
	if finishReason != nil {
		choice["finish_reason"] = *finishReason
	}
	return protocol.StreamEvent{Data: map[string]any{
		"id":      r.chatID,
		"object":  "chat.completion.chunk",
		"created": 0,
		"model":   r.meta.Model,
		"choices": []map[string]any{choice},
	}}
}

func (r *Runtime) anthropicLogicalTextDelta(action Action) []protocol.StreamEvent {
	delta := action.DeltaText
	if delta == "" {
		return nil
	}
	// Logical segment identity, not Anthropic thinking type. Same mode continues
	// the current text block; mode changes force close/open so one block maps to
	// one durable segment (and to non-stream content[] entries).
	blockKey := "answer"
	if normalizedMode(action.Mode) == "thinking" {
		blockKey = "thinking"
	}
	events := r.ensureAnthropicBlock(blockKey, map[string]any{"type": "text", "text": ""})
	r.anthropicText.WriteString(delta)
	events = append(events, protocol.StreamEvent{
		Event: "content_block_delta",
		Data: map[string]any{
			"type":  "content_block_delta",
			"index": r.anthropicBlockIndex,
			"delta": map[string]any{"type": "text_delta", "text": delta},
		},
	})
	return events
}

func (r *Runtime) completeAnthropic(action Action) []protocol.StreamEvent {
	events := make([]protocol.StreamEvent, 0)
	mode := normalizedMode(action.Mode)
	outcome := protocol.ResolveCompletionOutcome(action.FinishReason, action.Mode)
	if mode == "tool_call" {
		events = append(events, r.closeAnthropicBlock()...)
		events = append(events, r.anthropicToolUse(action)...)
		return append(events, r.anthropicStop(outcome.AnthropicStopReason(), action.StopSequence, action.OutputTokens)...)
	}
	if action.OutputText != "" {
		events = append(events, r.anthropicLogicalTextDelta(Action{
			Kind:      ActionDelta,
			DeltaText: action.OutputText,
			Mode:      action.Mode,
		})...)
	}
	events = append(events, r.closeAnthropicBlock()...)
	return append(events, r.anthropicStop(outcome.AnthropicStopReason(), action.StopSequence, action.OutputTokens)...)
}

func (r *Runtime) ensureAnthropicBlock(blockType string, block map[string]any) []protocol.StreamEvent {
	if r.anthropicBlockType == blockType {
		return nil
	}
	events := r.closeAnthropicBlock()
	r.anthropicBlockType = blockType
	events = append(events, protocol.StreamEvent{
		Event: "content_block_start",
		Data: map[string]any{
			"type":          "content_block_start",
			"index":         r.anthropicBlockIndex,
			"content_block": block,
		},
	})
	return events
}

func (r *Runtime) closeAnthropicBlock() []protocol.StreamEvent {
	if r.anthropicBlockType == "" {
		return nil
	}
	index := r.anthropicBlockIndex
	r.anthropicBlockIndex++
	r.anthropicBlockType = ""
	return []protocol.StreamEvent{{
		Event: "content_block_stop",
		Data:  map[string]any{"type": "content_block_stop", "index": index},
	}}
}

func (r *Runtime) anthropicToolUse(action Action) []protocol.StreamEvent {
	callID := strings.TrimSpace(action.ToolCallID)
	if callID == "" {
		callID = "toolu_" + uuid.NewString()
	}
	arguments := strings.TrimSpace(action.OutputText)
	index := r.anthropicBlockIndex
	r.anthropicBlockIndex++
	events := []protocol.StreamEvent{
		{
			Event: "content_block_start",
			Data: map[string]any{
				"type":  "content_block_start",
				"index": index,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    callID,
					"name":  strings.TrimSpace(action.ToolName),
					"input": map[string]any{},
				},
			},
		},
	}
	if arguments != "" {
		events = append(events, protocol.StreamEvent{
			Event: "content_block_delta",
			Data: map[string]any{
				"type":  "content_block_delta",
				"index": index,
				"delta": map[string]any{
					"type":         "input_json_delta",
					"partial_json": arguments,
				},
			},
		})
	}
	events = append(events, protocol.StreamEvent{
		Event: "content_block_stop",
		Data:  map[string]any{"type": "content_block_stop", "index": index},
	})
	return events
}

func (r *Runtime) anthropicStop(reason string, stopSequence string, outputTokens int) []protocol.StreamEvent {
	var stopValue any
	if strings.TrimSpace(stopSequence) != "" {
		stopValue = stopSequence
	}
	return []protocol.StreamEvent{
		{
			Event: "message_delta",
			Data: map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": reason, "stop_sequence": stopValue},
				"usage": map[string]any{"output_tokens": outputTokens},
			},
		},
		{
			Event: "message_stop",
			Data:  map[string]any{"type": "message_stop"},
		},
	}
}

func normalizedMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "thinking":
		return "thinking"
	case "tool_call":
		return "tool_call"
	case "tool_result":
		return "tool_result"
	default:
		return "assistant_message"
	}
}

func normalizedReasoningMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "reasoning", "reasoning_text":
		return "reasoning"
	default:
		return "summary"
	}
}

func stringPtr(value string) *string {
	return &value
}

func nonEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func parseJSON(raw string) any {
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	return value
}
