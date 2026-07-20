package turn

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/protocol"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	conversationresolve "github.com/zyf2007/ChatAPI/internal/service/chat/conversationresolve"
	conversationstate "github.com/zyf2007/ChatAPI/internal/service/chat/conversationstate"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	"github.com/zyf2007/ChatAPI/internal/service/chat/outputpolicy"
	protocolruntime "github.com/zyf2007/ChatAPI/internal/service/chat/protocolruntime"
	"go.uber.org/zap"
)

var ErrPendingNotFound = errors.New("pending turn not found")
var ErrPendingConflict = errors.New("pending turn already finalized")
var ErrOutputImageNotAllowed = errors.New("image generation is not enabled for this request")
var ErrOutputEventLimitExceeded = errors.New("message output event limit exceeded")

const outputEventLimitAbortReason = "message event limit exceeded"

type MutationErrorResolver func(context.Context, string, error) error
type TextNotifier func(context.Context, string, string, string)
type AdmissionHook func(context.Context, string) error

type OutputAssetService interface {
	Upload(context.Context, string, string, string, string, string, io.Reader) (outputasset.Uploaded, error)
	Consume(context.Context, string, string, string, string, func(outputasset.Resolved) error) (outputasset.Resolved, error)
}

type ExpireResult struct {
	ExpiredConversations int `json:"expired_conversations"`
	ExpiredActiveTurns   int `json:"expired_active_turns"`
}

type TurnIdentity struct {
	OwnerID   string
	RequestID string
}

type eventRoute struct {
	OwnerID        string
	RequestID      string
	ConversationID string
}

func eventRouteFromTurn(turn *PendingTurn) eventRoute {
	if turn == nil {
		return eventRoute{}
	}
	return eventRoute{
		OwnerID:        strings.TrimSpace(turn.OwnerID),
		RequestID:      strings.TrimSpace(turn.RequestID),
		ConversationID: strings.TrimSpace(turn.ConversationID),
	}
}

func eventRouteFromIdentity(conversationID string, identity TurnIdentity) eventRoute {
	return eventRoute{
		OwnerID:        strings.TrimSpace(identity.OwnerID),
		RequestID:      strings.TrimSpace(identity.RequestID),
		ConversationID: strings.TrimSpace(conversationID),
	}
}

func (r eventRoute) withConversation(conversation common.Conversation) eventRoute {
	if r.OwnerID == "" {
		r.OwnerID = conversationstate.OwnerID(conversation)
	}
	if r.ConversationID == "" {
		r.ConversationID = strings.TrimSpace(conversation.ID)
	}
	return r
}

type SubmitPrincipal struct {
	OwnerID string
	Actor   actor.Actor
}

type Service struct {
	Submitter                   *Submitter
	Pending                     pendingRegistryLike
	Store                       chat.Store
	Resolver                    *conversationresolve.Service
	Events                      chatevents.Publisher
	ResolveMutationError        MutationErrorResolver
	NotifyText                  TextNotifier
	EnsureMessageAdmission      AdmissionHook
	EnsureConversationAdmission AdmissionHook
	OwnerIDFromContext          func(context.Context) string
	ActorFromContext            func(context.Context) (actor.Actor, bool)
	Egress                      *egresssvc.Service
	Logger                      *zap.Logger
	OutputAssets                OutputAssetService
}

type TurnControlKind string

const (
	TurnControlRespond        TurnControlKind = "respond"
	TurnControlStreamDelta    TurnControlKind = "stream_delta"
	TurnControlStreamComplete TurnControlKind = "stream_complete"
	TurnControlAbort          TurnControlKind = "abort"
	TurnControlBuiltinTool    TurnControlKind = "builtin_tool"
)

type TurnControlCommand struct {
	ConversationID string
	ResponseID     string
	RequestID      string
	Action         OutputAction
}

func (c TurnControlCommand) Validate() error {
	if c.ConversationID == "" {
		return errors.New("conversation_id is required")
	}
	return c.Action.Validate()
}

func (s *Service) CreatePendingResponse(ctx context.Context, input SubmitInput) (map[string]any, error) {
	principal := s.submitPrincipal(ctx)
	if err := s.ensureAdmissions(ctx, principal.OwnerID); err != nil {
		return nil, err
	}
	input.OwnerID = principal.OwnerID
	input.Actor = principal.Actor
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, err
	} else {
		input.Target = target
	}
	turn, conversation, message, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", principal.OwnerID)).Error("create pending response submit failed", zap.Error(err))
		return nil, err
	}
	route := eventRouteFromTurn(turn)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishMessageAppended(ctx, route, conversation, message)
	s.publishTurnWaiting(ctx, turn, input.Request.LastUserContent)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", principal.OwnerID),
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	).Info("pending response created")
	go s.disconnectOnRequestDone(ctx, turn.ConversationID, turn.RequestID, "request disconnected")
	result, err := s.Pending.WaitTurn(ctx, turn)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = s.disconnectPendingRequest(context.Background(), turn.ConversationID, TurnIdentity{RequestID: turn.RequestID, OwnerID: turn.OwnerID}, "request disconnected", "request_disconnected", "Request Disconnected")
		}
		logging.BindContext(s.Logger, ctx,
			zap.String("owner.id", principal.OwnerID),
			zap.String("conversation.id", turn.ConversationID),
			zap.String("request.id", turn.RequestID),
		).Warn("pending response wait interrupted", zap.Error(err))
		return nil, err
	}
	return result.ResponseBody, nil
}

