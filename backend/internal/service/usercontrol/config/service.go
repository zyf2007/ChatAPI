package config

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/platform/urlsafety"
	"github.com/zyf2007/ChatAPI/internal/repository/chat"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	"go.uber.org/zap"
)

// ErrInvalidNtfyConfig is returned when enabled ntfy settings fail shared URL safety checks.
var ErrInvalidNtfyConfig = errors.New("invalid ntfy config")

type Deps struct {
	Configs configrepo.Store
	Chat    chat.Store
	Events  chatevents.Publisher
	Logger  *zap.Logger
	// Lookup is optional; production uses the system resolver via urlsafety defaults.
	Lookup urlsafety.HostLookup
}

type Service struct {
	configs configrepo.Store
	chat    chat.Store
	events  chatevents.Publisher
	logger  *zap.Logger
	lookup  urlsafety.HostLookup
}

func New(deps Deps) *Service {
	return &Service{
		configs: deps.Configs,
		chat:    deps.Chat,
		events:  deps.Events,
		logger:  deps.Logger,
		lookup:  deps.Lookup,
	}
}

func (s *Service) GetUserConfig(ctx context.Context, userID string) (common.UserConfig, error) {
	item, err := s.configs.GetUserConfig(ctx, strings.TrimSpace(userID), "settings")
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Debug("usercontrol config fetched user config")
	}
	return item, err
}

func (s *Service) UpdateUserConfig(ctx context.Context, userID string, value map[string]any) (common.UserConfig, error) {
	cloned := cloneMap(value)
	if err := s.validateNtfySettings(ctx, cloned); err != nil {
		return common.UserConfig{}, err
	}
	item, err := s.configs.SetUserConfig(ctx, common.SetUserConfigInput{
		UserID: strings.TrimSpace(userID),
		Key:    "settings",
		Value:  cloned,
	})
	if err == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", strings.TrimSpace(userID)), zap.String("config.key", "settings")).Info("usercontrol config updated user config")
	}
	return item, err
}

// validateNtfySettings enforces:
//   - enabled=true requires a non-empty URL that passes the shared safety policy (syntax + DNS + restricted IP).
//   - enabled=false allows any syntactically valid URL (including empty) so users can keep drafts while disabled;
//     private/restricted destinations are rejected even when disabled to avoid storing known-unsafe endpoints.
//
// This intentionally does not treat "disabled private URL" as a general allow-private capability.
func (s *Service) validateNtfySettings(ctx context.Context, value map[string]any) error {
	if value == nil {
		return nil
	}
	enabled := asBool(value["ntfy_url_enabled"])
	rawURL, hasURL := value["ntfy_url"]
	if !hasURL && !enabled {
		return nil
	}
	urlText := ""
	if hasURL && rawURL != nil {
		urlText = strings.TrimSpace(fmt.Sprint(rawURL))
		if urlText == "<nil>" {
			urlText = ""
		}
	}
	if !enabled {
		if urlText == "" {
			return nil
		}
		// Disabled drafts still must be syntactically valid ntfy URLs.
		// Restricted destinations are rejected so we never persist known-unsafe endpoints.
		parsed, syntax := urlsafety.ParseNtfyURL(urlText)
		if !syntax.OK {
			return fmt.Errorf("%w: %s", ErrInvalidNtfyConfig, syntax.Reason)
		}
		if parsed == nil {
			return nil
		}
		safety := urlsafety.AssessNtfyHost(ctx, parsed.Hostname, false, s.lookup)
		if !safety.OK {
			// For disabled saves, DNS resolution failures are soft: the URL is not active.
			// Only hard-reject when we positively identify a restricted destination or bad syntax above.
			if safety.IsPrivate {
				return fmt.Errorf("%w: %s", ErrInvalidNtfyConfig, safety.Reason)
			}
			// Literal private IPs are IsPrivate; hostname DNS failures are not — allow draft keep.
			return nil
		}
		return nil
	}
	if urlText == "" {
		return fmt.Errorf("%w: 启用 ntfy 时必须填写地址", ErrInvalidNtfyConfig)
	}
	safety := urlsafety.ValidateNtfyURLContext(ctx, urlText, false, s.lookup)
	if !safety.OK {
		return fmt.Errorf("%w: %s", ErrInvalidNtfyConfig, safety.Reason)
	}
	return nil
}

func (s *Service) DeleteConversation(ctx context.Context, conversationID string) (common.DeleteConversationsResult, error) {
	result, err := s.chat.DeleteConversations(ctx, []string{strings.TrimSpace(conversationID)})
	if err == nil {
		chatevents.PublishDeletedConversations(ctx, s.events, result)
	}
	return result, err
}

func (s *Service) DeleteConversations(ctx context.Context, conversationIDs []string) (common.DeleteConversationsResult, error) {
	result, err := s.chat.DeleteConversations(ctx, conversationIDs)
	if err == nil {
		chatevents.PublishDeletedConversations(ctx, s.events, result)
	}
	return result, err
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
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
