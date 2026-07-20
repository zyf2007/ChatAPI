package workspace

import (
	"testing"
	"time"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
)

func TestTimelineItemProjectsGeneratedImageURL(t *testing.T) {
	event := common.ConversationEvent{
		ID: "evt_image", Type: "builtin_tool",
		MediaAssets: []common.EventMediaAssetRef{{URL: "/api/media/assets/file_image", MediaType: "image/avif"}},
	}
	item := TimelineItemFromRaw(timelinesvc.ItemFromConversationEvent(event))
	if len(item.ContentParts) != 1 || item.ContentParts[0].Src != "/api/media/assets/file_image" || item.ContentParts[0].MediaType != "image/avif" {
		t.Fatalf("unexpected generated image projection: %#v", item)
	}
}

func TestBuildMessageContentPartsLegacyHistoryOmitsParts(t *testing.T) {
	// Historical ordinary messages without output_segments must omit content_parts so
	// the frontend can run parseRenderableContent / splitLegacyThinkingBlocks.
	message := common.Message{
		ID: "msg_legacy", Role: "assistant",
		Content:   "before <think>hidden</think> after",
		CreatedAt: time.Now().UTC(),
		Metadata:  map[string]any{},
	}
	parts := buildMessageContentParts(message)
	if parts != nil {
		t.Fatalf("legacy history without segments must omit content_parts, got %#v", parts)
	}
}

func TestBuildMessageContentPartsTypedAnswerKeepsLiteralThinkTags(t *testing.T) {
	message := common.Message{
		ID: "msg_typed", Role: "assistant",
		Content: "show <think>literal</think>",
		Metadata: map[string]any{
			"output_segments": []common.OutputSegment{
				{Mode: "answer", Text: "show <think>literal</think>"},
			},
		},
	}
	parts := buildMessageContentParts(message)
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "show <think>literal</think>" {
		t.Fatalf("typed answer literal <think> must stay plain text: %#v", parts)
	}
}

func TestBuildMessageContentPartsThinkingOnlyTypedSegments(t *testing.T) {
	message := common.Message{
		ID: "msg_think_only", Role: "assistant", Content: "",
		Metadata: map[string]any{
			"output_segments": []common.OutputSegment{
				{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
			},
		},
	}
	parts := buildMessageContentParts(message)
	if len(parts) != 1 || parts[0].Type != "thinking" || parts[0].Text != "alpha" || parts[0].ReasoningStreamMode != "summary" {
		t.Fatalf("thinking-only typed segments must project thinking part: %#v", parts)
	}
}

func TestBuildMessageContentPartsIgnoresDirtyToolSegments(t *testing.T) {
	// Tool payloads may literally contain <think> tags; Content is authoritative typed text.
	const toolContent = `{"query":"<think>literal</think>"}`
	for _, mode := range []string{"tool_call", "tool_result"} {
		t.Run(mode, func(t *testing.T) {
			message := common.Message{
				ID: "msg_tool", Role: "assistant",
				Content: toolContent,
				Metadata: map[string]any{
					"response_mode": mode,
					"output_segments": []common.OutputSegment{
						{Mode: "thinking", Text: "stale draft"},
						{Mode: "answer", Text: "stale answer"},
					},
					"arguments": toolContent,
					"output":    toolContent,
				},
			}
			parts := buildMessageContentParts(message)
			if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != toolContent {
				t.Fatalf("tool response_mode must project Content as single text part, got %#v", parts)
			}
			item := TimelineItemFromRaw(timelinesvc.Item{
				ID: message.ID, Kind: "message", CreatedAt: time.Now().UTC(),
				Message: &message,
			})
			if item.Message == nil || item.Message.Content != toolContent {
				t.Fatalf("tool Content must remain identical: %#v", item.Message)
			}
			if len(item.Message.ContentParts) != 1 ||
				item.Message.ContentParts[0].Type != "text" ||
				item.Message.ContentParts[0].Text != toolContent {
				t.Fatalf("tool content_parts must be authoritative typed Content: %#v", item.Message.ContentParts)
			}
		})
	}
	t.Run("empty_content", func(t *testing.T) {
		message := common.Message{
			ID: "msg_tool_empty", Role: "assistant", Content: "",
			Metadata: map[string]any{
				"response_mode": "tool_call",
				"output_segments": []common.OutputSegment{
					{Mode: "thinking", Text: "stale draft"},
				},
			},
		}
		parts := buildMessageContentParts(message)
		if parts != nil {
			t.Fatalf("empty tool Content must omit content_parts, got %#v", parts)
		}
	})
}

func TestBuildMessageContentPartsKeepsRequestBodyMultimodal(t *testing.T) {
	message := common.Message{
		ID: "msg_user", Role: "user", Content: "caption",
		Metadata: map[string]any{
			"request_debug": map[string]any{
				"request_format": "responses",
				"request_body": map[string]any{
					"model": "gpt-test",
					"input": []any{
						map[string]any{"type": "input_text", "text": "caption"},
						map[string]any{"type": "input_image", "image_url": "https://example.com/a.png", "media_type": "image/png"},
					},
				},
			},
		},
	}
	parts := buildMessageContentParts(message)
	if len(parts) != 2 || parts[0].Type != "text" || parts[0].Text != "caption" || parts[1].Type != "image" || parts[1].Src != "https://example.com/a.png" {
		t.Fatalf("request_body multimodal path changed: %#v", parts)
	}
}
