package main

import (
	"context"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	platformbrowser "github.com/zyf2007/ChatAPI/internal/platform/browser"
	platformemail "github.com/zyf2007/ChatAPI/internal/platform/email"
	platformntfy "github.com/zyf2007/ChatAPI/internal/platform/ntfy"
	auditrepo "github.com/zyf2007/ChatAPI/internal/repository/audit"
	authrepo "github.com/zyf2007/ChatAPI/internal/repository/auth"
	automationrepo "github.com/zyf2007/ChatAPI/internal/repository/automation"
	chatrepo "github.com/zyf2007/ChatAPI/internal/repository/chat"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	"github.com/zyf2007/ChatAPI/internal/repository/migrations"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	pgrepo "github.com/zyf2007/ChatAPI/internal/repository/postgresql"
	sqliterepo "github.com/zyf2007/ChatAPI/internal/repository/sqlite"
	storagerepo "github.com/zyf2007/ChatAPI/internal/repository/storage"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	auditsvc "github.com/zyf2007/ChatAPI/internal/service/audit"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/geetest"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/identity"
	labauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/lab"
	localauth "github.com/zyf2007/ChatAPI/internal/service/auth/authn/local"
	oidcsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/oidc"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/ratelimit"
	authsettings "github.com/zyf2007/ChatAPI/internal/service/auth/authn/settings"
	superadminsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/superadmin"
	totpsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/totp"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	appkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkeysvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	sessionsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	automationsettings "github.com/zyf2007/ChatAPI/internal/service/automation/settings"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	preprocesssettings "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess/settings"
	chatsettings "github.com/zyf2007/ChatAPI/internal/service/chat/settings"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	ntfynotify "github.com/zyf2007/ChatAPI/internal/service/notification/ntfy"
)

type runtimeStore interface {
	authrepo.Store
	chatrepo.Store
	configrepo.Store
	automationrepo.Store
	storagerepo.Store
	auditrepo.Store
	platformrepo.MaintenanceStore
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "chatapi server: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	backendRoot, err := detectBackendRoot()
	if err != nil {
		return err
	}
	if err := config.LoadEnv(backendRoot); err != nil {
		return fmt.Errorf("load env: %w", err)
	}
	cfg, err := config.FromEnv(config.ModeServe, backendRoot)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logFactory, err := logging.NewFactory(logging.NewConfig(cfg))
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	appLogger := logFactory.Layer(logging.LayerApp)

	store, cleanup, err := openStore(ctx, cfg, logFactory)
	if err != nil {
		return err
	}
	defer cleanup()

	policySvc := policy.NewService(cfg.SuperAdminEmail)
	sessionSvc, err := sessionsvc.NewService(sessionsvc.Config{
		Secret:     cfg.SessionSecret,
		CookieName: "chatapi_session",
		TTL:        7 * 24 * time.Hour,
		SecureOnly: false,
		Path:       "/",
	})
	if err != nil {
		return fmt.Errorf("init session service: %w", err)
	}

	accountSvc := account.NewService(store)
	superAdminSvc := superadminsvc.NewService(accountSvc, cfg)
	authSettingsSvc := authsettings.NewService(store, cfg)
	emailSender := buildEmailSender(cfg)
	verificationSvc := verification.NewService(store, emailSender)
	geetestSvc := geetest.NewService(cfg, nil)
	totpSvc := totpsvc.NewService(store, cfg.MasterKey, "ChatAPI")
	oidcSvc := oidcsvc.NewService(accountSvc, cfg)
	loginLimiter := ratelimit.NewService(5, time.Minute)
	auditSvc := auditsvc.NewService(store)
	localAuthSvc := localauth.NewService(accountSvc, store, policySvc, sessionSvc, verificationSvc)
	if _, _, err := superAdminSvc.Sync(ctx); err != nil {
		return fmt.Errorf("sync super admin: %w", err)
	}
	labSvc := labauth.NewService(cfg)
	accessSettingsSvc := authaccess.NewSettingsService(store, authaccess.Settings{
		GlobalRateLimitRequests: cfg.AccessRateLimitRequests,
		GlobalRateLimitWindow:   cfg.AccessRateLimitWindow,
	}, cfg.SettingsEnvironment("access"))
	accessSvc := authaccess.NewService(cfg, labSvc, accessSettingsSvc)
	identitySvc := identity.NewService(accountSvc)
	appKeySvc := appkeysvc.NewService(store)
	appKeySvc.Logger = logFactory.Layer(logging.LayerAuth)
	modelKeySvc := modelkeysvc.NewService(store, cfg.MasterKey)
	modelKeySvc.Logger = logFactory.Layer(logging.LayerAuth)
	querySvc := &turnquerysvc.Service{
		Store:  store,
		Logger: logFactory.Layer(logging.LayerTurnQuery),
	}
	chatSettingsSvc := chatsettings.New(store, cfg)
	pendingRegistry := pendingsvc.NewPendingRegistry()
	pendingRegistry.Logger = logFactory.Layer(logging.LayerPending)
	ntfyNotifySvc := ntfynotify.New(store, platformntfy.NewClient(nil), logFactory.Layer(logging.LayerApp))
	submitter := &turnsvc.Submitter{
		Store:   store,
		Pending: pendingRegistry,
		OutputEventLimit: func(ctx context.Context) (int, error) {
			current, err := chatSettingsSvc.Current(ctx)
			return current.MaxOutputEventsPerMessage, err
		},
		Hooks: turnsvc.SubmitHooks{
			// Only notify when a request becomes waiting for human reply.
			// Do not wire NotifyText to stream deltas, or every token would spam ntfy.
			NotifyWaiting: ntfyNotifySvc.NotifyWaiting,
		},
	}
	turnService := &turnsvc.Service{
		Submitter: submitter,
		Pending:   pendingRegistry,
		Store:     store,
		OwnerIDFromContext: func(ctx context.Context) string {
			if act, ok := actor.FromContext(ctx); ok && strings.TrimSpace(act.UserID) != "" {
				return strings.TrimSpace(act.UserID)
			}
			return ""
		},
		ActorFromContext: func(ctx context.Context) (actor.Actor, bool) {
			return actor.FromContext(ctx)
		},
		Logger: logFactory.Layer(logging.LayerTurn),
	}
	if _, err := turnService.DisconnectRecoveredPending(ctx, "server restarted"); err != nil {
		return fmt.Errorf("disconnect recovered pending turns: %w", err)
	}
	mediaSettingsSvc := preprocesssettings.New(store, cfg)
	realtimeSettingsSvc := workspacesettings.New(store, cfg)
	automationSettingsSvc := automationsettings.New(store)
	go expirePendingLoop(ctx, turnService, chatSettingsSvc, appLogger)

	handler := httprouter.New(httprouter.Deps{
		Config:             cfg,
		ChatRepo:           store,
		AuthRepo:           store,
		ConfigRepo:         store,
		AutomationRepo:     store,
		StorageRepo:        store,
		AuditRepo:          store,
		PlatformRepo:       store,
		Turn:               turnService,
		Query:              querySvc,
		ModelAPIKeys:       modelKeySvc,
		AppAPIKeys:         appKeySvc,
		Lab:                labSvc,
		LocalAuth:          localAuthSvc,
		Verification:       verificationSvc,
		Policy:             policySvc,
		Access:             accessSvc,
		AccessSettings:     accessSettingsSvc,
		AuthSettings:       authSettingsSvc,
		GeeTest:            geetestSvc,
		TOTP:               totpSvc,
		OIDC:               oidcSvc,
		LoginLimiter:       loginLimiter,
		Audit:              auditSvc,
		Accounts:           accountSvc,
		Identity:           identitySvc,
		UserSessions:       sessionSvc,
		LoggerFactory:      logFactory,
		ChatSettings:       chatSettingsSvc,
		MediaSettings:      mediaSettingsSvc,
		RealtimeSettings:   realtimeSettingsSvc,
		AutomationSettings: automationSettingsSvc,
	})

	server := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}

	if cfg.Mode == config.ModeLab && cfg.OpenBrowser {
		go func() {
			time.Sleep(600 * time.Millisecond)
			target := fmt.Sprintf("http://%s:%d", browserHost(cfg), cfg.Port)
			_ = platformbrowser.Open(target)
		}()
	}

	errCh := make(chan error, 1)
	go func() {
		appLogger.Info("http server starting")
		if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		// Restore the default signal behavior so a second Ctrl+C forces exit.
		stop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
		return nil
	case serveErr := <-errCh:
		return serveErr
	}
}

