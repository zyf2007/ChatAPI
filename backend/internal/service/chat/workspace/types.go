package workspace

import (
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
)

type ConversationSummary struct {
	ID                 string                       `json:"id"`
	Title              string                       `json:"title"`
	LastUserText       string                       `json:"last_user_text"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	LastMessageAt      time.Time                    `json:"last_message_at"`
	MessageCount       int                          `json:"message_count"`
	LastMessagePreview string                       `json:"last_message_preview"`
	RequestFormat      string                       `json:"request_format,omitempty"`
	RequestID          string                       `json:"request_id,omitempty"`
	Status             conversationstate.Status     `json:"status,omitempty"`
	DraftText          string                       `json:"draft_text,omitempty"`
	DraftSegments      []TimelineMessageContentPart `json:"draft_output_segments,omitempty"`
}

type TimelineMessageContentPart struct {
	Type                string `json:"type"`
	Text                string `json:"text,omitempty"`
	Src                 string `json:"src,omitempty"`
	MediaType           string `json:"media_type,omitempty"`
	ReasoningStreamMode string `json:"reasoning_stream_mode,omitempty"`
}

type TimelineMessage struct {
	ID           string                       `json:"id"`
	Role         string                       `json:"role"`
	Content      string                       `json:"content"`
	ContentParts []TimelineMessageContentPart `json:"content_parts,omitempty"`
	CreatedAt    time.Time                    `json:"created_at"`
	Status       string                       `json:"status,omitempty"`
	ResponseID   *string                      `json:"response_id,omitempty"`
	Metadata     map[string]any               `json:"metadata,omitempty"`
}

type TimelineItem struct {
	ID           string                       `json:"id"`
	Kind         string                       `json:"kind"`
	CreatedAt    time.Time                    `json:"created_at"`
	Message      *TimelineMessage             `json:"message,omitempty"`
	Event        *common.ConversationEvent    `json:"event,omitempty"`
	ContentParts []TimelineMessageContentPart `json:"content_parts,omitempty"`
}

type Command struct {
	ID                  string `json:"command_id"`
	Kind                string `json:"kind"`
	ConversationID      string `json:"conversation_id"`
	RequestID           string `json:"request_id"`
	Text                string `json:"text,omitempty"`
	Mode                string `json:"mode,omitempty"`
	ToolName            string `json:"tool_name,omitempty"`
	ToolCallID          string `json:"tool_call_id,omitempty"`
	Output              string `json:"output,omitempty"`
	BuiltinToolKind     string `json:"builtin_tool_kind,omitempty"`
	BuiltinToolQuery    string `json:"builtin_tool_query,omitempty"`
	BuiltinToolAssetID  string `json:"builtin_tool_asset_id,omitempty"`
	ReasoningStreamMode string `json:"reasoning_stream_mode,omitempty"`
	Error               string `json:"error,omitempty"`
}

type CommandAck struct {
	Type           string `json:"type"`
	CommandID      string `json:"command_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id"`
	AutoCompleted  bool   `json:"auto_completed,omitempty"`
}

type CommandError struct {
	Type           string `json:"type"`
	CommandID      string `json:"command_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	RequestID      string `json:"request_id,omitempty"`
	Code           string `json:"code"`
	Message        string `json:"message"`
}

func draftPartsFromSegments(segments []common.OutputSegment) []TimelineMessageContentPart {
	if len(segments) == 0 {
		return nil
	}
	parts := make([]TimelineMessageContentPart, 0, len(segments))
	for _, segment := range segments {
		if segment.Text == "" {
			continue
		}
		partType := "text"
		if conversationstate.SegmentMode(segment.Mode) == "thinking" {
			partType = "thinking"
		}
		parts = append(parts, TimelineMessageContentPart{
			Type:                partType,
			Text:                segment.Text,
			ReasoningStreamMode: segment.ReasoningStreamMode,
		})
	}
	return parts
}

func SummaryFromConversation(conversation common.Conversation) ConversationSummary {
	summary := conversationstate.SummaryFromConversation(conversation)
	return ConversationSummary{
		ID:                 summary.ID,
		Title:              summary.Title,
		LastUserText:       summary.LastUserText,
		CreatedAt:          summary.CreatedAt,
		UpdatedAt:          summary.UpdatedAt,
		LastMessageAt:      summary.LastMessageAt,
		MessageCount:       summary.MessageCount,
		LastMessagePreview: summary.LastMessagePreview,
		RequestFormat:      summary.RequestFormat,
		RequestID:          summary.RequestID,
		Status:             summary.Status,
		DraftText:          summary.DraftText,
		DraftSegments:      draftPartsFromSegments(summary.DraftSegments),
	}
}