func (s *Service) CreatePendingStream(ctx context.Context, input SubmitInput) (*PendingTurn, common.Conversation, error) {
	principal := s.submitPrincipal(ctx)
	if err := s.ensureAdmissions(ctx, principal.OwnerID); err != nil {
		return nil, common.Conversation{}, err
	}
	input.OwnerID = principal.OwnerID
	input.Actor = principal.Actor
	if target, err := s.resolveTarget(ctx, input); err != nil {
		return nil, common.Conversation{}, err
	} else {
		input.Target = target
	}
	turn, conversation, message, err := s.Submitter.Submit(ctx, input)
	if err != nil {
		logging.BindContext(s.Logger, ctx, zap.String("owner.id", principal.OwnerID)).Error("create pending stream submit failed", zap.Error(err))
		return nil, common.Conversation{}, err
	}
	route := eventRouteFromTurn(turn)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishMessageAppended(ctx, route, conversation, message)
	s.publishTurnWaiting(ctx, turn, input.Request.LastUserContent)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", principal.OwnerID),
		zap.String("conversation.id", conversation.ID),
		zap.String("request.id", turn.RequestID),
	).Info("pending stream created")
	go s.disconnectOnRequestDone(ctx, conversation.ID, turn.RequestID, "request disconnected")
	return turn, conversation, nil
}

func (s *Service) DisconnectRecoveredPending(ctx context.Context, reason string) (ExpireResult, error) {
	items, err := s.Store.ListConversations(ctx)
	if err != nil {
		return ExpireResult{}, err
	}
	result := ExpireResult{}
	for _, item := range items {
		if !conversationstate.IsPendingStatus(conversationstate.FromConversation(item).Status) {
			continue
		}
		identity, resolveErr := s.resolveStoredTurnIdentity(ctx, item.ID)
		if resolveErr != nil && !errors.Is(resolveErr, common.ErrNotFound) {
			return ExpireResult{}, resolveErr
		}
		if err := s.disconnectPendingRequest(ctx, item.ID, identity, reason, "recovered_pending_disconnected", "Recovered Pending Disconnected"); err == nil || errors.Is(err, common.ErrPendingDisconnected) {
			result.ExpiredConversations++
		} else {
			return ExpireResult{}, err
		}
	}
	return result, nil
}

func (s *Service) UpdateDraft(ctx context.Context, conversationID string, chunk string, mode string, reasoningStreamMode string) (map[string]any, error) {
	unlock := s.lockTurnMutation(conversationID)
	defer unlock()
	if err := s.ensureExpectedRequest(ctx, conversationID); err != nil {
		return nil, err
	}
	release, aborted, err := s.reserveOutputEventOrAbort(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if aborted != nil {
		return aborted, nil
	}
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()
	previousState, err := s.Pending.StartDelta(conversationID)
	if err != nil {
		return nil, s.resolveMutationError(ctx, conversationID, err)
	}
	guard := s.outputGuard(conversationID)
	var updated common.Conversation
	var completedMessage common.Message
	var completionInput common.CompletePendingInput
	nextDraft := ""
	decision, err := guard.Execute(outputpolicy.Input{Text: chunk, Mode: mode}, func(decision outputpolicy.Decision) error {
		conversation, getErr := s.Store.GetConversation(ctx, conversationID)
		if getErr != nil {
			return getErr
		}
		existing := conversationstate.FromConversation(conversation)
		outputSegments := appendOutputSegment(existing.OutputSegments, mode, decision.Text, reasoningStreamMode)
		// DraftText / message.Content are answer-only. Thinking is durable only in
		// output_segments so literal <think> tags never become synthetic markup.
		nextDraft = conversationstate.ContentFromSegments(outputSegments)
		if decision.Terminal {
			completePreviousState, startErr := s.Pending.StartComplete(conversationID)
			if startErr != nil {
				return startErr
			}
			preview := conversationstate.PreviewFromSegments(outputSegments)
			if preview == "" {
				preview = decision.OutputText
			}
			completionInput = common.CompletePendingInput{
				ConversationID:      conversationID,
				ResponseID:          responseIDForConversation(s.Pending, conversationID),
				OutputText:          nextDraft,
				OutputSegments:      outputSegments,
				OutputPreview:       preview,
				Mode:                mode,
				ReasoningStreamMode: reasoningStreamMode,
				OutputPolicy:        decision.Metadata(),
				OutputTokens:        decision.OutputTokens,
				FinishReason:        decision.FinishReason,
				StopSequence:        decision.StopSequence,
			}
			var completeErr error
			updated, completedMessage, completeErr = s.Store.CompletePendingTurn(ctx, completionInput)
			if completeErr != nil {
				s.Pending.RevertFinalize(conversationID, completePreviousState)
			}
			return completeErr
		}
		var updateErr error
		updated, updateErr = s.Store.UpdateDraft(ctx, common.UpdateDraftInput{
			ConversationID: conversationID,
			DraftText:      nextDraft,
			OutputSegments: outputSegments,
		})
		return updateErr
	})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		return nil, err
	}
	chunk = decision.Text
	action := OutputAction{
		Kind:                TurnControlStreamDelta,
		OutputText:          chunk,
		Mode:                mode,
		ReasoningStreamMode: reasoningStreamMode,
	}.Normalized()
	if decision.Terminal {
		deltaEvents := s.applyRuntimeAction(conversationID, action.RuntimeAction()).StreamEvents
		committed = true
		return s.finishCompletedTurn(ctx, completionInput, updated, completedMessage, "", deltaEvents, true)
	}
	_ = s.Pending.Publish(conversationID, PendingEvent{
		Action:       action,
		StreamEvents: s.applyRuntimeAction(conversationID, action.RuntimeAction()).StreamEvents,
	})
	s.publishConversationUpserted(ctx, s.eventRouteForContext(ctx, updated), updated)
	s.notifyText(ctx, s.ownerID(ctx), updated.Title, chunk)
	committed = true
	body := map[string]any{"draft_text": nextDraft, "draft_length": len([]rune(nextDraft))}
	if metadata := decision.Metadata(); len(metadata) > 0 {
		body["output_policy"] = metadata
	}
	return body, nil
}

