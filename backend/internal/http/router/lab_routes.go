package router

import (
	"github.com/go-chi/chi/v5"
	httphandler "github.com/zyf2007/ChatAPI/internal/http/handler"
)

func registerLabRoutes(router chi.Router, labHandler httphandler.LabHandler) {
	router.Get("/api/lab/workspace", labHandler.Workspace)
	router.Get("/api/ws-info", labHandler.PingInfo)
	router.Get("/lab/requests", labHandler.ListRequests)
	router.Get("/lab/requests/{requestID}", labHandler.GetRequest)
	router.Post("/lab/requests/{requestID}/copy-curl", labHandler.CopyRequestCurl)
	router.Post("/lab/requests/{requestID}/delta", labHandler.RequestDelta)
	router.Post("/lab/requests/{requestID}/complete", labHandler.RequestComplete)
	router.Post("/lab/requests/{requestID}/abort", labHandler.RequestAbort)
}

func registerLabUserRoutes(router chi.Router, userHandler httphandler.UserHandler, uploadHandler httphandler.UploadHandler) {
	router.Get("/api/user/virtual-models", userHandler.ListVirtualModels)
	router.Post("/api/user/virtual-models", userHandler.CreateVirtualModel)
	router.Delete("/api/user/virtual-models/{modelID}", userHandler.DeleteVirtualModel)
	router.Get("/api/user/config", userHandler.GetConfig)
	router.Post("/api/user/config", userHandler.SetConfig)
	router.Delete("/api/conversations/{conversationID}", userHandler.DeleteConversation)
	router.Post("/api/conversations/prune", userHandler.PruneConversations)
	router.Post("/api/conversations/{conversationID}/output-images", uploadHandler.UploadOutputImage)
	router.Get("/api/media/assets/{fileID}", uploadHandler.GetImage)
}

// Lab exposes workspace policies and instance diagnostics behind its access gate.
func registerLabSettingsRoutes(router chi.Router, adminHandler httphandler.AdminHandler) {
	router.Get("/api/admin/settings/catalog", adminHandler.SettingsCatalog)
	router.Get("/api/admin/settings/overview", adminHandler.SettingsOverview)
	router.Get("/api/admin/settings/runtime", adminHandler.RuntimeSettings)
	router.Get("/api/admin/monitor/stream", adminHandler.ServeMonitoringStream)
	router.Get("/api/admin/audit/logs", adminHandler.ListAuditLogs)
	router.Get("/api/admin/settings/{domain:access|media}", adminHandler.GetSettings)
	router.Patch("/api/admin/settings/{domain:access|media}", adminHandler.PatchSettings)
}