func TimelineItemFromRaw(item timelinesvc.Item) TimelineItem {
	out := TimelineItem{
		ID:        item.ID,
		Kind:      item.Kind,
		CreatedAt: item.CreatedAt,
		Event:     item.Event,
	}
	if item.Event != nil {
		out.ContentParts = buildEventContentParts(*item.Event)
	}
	if item.Message != nil {
		out.Message = &TimelineMessage{
			ID:           item.Message.ID,
			Role:         item.Message.Role,
			Content:      item.Message.Content,
			ContentParts: buildMessageContentParts(*item.Message),
			CreatedAt:    item.Message.CreatedAt,
			Status:       item.Message.Status,
			ResponseID:   item.Message.ResponseID,
			Metadata:     item.Message.Metadata,
		}
	}
	return out
}

func buildEventContentParts(event common.ConversationEvent) []TimelineMessageContentPart {
	if strings.TrimSpace(event.Type) != "builtin_tool" {
		return nil
	}
	parts := make([]TimelineMessageContentPart, 0, len(event.MediaAssets))
	for _, asset := range event.MediaAssets {
		url := strings.TrimSpace(asset.URL)
		if url == "" {
			continue
		}
		parts = append(parts, TimelineMessageContentPart{
			Type: "image", Src: url, MediaType: strings.TrimSpace(asset.MediaType),
		})
	}
	return parts
}

func buildMessageContentParts(message common.Message) []TimelineMessageContentPart {
	// User-side request reconstruction still wins for request messages with images.
	requestDebug, _ := message.Metadata["request_debug"].(map[string]any)
	requestBody, _ := requestDebug["request_body"].(map[string]any)
	requestFormat, _ := requestDebug["request_format"].(string)
	parts := partsFromRequestBody(strings.TrimSpace(requestFormat), requestBody)
	if len(parts) != 0 {
		return parts
	}
	// Tool turns own tool payload fields, not ordinary answer/thinking segments.
	// Historical dirty tool rows may still carry leftover output_segments; ignore them.
	// Content is the authoritative typed text so the frontend never legacy-parses
	// tool arguments/output that may literally contain <think>...</think>.
	if isToolResponseMode(message.Metadata) {
		if message.Content == "" {
			return nil
		}
		return []TimelineMessageContentPart{{Type: "text", Text: message.Content}}
	}
	// New assistant turns expose typed segments so the workspace never re-parses
	// tags from Content. Historical messages without segments omit content_parts so
	// the frontend can apply legacy <think> fallback from Content only in that case.
	if segments := conversationstate.DecodeOutputSegments(message.Metadata["output_segments"]); len(segments) > 0 {
		typed := make([]TimelineMessageContentPart, 0, len(segments))
		for _, segment := range segments {
			if segment.Text == "" {
				continue
			}
			partType := "text"
			if conversationstate.SegmentMode(segment.Mode) == "thinking" {
				partType = "thinking"
			}
			typed = append(typed, TimelineMessageContentPart{
				Type:                partType,
				Text:                segment.Text,
				ReasoningStreamMode: segment.ReasoningStreamMode,
			})
		}
		if len(typed) > 0 {
			return typed
		}
	}
	// No authoritative typed segments: omit content_parts entirely. Fabricating a
	// single text part would short-circuit frontend legacy <think> parsing.
	return nil
}

func isToolResponseMode(metadata map[string]any) bool {
	if len(metadata) == 0 {
		return false
	}
	mode, _ := metadata["response_mode"].(string)
	switch strings.TrimSpace(mode) {
	case "tool_call", "tool_result":
		return true
	default:
		return false
	}
}

func partsFromRequestBody(requestFormat string, body map[string]any) []TimelineMessageContentPart {
	if len(body) == 0 {
		return nil
	}
	req := protocol.ParseRequest(requestFormat, body)
	if len(req.InputParts) == 0 {
		return nil
	}
	parts := make([]TimelineMessageContentPart, 0, len(req.InputParts))
	for _, part := range req.InputParts {
		switch strings.TrimSpace(part.Type) {
		case "image":
			parts = append(parts, TimelineMessageContentPart{
				Type:      "image",
				Src:       strings.TrimSpace(part.URL),
				MediaType: strings.TrimSpace(part.MediaType),
			})
		case "text":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, TimelineMessageContentPart{
				Type: "text",
				Text: part.Text,
			})
		case "tool_result":
			if strings.TrimSpace(part.Text) == "" {
				continue
			}
			parts = append(parts, TimelineMessageContentPart{
				Type: "text",
				Text: part.Text,
			})
		}
	}
	return parts
}
