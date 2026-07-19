package conversationstate

import (
	"reflect"
	"testing"

	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

func TestDecodeOutputSegmentsSupportsJSONAndTypedValues(t *testing.T) {
	want := []common.OutputSegment{
		{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
		{Mode: "answer", Text: "beta"},
	}
	jsonValue := []any{
		map[string]any{"mode": "thinking", "text": "alpha", "reasoning_stream_mode": "summary"},
		map[string]any{"mode": "answer", "text": "beta"},
	}
	if got := DecodeOutputSegments(jsonValue); !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON metadata decode mismatch: got=%#v want=%#v", got, want)
	}
	if got := DecodeOutputSegments(want); !reflect.DeepEqual(got, want) {
		t.Fatalf("typed metadata decode mismatch: got=%#v want=%#v", got, want)
	}
	// Accidental exported-field keys from pre-tag marshal must still decode.
	exportedKeys := []any{
		map[string]any{"Mode": "thinking", "Text": "alpha", "ReasoningStreamMode": "summary"},
		map[string]any{"Mode": "answer", "Text": "beta"},
	}
	if got := DecodeOutputSegments(exportedKeys); !reflect.DeepEqual(got, want) {
		t.Fatalf("exported-key metadata decode mismatch: got=%#v want=%#v", got, want)
	}
}

func TestContentAndPreviewFromSegments(t *testing.T) {
	segments := []common.OutputSegment{
		{Mode: "thinking", Text: "alpha"},
		{Mode: "answer", Text: "show <think>literal</think>"},
		{Mode: "thinking", Text: "gamma"},
		{Mode: "answer", Text: "delta"},
	}
	if got := ContentFromSegments(segments); got != "show <think>literal</think>delta" {
		t.Fatalf("content contract changed: %q", got)
	}
	if got := PreviewFromSegments(segments); got != "show <think>literal</think>delta" {
		t.Fatalf("preview should prefer answer: %q", got)
	}
	thinkingOnly := []common.OutputSegment{{Mode: "thinking", Text: "only reason"}}
	if got := ContentFromSegments(thinkingOnly); got != "" {
		t.Fatalf("thinking-only content must be empty: %q", got)
	}
	if got := PreviewFromSegments(thinkingOnly); got != "only reason" {
		t.Fatalf("thinking-only preview should fall back to thinking text: %q", got)
	}
}

func TestFromConversationKeepsOutputSegmentOrderWithoutParsingText(t *testing.T) {
	conversation := common.Conversation{Metadata: map[string]any{
		"realtime_output_segments": []any{
			map[string]any{"mode": "answer", "text": "show <think>literal</think>"},
			map[string]any{"mode": "thinking", "text": "alpha"},
			map[string]any{"mode": "answer", "text": "beta"},
		},
	}}
	state := FromConversation(conversation)
	if len(state.OutputSegments) != 3 || state.OutputSegments[0].Text != "show <think>literal</think>" || state.OutputSegments[2].Text != "beta" {
		t.Fatalf("segment order or literal text changed: %#v", state.OutputSegments)
	}
}
