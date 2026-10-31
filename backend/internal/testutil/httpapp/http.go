package httpapp

import (
	"context"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/zyf2007/ChatAPI/internal/config"
	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
	httprouter "github.com/zyf2007/ChatAPI/internal/http/router"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/httpmetrics"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	"github.com/zyf2007/ChatAPI/internal/ops/readiness"
	"github.com/zyf2007/ChatAPI/internal/ops/setup"
	"github.com/zyf2007/ChatAPI/internal/platform/media"
	"github.com/zyf2007/ChatAPI/internal/platform/media/localstore"
	auditrepo "github.com/zyf2007/ChatAPI/internal/repository/audit"
	authrepo "github.com/zyf2007/ChatAPI/internal/repository/auth"
	automationrepo "github.com/zyf2007/ChatAPI/internal/repository/automation"
	chatrepo "github.com/zyf2007/ChatAPI/internal/repository/chat"
	configrepo "github.com/zyf2007/ChatAPI/internal/repository/config"
	platformrepo "github.com/zyf2007/ChatAPI/internal/repository/platform"
	storagerepo "github.com/zyf2007/ChatAPI/internal/repository/storage"
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
	totpsvc "github.com/zyf2007/ChatAPI/internal/service/auth/authn/totp"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authn/verification"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
	automationsvc "github.com/zyf2007/ChatAPI/internal/service/automation"
	automationsettings "github.com/zyf2007/ChatAPI/internal/service/automation/settings"
	catalogsvc "github.com/zyf2007/ChatAPI/internal/service/chat/catalog"
	controlsvc "github.com/zyf2007/ChatAPI/internal/service/chat/control"
	conversationresolve "github.com/zyf2007/ChatAPI/internal/service/chat/conversationresolve"
	egresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/egress"
	chatevents "github.com/zyf2007/ChatAPI/internal/service/chat/events"
	ingresssvc "github.com/zyf2007/ChatAPI/internal/service/chat/ingress"
	outputassetsvc "github.com/zyf2007/ChatAPI/internal/service/chat/outputasset"
	preprocesssvc "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess"
	preprocesssettings "github.com/zyf2007/ChatAPI/internal/service/chat/preprocess/settings"
	chatsettings "github.com/zyf2007/ChatAPI/internal/service/chat/settings"
	streamingsvc "github.com/zyf2007/ChatAPI/internal/service/chat/streaming"
	timelinesvc "github.com/zyf2007/ChatAPI/internal/service/chat/timeline"
	turnsvc "github.com/zyf2007/ChatAPI/internal/service/chat/turn"
	turnquerysvc "github.com/zyf2007/ChatAPI/internal/service/chat/turnquery"
	workspacesvc "github.com/zyf2007/ChatAPI/internal/service/chat/workspace"
	workspacesettings "github.com/zyf2007/ChatAPI/internal/service/chat/workspace/settings"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol"
	"github.com/zyf2007/ChatAPI/internal/service/usercontrol/conversationretention"
)

// Input exposes integration-test overrides. Production composition belongs to
// internal/bootstrap and does not use this mutable fixture graph.
type Input struct {
	Config             config.Config
	ChatRepo           chatrepo.Store
	AuthRepo           authrepo.Store
	ConfigRepo         configrepo.Store
	AutomationRepo     automationrepo.Store
	StorageRepo        storagerepo.Store
	AuditRepo          auditrepo.Store
	PlatformRepo       platformrepo.MaintenanceStore
	Turn               *turnsvc.Service
	Query              *turnquerysvc.Service
	ModelAPIKeys       *modelkey.Service
	Catalog            *catalogsvc.Service
	Control            *controlsvc.Service
	Ingress            *ingresssvc.Service
	Streaming          *streamingsvc.Service
	Egress             *egresssvc.Service
	Timeline           *timelinesvc.Service
	ChatEvents         *chatevents.Dispatcher
	AppAPIKeys         *appkey.Service
	Lab                *labauth.Service
	LocalAuth          *localauth.Service
	Verification       *verification.Service
	Policy             *policy.Service
	Access             *authaccess.Service
	AccessSettings     *authaccess.SettingsService
	AuthSettings       *authsettings.Service
	GeeTest            *geetest.Service
	TOTP               *totpsvc.Service
	OIDC               *oidcsvc.Service
	LoginLimiter       *ratelimit.Service
	AdminControl       *admincontrol.Service
	Audit              *auditsvc.Service
	Accounts           *account.Service
	Identity           *identity.Service
	UserControl        *usercontrol.Service
	UserSessions       *session.Service
	LoggerFactory      *logging.Factory
	Workspace          *workspacesvc.Service
	WorkspaceHub       *workspacesvc.Hub
	Automation         *automationsvc.Service
	AutomationEvents   *automationsvc.Dispatcher
	AdminSettings      *adminsettings.Service
	AdminMonitoring    *adminmonitoring.Service
	ChatSettings       *chatsettings.Service
	MediaSettings      *preprocesssettings.Service
	MediaProcessor     media.Processor
	RealtimeSettings   *workspacesettings.Service
	AutomationSettings *automationsettings.Service
}

