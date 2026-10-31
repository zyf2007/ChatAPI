package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
)

type ChatAPIHandler struct {
	Turn      *turnsvc.Service
	Query     *turnquerysvc.Service
	Timeline  *timelinesvc.Service
	Ingress   *ingresssvc.Service
	Streaming *streamingsvc.Service
	Catalog   *catalogsvc.Service
	Control   *controlsvc.Service
	Egress    *egresssvc.Service
	Logger    *zap.Logger
}

func (h ChatAPIHandler) Responses(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "responses")
}

func (h ChatAPIHandler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "chat_completions")
}

func (h ChatAPIHandler) AnthropicMessages(w http.ResponseWriter, r *http.Request) {
	h.handleProtocolRequest(w, r, "anthropic_messages")
}

func (h ChatAPIHandler) ListModels(w http.ResponseWriter, r *http.Request) {
	ownerID := ""
	if principal, ok := modelkey.PrincipalFromContext(r.Context()); ok {
		ownerID = strings.TrimSpace(principal.UserID)
	} else if value, ok := labOwnerID(r); ok {
		ownerID = value
	}
	if ownerID == "" || h.Catalog == nil {
		httpx.WriteJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{
				"message": "unauthorized",
				"type":    "invalid_request_error",
			},
		})
		return
	}

	items, err := h.Catalog.ListModelsForOwner(r.Context(), ownerID)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("owner.id", ownerID)).Error("list models failed", zap.Error(err))
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{
				"message": "internal server error",
				"type":    "server_error",
			},
		})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   items,
	})
}

func (h ChatAPIHandler) ListConversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID := strings.TrimSpace(chi.URLParam(r, "conversationID"))
	items, err := h.Query.ListMessages(r.Context(), conversationID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h ChatAPIHandler) DeltaOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlStreamDelta)
}

func (h ChatAPIHandler) CompleteOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlStreamComplete)
}

func (h ChatAPIHandler) AbortOutput(w http.ResponseWriter, r *http.Request) {
	h.executeTurnControl(w, r, turnsvc.TurnControlAbort)
}

func (h ChatAPIHandler) handleProtocolRequest(w http.ResponseWriter, r *http.Request, requestFormat string) {
	rawBody, readErr := io.ReadAll(r.Body)
	logging.DebugProtocolRequestBody(h.Logger, r.Context(), requestFormat, rawBody)
	logger := logging.BindContext(h.Logger, r.Context(),
		zap.String("protocol", requestFormat),
		zap.Int("request.body.bytes", len(rawBody)),
	)
	if readErr != nil {
		logger.Warn("read protocol request body failed", zap.Error(readErr))
		httpx.WriteJSON(w, http.StatusBadRequest, h.egress().InvalidJSONBody(requestFormat))
		return
	}

	var body map[string]any
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&body); err != nil {
		logger.Warn("invalid protocol request json", zap.Error(err))
		httpx.WriteJSON(w, http.StatusBadRequest, h.egress().InvalidJSONBody(requestFormat))
		return
	}
	requestMeta := httpx.CaptureRequestMeta(r)
	parsedReq, err := h.Ingress.Parse(r.Context(), requestFormat, body, requestMeta)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("protocol", requestFormat)).Warn("protocol ingress failed", zap.Error(err))
		httpx.WriteJSON(w, h.egress().ErrorStatus(err), h.egress().ErrorBody(requestFormat, err))
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("protocol", requestFormat),
		zap.String("model", parsedReq.Request.Model),
		zap.Bool("stream", parsedReq.Request.Stream),
	).Info("accepted protocol request")
	if parsedReq.Request.Stream {
		h.handleStreamRequest(w, r, parsedReq)
		return
	}

	responseBody, err := h.Ingress.SubmitResponse(r.Context(), parsedReq)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("protocol", requestFormat)).Error("create pending response failed", zap.Error(err))
		httpx.WriteJSON(w, h.egress().ErrorStatus(err), h.egress().ErrorBody(requestFormat, err))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, responseBody)
}

