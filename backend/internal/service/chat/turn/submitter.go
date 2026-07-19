package turn

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/protocol/debugview"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputpolicy"
	protocolruntime "github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
)

type Store interface {
	CreatePendingTurn(context.Context, common.CreatePendingInput) (common.Conversation, common.Message, error)
}

type PendingRegistrar interface {
	Add(*PendingTurn)
	GetByConversationID(conversationID string) (*PendingTurn, bool)
}

type SubmitHooks struct {
	AfterCreate   func(ctx context.Context, request protocol.TurnRequest, conversationID string, responseID string)
	NotifyWaiting func(ctx context.Context, ownerID string, title string, userText string)
}

type Submitter struct {
	Store            Store
	Pending          PendingRegistrar
	Hooks            SubmitHooks
	Materializer     *RequestMaterializer
	OutputEventLimit func(context.Context) (int, error)
}

func (s *Submitter) Submit(ctx context.Context, input SubmitInput) (*PendingTurn, common.Conversation, common.Message, error) {
	materialized, err := s.materializeRequest(ctx, input.OwnerID, input.Request)
	if err != nil {
		return nil, common.Conversation{}, common.Message{}, err
	}
	input.Request = materialized.Request
	input.PreparedImages = materialized.PreparedImages
	input.RequestBody = materialized.RequestBody
	debugProjection := debugview.ProjectRequest(input.Request)
	outputGuard, err := outputpolicy.NewGuard(input.Request)
	if err != nil {
		s.cleanupPreparedImages(ctx, input.PreparedImages)
		return nil, common.Conversation{}, common.Message{}, err
	}
	maxOutputEvents := 0
	if s.OutputEventLimit != nil {
		maxOutputEvents, err = s.OutputEventLimit(ctx)
		if err != nil {
			s.cleanupPreparedImages(ctx, input.PreparedImages)
			return nil, common.Conversation{}, common.Message{}, err
		}
	}

	requestID := "req_" + uuid.NewString()
	responseID := "resp_" + uuid.NewString()
	conversationID := input.Target.ConversationID
	if conversationID == "" {
		conversationID = "conv_" + uuid.NewString()
	}

	conversation, message, err := s.Store.CreatePendingTurn(ctx, common.CreatePendingInput{
		ConversationID:     conversationID,
		RequestID:          requestID,
		ResponseID:         responseID,
		OwnerID:            input.OwnerID,
		ReuseConversation:  input.Target.Reuse,
		RequestFormat:      input.Request.Protocol.String(),
		Model:              input.Request.Model,
		SystemContent:      input.Request.SystemContent,
		DeveloperContent:   input.Request.DeveloperContent,
		AssistantContent:   input.Request.AssistantContent,
		UserContent:        input.Request.LastUserContent,
		UserMessageContent: buildUserMessageContent(input.Request),
		RequestMethod:      input.RequestMeta.RequestMethod,
		RequestPath:        input.RequestMeta.RequestPath,
		RequestQuery:       input.RequestMeta.RequestQuery,
		RequestHeaders:     input.RequestMeta.RequestHeaders,
		RequestBody:        input.RequestBody,
		// Store the source fact, a normalized snapshot, and a UI cache separately.
		RawRequestBody: input.Request.RawBody,
		RequestOptions: protocol.RequestOptionsDebug(input.Request),
		OptionChips:    optionChipsAsAny(debugProjection.OptionChips),
		ToolSchemas:    protocol.RawToolSchemas(input.Request.ToolSchemas),
		BuiltinTools:   builtinToolsAsAny(debugProjection.BuiltinTools),
		ToolChoice:     common.RequestToolChoice{Type: input.Request.ToolChoice.Type, Name: input.Request.ToolChoice.Name},
		ResponseFormat: common.RequestResponseFormat{
			Type:   input.Request.ResponseFormat.Type,
			Name:   input.Request.ResponseFormat.Name,
			Schema: input.Request.ResponseFormat.Schema,
		},
		PreparedImages: toCreatePendingImageAssets(input.PreparedImages),
	})
	if err != nil {
		s.cleanupPreparedImages(ctx, input.PreparedImages)
		return nil, common.Conversation{}, common.Message{}, err
	}

	turn := &PendingTurn{
		RequestID:         requestID,
		ConversationID:    conversationID,
		ResponseID:        responseID,
		OwnerID:           input.OwnerID,
		ToolCallIDs:       extractSubmitToolCallIDs(input.Request),
		Actor:             input.Actor,
		RequestFormat:     input.Request.Protocol.String(),
		Model:             input.Request.Model,
		NormalizedRequest: input.Request,
		RequestMeta:       input.RequestMeta,
		Runtime: protocolruntime.New(protocol.ConversationMeta{
			Protocol:   input.Request.Protocol,
			Model:      input.Request.Model,
			ResponseID: responseID,
		}),
		OutputGuard:     outputGuard,
		MaxOutputEvents: maxOutputEvents,
		MutationMu:      &sync.Mutex{},
		CreatedAt:       time.Now().UTC(),
		Events:          make(chan PendingEvent, 32),
		Done:            make(chan PendingResult, 1),
	}
	s.Pending.Add(turn)
	if s.Hooks.AfterCreate != nil {
		s.Hooks.AfterCreate(ctx, input.Request, conversationID, responseID)
	}
	if s.Hooks.NotifyWaiting != nil {
		if _, waiting := s.Pending.GetByConversationID(conversationID); waiting {
			// Prefer the latest user turn text for notifications.
			notifyText := strings.TrimSpace(input.Request.LastUserContent)
			if notifyText == "" {
				notifyText = strings.TrimSpace(input.Request.UserContent)
			}
			s.Hooks.NotifyWaiting(ctx, input.OwnerID, conversation.Title, notifyText)
		}
	}
	return turn, conversation, message, nil
}

