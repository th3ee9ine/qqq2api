//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResourceDeleteRoutesRejectAccountAdministrators(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAccountAdmin)
		c.Next()
	})

	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Account:     adminhandler.NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
		OAuth:       adminhandler.NewOAuthHandler(nil),
		OpenAIOAuth: adminhandler.NewOpenAIOAuthHandler(nil, nil, nil, nil),
		Proxy:       adminhandler.NewProxyHandler(nil),
	}}
	pass := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	admin := router.Group("/api/v1/admin")
	registerAccountRoutes(admin, handlers, pass)
	registerProxyRoutes(admin, handlers, pass)

	for _, tt := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "delete account", method: http.MethodDelete, path: "/api/v1/admin/accounts/42"},
		{name: "batch delete accounts", method: http.MethodPost, path: "/api/v1/admin/accounts/batch-delete"},
		{name: "delete proxy IP", method: http.MethodDelete, path: "/api/v1/admin/proxies/7"},
		{name: "batch delete proxy IPs", method: http.MethodPost, path: "/api/v1/admin/proxies/batch-delete"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(tt.method, tt.path, nil)
			router.ServeHTTP(response, request)

			require.Equal(t, http.StatusForbidden, response.Code)
			require.Contains(t, response.Body.String(), `"code":"FORBIDDEN"`)
		})
	}
}