func (h ChatAPIHandler) handleStreamRequest(w http.ResponseWriter, r *http.Request, parsed ingresssvc.ParsedRequest) {
	ctx := logging.WithConnectionID(r.Context(), logging.NewConnectionID("sse"))
	startedAt := time.Now()
	flusher, ok := w.(http.Flusher)
	if !ok {
		logging.BindContext(h.Logger, ctx).Error("streaming not supported by response writer")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	turn, _, err := h.Ingress.SubmitStream(ctx, parsed)
	if err != nil {
		logging.BindContext(h.Logger, ctx,
			zap.String("protocol", parsed.Request.Protocol.String()),
			zap.Error(err),
		).Error("create pending stream failed")
		httpx.WriteJSON(w, h.egress().ErrorStatus(err), h.egress().ErrorBody(parsed.Request.Protocol.String(), err))
		return
	}
	logging.BindContext(h.Logger, ctx,
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	).Info("stream pending turn created")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	logger := logging.BindContext(h.Logger, ctx,
		zap.String("protocol", parsed.Request.Protocol.String()),
		zap.String("conversation.id", turn.ConversationID),
		zap.String("request.id", turn.RequestID),
	)
	logger.Info("protocol stream started")
	if err := h.Streaming.StreamPendingTurn(ctx, w, turn); err != nil {
		logger.Warn("protocol stream ended",
			zap.Duration("stream.duration", time.Since(startedAt)),
			zap.String("stream.end_reason", "write_failed"),
			zap.Error(err),
		)
		return
	}
	logger.Info("protocol stream ended",
		zap.Duration("stream.duration", time.Since(startedAt)),
		zap.String("stream.end_reason", "completed"),
	)
}

func (h ChatAPIHandler) egress() *egresssvc.Service {
	if h.Egress != nil {
		return h.Egress
	}
	return egresssvc.New()
}

func (h ChatAPIHandler) executeTurnControl(w http.ResponseWriter, r *http.Request, kind turnsvc.TurnControlKind) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("turn.control", string(kind))).Warn("invalid turn control json")
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	command, err := buildTurnControlCommand(kind, body)
	if err != nil {
		logging.BindContext(h.Logger, r.Context(), zap.String("turn.control", string(kind))).Warn("invalid turn control command")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	command.OwnerID = ownerIDFromControlContext(r)
	result, err := h.control().Execute(r.Context(), command)
	if err != nil {
		if writeControlError(w, err) {
			return
		}
		httpx.WriteJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]any{"code": "turn_control_failed", "message": err.Error()}})
		return
	}
	logging.BindContext(h.Logger, r.Context(),
		zap.String("turn.control", string(kind)),
		zap.String("conversation.id", command.ConversationID),
	).Info("turn control executed")
	httpx.WriteJSON(w, http.StatusOK, result.Body)
}

func (h ChatAPIHandler) control() *controlsvc.Service {
	if h.Control != nil {
		return h.Control
	}
	return controlsvc.New(h.Query, h.Turn, h.Logger)
}

func ownerIDFromControlContext(r *http.Request) string {
	if principal, ok := appkey.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID)
	}
	if principal, ok := session.PrincipalFromContext(r.Context()); ok && strings.TrimSpace(principal.UserID) != "" {
		return strings.TrimSpace(principal.UserID)
	}
	return ""
}

func buildTurnControlCommand(kind turnsvc.TurnControlKind, body map[string]any) (controlsvc.Command, error) {
	conversationID, err := mustConversationID(body)
	if err != nil {
		return controlsvc.Command{}, err
	}
	requestID := strings.TrimSpace(stringValue(body["request_id"], ""))
	if requestID == "" {
		return controlsvc.Command{}, errors.New("request_id is required")
	}
	return controlsvc.Command{
		Source:         controlsvc.SourceAPI,
		ConversationID: conversationID,
		ResponseID:     stringValue(body["response_id"], ""),
		RequestID:      requestID,
		Action: turnsvc.OutputAction{
			Kind:                kind,
			OutputText:          stringValue(body["text"], ""),
			Mode:                stringValue(body["mode"], ""),
			ToolName:            stringValue(body["tool_name"], ""),
			ToolCallID:          stringValue(body["tool_call_id"], ""),
			ToolOutput:          stringValue(body["output"], ""),
			BuiltinToolKind:     stringValue(body["builtin_tool_kind"], ""),
			BuiltinToolQuery:    stringValue(body["builtin_tool_query"], ""),
			BuiltinToolAssetID:  stringValue(body["builtin_tool_asset_id"], ""),
			ReasoningStreamMode: stringValue(body["reasoning_stream_mode"], ""),
			AbortReason:         stringValue(body["error"], ""),
		},
	}, nil
}

func mustConversationID(input map[string]any) (string, error) {
	conversationID, ok := input["conversation_id"].(string)
	if !ok || strings.TrimSpace(conversationID) == "" {
		return "", errors.New("conversation_id is required")
	}
	return strings.TrimSpace(conversationID), nil
}

func stringValue(value any, fallback string) string {
	if raw, ok := value.(string); ok && strings.TrimSpace(raw) != "" {
		return strings.TrimSpace(raw)
	}
	return fallback
}
