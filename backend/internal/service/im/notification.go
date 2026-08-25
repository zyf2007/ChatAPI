package im

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"

	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
)

func (s *Service) notificationWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.notifyWake:
			ownerID, job, ok := s.takeNotification()
			if !ok {
				continue
			}
			s.sendWaitingNotification(ctx, ownerID, job.waiting)
			s.finishNotification(ownerID)
		}
	}
}

func (s *Service) takeNotification() (string, notificationJob, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ownerID, job := range s.notification {
		if s.notifyInFlight[ownerID] {
			continue
		}
		delete(s.notification, ownerID)
		s.notifyInFlight[ownerID] = true
		for candidate := range s.notification {
			if !s.notifyInFlight[candidate] {
				s.signalNotificationLocked()
				break
			}
		}
		return ownerID, job, true
	}
	return "", notificationJob{}, false
}

func (s *Service) finishNotification(ownerID string) {
	s.mu.Lock()
	delete(s.notifyInFlight, ownerID)
	if _, ok := s.notification[ownerID]; ok {
		s.signalNotificationLocked()
	}
	s.mu.Unlock()
}

func (s *Service) sendWaitingNotification(ctx context.Context, ownerID string, waiting chatevents.WaitingTurn) {
	if !s.waitingStillPending(ownerID, waiting.ConversationID, waiting.RequestID) {
		s.clearLatestWaiting(ownerID, waiting.ConversationID, waiting.RequestID)
		return
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.requireActiveOwner(checkCtx, ownerID)
	cancel()
	if err != nil {
		return
	}

	text := waitingNotificationText(waiting)
	clientID := notificationClientID(waiting.RequestID)
	for attempt := range 3 {
		if !s.waitingStillPending(ownerID, waiting.ConversationID, waiting.RequestID) {
			s.clearLatestWaiting(ownerID, waiting.ConversationID, waiting.RequestID)
			return
		}
		err = s.withRuntime(ownerID, func(provider Provider, account Account) error {
			if !provider.Ready(account) {
				return ErrProviderNotReady
			}
			sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			if err := provider.Send(sendCtx, account, OutboundMessage{Text: text, ClientID: clientID}); err != nil {
				switch {
				case errors.Is(err, ErrProviderNotReady):
					s.markProviderNotReady(ownerID, provider.ReadinessVersion(account))
				case errors.Is(err, ErrReauthRequired):
					s.markReauthRequired(ownerID)
				case attempt == 2:
					s.recordOwnerError(ownerID, "微信通知发送失败；新请求到达时会再次尝试")
				}
				return err
			}
			if s.waitingStillPending(ownerID, waiting.ConversationID, waiting.RequestID) {
				s.mu.Lock()
				s.selected[ownerID] = waiting.ConversationID
				s.mu.Unlock()
			}
			s.recordOutbound(ownerID)
			return nil
		})
		if err == nil || errors.Is(err, ErrConnectionNotFound) {
			return
		}
		if errors.Is(err, ErrProviderNotReady) || errors.Is(err, ErrReauthRequired) {
			return
		}
		if attempt < 2 && waitForContext(ctx, time.Duration(attempt+1)*time.Second) {
			continue
		}
	}
	if err != nil {
		s.Logger.Warn("send IM waiting notification failed", zap.String("owner_id", ownerID), zap.Error(err))
	}
}

func (s *Service) withRuntime(ownerID string, fn func(Provider, Account) error) error {
	s.mu.Lock()
	runtime := s.runtimes[ownerID]
	generation := s.accountGen[ownerID]
	s.mu.Unlock()
	if runtime == nil {
		return ErrConnectionNotFound
	}
	runtime.barrier.Lock()
	defer runtime.barrier.Unlock()
	s.mu.Lock()
	if s.runtimes[ownerID] != runtime || s.accountGen[ownerID] != generation {
		s.mu.Unlock()
		return ErrConnectionNotFound
	}
	account := s.accounts[ownerID]
	provider := s.providers[account.Provider]
	s.mu.Unlock()
	if provider == nil {
		return errors.New("IM provider is unavailable")
	}
	return fn(provider, account)
}

func (s *Service) waitingStillPending(ownerID, conversationID, requestID string) bool {
	if s.Pending == nil {
		return false
	}
	for _, pending := range s.Pending.ListByOwnerID(ownerID) {
		if pending != nil && pending.ConversationID == conversationID && pending.RequestID == requestID {
			return true
		}
	}
	return false
}

func (s *Service) clearLatestWaiting(ownerID, conversationID, requestID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.latestWaiting[ownerID]
	if ok && current.ConversationID == conversationID && current.RequestID == requestID {
		delete(s.latestWaiting, ownerID)
	}
}

func (s *Service) markProviderNotReady(ownerID, invalidReadinessVersion string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status := s.statuses[ownerID]; status != nil {
		status.contextInvalid = true
		status.invalidReadinessVersion = invalidReadinessVersion
		status.lastError = "微信回复上下文已过期，请从扫码微信发送 /bind"
		now := time.Now().UTC()
		status.lastErrorAt = &now
	}
}

func (s *Service) markReauthRequired(ownerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status := s.statuses[ownerID]; status != nil {
		status.reauthRequired = true
		status.workerState = "reauth_required"
		status.lastError = "微信登录已失效，请重新扫码连接"
		now := time.Now().UTC()
		status.lastErrorAt = &now
	}
}

func (s *Service) recordOutbound(ownerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status := s.statuses[ownerID]; status != nil {
		now := time.Now().UTC()
		status.lastOutboundAt = &now
		status.lastError = ""
		status.lastErrorAt = nil
	}
}

func (s *Service) recordOwnerError(ownerID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status := s.statuses[ownerID]; status != nil {
		status.lastError = message
		now := time.Now().UTC()
		status.lastErrorAt = &now
	}
}

func waitingNotificationText(waiting chatevents.WaitingTurn) string {
	conversationRef := shortRef(waiting.ConversationID)
	model := truncateText(strings.TrimSpace(waiting.Model), 80)
	if model == "" {
		model = "unknown"
	}
	userText := truncateText(strings.TrimSpace(waiting.LastUserText), 1200)
	if userText == "" {
		userText = "（没有可展示的用户文本）"
	}
	return fmt.Sprintf(
		"ChatAPI 新请求\n编号：%s\n模型：%s\n\n%s\n\n直接回复将结束该请求。\n/list 查看等待请求 · /use <编号> 切换 · /abort 中止 · /help 帮助",
		conversationRef, model, userText,
	)
}

func notificationClientID(requestID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(requestID)))
	return "chatapi-wait-" + hex.EncodeToString(sum[:12])
}

func shortRef(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func waitForContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func truncateText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if maxRunes <= 0 || utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	runes := []rune(value)
	return string(runes[:maxRunes]) + "…"
}
