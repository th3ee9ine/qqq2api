//go:build unit

package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	adminhandler "github.com/th3ee9ine/qqq2api/internal/handler/admin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
)

func TestAccountOwnershipScopeMountedOnAccountAndOpenAIRouteGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxkey.AccountAdminID, int64(41)))
		c.Next()
	})

	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account:     adminhandler.NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
		OpenAIOAuth: adminhandler.NewOpenAIOAuthHandler(nil, nil, nil, nil),
	}}
	admin := router.Group("/api/v1/admin")
	pass := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	registerAccountRoutes(admin, handlers, pass)
	registerOpenAIOAuthRoutes(admin, handlers)

	for _, path := range []string{
		"/api/v1/admin/accounts/42",
		"/api/v1/admin/openai/accounts/42/quota",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusInternalServerError, response.Code)
			require.Contains(t, response.Body.String(), "ACCOUNT_OWNERSHIP_NOT_CONFIGURED")
		})
	}
}
