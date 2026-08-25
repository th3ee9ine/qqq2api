package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPluginManagementRoutesRequireAdminAndMutationsRequireStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Plugin: adminhandler.NewPluginHandler(nil),
	}}
	pass := func(c *gin.Context) { c.Next() }

	t.Run("management is behind admin authentication", func(t *testing.T) {
		router := gin.New()
		adminCalls := 0
		adminAuth := func(c *gin.Context) {
			adminCalls++
			c.AbortWithStatus(http.StatusUnauthorized)
		}
		RegisterAdminRoutes(
			router.Group("/api/v1"),
			handlers,
			servermiddleware.AdminAuthMiddleware(adminAuth),
			servermiddleware.AuditLogMiddleware(pass),
			servermiddleware.StepUpAuthMiddleware(pass),
			nil,
			nil,
		)

		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/plugins", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusUnauthorized, response.Code)
		require.Equal(t, 1, adminCalls)
	})

	t.Run("state change requires step up", func(t *testing.T) {
		router := gin.New()
		stepUpCalls := 0
		stepUp := func(c *gin.Context) {
			stepUpCalls++
			c.AbortWithStatus(http.StatusPreconditionRequired)
		}
		RegisterAdminRoutes(
			router.Group("/api/v1"),
			handlers,
			servermiddleware.AdminAuthMiddleware(pass),
			servermiddleware.AuditLogMiddleware(pass),
			servermiddleware.StepUpAuthMiddleware(stepUp),
			nil,
			nil,
		)

		request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/plugins/1/enable", strings.NewReader(`{"accept_untested":true,"rollout_percent":100}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)

		require.Equal(t, http.StatusPreconditionRequired, response.Code)
		require.Equal(t, 1, stepUpCalls)
	})
}