func (s *Service) EmitBuiltinTool(ctx context.Context, command TurnControlCommand) (map[string]any, error) {
	unlock := s.lockTurnMutation(command.ConversationID)
	defer unlock()
	if err := s.ensureExpectedRequest(withExpectedRequest(ctx, command.RequestID), command.ConversationID); err != nil {
		return nil, err
	}
	action := command.Action.Normalized()
	turn, ok := s.Pending.GetByConversationID(command.ConversationID)
	if !ok || turn == nil {
		return nil, ErrPendingNotFound
	}
	identity, err := s.resolveActiveTurnIdentity(ctx, command.ConversationID)
	if err != nil {
		return nil, err
	}
	conversation, err := s.Store.GetConversation(ctx, command.ConversationID)
	if err != nil {
		return nil, err
	}
	kind := action.BuiltinToolKind
	spec, ok := protocolruntime.LookupBuiltinToolSpec(kind)
	if !ok {
		return nil, errors.New("unsupported builtin tool kind: " + kind)
	}
	title := spec.Title
	detail := ""
	level := "info"
	metadata := map[string]any{
		"builtin_tool_kind": kind,
		"request_id":        identity.RequestID,
	}
	if spec.RequiresQuery {
		detail = action.BuiltinToolQuery
		metadata["query"] = detail
	}
	if !pendingRequestHasBuiltinTool(turn, kind) {
		return nil, errors.New("builtin tool is not enabled for this request: " + kind)
	}
	release, aborted, err := s.reserveOutputEventOrAbort(ctx, command.ConversationID)
	if err != nil {
		return nil, err
	}
	if aborted != nil {
		return aborted, nil
	}
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()
	eventInput := common.AppendConversationEventInput{
		ConversationID: command.ConversationID,
		OwnerID:        identity.OwnerID,
		Type:           "builtin_tool",
		Level:          level,
		Title:          title,
		Detail:         detail,
		RequestID:      identity.RequestID,
		Metadata:       metadata,
	}
	var event common.ConversationEvent
	if spec.RequiresAsset {
		if s.OutputAssets == nil {
			return nil, errors.New("output image assets are unavailable")
		}
		resolved, resolveErr := s.OutputAssets.Consume(ctx, identity.OwnerID, command.ConversationID, identity.RequestID, action.BuiltinToolAssetID, func(resolved outputasset.Resolved) error {
			eventInput.Detail = "Image generated"
			var appendErr error
			event, appendErr = s.Store.AppendConversationEventWithAsset(ctx, common.AppendConversationEventWithAssetInput{
				Event: eventInput, AssetID: resolved.Asset.ID, AssetURL: resolved.URL, Purpose: "image_generation_result",
			})
			return appendErr
		})
		if resolveErr != nil {
			return nil, resolveErr
		}
		action.BuiltinToolResult = resolved.Base64
	} else {
		event, err = s.Store.AppendConversationEvent(ctx, eventInput)
	}
	if err != nil {
		return nil, err
	}
	route := eventRouteFromIdentity(command.ConversationID, identity)
	s.publishConversationEventAppended(ctx, route, conversation, event)
	_ = s.Pending.Publish(command.ConversationID, PendingEvent{
		Action:       action,
		StreamEvents: s.applyRuntimeAction(command.ConversationID, action.RuntimeAction()).StreamEvents,
	})
	committed = true
	return map[string]any{"ok": true, "event": event}, nil
}

func (s *Service) UploadOutputImage(ctx context.Context, ownerID string, conversationID string, originalName string, mediaType string, reader io.Reader) (outputasset.Uploaded, error) {
	if s == nil || s.Pending == nil || s.OutputAssets == nil {
		return outputasset.Uploaded{}, ErrPendingNotFound
	}
	turn, ok := s.Pending.GetByConversationID(strings.TrimSpace(conversationID))
	if !ok || turn == nil || strings.TrimSpace(turn.OwnerID) != strings.TrimSpace(ownerID) {
		return outputasset.Uploaded{}, ErrPendingNotFound
	}
	if !pendingRequestHasBuiltinTool(turn, "image_generation") {
		return outputasset.Uploaded{}, ErrOutputImageNotAllowed
	}
	identity, err := s.resolveActiveTurnIdentity(ctx, conversationID)
	if err != nil {
		return outputasset.Uploaded{}, err
	}
	return s.OutputAssets.Upload(ctx, identity.OwnerID, conversationID, identity.RequestID, originalName, mediaType, reader)
}