func optionChipsAsAny(items []debugview.OptionChip) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func builtinToolsAsAny(items []protocol.BuiltinTool) []any {
	if len(items) == 0 {
		return nil
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, item)
	}
	return out
}

func (s *Submitter) materializeRequest(ctx context.Context, ownerID string, request protocol.TurnRequest) (MaterializedRequest, error) {
	if s == nil || s.Materializer == nil {
		return MaterializedRequest{
			Request:     request,
			RequestBody: protocol.BuildRequestBody(request),
		}, nil
	}
	return s.Materializer.Materialize(ctx, ownerID, request)
}

func extractSubmitToolCallIDs(request protocol.TurnRequest) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	for _, part := range request.InputParts {
		if part.ToolCallID == "" {
			continue
		}
		id := strings.TrimSpace(part.ToolCallID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func toCreatePendingImageAssets(images []media.DraftAsset) []common.CreatePendingImageAssetInput {
	if len(images) == 0 {
		return nil
	}
	items := make([]common.CreatePendingImageAssetInput, 0, len(images))
	for _, image := range images {
		items = append(items, common.CreatePendingImageAssetInput{
			FileID:            image.FileID,
			Path:              image.Path,
			MediaType:         image.MediaType,
			Bytes:             image.Bytes,
			SHA256:            image.SHA256,
			Width:             image.Width,
			Height:            image.Height,
			SourceKind:        image.SourceKind,
			OriginalName:      image.OriginalName,
			OriginalMediaType: image.OriginalMediaType,
			InputPartIndex:    image.InputPartIndex,
		})
	}
	return items
}

func (s *Submitter) cleanupPreparedImages(ctx context.Context, images []media.DraftAsset) {
	if s == nil || s.Materializer == nil {
		return
	}
	s.Materializer.Cleanup(ctx, images)
}

func buildUserMessageContent(request protocol.TurnRequest) string {
	body := protocol.BuildRequestBody(request)
	switch request.Protocol {
	case protocol.ProtocolChatCompletions:
		if messages, ok := body["messages"].([]any); ok {
			for idx := len(messages) - 1; idx >= 0; idx-- {
				record, ok := messages[idx].(map[string]any)
				if !ok || strings.TrimSpace(rawStringValue(record["role"], "")) != "user" {
					continue
				}
				return marshalJSON(record["content"])
			}
		}
	case protocol.ProtocolAnthropicMessages:
		if messages, ok := body["messages"].([]any); ok {
			for idx := len(messages) - 1; idx >= 0; idx-- {
				record, ok := messages[idx].(map[string]any)
				if !ok || strings.TrimSpace(rawStringValue(record["role"], "")) != "user" {
					continue
				}
				return marshalJSON(record["content"])
			}
		}
	default:
		if input, ok := body["input"].([]any); ok {
			return marshalJSON(input)
		}
	}
	return request.UserContent
}

func rawStringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok {
		return raw
	}
	return fallback
}

func marshalJSON(value any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
