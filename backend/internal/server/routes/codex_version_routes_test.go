package routes

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	adminhandler "github.com/th3ee9ine/qqq2api/internal/handler/admin"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCodexVersionSettingsRoutesRequireAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{service.RoleAdmin, service.RoleAccountAdmin, service.RoleUser} {
		t.Run(role, func(t *testing.T) {
			router := gin.New()
			group := router.Group("/api/v1/admin", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUserRole), role); c.Next() })
			h := &handler.Handlers{Admin: &handler.AdminHandlers{Setting: adminhandler.NewSettingHandler(nil, nil, nil, nil, nil, nil, nil)}}
			registerSettingsRoutes(group, h, middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() }))
			for _, tc := range []struct{ method, path string }{{http.MethodGet, "/settings/openai-codex/versions"}, {http.MethodPost, "/settings/openai-codex/sync"}} {
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest(tc.method, "/api/v1/admin"+tc.path, nil))
				if role == service.RoleAdmin {
					require.Equal(t, http.StatusServiceUnavailable, rec.Code)
				} else {
					require.Equal(t, http.StatusForbidden, rec.Code)
				}
			}
		})
	}
}