func pendingRequestHasBuiltinTool(turn *PendingTurn, kind string) bool {
	if turn == nil {
		return false
	}
	return protocolruntime.RequestSupportsBuiltinTool(turn.NormalizedRequest, kind)
}

func appendOutputSegment(existing []common.OutputSegment, mode string, text string, reasoningStreamMode string) []common.OutputSegment {
	if text == "" {
		return existing
	}
	segments := append([]common.OutputSegment(nil), existing...)
	mode = conversationstate.SegmentMode(mode)
	if len(segments) > 0 && segments[len(segments)-1].Mode == mode && segments[len(segments)-1].ReasoningStreamMode == reasoningStreamMode {
		segments[len(segments)-1].Text += text
		return segments
	}
	return append(segments, common.OutputSegment{
		Mode:                mode,
		Text:                text,
		ReasoningStreamMode: reasoningStreamMode,
	})
}

func (s *Service) CompleteConversation(ctx context.Context, input common.CompletePendingInput) (map[string]any, error) {
	unlock := s.lockTurnMutation(input.ConversationID)
	defer unlock()
	if err := s.ensureExpectedRequest(ctx, input.ConversationID); err != nil {
		return nil, err
	}
	release, aborted, err := s.reserveOutputEventOrAbort(ctx, input.ConversationID)
	if err != nil {
		return nil, err
	}
	if aborted != nil {
		return aborted, nil
	}
	committed := false
	defer func() {
		if !committed {
			release()
		}
	}()
	guard := s.outputGuard(input.ConversationID)
	var conversation common.Conversation
	var message common.Message
	completePreviousState := ""
	decision, err := guard.Execute(outputpolicy.Input{Text: input.OutputText, Mode: input.Mode, Final: true}, func(decision outputpolicy.Decision) error {
		var startErr error
		completePreviousState, startErr = s.Pending.StartComplete(input.ConversationID)
		if startErr != nil {
			return startErr
		}
		conversationBefore, getErr := s.Store.GetConversation(ctx, input.ConversationID)
		if getErr != nil {
			s.Pending.RevertFinalize(input.ConversationID, completePreviousState)
			return getErr
		}
		existingState := conversationstate.FromConversation(conversationBefore)
		mode := strings.TrimSpace(input.Mode)
		switch mode {
		case "tool_call", "tool_result":
			// Tool messages never own ordinary answer/thinking segments. Clear any
			// draft segments so tool completion cannot inherit leftover stream state
			// into message.metadata.output_segments or tool Content fallback paths.
			input.OutputSegments = nil
			// tool_result Content/metadata.output share one payload. When callers only
			// supply ToolOutput, promote it to OutputText before store fail-safes run.
			// Emptiness is trim-checked; non-empty payload keeps its original text.
			if mode == "tool_result" && strings.TrimSpace(input.OutputText) == "" && strings.TrimSpace(input.ToolOutput) != "" {
				input.OutputText = input.ToolOutput
			}
			if strings.TrimSpace(input.OutputText) == "" {
				input.OutputText = decision.Text
			}
			if strings.TrimSpace(input.OutputPreview) == "" {
				input.OutputPreview = decision.OutputText
			}
			if strings.TrimSpace(input.OutputPreview) == "" {
				input.OutputPreview = decision.Text
			}
			if strings.TrimSpace(input.OutputPreview) == "" {
				input.OutputPreview = input.OutputText
			}
		default:
			input.OutputSegments = appendOutputSegment(existingState.OutputSegments, input.Mode, decision.Text, input.ReasoningStreamMode)
			// Structured segments are the source of truth. Content is answer-only;
			// thinking-only turns persist empty Content and keep reasoning in metadata.
			input.OutputText = conversationstate.ContentFromSegments(input.OutputSegments)
			input.OutputPreview = conversationstate.PreviewFromSegments(input.OutputSegments)
			if input.OutputPreview == "" {
				input.OutputPreview = decision.OutputText
			}
			if input.OutputPreview == "" && decision.Text != "" && conversationstate.SegmentMode(input.Mode) != "thinking" {
				input.OutputPreview = decision.Text
			}
		}
		input.OutputPolicy = decision.Metadata()
		input.OutputTokens = decision.OutputTokens
		input.FinishReason = decision.FinishReason
		input.StopSequence = decision.StopSequence
		var completeErr error
		conversation, message, completeErr = s.Store.CompletePendingTurn(ctx, input)
		if completeErr != nil {
			s.Pending.RevertFinalize(input.ConversationID, completePreviousState)
		}
		return completeErr
	})
	if err != nil {
		return nil, s.resolveMutationError(ctx, input.ConversationID, err)
	}
	committed = true
	return s.finishCompletedTurn(ctx, input, conversation, message, decision.Text, nil, false)
}

