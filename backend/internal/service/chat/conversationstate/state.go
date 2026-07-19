package conversationstate

import (
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
)

type Status string

const (
	StatusWaiting      Status = "waiting"
	StatusStreaming    Status = "streaming"
	StatusClosed       Status = "closed"
	StatusAborted      Status = "aborted"
	StatusDisconnected Status = "disconnected"
	StatusExpired      Status = "expired"
)

type Runtime struct {
	OwnerID        string
	RequestID      string
	RequestFormat  string
	Model          string
	Status         Status
	DraftText      string
	OutputSegments []common.OutputSegment
}

type Summary struct {
	ID                 string                 `json:"id"`
	Title              string                 `json:"title"`
	LastUserText       string                 `json:"last_user_text"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
	LastMessageAt      time.Time              `json:"last_message_at"`
	MessageCount       int                    `json:"message_count"`
	LastMessagePreview string                 `json:"last_message_preview"`
	RequestFormat      string                 `json:"request_format,omitempty"`
	RequestID          string                 `json:"request_id,omitempty"`
	Status             Status                 `json:"status,omitempty"`
	DraftText          string                 `json:"draft_text,omitempty"`
	DraftSegments      []common.OutputSegment `json:"draft_output_segments,omitempty"`
}

func FromConversation(conversation common.Conversation) Runtime {
	return Runtime{
		OwnerID:        metadataString(conversation.Metadata, "owner_id"),
		RequestID:      metadataString(conversation.Metadata, "request_id"),
		RequestFormat:  metadataString(conversation.Metadata, "request_format"),
		Model:          metadataString(conversation.Metadata, "model"),
		Status:         Status(metadataString(conversation.Metadata, "realtime_status")),
		DraftText:      metadataText(conversation.Metadata, "realtime_draft_text"),
		OutputSegments: DecodeOutputSegments(conversation.Metadata["realtime_output_segments"]),
	}
}

func SummaryFromConversation(conversation common.Conversation) Summary {
	runtime := FromConversation(conversation)
	return Summary{
		ID: conversation.ID, Title: conversation.Title, LastUserText: conversation.LastUserText,
		CreatedAt: conversation.CreatedAt, UpdatedAt: conversation.UpdatedAt, LastMessageAt: conversation.LastMessageAt,
		MessageCount: conversation.MessageCount, LastMessagePreview: conversation.LastMessagePreview,
		RequestFormat: runtime.RequestFormat, RequestID: runtime.RequestID, Status: runtime.Status,
		// DraftText stays answer-only; DraftSegments is the ordered live truth for the workspace.
		DraftText: runtime.DraftText, DraftSegments: runtime.OutputSegments,
	}
}

func RequestID(conversation common.Conversation) string {
	return FromConversation(conversation).RequestID
}
func OwnerID(conversation common.Conversation) string { return FromConversation(conversation).OwnerID }

func RequestFormat(conversation common.Conversation) string {
	value := RequestFormatRaw(conversation)
	if value == "" {
		return string(protocol.ProtocolResponses)
	}
	return value
}

func RequestFormatRaw(conversation common.Conversation) string {
	return FromConversation(conversation).RequestFormat
}

func Model(conversation common.Conversation, fallback string) string {
	value := FromConversation(conversation).Model
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func IsPendingStatus(status Status) bool { return status == StatusWaiting || status == StatusStreaming }

func metadataString(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func metadataText(metadata map[string]any, key string) string {
	if len(metadata) == 0 {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}

// DecodeOutputSegments is the single boundary codec for JSON metadata and
// in-memory values. It intentionally does not inspect or parse segment text.
// Keys are accepted in both canonical snake/lowercase form and the accidental
// exported-field form produced before OutputSegment had JSON tags.
func DecodeOutputSegments(raw any) []common.OutputSegment {
	switch typed := raw.(type) {
	case []common.OutputSegment:
		return append([]common.OutputSegment(nil), typed...)
	case []map[string]any:
		return decodeOutputSegmentMaps(typed)
	case []any:
		items := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			value, ok := item.(map[string]any)
			if ok {
				items = append(items, value)
			}
		}
		return decodeOutputSegmentMaps(items)
	default:
		return nil
	}
}

func decodeOutputSegmentMaps(items []map[string]any) []common.OutputSegment {
	segments := make([]common.OutputSegment, 0, len(items))
	for _, value := range items {
		mode := segmentString(value, "mode", "Mode")
		text := segmentString(value, "text", "Text")
		reasoning := segmentString(value, "reasoning_stream_mode", "ReasoningStreamMode")
		if text == "" {
			continue
		}
		segments = append(segments, common.OutputSegment{
			Mode:                mode,
			Text:                text,
			ReasoningStreamMode: reasoning,
		})
	}
	return segments
}

func segmentString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		raw, ok := value[key]
		if !ok || raw == nil {
			continue
		}
		if typed, ok := raw.(string); ok {
			return typed
		}
	}
	return ""
}

// SegmentMode normalizes durable segment modes. Unknown values collapse to answer
// so Content/protocol builders never invent a third content class.
func SegmentMode(mode string) string {
	if strings.TrimSpace(mode) == "thinking" {
		return "thinking"
	}
	return "answer"
}

// ContentFromSegments is the message.Content contract for new structured turns:
// answer text only, in order. Thinking never enters Content, including thinking-only
// turns which deliberately persist an empty Content and keep facts in output_segments.
func ContentFromSegments(segments []common.OutputSegment) string {
	var output strings.Builder
	for _, segment := range segments {
		if SegmentMode(segment.Mode) != "answer" || segment.Text == "" {
			continue
		}
		output.WriteString(segment.Text)
	}
	return output.String()
}

// PreviewFromSegments builds sidebar/notification preview text.
// Prefer answer; only fall back to thinking text when the turn has no answer so a
// thinking-only completion remains discoverable without pretending it is an answer.
func PreviewFromSegments(segments []common.OutputSegment) string {
	if answer := ContentFromSegments(segments); answer != "" {
		return answer
	}
	var thinking strings.Builder
	for _, segment := range segments {
		if SegmentMode(segment.Mode) == "thinking" && segment.Text != "" {
			thinking.WriteString(segment.Text)
		}
	}
	return thinking.String()
}
