package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/actor"
	"github.com/zyf2007/ChatAPI/internal/config"
	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/httpmetrics"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/ops/readiness"
	"github.com/zyf2007/ChatAPI/internal/ops/setup"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	platformntfy "github.com/zyf2007/ChatAPI/internal/platform/ntfy"
	"github.com/zyf2007/ChatAPI/internal/service/account"
	"github.com/zyf2007/ChatAPI/internal/service/admincontrol"
	adminmonitoring "github.com/zyf2007/ChatAPI/internal/service/admincontrol/monitoring"
	adminsettings "github.com/zyf2007/ChatAPI/internal/service/admincontrol/settings"
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
	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	automationsettings "github.com/zyf2007/ChatAPI/internal/service/automation/settings"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	conversationresolve "github.com/zyf2007/ChatAPI/internal/service/chat/conversationresolve"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	outputassetsvc "github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	pendingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/pending"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
	preprocesssettings "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess/settings"
	chatsettings "github.com/zyf2007/ChatAPI/internal/service/chat/settings"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	imsvc "github.com/zyf2007/ChatAPI/internal/service/im"
	"github.com/zyf2007/ChatAPI/internal/service/im/clawbot"
	ntfynotify "github.com/zyf2007/ChatAPI/internal/service/notification/ntfy"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/conversationretention"
)

type Services struct {
	Turn         *turnsvc.Service
	ChatSettings *chatsettings.Service
	Audit        *auditsvc.Service
	IM           *imsvc.Service
}

type applicationInput struct {
	config         config.Config
	store          runtimeStore
	loggerFactory  *logging.Factory
	mediaProcessor media.Processor
}

type applicationResult struct {
	router        httprouter.Deps
	services      Services
	notifications *ntfynotify.Service
}

type authModule struct {
	policy         *policy.Service
	sessions       *sessionsvc.Service
	accounts       *account.Service
	audit          *auditsvc.Service
	local          *localauth.Service
	verification   *verification.Service
	settings       *authsettings.Service
	access         *authaccess.Service
	accessSettings *authaccess.SettingsService
	geetest        *geetest.Service
	totp           *totpsvc.Service
	oidc           *oidcsvc.Service
	loginLimiter   *ratelimit.Service
	identity       *identity.Service
	appKeys        *appkeysvc.Service
	modelKeys      *modelkeysvc.Service
}

type chatModule struct {
	turn               *turnsvc.Service
	query              *turnquerysvc.Service
	control            *controlsvc.Service
	timeline           *timelinesvc.Service
	ingress            *ingresssvc.Service
	streaming          *streamingsvc.Service
	egress             *egresssvc.Service
	catalog            *catalogsvc.Service
	settings           *chatsettings.Service
	mediaSettings      *preprocesssettings.Service
	realtimeSettings   *workspacesettings.Service
	automationSettings *automationsettings.Service
	workspaceHub       *workspacesvc.Hub
	events             *chatevents.Dispatcher
	automation         *automationsvc.Service
	im                 *imsvc.Service
	notifications      *ntfynotify.Service
	outputUploader     httphandler.OutputImageUploader
}

type adminModule struct {
	user       *usercontrol.Service
	admin      *admincontrol.Service
	monitoring *adminmonitoring.Service
}

func assembleApplication(ctx context.Context, input applicationInput) (applicationResult, error) {
	auth, err := buildAuthModule(ctx, input)
	if err != nil {
		return applicationResult{}, err
	}
	chat := buildChatModule(input, auth)
	auth.accounts.SetOwnerRevoker(chat.im.RevokeOwner)
	admin, err := buildAdminModule(input, auth, chat)
	if err != nil {
		_ = chat.notifications.Close()
		return applicationResult{}, err
	}
	return applicationResult{
		router:        buildRouter(input, auth, chat, admin),
		services:      Services{Turn: chat.turn, ChatSettings: chat.settings, Audit: auth.audit, IM: chat.im},
		notifications: chat.notifications,
	}, nil
}

