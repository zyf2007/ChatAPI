package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/zyf2007/ChatAPI/internal/config"
	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
	httpmiddleware "github.com/zyf2007/ChatAPI/internal/http/middleware"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/httpmetrics"
	"github.com/zyf2007/ChatAPI/internal/ops/observability/logging"
	authaccess "github.com/zyf2007/ChatAPI/internal/service/auth/access"
	sessionrestore "github.com/zyf2007/ChatAPI/internal/service/auth/authn/sessionrestore"
	appkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/appkey"
	modelkey "github.com/zyf2007/ChatAPI/internal/service/auth/authz/modelkey"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/policy"
	"github.com/zyf2007/ChatAPI/internal/service/auth/authz/session"
)

type Deps struct {
	Config        config.Config
	LoggerFactory *logging.Factory
	Access        *authaccess.Service
	Policy        *policy.Service
	UserSessions  *session.Service
	ModelAPIKeys  *modelkey.Service
	AppAPIKeys    *appkey.Service

	Chat      httphandler.ChatAPIHandler
	App       httphandler.AppAPIHandler
	Auth      httphandler.AuthHandler
	User      httphandler.UserHandler
	IM        httphandler.IMHandler
	Admin     httphandler.AdminHandler
	Lab       httphandler.LabHandler
	Workspace httphandler.WorkspaceHandler
	Upload    httphandler.UploadHandler
	Health    httphandler.HealthHandler
	Readiness httphandler.ReadinessHandler
	Setup     httphandler.SetupHandler
	Metrics   httphandler.MetricsHandler
}

