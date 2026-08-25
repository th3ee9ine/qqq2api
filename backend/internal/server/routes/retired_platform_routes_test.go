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
		"GET /api/v1/admin/users",
		"GET /api/v1/admin/users/:id",
		"POST /api/v1/admin/users",
		"PUT /api/v1/admin/users/:id",
		"DELETE /api/v1/admin/users/:id",
		"GET /api/v1/admin/user-attributes",
		"GET /api/v1/admin/subscriptions",
		"POST /api/v1/admin/subscriptions/assign",
		"GET /api/v1/admin/groups/:id/subscriptions",
		"GET /api/v1/admin/users/:id/subscriptions",
		"GET /api/v1/admin/redeem-codes",
		"POST /api/v1/admin/redeem-codes/generate",
		"GET /api/v1/admin/promo-codes",
		"POST /api/v1/admin/promo-codes",
		"GET /api/v1/admin/announcements",
		"POST /api/v1/admin/announcements",
		"GET /api/v1/admin/channels",
		"GET /api/v1/admin/channels/:id",
		"GET /api/v1/admin/channels/pricing/sync-models",
		"POST /api/v1/admin/channels",
		"PUT /api/v1/admin/channels/:id",
		"DELETE /api/v1/admin/channels/:id",
		"GET /api/v1/admin/channel-monitors",
		"POST /api/v1/admin/channel-monitors",
		"GET /api/v1/admin/channel-monitor-templates",
		"GET /api/v1/admin/channel-monitor-v2/config",
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
		"GET /api/v1/admin/payment/dashboard",
		"GET /api/v1/admin/payment/orders",
		"GET /api/v1/admin/dashboard/users-trend",
		"GET /api/v1/admin/dashboard/users-ranking",
		"POST /api/v1/admin/dashboard/users-usage",
		"GET /api/v1/admin/dashboard/user-breakdown",
		"GET /api/v1/admin/ops/user-concurrency",
		"POST /api/v1/admin/risk-control/users/:user_id/unban",
		"GET /api/v1/admin/usage/search-users",
	} {
		require.False(t, registered[route], route)
	}
}

func TestReducedPublicAndAuthenticatedRoutesDoNotRestoreSelfServiceFeatures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{}
	pass := func(c *gin.Context) { c.Next() }
	v1 := router.Group("/api/v1")

	RegisterAuthRoutes(
		v1,
		handlers,
		servermiddleware.JWTAuthMiddleware(pass),
		servermiddleware.AuditLogMiddleware(pass),
		nil,
		nil,
		nil,
	)
	RegisterUserRoutes(
		v1,
		handlers,
		servermiddleware.JWTAuthMiddleware(pass),
		servermiddleware.AuditLogMiddleware(pass),
		nil,
		nil,
	)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, route := range []string{
		"POST /api/v1/auth/register",
		"POST /api/v1/auth/send-verify-code",
		"POST /api/v1/auth/validate-promo-code",
		"POST /api/v1/auth/validate-invitation-code",
		"POST /api/v1/auth/forgot-password",
		"POST /api/v1/auth/reset-password",
		"POST /api/v1/auth/passkey/login/begin",
		"GET /api/v1/auth/oauth/github/start",
		"GET /api/v1/auth/oauth/google/start",
		"GET /api/v1/auth/oauth/linuxdo/start",
		"GET /api/v1/auth/oauth/wechat/start",
		"GET /api/v1/auth/oauth/oidc/start",
		"GET /api/v1/auth/oauth/dingtalk/start",
		"GET /api/v1/settings/email-unsubscribe",
		"GET /api/v1/user/profile",
		"PUT /api/v1/user/password",
		"GET /api/v1/user/aff",
		"GET /api/v1/user/platform-quotas",
		"GET /api/v1/user/passkeys",
		"GET /api/v1/groups/rates",
		"GET /api/v1/channels/available",
		"GET /api/v1/usage",
		"GET /api/v1/announcements",
		"POST /api/v1/redeem",
		"GET /api/v1/redeem/history",
		"GET /api/v1/subscriptions",
		"GET /api/v1/subscriptions/active",
		"GET /api/v1/channel-monitors",
		"GET /api/v1/channel-monitor-v2/snapshot",
		"GET /api/v1/model-plaza",
		"GET /api/v1/payment/config",
		"POST /api/v1/payment/orders",
		"POST /api/v1/payment/webhook/stripe",
	} {
		require.False(t, registered[route], route)
	}

	for _, route := range []string{
		"POST /api/v1/auth/login",
		"POST /api/v1/auth/login/2fa",
		"POST /api/v1/auth/refresh",
		"POST /api/v1/auth/logout",
		"GET /api/v1/auth/me",
		"GET /api/v1/settings/public",
		"GET /api/v1/keys",
		"GET /api/v1/groups/available",
		"GET /api/v1/user/totp/status",
	} {
		require.True(t, registered[route], route)
	}
}
