package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminRoutesRetiredPlatformEndpointsAreNotRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{}}
	pass := func(c *gin.Context) { c.Next() }

	RegisterAdminRoutes(
		router.Group("/api/v1"),
		handlers,
		servermiddleware.AdminAuthMiddleware(pass),
		servermiddleware.AuditLogMiddleware(pass),
		servermiddleware.StepUpAuthMiddleware(pass),
		nil,
		nil,
	)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"GET /api/v1/admin/channels",
		"GET /api/v1/admin/channels/:id",
		"GET /api/v1/admin/channels/pricing/sync-models",
		"POST /api/v1/admin/channels",
		"PUT /api/v1/admin/channels/:id",
		"DELETE /api/v1/admin/channels/:id",
		"POST /api/v1/admin/gemini/oauth/auth-url",
		"POST /api/v1/admin/gemini/oauth/exchange-code",
		"POST /api/v1/admin/antigravity/oauth/auth-url",
		"POST /api/v1/admin/antigravity/oauth/exchange-code",
		"POST /api/v1/admin/grok/oauth/auth-url",
		"POST /api/v1/admin/grok/oauth/exchange-code",
		"GET /api/v1/admin/grok/accounts/:id/quota",
		"GET /api/v1/admin/cn-providers/accounts/:id/quota",
		"GET /api/v1/admin/cn-providers/accounts/:id/balance",
		"GET /api/v1/admin/backups/s3-config",
		"POST /api/v1/admin/backups",
		"GET /api/v1/admin/backups",
		"GET /api/v1/admin/backups/:id",
		"DELETE /api/v1/admin/backups/:id",
		"GET /api/v1/admin/backups/:id/download-url",
		"POST /api/v1/admin/backups/:id/restore",
		"GET /api/v1/admin/data-management/agent/health",
		"POST /api/v1/admin/data-management/backups",
		"GET /api/v1/admin/data-management/backups",
		"GET /api/v1/admin/data-management/backups/:job_id",
	} {
		require.False(t, registered[route], route)
	}
}
