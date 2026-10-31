package profile

import (
	"context"
	"errors"

	"github.com/zyf2007/ChatAPI/internal/config"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/repository/common"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	"go.uber.org/zap"
)

var ErrNewPasswordRequired = localauth.ErrNewPasswordRequired

type identityService interface {
	GetUser(context.Context, string) (common.User, error)
}

type localAuthService interface {
	UpdatePasswordForUser(context.Context, string, string) error
}

type settingsService interface {
	Public(context.Context) (authsettings.PublicSettings, error)
}

type totpService interface {
	IsEnabled(context.Context, string) bool
}

type rolePolicy interface {
	EffectiveRole(common.User) string
}
type realtimeSettings interface {
	Current(context.Context) (workspacesettings.Settings, error)
}
type conversationCounter interface {
	CountConversationsForOwner(context.Context, string) (int, error)
}

type Deps struct {
	Identity          identityService
	LocalAuth         localAuthService
	Settings          settingsService
	TOTP              totpService
	Policy            rolePolicy
	Logger            *zap.Logger
	Realtime          realtimeSettings
	Conversations     conversationCounter
	ConversationLimit func(context.Context) int
}

type Service struct {
	identity          identityService
	localAuth         localAuthService
	settings          settingsService
	totp              totpService
	policy            rolePolicy
	logger            *zap.Logger
	realtime          realtimeSettings
	conversations     conversationCounter
	conversationLimit func(context.Context) int
}

type SessionView struct {
	Mode                          config.Mode    `json:"mode"`
	Authenticated                 bool           `json:"authenticated"`
	User                          map[string]any `json:"user"`
	TOTPEnabled                   bool           `json:"totp_enabled"`
	RegistrationEnabled           bool           `json:"registration_enabled"`
	GeeTestEnabled                bool           `json:"geetest_enabled"`
	GeeTestCaptchaID              string         `json:"geetest_captcha_id"`
	CurrentConnectionCount        int            `json:"current_connection_count"`
	RealtimeMaxConnectionsPerUser int            `json:"realtime_max_connections_per_user"`
	CurrentConversationCount      int            `json:"current_conversation_count"`
	UserConversationLimit         int            `json:"user_conversation_limit"`
	OIDCEnabled                   bool           `json:"oidc_enabled"`
	OIDCProviderName              string         `json:"oidc_provider_name"`
	LocalPasswordLoginEnabled     bool           `json:"local_password_login_enabled"`
	EmailVerificationEnabled      bool           `json:"email_verification_enabled"`
}

func New(deps Deps) *Service {
	policyService := deps.Policy
	if policyService == nil {
		policyService = policy.NewService()
	}
	return &Service{
		identity:          deps.Identity,
		localAuth:         deps.LocalAuth,
		settings:          deps.Settings,
		totp:              deps.TOTP,
		policy:            policyService,
		logger:            deps.Logger,
		realtime:          deps.Realtime,
		conversations:     deps.Conversations,
		conversationLimit: deps.ConversationLimit,
	}
}

// BuildLabSessionView uses workspace policy sources without loading a Serve account.
func (s *Service) BuildLabSessionView(ctx context.Context, cfg config.Config) SessionView {
	return SessionView{
		Mode:                          config.ModeLab,
		Authenticated:                 true,
		User:                          map[string]any{"id": labauth.DefaultOwnerID, "username": "lab", "role": "admin"},
		RealtimeMaxConnectionsPerUser: s.realtimeLimit(ctx, cfg),
		CurrentConversationCount:      s.conversationCount(ctx, labauth.DefaultOwnerID),
		UserConversationLimit:         s.conversationLimitFor(ctx),
	}
}

func (s *Service) BuildAnonymousSessionView(ctx context.Context, cfg config.Config) (SessionView, error) {
	settings, err := s.PublicSettings(ctx, cfg)
	if err != nil {
		logging.BindContext(s.logger, ctx).Warn("usercontrol profile build anonymous session view failed", zap.Error(err))
		return SessionView{}, err
	}
	view := SessionView{
		Mode:                          cfg.Mode,
		Authenticated:                 false,
		User:                          nil,
		TOTPEnabled:                   false,
		RegistrationEnabled:           settings.RegistrationEnabled,
		GeeTestEnabled:                settings.GeeTestEnabled,
		GeeTestCaptchaID:              settings.GeeTestCaptchaID,
		CurrentConnectionCount:        0,
		RealtimeMaxConnectionsPerUser: s.realtimeLimit(ctx, cfg),
		CurrentConversationCount:      0,
		UserConversationLimit:         s.conversationLimitFor(ctx),
		OIDCEnabled:                   settings.OIDCEnabled,
		OIDCProviderName:              settings.OIDCProviderName,
		LocalPasswordLoginEnabled:     settings.LocalPasswordLoginEnabled,
		EmailVerificationEnabled:      settings.EmailVerificationEnabled,
	}
	logging.BindContext(s.logger, ctx,
		zap.Bool("auth.authenticated", false),
		zap.Bool("auth.oidc_enabled", view.OIDCEnabled),
		zap.Bool("auth.registration_enabled", view.RegistrationEnabled),
	).Debug("usercontrol profile built anonymous session view")
	return view, nil
}