type Services struct {
	Turn         *turnsvc.Service
	ChatSettings *chatsettings.Service
	Audit        *auditsvc.Service
}

type HTTPResult struct {
	Router   httprouter.Deps
	Services Services
}

func assemble(input Input) (HTTPResult, error) {
	logger := func(layer string) *zap.Logger {
		if input.LoggerFactory == nil {
			return zap.NewNop()
		}
		return input.LoggerFactory.Layer(layer)
	}
	if input.MediaSettings == nil && input.ConfigRepo != nil {
		input.MediaSettings = preprocesssettings.New(input.ConfigRepo, input.Config)
	}
	if input.Lab == nil {
		input.Lab = labauth.NewService(input.Config)
	}
	if input.AccessSettings == nil && input.AuthRepo != nil {
		input.AccessSettings = authaccess.NewSettingsService(input.AuthRepo, authaccess.Settings{
			GlobalRateLimitRequests: input.Config.AccessRateLimitRequests,
			GlobalRateLimitWindow:   input.Config.AccessRateLimitWindow,
		}, input.Config.SettingsEnvironment("access"))
	}
	if input.Access == nil {
		input.Access = authaccess.NewService(input.Config, input.Lab, input.AccessSettings)
	}
	if input.Audit == nil && input.AuditRepo != nil {
		input.Audit = auditsvc.NewService(input.AuditRepo)
	}

	mediaStore := localstore.Store{RootDir: input.Config.MediaDerivedDir}
	outputImages := outputassetsvc.New(input.Config, input.StorageRepo, mediaStore, input.MediaProcessor)
	outputImages.Settings = input.MediaSettings
	if input.Turn != nil {
		input.Turn.OutputAssets = outputImages
	}
	var outputImageUploader httphandler.OutputImageUploader
	if input.Turn != nil {
		outputImageUploader = input.Turn
	}

	if input.Control == nil {
		input.Control = controlsvc.New(input.Query, input.Turn, logger(logging.LayerTurnQuery))
	}
	if input.Timeline == nil {
		input.Timeline = timelinesvc.New(input.ChatRepo, logger(logging.LayerTurnQuery))
	}
	if input.Ingress == nil {
		input.Ingress = ingresssvc.New(input.Turn)
	}
	if input.Streaming == nil {
		input.Streaming = streamingsvc.New()
	}
	if input.Egress == nil {
		input.Egress = egresssvc.New()
	}
	if input.Catalog == nil {
		input.Catalog = catalogsvc.New(input.ModelAPIKeys)
	}
	if input.Workspace == nil {
		input.Workspace = workspacesvc.New(input.Query, input.Timeline, input.Control)
	}
	if input.WorkspaceHub == nil {
		input.WorkspaceHub = workspacesvc.NewHub(input.Workspace)
	}
	if input.AdminMonitoring == nil {
		input.AdminMonitoring = adminmonitoring.New(input.WorkspaceHub)
	}
	if input.ChatSettings == nil && input.ConfigRepo != nil {
		input.ChatSettings = chatsettings.New(input.ConfigRepo, input.Config)
	}
	if input.RealtimeSettings == nil && input.ConfigRepo != nil {
		input.RealtimeSettings = workspacesettings.New(input.ConfigRepo, input.Config)
	}
	if input.AutomationSettings == nil && input.ConfigRepo != nil {
		input.AutomationSettings = automationsettings.New(input.ConfigRepo)
	}
	input.WorkspaceHub.SetSettings(input.RealtimeSettings)
	if input.ChatEvents == nil {
		input.ChatEvents = chatevents.NewDispatcher(workspacesvc.NewRealtimePublisher(input.WorkspaceHub))
	}
	if input.AutomationRepo == nil {
		input.AutomationRepo, _ = input.ConfigRepo.(automationrepo.Store)
	}
	if input.Automation == nil && input.AutomationRepo != nil && input.Turn != nil && input.Turn.Pending != nil {
		if input.AutomationEvents == nil {
			input.AutomationEvents = automationsvc.NewDispatcher(workspacesvc.NewAutomationRealtimePublisher(input.WorkspaceHub))
		}
		input.Automation = automationsvc.New(automationsvc.Deps{
			Rules: input.AutomationRepo, ModelKeys: input.AuthRepo, Control: input.Control, Pending: input.Turn.Pending,
			Events: input.AutomationEvents, Logger: logger(logging.LayerTurn), Settings: input.AutomationSettings,
		})
	}
	input.Workspace.SetAutomation(input.Automation)
	input.Control.Subscribe(input.Automation)
	input.ChatEvents.Subscribe(input.Automation)

	conversationLimit := func(ctx context.Context) int {
		if input.AccessSettings == nil {
			return 0
		}
		settings, err := input.AccessSettings.Get(ctx)
		if err != nil {
			logging.BindContext(logger(logging.LayerHTTP), ctx).Warn("failed to load user conversation limit", zap.Error(err))
			return 0
		}
		return settings.UserConversationLimit
	}
	if input.UserControl == nil {
		input.UserControl = usercontrol.New(usercontrol.Deps{
			Identity: input.Identity, LocalAuth: input.LocalAuth, Settings: input.AuthSettings,
			TOTP: input.TOTP, Policy: input.Policy, Query: input.Query, Turn: input.Control,
			Configs: input.ConfigRepo, Storage: input.StorageRepo, Chat: input.ChatRepo,
			AppKeysStore: input.AuthRepo, AppKeys: input.AppAPIKeys, ModelKeys: input.ModelAPIKeys,
			Accounts: input.Accounts, Logger: logger(logging.LayerUserControl), Events: input.ChatEvents,
			Automation: input.Automation, RealtimeSettings: input.RealtimeSettings, ConversationLimit: conversationLimit,
		})
	}
	var retentionPruner conversationretention.Pruner
	if input.UserControl != nil && input.UserControl.Conversations != nil {
		retentionPruner = input.UserControl.Conversations
	}
	conversationRetention := conversationretention.New(input.Config.Mode, input.Accounts, retentionPruner, conversationLimit, logger(logging.LayerHTTP))
	if input.Turn != nil {
		input.Turn.ConversationCreated = conversationRetention.Enforce
		input.Turn.ConversationTerminal = conversationRetention.Enforce
	}
	if input.AdminSettings == nil && input.AuthSettings != nil && input.AccessSettings != nil {
		accessDomain, err := adminsettings.Combine("access", "访问限流", input.AccessSettings.AdminDomain(), input.RealtimeSettings, input.ChatSettings)
		if err != nil {
			return HTTPResult{}, fmt.Errorf("combine admin access settings: %w", err)
		}
		input.AdminSettings = adminsettings.New(input.Config,
			adminsettings.Domain{Settings: input.AuthSettings.AdminDomain()},
			adminsettings.Domain{Settings: accessDomain, AfterUpdate: conversationRetention.SettingsUpdated},
			adminsettings.Domain{Settings: input.MediaSettings},
			adminsettings.Domain{Settings: input.AutomationSettings},
		)
	}
	if input.AdminControl == nil {
		input.AdminControl = admincontrol.New(admincontrol.Deps{
			Accounts: input.Accounts, Query: input.Query, Control: input.Control,
			ChatStore: input.ChatRepo, StorageStore: input.StorageRepo, KeyStore: input.AuthRepo,
			Events: input.ChatEvents, Settings: input.AdminSettings,
		})
	}
	input.AdminControl.SetSettings(input.AdminSettings)

	if input.Turn != nil {
		if input.Turn.Resolver == nil {
			input.Turn.Resolver = conversationresolve.New(input.ChatRepo, input.Turn.Pending)
		}
		if input.Turn.Egress == nil {
			input.Turn.Egress = input.Egress
		}
		if input.Turn.Submitter != nil && input.Turn.Submitter.Materializer == nil {
			preprocessor := preprocesssvc.New(input.Config, input.MediaProcessor)
			preprocessor.Settings = input.MediaSettings
			input.Turn.Submitter.Materializer = &turnsvc.RequestMaterializer{
				Preprocessor: preprocessor, AssetPersister: mediaStore,
				DeletionFailures: input.StorageRepo, PreparedImageClean: mediaStore,
			}
		}
		if input.Turn.Events == nil {
			input.Turn.Events = input.ChatEvents
		}
	}

	metricsRegistry := httpmetrics.NewRegistry()
	routerDeps := httprouter.Deps{
		Config: input.Config, LoggerFactory: input.LoggerFactory, Access: input.Access,
		Policy: input.Policy, UserSessions: input.UserSessions,
		ModelAPIKeys: input.ModelAPIKeys, AppAPIKeys: input.AppAPIKeys,
		Chat: httphandler.ChatAPIHandler{
			Turn: input.Turn, Query: input.Query, Timeline: input.Timeline, Ingress: input.Ingress,
			Streaming: input.Streaming, Catalog: input.Catalog, Control: input.Control,
			Egress: input.Egress, Logger: logger(logging.LayerHTTP),
		},
		App: httphandler.AppAPIHandler{
			Turn: input.Turn, Query: input.Query, Timeline: input.Timeline, Logger: logger(logging.LayerTurnQuery),
		},
		Auth: httphandler.AuthHandler{
			Config: input.Config, LocalAuth: input.LocalAuth, Verification: input.Verification,
			Policy: input.Policy, Settings: input.AuthSettings, GeeTest: input.GeeTest,
			TOTP: input.TOTP, OIDC: input.OIDC, Audit: input.Audit,
			LoginLimiter: input.LoginLimiter, Sessions: input.UserSessions, Logger: logger(logging.LayerAuth),
		},
		User: httphandler.UserHandler{
			Config: input.Config, UserControl: input.UserControl, Timeline: input.Timeline, Logger: logger(logging.LayerAuth),
		},
		Admin: httphandler.AdminHandler{
			Control: input.AdminControl, Timeline: input.Timeline, Audit: input.Audit,
			Logger: logger(logging.LayerAudit), Monitoring: input.AdminMonitoring,
		},
		Lab: httphandler.LabHandler{
			Config: input.Config, Query: input.Query, Turn: input.Turn, Control: input.Control, Logger: logger(logging.LayerHTTP),
		},
		Workspace: httphandler.WorkspaceHandler{Hub: input.WorkspaceHub, Logger: logger(logging.LayerHTTP)},
		Upload: httphandler.UploadHandler{
			Storage: input.StorageRepo, OutputImages: outputImageUploader, OutputImageMaxBytes: input.Config.UploadMaxBytes,
		},
		Health:    httphandler.HealthHandler{Config: input.Config, Store: input.PlatformRepo},
		Readiness: httphandler.ReadinessHandler{Service: readiness.NewService(input.Config, input.PlatformRepo)},
		Setup:     httphandler.SetupHandler{Service: setup.NewService(input.AuthRepo, input.Config)},
		Metrics:   httphandler.MetricsHandler{Registry: metricsRegistry},
	}
	return HTTPResult{
		Router:   routerDeps,
		Services: Services{Turn: input.Turn, ChatSettings: input.ChatSettings, Audit: input.Audit},
	}, nil
}

func NewRouter(input Input) (http.Handler, error) {
	result, err := assemble(input)
	if err != nil {
		return nil, err
	}
	return httprouter.New(result.Router), nil
}

func MustNewRouter(input Input) http.Handler {
	handler, err := NewRouter(input)
	if err != nil {
		panic(err)
	}
	return handler
}