func (s *Service) finishCompletedTurn(
	ctx context.Context,
	input common.CompletePendingInput,
	conversation common.Conversation,
	message common.Message,
	completionDelta string,
	prefixEvents []protocol.StreamEvent,
	autoCompleted bool,
) (map[string]any, error) {
	route := s.eventRouteForContext(ctx, conversation)
	s.publishConversationUpserted(ctx, route, conversation)
	responseBody := s.egress().CompleteBody(conversation, input, message)
	action := OutputAction{
		Kind:                TurnControlStreamComplete,
		OutputText:          completionDelta,
		OutputSegments:      input.OutputSegments,
		Mode:                input.Mode,
		ReasoningStreamMode: input.ReasoningStreamMode,
		ToolName:            input.ToolName,
		ToolCallID:          input.ToolCallID,
		ToolOutput:          stringValue(input.ToolOutput, message.Content),
		FinishReason:        input.FinishReason,
		StopSequence:        input.StopSequence,
		OutputTokens:        input.OutputTokens,
	}.Normalized()
	streamEvents := append([]protocol.StreamEvent(nil), prefixEvents...)
	streamEvents = append(streamEvents, s.applyRuntimeAction(input.ConversationID, action.RuntimeAction()).StreamEvents...)
	_ = s.Pending.Publish(input.ConversationID, PendingEvent{
		Action:       action,
		ResponseBody: responseBody,
		StreamEvents: streamEvents,
	})
	if err := s.Pending.Resolve(input.ConversationID, PendingResult{ResponseBody: responseBody}); err != nil {
		return nil, err
	}
	s.publishMessageAppended(ctx, route, conversation, message)
	s.notifyText(ctx, s.ownerID(ctx), conversation.Title, message.Content)
	body := map[string]any{"conversation": conversation, "output_text": message.Content}
	if autoCompleted {
		body["auto_completed"] = true
	}
	if len(input.OutputPolicy) > 0 {
		body["output_policy"] = input.OutputPolicy
	}
	return body, nil
}

func (s *Service) AbortConversation(ctx context.Context, conversationID string, reason string) error {
	unlock := s.lockTurnMutation(conversationID)
	defer unlock()
	return s.abortConversationLocked(ctx, conversationID, reason)
}

func (s *Service) abortConversationLocked(ctx context.Context, conversationID string, reason string) error {
	if err := s.ensureExpectedRequest(ctx, conversationID); err != nil {
		return err
	}
	previousState, err := s.Pending.StartAbort(conversationID)
	if err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Warn("turn abort start rejected", zap.Error(err))
		return s.resolveMutationError(ctx, conversationID, err)
	}
	identity, err := s.resolveActiveTurnIdentity(ctx, conversationID)
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		return err
	}
	result, err := s.Store.AbortPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: conversationID,
		Reason:         reason,
		Identity:       common.TurnIdentity{OwnerID: identity.OwnerID, RequestID: identity.RequestID},
		EventType:      "request_aborted",
		EventLevel:     "warn",
		EventTitle:     "Request Aborted",
		EventDetail:    reason,
		EventMetadata:  map[string]any{"reason": reason},
	})
	if err != nil {
		s.Pending.RevertFinalize(conversationID, previousState)
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", conversationID),
			zap.String("turn.action", "abort"),
		).Error("turn abort persistence failed", zap.Error(err))
		return err
	}
	conversation := result.Conversation
	route := eventRouteFromIdentity(conversationID, identity)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishConversationEventAppended(ctx, route, conversation, result.Event)
	body := s.egress().AbortBody(conversation, reason)
	action := OutputAction{Kind: TurnControlAbort, AbortReason: reason}.Normalized()
	runtimeAction := action.RuntimeAction()
	runtimeAction.ErrorBody = body
	_ = s.Pending.Publish(conversationID, PendingEvent{
		Action:       action,
		ErrorBody:    body,
		StreamEvents: s.applyRuntimeAction(conversationID, runtimeAction).StreamEvents,
	})
	s.notifyText(ctx, identity.OwnerID, conversation.Title, reason)
	logging.BindContext(s.Logger, ctx,
		zap.String("owner.id", identity.OwnerID),
		zap.String("conversation.id", conversationID),
		zap.String("request.format", conversationstate.RequestFormat(conversation)),
		zap.String("turn.action", "abort"),
	).Info("turn aborted conversation")
	return s.Pending.Abort(conversationID, body)
}

