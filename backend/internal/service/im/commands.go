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

	"github.com/zyf2007/ChatAPI/internal/actor"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
)

const maxInboundTextRunes = 8000

func (s *Service) handleInbound(ctx context.Context, runtime *accountRuntime, inbound InboundMessage) error {
	ownerID := runtime.ownerID
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	err := s.requireActiveOwner(checkCtx, ownerID)
	cancel()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	s.mu.Lock()
	if status := s.statuses[ownerID]; status != nil {
		status.lastInboundAt = &now
	}
	s.mu.Unlock()

	text := strings.TrimSpace(inbound.Text)
	if text == "" {
		s.replyBestEffort(ctx, runtime, inbound, "首版微信接入仅支持单条文本消息。")
		return nil
	}
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > maxInboundTextRunes {
		s.replyBestEffort(ctx, runtime, inbound, "消息过长，请缩短后重试。")
		return nil
	}

	command, argument := splitCommand(text)
	switch command {
	case "/bind":
		status := s.connectionSummary(ownerID)
		s.replyBestEffort(ctx, runtime, inbound, "绑定已刷新。"+status+"\n\n"+commandHelp())
	case "/help":
		s.replyBestEffort(ctx, runtime, inbound, commandHelp())
	case "/list":
		s.replyBestEffort(ctx, runtime, inbound, s.pendingList(ownerID))
	case "/use":
		pending, err := s.selectPending(ownerID, argument)
		if err != nil {
			s.replyBestEffort(ctx, runtime, inbound, err.Error())
			return nil
		}
		s.replyBestEffort(ctx, runtime, inbound, fmt.Sprintf("已选择请求 %s（%s）。直接回复即可结束该请求。", shortRef(pending.ConversationID), pending.Model))
	case "/abort":
		s.handleAbort(ctx, runtime, inbound, argument)
	case "":
		s.handleComplete(ctx, runtime, inbound, text)
	default:
		s.replyBestEffort(ctx, runtime, inbound, "未知命令。\n\n"+commandHelp())
	}
	return nil
}

func (s *Service) handleComplete(ctx context.Context, runtime *accountRuntime, inbound InboundMessage, text string) {
	pending, err := s.currentPending(runtime.ownerID)
	if err != nil {
		s.replyBestEffort(ctx, runtime, inbound, err.Error())
		return
	}
	if s.Control == nil {
		s.replyBestEffort(ctx, runtime, inbound, "回复服务暂不可用，请打开 Web 工作区处理。")
		return
	}
	controlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	controlCtx = actor.WithActor(controlCtx, actor.Actor{
		UserID: runtime.ownerID, Source: "im", EntryPoint: ProviderClawBot, PrincipalID: runtime.ownerID,
	})
	_, err = s.Control.Execute(controlCtx, controlsvc.Command{
		Source: controlsvc.SourceIM, OwnerID: runtime.ownerID,
		ConversationID: pending.ConversationID, ResponseID: pending.ResponseID, RequestID: pending.RequestID,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlStreamComplete, OutputText: text, Mode: "assistant_message"},
	})
	if err != nil {
		s.clearSelectionIf(runtime.ownerID, pending.ConversationID)
		s.replyBestEffort(ctx, runtime, inbound, "该请求已经结束、失效或不可由当前账号处理。请发送 /list 刷新。")
		return
	}
	s.clearSelectionIf(runtime.ownerID, pending.ConversationID)
	s.replyBestEffort(ctx, runtime, inbound, fmt.Sprintf("已结束请求 %s。", shortRef(pending.ConversationID)))
}

func (s *Service) handleAbort(ctx context.Context, runtime *accountRuntime, inbound InboundMessage, reason string) {
	pending, err := s.currentPending(runtime.ownerID)
	if err != nil {
		s.replyBestEffort(ctx, runtime, inbound, err.Error())
		return
	}
	if s.Control == nil {
		s.replyBestEffort(ctx, runtime, inbound, "回复服务暂不可用，请打开 Web 工作区处理。")
		return
	}
	if strings.TrimSpace(reason) == "" {
		reason = "operator aborted the request from WeChat ClawBot"
	}
	controlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	controlCtx = actor.WithActor(controlCtx, actor.Actor{
		UserID: runtime.ownerID, Source: "im", EntryPoint: ProviderClawBot, PrincipalID: runtime.ownerID,
	})
	_, err = s.Control.Execute(controlCtx, controlsvc.Command{
		Source: controlsvc.SourceIM, OwnerID: runtime.ownerID,
		ConversationID: pending.ConversationID, ResponseID: pending.ResponseID, RequestID: pending.RequestID,
		Action: turnsvc.OutputAction{Kind: turnsvc.TurnControlAbort, AbortReason: reason},
	})
	if err != nil {
		s.clearSelectionIf(runtime.ownerID, pending.ConversationID)
		s.replyBestEffort(ctx, runtime, inbound, "该请求已经结束、失效或不可由当前账号处理。请发送 /list 刷新。")
		return
	}
	s.clearSelectionIf(runtime.ownerID, pending.ConversationID)
	s.replyBestEffort(ctx, runtime, inbound, fmt.Sprintf("已中止请求 %s。", shortRef(pending.ConversationID)))
}