func buildAuthModule(ctx context.Context, input applicationInput) (authModule, error) {
	cfg, store := input.config, input.store
	policySvc := policy.NewService(cfg.SuperAdminEmail)
	sessionSvc, err := sessionsvc.NewService(sessionsvc.Config{Secret: cfg.SessionSecret, CookieName: "chatapi_session", TTL: 7 * 24 * time.Hour, SecureOnly: false, Path: "/"})
	if err != nil {
		return authModule{}, fmt.Errorf("init session service: %w", err)
	}
	accounts := account.NewService(store)
	authSettings := authsettings.NewService(store, cfg)
	verificationSvc := verification.NewService(store, buildEmailSender(cfg))
	labSvc := labauth.NewService(cfg)
	accessSettings := authaccess.NewSettingsService(store, authaccess.Settings{GlobalRateLimitRequests: cfg.AccessRateLimitRequests, GlobalRateLimitWindow: cfg.AccessRateLimitWindow}, cfg.SettingsEnvironment("access"))
	appKeys := appkeysvc.NewService(store, cfg.MasterKey)
	modelKeys := modelkeysvc.NewService(store, cfg.MasterKey)
	appKeys.Logger = input.loggerFactory.Layer(logging.LayerAuth)
	modelKeys.Logger = input.loggerFactory.Layer(logging.LayerAuth)
	module := authModule{
		policy: policySvc, sessions: sessionSvc, accounts: accounts, audit: auditsvc.NewService(store),
		verification: verificationSvc, settings: authSettings, accessSettings: accessSettings,
		access: authaccess.NewService(cfg, labSvc, accessSettings), geetest: geetest.NewService(cfg, nil),
		totp: totpsvc.NewService(store, cfg.MasterKey, "ChatAPI"), oidc: oidcsvc.NewService(accounts, cfg),
		loginLimiter: ratelimit.NewService(5, time.Minute), identity: identity.NewService(accounts),
		appKeys: appKeys, modelKeys: modelKeys,
	}
	module.local = localauth.NewService(accounts, store, policySvc, sessionSvc, verificationSvc)
	if _, _, err := superadminsvc.NewService(accounts, cfg).Sync(ctx); err != nil {
		return authModule{}, fmt.Errorf("sync super admin: %w", err)
	}
	return module, nil
}

func buildChatModule(input applicationInput, auth authModule) chatModule {
	cfg, store := input.config, input.store
	logger := func(layer string) *zap.Logger { return input.loggerFactory.Layer(layer) }
	query := &turnquerysvc.Service{Store: store, Logger: logger(logging.LayerTurnQuery)}
	settings := chatsettings.New(store, cfg)
	pending := pendingsvc.NewPendingRegistry()
	pending.Logger = logger(logging.LayerPending)
	notifications := ntfynotify.New(store, platformntfy.NewClient(nil), logger(logging.LayerApp))
	submitter := &turnsvc.Submitter{Store: store, Pending: pending, OutputEventLimit: func(ctx context.Context) (int, error) {
		current, err := settings.Current(ctx)
		return current.MaxOutputEventsPerMessage, err
	}, Hooks: turnsvc.SubmitHooks{NotifyWaiting: notifications.NotifyWaiting}}
	turn := &turnsvc.Service{Submitter: submitter, Pending: pending, Store: store, OwnerIDFromContext: actor.OwnerIDFromContext, ActorFromContext: actor.FromContext, Logger: logger(logging.LayerTurn)}
	control := controlsvc.New(query, turn, logger(logging.LayerTurnQuery))
	timeline := timelinesvc.New(store, logger(logging.LayerTurnQuery))
	egress := egresssvc.New()
	workspace := workspacesvc.New(query, timeline, control)
	hub := workspacesvc.NewHub(workspace)
	realtimeSettings := workspacesettings.New(store, cfg)
	hub.SetSettings(realtimeSettings)
	events := chatevents.NewDispatcher(workspacesvc.NewRealtimePublisher(hub))
	automationSettings := automationsettings.New(store)
	automationEvents := automationsvc.NewDispatcher(workspacesvc.NewAutomationRealtimePublisher(hub))
	automation := automationsvc.New(automationsvc.Deps{Rules: store, ModelKeys: store, Control: control, Pending: pending, Events: automationEvents, Logger: logger(logging.LayerTurn), Settings: automationSettings})
	imService := imsvc.NewService(store, pending, control, cfg.MasterKey, logger(logging.LayerIM), clawbot.NewProvider(nil))
	workspace.SetAutomation(automation)
	control.Subscribe(automation)
	events.Subscribe(automation)
	events.Subscribe(imService)
	mediaSettings := preprocesssettings.New(store, cfg)
	mediaStore := localstore.Store{RootDir: cfg.MediaDerivedDir}
	outputImages := outputassetsvc.New(cfg, store, mediaStore, input.mediaProcessor)
	outputImages.Settings = mediaSettings
	turn.OutputAssets = outputImages
	turn.Resolver = conversationresolve.New(store, pending)
	turn.Egress = egress
	turn.Events = events
	preprocessor := preprocesssvc.New(cfg, input.mediaProcessor)
	preprocessor.Settings = mediaSettings
	submitter.Materializer = &turnsvc.RequestMaterializer{Preprocessor: preprocessor, AssetPersister: mediaStore, DeletionFailures: store, PreparedImageClean: mediaStore}
	return chatModule{
		turn: turn, query: query, control: control, timeline: timeline, ingress: ingresssvc.New(turn), streaming: streamingsvc.New(),
		egress: egress, catalog: catalogsvc.New(auth.modelKeys), settings: settings, mediaSettings: mediaSettings,
		realtimeSettings: realtimeSettings, automationSettings: automationSettings, workspaceHub: hub,
		events: events, automation: automation, im: imService, notifications: notifications, outputUploader: turn,
	}
}