func (s *Service) ExpirePendingTurns(ctx context.Context, ttl time.Duration, now time.Time) (ExpireResult, error) {
	if ttl <= 0 {
		return ExpireResult{}, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cutoff := now.UTC().Add(-ttl)
	body := pendingExpiredBody(ttl)
	activeExpired := s.Pending.ExpireOlderThan(cutoff, body)
	dbResult, err := s.Store.ExpirePendingTurns(ctx, cutoff)
	if err != nil {
		return ExpireResult{}, err
	}
	return ExpireResult{ExpiredConversations: dbResult.ExpiredConversations, ExpiredActiveTurns: activeExpired}, nil
}

func (s *Service) ExecuteTurnControl(ctx context.Context, command TurnControlCommand) (map[string]any, error) {
	if err := command.Validate(); err != nil {
		logging.BindContext(s.Logger, ctx,
			zap.String("conversation.id", command.ConversationID),
			zap.String("turn.control.kind", string(command.Action.Kind)),
		).Warn("turn control validation failed", zap.Error(err))
		return nil, err
	}
	ctx = withExpectedRequest(ctx, command.RequestID)
	action := command.Action.Normalized()
	logging.BindContext(s.Logger, ctx,
		zap.String("conversation.id", command.ConversationID),
		zap.String("turn.control.kind", string(action.Kind)),
		zap.String("response.id", command.ResponseID),
	).Debug("turn control dispatch")
	var result map[string]any
	var err error
	switch action.Kind {
	case TurnControlRespond, TurnControlStreamComplete:
		result, err = s.CompleteConversation(ctx, common.CompletePendingInput{
			ConversationID:      command.ConversationID,
			ResponseID:          command.ResponseID,
			OutputText:          action.OutputText,
			Mode:                action.Mode,
			ToolName:            action.ToolName,
			ToolCallID:          action.ToolCallID,
			ToolOutput:          action.ToolOutput,
			ReasoningStreamMode: action.ReasoningStreamMode,
		})
	case TurnControlStreamDelta:
		result, err = s.UpdateDraft(ctx, command.ConversationID, action.OutputText, action.Mode, action.ReasoningStreamMode)
	case TurnControlBuiltinTool:
		command.Action = action
		result, err = s.EmitBuiltinTool(ctx, command)
	case TurnControlAbort:
		err = s.AbortConversation(ctx, command.ConversationID, action.AbortReason)
		result = map[string]any{"ok": true}
	default:
		return nil, errors.New("unsupported turn control kind: " + string(action.Kind))
	}
	return result, err
}

func (s *Service) reserveOutputEvent(conversationID string) (func(), error) {
	registry, ok := s.Pending.(interface {
		ReserveOutputEvent(string) (bool, string, error)
		ReleaseOutputEvent(string, string)
	})
	if !ok {
		return func() {}, nil
	}
	exceeded, requestID, err := registry.ReserveOutputEvent(conversationID)
	if err != nil {
		return func() {}, err
	}
	if exceeded {
		return func() {}, ErrOutputEventLimitExceeded
	}
	return func() { registry.ReleaseOutputEvent(conversationID, requestID) }, nil
}

func (s *Service) reserveOutputEventOrAbort(ctx context.Context, conversationID string) (func(), map[string]any, error) {
	release, err := s.reserveOutputEvent(conversationID)
	if !errors.Is(err, ErrOutputEventLimitExceeded) {
		return release, nil, err
	}
	if err := s.abortConversationLocked(ctx, conversationID, outputEventLimitAbortReason); err != nil {
		return func() {}, nil, err
	}
	return func() {}, map[string]any{"ok": true, "aborted": true, "reason": outputEventLimitAbortReason}, nil
}

func (s *Service) ExecuteTurnControlByRequestID(ctx context.Context, requestID string, command TurnControlCommand) (map[string]any, error) {
	request, err := s.Store.GetRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	command.ConversationID = request.ConversationID
	command.RequestID = request.RequestID
	return s.ExecuteTurnControl(ctx, command)
}

func (s *Service) ActiveRequestID(conversationID string) (string, bool) {
	if s == nil || s.Pending == nil {
		return "", false
	}
	turn, ok := s.Pending.GetByConversationID(strings.TrimSpace(conversationID))
	if !ok || turn == nil || strings.TrimSpace(turn.RequestID) == "" {
		return "", false
	}
	return strings.TrimSpace(turn.RequestID), true
}

func (s *Service) ensureAdmissions(ctx context.Context, ownerID string) error {
	if s.EnsureMessageAdmission != nil {
		if err := s.EnsureMessageAdmission(ctx, ownerID); err != nil {
			return err
		}
	}
	if s.EnsureConversationAdmission != nil {
		if err := s.EnsureConversationAdmission(ctx, ownerID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ownerID(ctx context.Context) string {
	if s.OwnerIDFromContext == nil {
		return ""
	}
	return s.OwnerIDFromContext(ctx)
}

func (s *Service) actor(ctx context.Context) actor.Actor {
	if s.ActorFromContext == nil {
		return actor.Actor{}
	}
	value, ok := s.ActorFromContext(ctx)
	if !ok {
		return actor.Actor{}
	}
	return value
}

func (s *Service) submitPrincipal(ctx context.Context) SubmitPrincipal {
	return SubmitPrincipal{
		OwnerID: strings.TrimSpace(s.ownerID(ctx)),
		Actor:   s.actor(ctx),
	}
}

func (s *Service) notifyText(ctx context.Context, ownerID string, title string, text string) {
	if s.NotifyText != nil {
		s.NotifyText(ctx, ownerID, title, text)
	}
}

func (s *Service) resolveMutationError(ctx context.Context, conversationID string, err error) error {
	if s.ResolveMutationError == nil {
		return err
	}
	return s.ResolveMutationError(ctx, conversationID, err)
}

func (s *Service) egress() *egresssvc.Service {
	if s != nil && s.Egress != nil {
		return s.Egress
	}
	return egresssvc.New()
}

func (s *Service) applyRuntimeAction(conversationID string, action protocolruntime.Action) protocolruntime.Result {
	if s == nil || s.Pending == nil {
		return protocolruntime.Result{}
	}
	turn, ok := s.Pending.GetByConversationID(conversationID)
	if !ok || turn == nil || turn.Runtime == nil {
		return protocolruntime.Result{}
	}
	return turn.Runtime.Apply(action)
}

func (s *Service) outputGuard(conversationID string) *outputpolicy.Guard {
	if s == nil || s.Pending == nil {
		guard, _ := outputpolicy.NewGuard(protocol.TurnRequest{})
		return guard
	}
	turn, ok := s.Pending.GetByConversationID(conversationID)
	if !ok || turn == nil {
		guard, _ := outputpolicy.NewGuard(protocol.TurnRequest{})
		return guard
	}
	if turn.OutputGuard == nil {
		turn.OutputGuard, _ = outputpolicy.NewGuard(turn.NormalizedRequest)
	}
	return turn.OutputGuard
}

func (s *Service) lockTurnMutation(conversationID string) func() {
	if s == nil || s.Pending == nil {
		return func() {}
	}
	turn, ok := s.Pending.GetByConversationID(strings.TrimSpace(conversationID))
	if !ok || turn == nil || turn.MutationMu == nil {
		return func() {}
	}
	turn.MutationMu.Lock()
	return turn.MutationMu.Unlock
}

type expectedRequestContextKey struct{}

func withExpectedRequest(ctx context.Context, requestID string) context.Context {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, expectedRequestContextKey{}, requestID)
}

func expectedRequestID(ctx context.Context) string {
	value, _ := ctx.Value(expectedRequestContextKey{}).(string)
	return strings.TrimSpace(value)
}

func (s *Service) ensureExpectedRequest(ctx context.Context, conversationID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	expected := expectedRequestID(ctx)
	if expected == "" {
		return nil
	}
	actual, ok := s.ActiveRequestID(conversationID)
	if !ok {
		return ErrPendingNotFound
	}
	if actual != expected {
		return ErrPendingConflict
	}
	return nil
}

func responseIDForConversation(pending pendingRegistryLike, conversationID string) string {
	if pending == nil {
		return ""
	}
	turn, ok := pending.GetByConversationID(conversationID)
	if !ok || turn == nil {
		return ""
	}
	return strings.TrimSpace(turn.ResponseID)
}

func pendingExpiredBody(ttl time.Duration) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"message": "pending turn expired after " + ttl.String(),
			"type":    "request_timeout",
			"code":    "request_timeout",
		},
	}
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}