func (s *Service) replyBestEffort(ctx context.Context, runtime *accountRuntime, inbound InboundMessage, text string) {
	s.mu.Lock()
	if s.runtimes[runtime.ownerID] != runtime || s.accountGen[runtime.ownerID] != runtime.generation {
		s.mu.Unlock()
		return
	}
	account := s.accounts[runtime.ownerID]
	provider := s.providers[account.Provider]
	s.mu.Unlock()
	if provider == nil {
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	err := provider.Send(sendCtx, account, OutboundMessage{
		To: inbound.From, ContextToken: inbound.ContextToken, Text: text,
		ClientID: inboundReplyClientID(inbound.ID, text),
	})
	cancel()
	if err == nil {
		now := time.Now().UTC()
		s.mu.Lock()
		if status := s.statuses[runtime.ownerID]; status != nil {
			status.lastOutboundAt = &now
			status.lastError = ""
			status.lastErrorAt = nil
		}
		s.mu.Unlock()
		return
	}
	if errors.Is(err, ErrReauthRequired) {
		s.markReauthRequired(runtime.ownerID)
	} else if errors.Is(err, ErrProviderNotReady) {
		invalidVersion := inbound.ReadinessVersion
		if invalidVersion == "" {
			invalidVersion = provider.ReadinessVersion(account)
		}
		s.markProviderNotReady(runtime.ownerID, invalidVersion)
	} else {
		s.recordOwnerError(runtime.ownerID, "微信确认消息发送失败")
	}
}

func (s *Service) currentPending(ownerID string) (*turnsvc.PendingTurn, error) {
	if s.Pending == nil {
		return nil, errors.New("回复服务暂不可用，请打开 Web 工作区处理。")
	}
	items := sortedPending(s.Pending.ListByOwnerID(ownerID))
	if len(items) == 0 {
		return nil, errors.New("当前没有等待中的请求。")
	}
	s.mu.Lock()
	selectedID := s.selected[ownerID]
	s.mu.Unlock()
	if selectedID != "" {
		for _, item := range items {
			if item.ConversationID == selectedID {
				copy := *item
				return &copy, nil
			}
		}
	}
	latest := items[len(items)-1]
	s.mu.Lock()
	s.selected[ownerID] = latest.ConversationID
	s.mu.Unlock()
	copy := *latest
	return &copy, nil
}

func (s *Service) selectPending(ownerID, reference string) (*turnsvc.PendingTurn, error) {
	if s.Pending == nil {
		return nil, errors.New("回复服务暂不可用，请打开 Web 工作区处理。")
	}
	reference = strings.ToLower(strings.TrimSpace(reference))
	if len(reference) < 4 {
		return nil, errors.New("用法：/use <至少 4 位请求编号>")
	}
	var matches []*turnsvc.PendingTurn
	for _, item := range sortedPending(s.Pending.ListByOwnerID(ownerID)) {
		conversationID := strings.ToLower(item.ConversationID)
		requestID := strings.ToLower(item.RequestID)
		if strings.HasPrefix(conversationID, reference) || strings.HasPrefix(requestID, reference) {
			matches = append(matches, item)
		}
	}
	if len(matches) == 0 {
		return nil, errors.New("没有找到该请求，请发送 /list 查看最新编号。")
	}
	if len(matches) > 1 {
		return nil, errors.New("编号不唯一，请输入更多字符。")
	}
	selected := *matches[0]
	s.mu.Lock()
	s.selected[ownerID] = selected.ConversationID
	s.mu.Unlock()
	return &selected, nil
}

func (s *Service) pendingList(ownerID string) string {
	if s.Pending == nil {
		return "回复服务暂不可用，请打开 Web 工作区处理。"
	}
	items := sortedPending(s.Pending.ListByOwnerID(ownerID))
	if len(items) == 0 {
		return "当前没有等待中的请求。"
	}
	s.mu.Lock()
	selectedID := s.selected[ownerID]
	s.mu.Unlock()
	var builder strings.Builder
	builder.WriteString("等待中的请求：\n")
	start := 0
	if len(items) > 10 {
		start = len(items) - 10
	}
	for _, item := range items[start:] {
		marker := "  "
		if item.ConversationID == selectedID {
			marker = "→ "
		}
		fmt.Fprintf(&builder, "%s%s · %s\n", marker, shortRef(item.ConversationID), truncateText(item.Model, 60))
	}
	builder.WriteString("发送 /use <编号> 切换；直接回复会结束当前选中的请求。")
	return strings.TrimSpace(builder.String())
}

func (s *Service) clearSelectionIf(ownerID, conversationID string) {
	s.mu.Lock()
	if s.selected[ownerID] == conversationID {
		delete(s.selected, ownerID)
	}
	s.mu.Unlock()
}

func (s *Service) connectionSummary(ownerID string) string {
	s.mu.Lock()
	status := s.statusLocked(ownerID)
	s.mu.Unlock()
	if status.Ready {
		return "当前连接已可接收 ChatAPI 通知。"
	}
	return "已收到你的消息，连接将在状态保存后开始接收通知。"
}

func splitCommand(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", ""
	}
	command := fields[0]
	return strings.ToLower(command), strings.TrimSpace(text[len(command):])
}

func commandHelp() string {
	return "微信 ClawBot 命令：\n直接回复：结束当前请求\n/list：查看等待请求\n/use <编号>：切换请求\n/abort [原因]：中止请求\n/bind：刷新绑定\n/help：显示帮助\n\n首版不支持流式片段、思考、工具调用、媒体或群聊。"
}

func inboundReplyClientID(messageID, text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(messageID) + "\x00" + text))
	return "chatapi-reply-" + hex.EncodeToString(sum[:12])
}