func buildAdminModule(input applicationInput, auth authModule, chat chatModule) (adminModule, error) {
	logger := func(layer string) *zap.Logger { return input.loggerFactory.Layer(layer) }
	conversationLimit := func(ctx context.Context) int {
		settings, err := auth.accessSettings.Get(ctx)
		if err != nil {
			logging.BindContext(logger(logging.LayerHTTP), ctx).Warn("failed to load user conversation limit", zap.Error(err))
			return 0
		}
		return settings.UserConversationLimit
	}
	user := usercontrol.New(usercontrol.Deps{
		Identity: auth.identity, LocalAuth: auth.local, Settings: auth.settings, TOTP: auth.totp, Policy: auth.policy,
		Query: chat.query, Turn: chat.control, Configs: input.store, Storage: input.store, Chat: input.store,
		AppKeysStore: input.store, AppKeys: auth.appKeys, ModelKeys: auth.modelKeys, Accounts: auth.accounts,
		Logger: logger(logging.LayerUserControl), Events: chat.events, Automation: chat.automation,
		RealtimeSettings: chat.realtimeSettings, ConversationLimit: conversationLimit,
	})
	retention := conversationretention.New(auth.accounts, user.Conversations, conversationLimit, logger(logging.LayerHTTP))
	chat.turn.ConversationCreated = retention.Enforce
	chat.turn.ConversationTerminal = retention.Enforce
	accessDomain, err := adminsettings.Combine("access", "访问限流", auth.accessSettings.AdminDomain(), chat.realtimeSettings, chat.settings)
	if err != nil {
		return adminModule{}, fmt.Errorf("combine admin access settings: %w", err)
	}
	settings := adminsettings.New(input.config,
		adminsettings.Domain{Settings: auth.settings.AdminDomain()},
		adminsettings.Domain{Settings: accessDomain, AfterUpdate: retention.SettingsUpdated},
		adminsettings.Domain{Settings: chat.mediaSettings},
		adminsettings.Domain{Settings: chat.automationSettings},
	)
	admin := admincontrol.New(admincontrol.Deps{Accounts: auth.accounts, Query: chat.query, Control: chat.control, ChatStore: input.store, StorageStore: input.store, KeyStore: input.store, Events: chat.events, Settings: settings})
	admin.SetSettings(settings)
	return adminModule{user: user, admin: admin, monitoring: adminmonitoring.New(chat.workspaceHub)}, nil
}

func buildRouter(input applicationInput, auth authModule, chat chatModule, admin adminModule) httprouter.Deps {
	cfg := input.config
	logger := func(layer string) *zap.Logger { return input.loggerFactory.Layer(layer) }
	return httprouter.Deps{
		Config: cfg, LoggerFactory: input.loggerFactory, Access: auth.access, Policy: auth.policy, UserSessions: auth.sessions, ModelAPIKeys: auth.modelKeys, AppAPIKeys: auth.appKeys,
		Chat:      httphandler.ChatAPIHandler{Turn: chat.turn, Query: chat.query, Timeline: chat.timeline, Ingress: chat.ingress, Streaming: chat.streaming, Catalog: chat.catalog, Control: chat.control, Egress: chat.egress, Logger: logger(logging.LayerHTTP)},
		App:       httphandler.AppAPIHandler{Turn: chat.turn, Query: chat.query, Timeline: chat.timeline, Logger: logger(logging.LayerTurnQuery)},
		Auth:      httphandler.AuthHandler{Config: cfg, LocalAuth: auth.local, Verification: auth.verification, Policy: auth.policy, Settings: auth.settings, GeeTest: auth.geetest, TOTP: auth.totp, OIDC: auth.oidc, Audit: auth.audit, LoginLimiter: auth.loginLimiter, Sessions: auth.sessions, Logger: logger(logging.LayerAuth)},
		User:      httphandler.UserHandler{Config: cfg, UserControl: admin.user, Timeline: chat.timeline, Logger: logger(logging.LayerAuth)},
		IM:        httphandler.IMHandler{Service: chat.im},
		Admin:     httphandler.AdminHandler{Control: admin.admin, Timeline: chat.timeline, Audit: auth.audit, Logger: logger(logging.LayerAudit), Monitoring: admin.monitoring},
		Lab:       httphandler.LabHandler{Config: cfg, Query: chat.query, Turn: chat.turn, Control: chat.control, Logger: logger(logging.LayerHTTP)},
		Workspace: httphandler.WorkspaceHandler{Hub: chat.workspaceHub, Logger: logger(logging.LayerHTTP)},
		Upload:    httphandler.UploadHandler{Storage: input.store, OutputImages: chat.outputUploader, OutputImageMaxBytes: cfg.UploadMaxBytes},
		Health:    httphandler.HealthHandler{Config: cfg, Store: input.store},
		Readiness: httphandler.ReadinessHandler{Service: readiness.NewService(cfg, input.store)},
		Setup:     httphandler.SetupHandler{Service: setup.NewService(input.store, cfg)},
		Metrics:   httphandler.MetricsHandler{Registry: httpmetrics.NewRegistry()},
	}
}
