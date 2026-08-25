package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/zyf2007/ChatAPI/internal/http/httpx"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	imsvc "github.com/zyf2007/ChatAPI/internal/service/im"
)

type IMHandler struct {
	Service *imsvc.Service
}

func (h IMHandler) Status(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := imOwnerID(r)
	if !ok || h.Service == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	status, err := h.Service.GetStatus(r.Context(), ownerID)
	if err != nil {
		writeIMError(w, err, http.StatusInternalServerError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (h IMHandler) StartLogin(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := imOwnerID(r)
	if !ok || h.Service == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	view, err := h.Service.BeginLogin(r.Context(), ownerID, imsvc.ProviderClawBot)
	if err != nil {
		writeIMError(w, err, http.StatusBadGateway)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h IMHandler) PollLogin(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := imOwnerID(r)
	if !ok || h.Service == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var input struct {
		VerifyCode string `json:"verify_code"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "session_id"))
	if sessionID == "" {
		http.Error(w, "login session is required", http.StatusBadRequest)
		return
	}
	view, err := h.Service.PollLogin(r.Context(), ownerID, sessionID, input.VerifyCode)
	if err != nil {
		writeIMError(w, err, http.StatusBadGateway)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, view)
}

func (h IMHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	ownerID, ok := imOwnerID(r)
	if !ok || h.Service == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.Service.Disconnect(r.Context(), ownerID); err != nil {
		writeIMError(w, err, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func imOwnerID(r *http.Request) (string, bool) {
	principal, ok := session.PrincipalFromContext(r.Context())
	ownerID := strings.TrimSpace(principal.UserID)
	return ownerID, ok && ownerID != ""
}

func writeIMError(w http.ResponseWriter, err error, fallback int) {
	status := fallback
	message := "微信 ClawBot 请求失败，请稍后重试"
	switch {
	case errors.Is(err, imsvc.ErrOwnerInactive):
		status = http.StatusForbidden
		message = "当前用户已停用"
	case errors.Is(err, imsvc.ErrLoginNotFound), errors.Is(err, imsvc.ErrConnectionNotFound):
		status = http.StatusNotFound
		message = "微信登录会话或连接不存在，请重新开始"
	case errors.Is(err, imsvc.ErrLoginBusy):
		status = http.StatusConflict
		message = "正在查询二维码状态，请稍后重试"
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
		message = "微信服务响应超时，请稍后重试"
	}
	httpx.WriteJSON(w, status, map[string]any{"error": message})
}
