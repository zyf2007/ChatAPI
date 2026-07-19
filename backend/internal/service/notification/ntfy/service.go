package ntfy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/ntfy"
	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"go.uber.org/zap"
)

const userSettingsKey = "settings"
const maxBodyRunes = 280
const sendTimeout = 5 * time.Second

// Service sends user-configured ntfy notifications for pending chat turns.
type Service struct {
	configs configrepo.Store
	client  *ntfy.Client
	logger  *zap.Logger
}

func New(configs configrepo.Store, client *ntfy.Client, logger *zap.Logger) *Service {
	if client == nil {
		client = ntfy.NewClient(nil)
	}
	return &Service{
		configs: configs,
		client:  client,
		logger:  logger,
	}
}

// NotifyWaiting is intended for Submitter.Hooks.NotifyWaiting.
// It fires when an external model request enters the pending/waiting state.
func (s *Service) NotifyWaiting(ctx context.Context, ownerID string, title string, userText string) {
	if s == nil || s.configs == nil || s.client == nil {
		return
	}
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" {
		return
	}

	url, enabled, err := s.resolveNtfyTarget(ctx, ownerID)
	if err != nil {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", ownerID),
			zap.Error(err),
		).Debug("ntfy target resolve skipped")
		return
	}
	if !enabled || url == "" {
		return
	}

	safety := urlsafety.ValidateNtfyURL(url, false)
	if !safety.OK {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", ownerID),
			zap.String("ntfy.reason", safety.Reason),
		).Warn("ntfy url rejected")
		return
	}

	msgTitle := strings.TrimSpace(title)
	if msgTitle == "" {
		msgTitle = "ChatAPI"
	}
	msgTitle = "ChatAPI · " + truncateRunes(msgTitle, 64)

	body := strings.TrimSpace(userText)
	if body == "" {
		body = "收到一条新的模型调用请求，请打开工作台回复。"
	} else {
		body = "新请求：\n" + truncateRunes(body, maxBodyRunes)
	}

	// Detach from request cancellation so a client disconnect does not drop the push.
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), sendTimeout)
	defer cancel()

	if err := s.client.Send(sendCtx, ntfy.Message{
		URL:   url,
		Title: msgTitle,
		Text:  body,
	}); err != nil {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", ownerID),
			zap.Error(err),
		).Warn("ntfy send failed")
		return
	}

	logging.BindContext(s.logger, ctx,
		zap.String("owner.id", ownerID),
	).Info("ntfy notification sent")
}

func (s *Service) resolveNtfyTarget(ctx context.Context, ownerID string) (url string, enabled bool, err error) {
	item, err := s.configs.GetUserConfig(ctx, ownerID, userSettingsKey)
	if err != nil {
		if errors.Is(err, common.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	value := item.Value
	if value == nil {
		return "", false, nil
	}

	enabled = asBool(value["ntfy_url_enabled"])
	url = strings.TrimSpace(fmt.Sprint(value["ntfy_url"]))
	if url == "<nil>" {
		url = ""
	}
	if !enabled || url == "" {
		return "", false, nil
	}
	return url, true, nil
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "on":
			return true
		default:
			return false
		}
	case float64:
		return v != 0
	case int:
		return v != 0
	case int64:
		return v != 0
	default:
		return false
	}
}

func truncateRunes(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || text == "" {
		return text
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	if limit == 1 {
		return string(runes[:1])
	}
	return string(runes[:limit-1]) + "…"
}
