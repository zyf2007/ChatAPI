package egress

import (
	"strings"

	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
)

type Service struct{}

func New() *Service { return &Service{} }

func (s *Service) InvalidJSONBody(requestFormat string) map[string]any {
	return protocol.InvalidJSONError(requestFormat)
}

func (s *Service) ErrorStatus(err error) int { return protocol.HTTPStatus(err) }

func (s *Service) ErrorBody(requestFormat string, err error) map[string]any {
	return protocol.BuildErrorBody(requestFormat, err)
}

func (s *Service) InternalErrorBody(requestFormat string, err error) map[string]any {
	if err == nil {
		return protocol.BuildErrorBody(requestFormat, protocol.InternalError("internal server error"))
	}
	return protocol.BuildErrorBody(requestFormat, protocol.InternalError(err.Error()))
}

func (s *Service) AbortBody(conversation common.Conversation, reason string) map[string]any {
	return protocol.AbortError(requestFormatOfConversation(conversation), reason)
}

func (s *Service) CompleteBody(conversation common.Conversation, input common.CompletePendingInput, message common.Message) map[string]any {
	return protocol.BuildResponseForMeta(protocol.ConversationMeta{
		Protocol:   protocol.ParseProtocol(requestFormatOfConversation(conversation)),
		Model:      conversationstate.Model(conversation, "chatapi-lab"),
		ResponseID: stringValue(conversation.ResponseID, input.ResponseID),
	}, protocol.TurnResult{
		ResponseID:          stringValue(conversation.ResponseID, input.ResponseID),
		OutputText:          message.Content,
		OutputSegments:      outputSegmentsFromMessage(message),
		Mode:                input.Mode,
		ReasoningStreamMode: input.ReasoningStreamMode,
		ToolName:            input.ToolName,
		ToolCallID:          input.ToolCallID,
		ToolOutput:          stringValue(input.ToolOutput, message.Content),
		FinishReason:        input.FinishReason,
		StopSequence:        input.StopSequence,
		Usage:               protocol.Usage{OutputTokens: input.OutputTokens},
	})
}

func outputSegmentsFromMessage(message common.Message) []protocol.OutputSegment {
	if message.Metadata == nil {
		return nil
	}
	return convertSegments(conversationstate.DecodeOutputSegments(message.Metadata["output_segments"]))
}

func convertSegments(segments []common.OutputSegment) []protocol.OutputSegment {
	if len(segments) == 0 {
		return nil
	}
	out := make([]protocol.OutputSegment, 0, len(segments))
	for _, segment := range segments {
		if segment.Text == "" {
			continue
		}
		out = append(out, protocol.OutputSegment{
			Mode: segment.Mode, Text: segment.Text, ReasoningStreamMode: segment.ReasoningStreamMode,
		})
	}
	return out
}

func requestFormatOfConversation(conversation common.Conversation) string {
	return conversationstate.RequestFormat(conversation)
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