func (s *Service) resolveTarget(ctx context.Context, input SubmitInput) (SubmitTarget, error) {
	if s.Resolver == nil {
		return SubmitTarget{}, nil
	}
	target, err := s.Resolver.Resolve(ctx, conversationresolve.ResolveInput{
		OwnerID: input.OwnerID,
		Request: input.Request,
	})
	if err != nil {
		return SubmitTarget{}, err
	}
	return SubmitTarget{
		ConversationID: target.ConversationID,
		Reuse:          target.Reuse,
		Source:         target.Source,
	}, nil
}

type pendingRegistryLike interface {
	WaitTurn(context.Context, *PendingTurn) (PendingResult, error)
	StartDelta(string) (string, error)
	RevertFinalize(string, string)
	Publish(string, PendingEvent) error
	StartComplete(string) (string, error)
	Resolve(string, PendingResult) error
	StartAbort(string) (string, error)
	Abort(string, map[string]any) error
	ExpireOlderThan(time.Time, map[string]any) int
	Add(*PendingTurn)
	GetByConversationID(string) (*PendingTurn, bool)
	FindByToolCallID(string, string) (*PendingTurn, bool)
	FindConversationIDByToolCallID(string, string) (string, bool)
}

func (s *Service) disconnectOnRequestDone(ctx context.Context, conversationID string, requestID string, reason string) {
	if s == nil {
		return
	}
	if ctx == nil {
		return
	}
	<-ctx.Done()
	if !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return
	}
	if _, pending := s.Pending.GetByConversationID(conversationID); !pending {
		return
	}
	_ = s.disconnectPendingRequest(context.Background(), conversationID, TurnIdentity{RequestID: requestID}, reason, "request_disconnected", "Request Disconnected")
}