func New(deps Deps) http.Handler {
	router := chi.NewRouter()
	httpLogger := deps.logger(logging.LayerHTTP)
	authLogger := deps.logger(logging.LayerAuth)
	metricsRegistry := deps.Metrics.Registry
	if metricsRegistry == nil {
		metricsRegistry = httpmetrics.NewRegistry()
		deps.Metrics.Registry = metricsRegistry
	}

	router.Use(cors.Handler(cors.Options{
		AllowedOrigins:   deps.Config.CORSOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-ChatAPI-App-Key", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	router.Use(httpmiddleware.RecordHTTPMetrics(metricsRegistry))
	router.Use(requestLoggingMiddleware(deps.LoggerFactory, httpLogger))
	router.Use(httpmiddleware.RequireLabAccess(deps.Access, authLogger))
	router.Use(httpmiddleware.RequireAccessRateLimit(deps.Access))
	router.Use(httpmiddleware.LoadLabActor(deps.Access, authLogger))
	router.Use(httpmiddleware.LoadUserSession(sessionrestore.NewService(deps.UserSessions), authLogger))
	router.Use(httpmiddleware.RequireSessionCSRF(deps.Access, deps.Policy, authLogger))

	chatHandler := deps.Chat
	appHandler := deps.App
	authHandler := deps.Auth
	userHandler := deps.User
	adminHandler := deps.Admin
	labHandler := deps.Lab
	workspaceHandler := deps.Workspace
	uploadHandler := deps.Upload
	healthHandler := deps.Health
	readinessHandler := deps.Readiness
	setupHandler := deps.Setup
	metricsHandler := deps.Metrics

	modelAuth := httpmiddleware.RequireModelAPIKey(deps.ModelAPIKeys, authLogger)
	modelPrincipalAccess := httpmiddleware.RequirePrincipalAccess(deps.Access, authLogger)
	appAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return chainMiddleware(
			httpmiddleware.RequireAppAPIKey(deps.AppAPIKeys, deps.Policy, deps.Config.TrustedProxies, authLogger, scopes...),
			httpmiddleware.RequirePrincipalAccess(deps.Access, authLogger),
		)
	}
	userAuth := httpmiddleware.RequireUserSession(deps.Policy, authLogger)
	userPrincipalAccess := httpmiddleware.RequirePrincipalAccess(deps.Access, authLogger)
	userOrAppAuth := func(scopes ...string) func(http.Handler) http.Handler {
		return httpmiddleware.RequireUserSessionOrAppAPI(deps.Policy, appAuth(scopes...), userAuth)
	}
	adminAuth := func(next http.Handler) http.Handler {
		return httpmiddleware.RequireAdmin(deps.Policy, authLogger)(httpmiddleware.RequireUserSession(deps.Policy, authLogger)(next))
	}

	router.Get("/api/health", healthHandler.ServeHTTP)
	router.Get("/api/ready", readinessHandler.ServeHTTP)
	router.Get("/api/ws", workspaceHandler.ServeWS)
	router.With(userAuth, userPrincipalAccess).Get("/api/media/assets/{fileID}", uploadHandler.GetImage)
	router.With(userAuth, userPrincipalAccess).Post("/api/conversations/{conversationID}/output-images", uploadHandler.UploadOutputImage)
	router.Get("/api/setup/status", setupHandler.Status)
	router.Get("/setup", setupHandler.HTML)
	router.Post("/setup", setupHandler.Create)
	if deps.Config.MetricsEnabled {
		router.Get("/metrics", metricsHandler.ServeHTTP)
	}

	registerProtocolRoutes(router, deps.Config, modelAuth, modelPrincipalAccess, chatHandler)
	if deps.Config.Mode == config.ModeLab {
		registerLabRoutes(router, labHandler)
		mountSPA(router, deps.Config)
		return router
	}

	router.Post("/api/auth/register", authHandler.Register)
	router.Get("/api/auth/register/config", authHandler.RegisterConfig)
	router.Post("/api/auth/register/send-code", authHandler.RegisterSendCode)
	router.Post("/api/auth/login", authHandler.Login)
	router.Post("/api/auth/logout", authHandler.Logout)
	router.Get("/api/auth/session", userHandler.Session)
	router.Get("/api/auth/oidc/config", authHandler.OIDCConfig)
	router.Get("/api/auth/oidc/login", authHandler.OIDCLogin)
	router.With(userAuth, userPrincipalAccess).Get("/api/auth/oidc/link", authHandler.OIDCLink)
	router.Get("/api/auth/oidc/callback", authHandler.OIDCCallback)
	router.Get("/api/auth/password/config", authHandler.PasswordConfig)
	router.Post("/api/auth/password/send-code", authHandler.PasswordSendCode)
	router.Post("/api/auth/verification/send", authHandler.SendVerification)
	router.Post("/api/auth/verification/verify", authHandler.VerifyCode)
	router.Post("/api/auth/password/forgot", authHandler.ForgotPassword)
	router.Post("/api/auth/password/reset", authHandler.ResetPassword)
	router.With(userAuth, userPrincipalAccess).Get("/api/auth/totp/setup", authHandler.TOTPSetup)
	router.With(userAuth, userPrincipalAccess).Post("/api/auth/totp/confirm", authHandler.TOTPConfirm)
	router.With(userAuth, userPrincipalAccess).Post("/api/auth/totp/reset", authHandler.TOTPReset)

	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations", userHandler.ListConversations)
	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations/{conversationID}/messages", userHandler.ListConversationMessages)
	router.With(userOrAppAuth("conversations:read")).Get("/api/conversations/{conversationID}/timeline", userHandler.ListConversationTimeline)
	router.With(userAuth, userPrincipalAccess).Post("/api/conversations/{conversationID}/abort", userHandler.AbortConversation)
	router.With(userAuth, userPrincipalAccess).Delete("/api/conversations/{conversationID}", userHandler.DeleteConversation)
	router.With(userAuth, userPrincipalAccess).Post("/api/conversations/prune", userHandler.PruneConversations)

	router.With(userAuth, userPrincipalAccess).Get("/api/user/app-keys", userHandler.ListAppKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/app-keys", userHandler.CreateAppKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/app-keys/{keyID}", userHandler.RevokeAppKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/app-keys/{keyID}/secret", userHandler.RevealAppKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/model-keys", userHandler.ListModelKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/model-keys", userHandler.CreateModelKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/model-keys/{keyID}", userHandler.RevokeModelKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/model-keys/{keyID}/secret", userHandler.RevealModelKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/virtual-models", userHandler.ListVirtualModels)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/virtual-models", userHandler.CreateVirtualModel)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/virtual-models/{modelID}", userHandler.DeleteVirtualModel)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/identities", userHandler.ListIdentities)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/config", userHandler.GetConfig)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/config", userHandler.SetConfig)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/im/clawbot", deps.IM.Status)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/im/clawbot/login", deps.IM.StartLogin)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/im/clawbot/login/{session_id}/poll", deps.IM.PollLogin)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/im/clawbot", deps.IM.Disconnect)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/password", userHandler.ChangePassword)
	router.With(userAuth, userPrincipalAccess).Get("/api/automation/rules", userHandler.ListAutomationRules)
	router.With(userAuth, userPrincipalAccess).Post("/api/automation/rules", userHandler.SaveAutomationRule)
	router.With(userAuth, userPrincipalAccess).Put("/api/automation/rules/{ruleID}", userHandler.SaveAutomationRule)
	router.With(userAuth, userPrincipalAccess).Delete("/api/automation/rules/{ruleID}", userHandler.DeleteAutomationRule)

	router.With(userAuth, userPrincipalAccess).Get("/api/user/api-keys", userHandler.ListAppKeys)
	router.With(userAuth, userPrincipalAccess).Post("/api/user/api-keys", userHandler.CreateAppKey)
	router.With(userAuth, userPrincipalAccess).Delete("/api/user/api-keys/{keyID}", userHandler.RevokeAppKey)
	router.With(userAuth, userPrincipalAccess).Get("/api/user/api-keys/{keyID}/secret", userHandler.RevealAppKey)

	router.With(adminAuth).Get("/api/admin/users", adminHandler.ListUsers)
	router.With(adminAuth).Post("/api/admin/users", adminHandler.CreateUser)
	router.With(adminAuth).Get("/api/admin/users/{userID}", adminHandler.GetUser)
	router.With(adminAuth).Get("/api/admin/users/{userID}/conversations", adminHandler.ListUserConversations)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/conversations/{conversationID}", adminHandler.DeleteUserConversation)
	router.With(adminAuth).Put("/api/admin/users/{userID}/role", adminHandler.SetUserRole)
	router.With(adminAuth).Post("/api/admin/users/{userID}/disable", adminHandler.DisableUser)
	router.With(adminAuth).Post("/api/admin/users/{userID}/enable", adminHandler.EnableUser)
	router.With(adminAuth).Put("/api/admin/users/{userID}/password", adminHandler.ResetUserPassword)
	router.With(adminAuth).Get("/api/admin/users/{userID}/identities", adminHandler.ListUserIdentities)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/identities/{identityID}", adminHandler.DeleteUserIdentity)
	router.With(adminAuth).Get("/api/admin/users/{userID}/app-keys", adminHandler.ListUserAppKeys)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/app-keys/{keyID}", adminHandler.RevokeUserAppKey)
	router.With(adminAuth).Get("/api/admin/users/{userID}/model-keys", adminHandler.ListUserModelKeys)
	router.With(adminAuth).Delete("/api/admin/users/{userID}/model-keys/{keyID}", adminHandler.RevokeUserModelKey)
	router.With(adminAuth).Get("/api/admin/users/{userID}/delete-preview", adminHandler.DeletePreview)
	router.With(adminAuth).Delete("/api/admin/users/{userID}", adminHandler.DeleteUser)
	router.With(adminAuth).Post("/api/admin/users/{userID}/transfer-ownership", adminHandler.TransferOwnership)
	router.With(adminAuth).Get("/api/admin/users/{userID}/ownership-items", adminHandler.OwnershipItems)
	router.With(adminAuth).Post("/api/admin/users/{userID}/transfer-ownership-selection", adminHandler.TransferOwnershipSelection)

	router.With(adminAuth).Get("/api/admin/requests", adminHandler.ListRequests)
	router.With(adminAuth).Get("/api/admin/requests/{requestID}", adminHandler.GetRequest)
	router.With(adminAuth).Get("/api/admin/conversations", adminHandler.ListConversations)
	router.With(adminAuth).Get("/api/admin/conversations/{conversationID}/messages", adminHandler.ListConversationMessages)
	router.With(adminAuth).Get("/api/admin/conversations/{conversationID}/timeline", adminHandler.ListConversationTimeline)
	router.With(adminAuth).Post("/api/admin/conversations/{conversationID}/abort", adminHandler.AbortConversation)
	router.With(adminAuth).Post("/api/admin/conversations/{conversationID}/complete", adminHandler.CompleteConversation)
	router.With(adminAuth).Delete("/api/admin/conversations/{conversationID}", adminHandler.DeleteConversation)
	router.With(adminAuth).Get("/api/admin/audit/logs", adminHandler.ListAuditLogs)
	router.With(adminAuth).Get("/api/admin/settings/catalog", adminHandler.SettingsCatalog)
	router.With(adminAuth).Get("/api/admin/monitor/stream", adminHandler.ServeMonitoringStream)
	router.With(adminAuth).Get("/api/admin/settings/overview", adminHandler.SettingsOverview)
	router.With(adminAuth).Get("/api/admin/settings/runtime", adminHandler.RuntimeSettings)
	router.With(adminAuth).Get("/api/admin/settings/{domain}", adminHandler.GetSettings)
	router.With(adminAuth).Patch("/api/admin/settings/{domain}", adminHandler.PatchSettings)

	router.With(appAuth("requests:read")).Get("/api/requests", appHandler.ListRequests)
	router.With(appAuth("requests:read")).Get("/api/requests/{requestID}", appHandler.GetRequest)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/delta", chatHandler.DeltaOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/complete", chatHandler.CompleteOutput)
	router.With(userOrAppAuth("requests:respond")).Post("/api/chat/output/abort", chatHandler.AbortOutput)

	mountSPA(router, deps.Config)
	return router
}