func expirePendingLoop(ctx context.Context, turns *turnsvc.Service, settings *chatsettings.Service, logger *zap.Logger) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			current, err := settings.Current(ctx)
			if err != nil {
				logger.Warn("load chat settings for pending expiry failed", zap.Error(err))
				continue
			}
			if current.PendingTurnTTL <= 0 {
				continue
			}
			if _, err := turns.ExpirePendingTurns(ctx, current.PendingTurnTTL, now); err != nil {
				logger.Warn("expire pending turns failed", zap.Error(err))
			}
		}
	}
}

func detectBackendRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return detectBackendRootFrom(wd), nil
}

func detectBackendRootFrom(start string) string {
	current := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return filepath.Clean(start)
		}
		current = parent
	}
}

func openStore(ctx context.Context, cfg config.Config, logFactory *logging.Factory) (runtimeStore, func(), error) {
	driver := strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver))
	switch driver {
	case "sqlite":
		store, err := sqliterepo.Open(cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := migrations.Bootstrap(ctx, store.DB()); err != nil {
			_ = store.Close()
			return nil, nil, fmt.Errorf("bootstrap sqlite migrations: %w", err)
		}
		return store, func() { _ = store.Close() }, nil
	case "postgres", "postgresql":
		store, err := pgrepo.Open(ctx, cfg.DatabaseDSN)
		if err != nil {
			return nil, nil, fmt.Errorf("open postgresql store: %w", err)
		}
		store.Logger = logFactory.Layer(logging.LayerRepository)
		if err := pgrepo.Bootstrap(ctx, store.Pool()); err != nil {
			store.Close()
			return nil, nil, fmt.Errorf("bootstrap postgresql migrations: %w", err)
		}
		return store, func() { store.Close() }, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database driver: %s", cfg.DatabaseDriver)
	}
}

func buildEmailSender(cfg config.Config) platformemail.Sender {
	smtpCfg := platformemail.SMTPConfigFromConfig(cfg)
	if !smtpCfg.Enabled {
		return nil
	}
	return platformemail.NewSMTPSender(smtpCfg)
}

func browserHost(cfg config.Config) string {
	host := strings.TrimSpace(cfg.Host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "127.0.0.1"
	}
	return host
}