func (s *Service) disconnectPendingRequest(ctx context.Context, conversationID string, identity TurnIdentity, reason string, eventType string, title string) error {
	unlock := s.lockTurnMutation(conversationID)
	defer unlock()
	resolved, err := s.resolveIdentityForMutation(ctx, conversationID, identity)
	if err != nil {
		return err
	}
	result, err := s.Store.DisconnectPendingTurnWithEvent(ctx, common.PendingTurnLifecycleMutationInput{
		ConversationID: conversationID,
		Reason:         reason,
		Identity:       common.TurnIdentity{OwnerID: resolved.OwnerID, RequestID: resolved.RequestID},
		EventType:      eventType,
		EventLevel:     "warn",
		EventTitle:     title,
		EventDetail:    reason,
		EventMetadata: map[string]any{
			"reason":          reason,
			"event_type":      eventType,
			"conversation_id": conversationID,
			"request_id":      resolved.RequestID,
			"owner_id":        resolved.OwnerID,
			"disconnected_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	if err != nil {
		if errors.Is(err, common.ErrPendingDisconnected) {
			return err
		}
		return err
	}
	conversation := result.Conversation
	route := eventRouteFromIdentity(conversationID, resolved)
	s.publishConversationUpserted(ctx, route, conversation)
	s.publishConversationEventAppended(ctx, route, conversation, result.Event)
	body := map[string]any{
		"error": map[string]any{
			"message": reason,
			"type":    eventType,
			"code":    eventType,
		},
	}
	action := OutputAction{Kind: TurnControlAbort, AbortReason: reason}.Normalized()
	runtimeAction := action.RuntimeAction()
	runtimeAction.ErrorBody = body
	_ = s.Pending.Publish(conversationID, PendingEvent{
		Action:       action,
		ErrorBody:    body,
		StreamEvents: s.applyRuntimeAction(conversationID, runtimeAction).StreamEvents,
	})
	_ = s.Pending.Abort(conversationID, body)
	logging.BindContext(s.Logger, context.Background(),
		zap.String("conversation.id", conversationID),
		zap.String("request.id", resolved.RequestID),
	).Info("pending request disconnected")
	return nil
}

func (s *Service) publishConversationUpserted(ctx context.Context, route eventRoute, conversation common.Conversation) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	s.Events.Publish(ctx, chatevents.Event{
		Type:           chatevents.TypeConversationUpserted,
		OwnerID:        route.OwnerID,
		ConversationID: route.ConversationID,
		RequestID:      route.RequestID,
		ControlManaged: expectedRequestID(ctx) != "",
		Conversation:   conversation,
	})
}

func (s *Service) publishTurnWaiting(ctx context.Context, turn *PendingTurn, lastUserText string) {
	if s == nil || s.Events == nil || turn == nil || strings.TrimSpace(turn.OwnerID) == "" {
		return
	}
	waiting := chatevents.WaitingTurn{
		OwnerID: strings.TrimSpace(turn.OwnerID), RequestID: strings.TrimSpace(turn.RequestID),
		ResponseID: strings.TrimSpace(turn.ResponseID), ConversationID: strings.TrimSpace(turn.ConversationID),
		Protocol: strings.TrimSpace(turn.RequestFormat), Model: strings.TrimSpace(turn.Model),
		LastUserText: lastUserText,
	}
	s.Events.Publish(ctx, chatevents.Event{
		Type: chatevents.TypeTurnWaiting, OwnerID: waiting.OwnerID,
		ConversationID: waiting.ConversationID, WaitingTurn: &waiting,
	})
}

func (s *Service) publishMessageAppended(ctx context.Context, route eventRoute, conversation common.Conversation, message common.Message) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	msg := message
	s.Events.Publish(ctx, chatevents.Event{
		Type:           chatevents.TypeMessageAppended,
		OwnerID:        route.OwnerID,
		ConversationID: route.ConversationID,
		RequestID:      route.RequestID,
		Conversation:   conversation,
		Message:        &msg,
	})
}

func (s *Service) publishConversationEventAppended(ctx context.Context, route eventRoute, conversation common.Conversation, event common.ConversationEvent) {
	if s == nil || s.Events == nil {
		return
	}
	route = route.withConversation(conversation)
	if route.OwnerID == "" {
		return
	}
	evt := event
	s.Events.Publish(ctx, chatevents.Event{
		Type:              chatevents.TypeConversationEventAppended,
		OwnerID:           route.OwnerID,
		ConversationID:    route.ConversationID,
		RequestID:         route.RequestID,
		Conversation:      conversation,
		ConversationEvent: &evt,
	})
}

func (s *Service) eventRouteForContext(ctx context.Context, conversation common.Conversation) eventRoute {
	return eventRoute{
		OwnerID:        strings.TrimSpace(s.ownerID(ctx)),
		ConversationID: strings.TrimSpace(conversation.ID),
	}.withConversation(conversation)
}

func (s *Service) resolveActiveTurnIdentity(ctx context.Context, conversationID string) (TurnIdentity, error) {
	if s == nil {
		return TurnIdentity{}, nil
	}
	if turn, ok := s.Pending.GetByConversationID(conversationID); ok && turn != nil {
		return TurnIdentity{
			OwnerID:   strings.TrimSpace(turn.OwnerID),
			RequestID: strings.TrimSpace(turn.RequestID),
		}, nil
	}
	return s.resolveStoredTurnIdentity(ctx, conversationID)
}

func (s *Service) resolveStoredTurnIdentity(ctx context.Context, conversationID string) (TurnIdentity, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(conversationID) == "" {
		return TurnIdentity{}, common.ErrNotFound
	}
	req, err := s.Store.GetLatestRequestForConversation(ctx, conversationID)
	if err != nil {
		return TurnIdentity{}, err
	}
	return TurnIdentity{
		OwnerID:   strings.TrimSpace(req.OwnerID),
		RequestID: strings.TrimSpace(req.RequestID),
	}, nil
}

func (s *Service) resolveIdentityForMutation(ctx context.Context, conversationID string, base TurnIdentity) (TurnIdentity, error) {
	identity := TurnIdentity{
		OwnerID:   strings.TrimSpace(base.OwnerID),
		RequestID: strings.TrimSpace(base.RequestID),
	}
	if identity.OwnerID != "" && identity.RequestID != "" {
		return identity, nil
	}
	resolved, err := s.resolveActiveTurnIdentity(ctx, conversationID)
	if err != nil {
		if identity.OwnerID != "" || identity.RequestID != "" {
			return identity, nil
		}
		return TurnIdentity{}, err
	}
	if identity.OwnerID == "" {
		identity.OwnerID = resolved.OwnerID
	}
	if identity.RequestID == "" {
		identity.RequestID = resolved.RequestID
	}
	if identity.OwnerID == "" {
		identity.OwnerID = strings.TrimSpace(s.ownerID(ctx))
	}
	return identity, nil
}