func (s *Service) BuildAuthenticatedSessionView(ctx context.Context, cfg config.Config, userID string) (SessionView, error) {
	settings, err := s.PublicSettings(ctx, cfg)
	if err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", userID)).Warn("usercontrol profile build authenticated session view failed to load settings", zap.Error(err))
		return SessionView{}, err
	}
	user, err := s.identity.GetUser(ctx, userID)
	if err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", userID)).Warn("usercontrol profile build authenticated session view failed to load user", zap.Error(err))
		return SessionView{}, err
	}
	view := SessionView{
		Mode:          cfg.Mode,
		Authenticated: true,
		User: map[string]any{
			"id":       user.ID,
			"username": user.Username,
			"role":     s.policy.EffectiveRole(user),
		},
		TOTPEnabled:                   s.totp != nil && s.totp.IsEnabled(ctx, userID),
		RegistrationEnabled:           settings.RegistrationEnabled,
		GeeTestEnabled:                settings.GeeTestEnabled,
		GeeTestCaptchaID:              settings.GeeTestCaptchaID,
		CurrentConnectionCount:        0,
		RealtimeMaxConnectionsPerUser: s.realtimeLimit(ctx, cfg),
		CurrentConversationCount:      s.conversationCount(ctx, userID),
		UserConversationLimit:         s.conversationLimitFor(ctx),
		OIDCEnabled:                   settings.OIDCEnabled,
		OIDCProviderName:              settings.OIDCProviderName,
		LocalPasswordLoginEnabled:     settings.LocalPasswordLoginEnabled,
		EmailVerificationEnabled:      settings.EmailVerificationEnabled,
	}
	logging.BindContext(s.logger, ctx,
		zap.String("owner.id", user.ID),
		zap.Bool("auth.authenticated", true),
		zap.String("auth.role", view.User["role"].(string)),
		zap.Bool("auth.totp_enabled", view.TOTPEnabled),
	).Debug("usercontrol profile built authenticated session view")
	return view, nil
}

func (s *Service) realtimeLimit(ctx context.Context, cfg config.Config) int {
	if s.realtime == nil {
		return cfg.RealtimeMaxConnectionsPerUser
	}
	current, err := s.realtime.Current(ctx)
	if err != nil {
		return cfg.RealtimeMaxConnectionsPerUser
	}
	return current.MaxConnectionsPerUser
}

func (s *Service) conversationCount(ctx context.Context, userID string) int {
	if s.conversations == nil {
		return 0
	}
	count, err := s.conversations.CountConversationsForOwner(ctx, userID)
	if err != nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", userID)).Warn("usercontrol profile failed to count conversations", zap.Error(err))
		return 0
	}
	return count
}

func (s *Service) conversationLimitFor(ctx context.Context) int {
	if s.conversationLimit == nil {
		return 0
	}
	return s.conversationLimit(ctx)
}

func (s *Service) ChangePassword(ctx context.Context, userID string, password string) error {
	if s.localAuth == nil {
		logging.BindContext(s.logger, ctx, zap.String("owner.id", userID)).Warn("usercontrol profile password change failed", zap.String("auth.reason", "local_auth_unavailable"))
		return ErrNewPasswordRequired
	}
	err := s.localAuth.UpdatePasswordForUser(ctx, userID, password)
	if err != nil {
		logging.BindContext(s.logger, ctx,
			zap.String("owner.id", userID),
			zap.String("auth.reason", "password_change_failed"),
		).Warn("usercontrol profile password change failed", zap.Error(err))
		return err
	}
	logging.BindContext(s.logger, ctx, zap.String("owner.id", userID)).Info("usercontrol profile password changed")
	return nil
}

func (s *Service) PublicSettings(ctx context.Context, cfg config.Config) (authsettings.PublicSettings, error) {
	if s.settings == nil {
		return fallbackPublicSettings(ctx, s.logger, cfg), nil
	}
	settings, err := s.settings.Public(ctx)
	if err != nil {
		if errors.Is(err, authsettings.ErrInvalidSettings) {
			logging.BindContext(s.logger, ctx,
				zap.String("auth.settings_source", "service"),
				zap.String("auth.settings_fallback", "config"),
			).Warn("usercontrol profile falling back to config public settings", zap.Error(err))
			return fallbackPublicSettings(ctx, s.logger, cfg), nil
		}
		logging.BindContext(s.logger, ctx, zap.String("auth.settings_source", "service")).Warn("usercontrol profile failed to resolve public settings", zap.Error(err))
		return authsettings.PublicSettings{}, err
	}
	logging.BindContext(s.logger, ctx,
		zap.String("auth.settings_source", "service"),
		zap.Bool("auth.oidc_enabled", settings.OIDCEnabled),
		zap.Bool("auth.registration_enabled", settings.RegistrationEnabled),
	).Debug("usercontrol profile resolved public settings")
	return settings, nil
}

func (s *Service) GetUser(ctx context.Context, userID string) (common.User, error) {
	if s.identity == nil {
		return common.User{}, common.ErrNotFound
	}
	return s.identity.GetUser(ctx, userID)
}

func fallbackPublicSettings(ctx context.Context, logger *zap.Logger, cfg config.Config) authsettings.PublicSettings {
	settings := authsettings.PublicSettings{
		LocalPasswordLoginEnabled: true,
		RegistrationEnabled:       true,
		GeeTestEnabled:            cfg.GeetestCaptchaID != "",
		GeeTestCaptchaID:          cfg.GeetestCaptchaID,
		OIDCEnabled:               cfg.Mode != config.ModeLab && cfg.OIDCEnabled,
	}
	logging.BindContext(logger, ctx,
		zap.String("auth.settings_source", "config_fallback"),
		zap.Bool("auth.oidc_enabled", settings.OIDCEnabled),
		zap.Bool("auth.registration_enabled", settings.RegistrationEnabled),
	).Debug("usercontrol profile resolved public settings")
	return settings
}
