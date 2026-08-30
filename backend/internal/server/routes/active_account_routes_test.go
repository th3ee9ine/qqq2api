package routes

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	servermiddleware "github.com/th3ee9ine/qqq2api/internal/server/middleware"
)

func TestAdminRoutesClaudeAndOpenAIAccountCapabilitiesAreRegistered(t *testing.T) {
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
	)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"GET /api/v1/admin/channels/model-pricing",
		"POST /api/v1/admin/accounts",
		"PUT /api/v1/admin/accounts/:id",
		"POST /api/v1/admin/accounts/:id/test",
		"GET /api/v1/admin/accounts/:id/usage",
		"GET /api/v1/admin/accounts/:id/models",
		"POST /api/v1/admin/accounts/:id/apply-oauth-credentials",
		"POST /api/v1/admin/accounts/import/codex-session",
		"POST /api/v1/admin/accounts/generate-auth-url",
		"POST /api/v1/admin/accounts/generate-setup-token-url",
		"POST /api/v1/admin/accounts/exchange-code",
		"POST /api/v1/admin/accounts/exchange-setup-token-code",
		"POST /api/v1/admin/accounts/cookie-auth",
		"POST /api/v1/admin/accounts/setup-token-cookie-auth",
		"POST /api/v1/admin/openai/generate-auth-url",
		"POST /api/v1/admin/openai/exchange-code",
		"POST /api/v1/admin/openai/refresh-token",
		"POST /api/v1/admin/openai/accounts/:id/subscription/refresh",
		"POST /api/v1/admin/openai/create-from-oauth",
		"POST /api/v1/admin/openai/create-from-codex-pat",
		"GET /api/v1/admin/openai/accounts/:id/quota",
		"POST /api/v1/admin/openai/accounts/:id/quota/refresh",
	} {
		require.True(t, registered[route], route)
	}
}
