package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	adminhandler "github.com/th3ee9ine/qqq2api/internal/handler/admin"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestReliabilityRouteIsSuperAdministratorOnlyAndNoStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "super administrator", role: service.RoleAdmin, wantStatus: http.StatusServiceUnavailable},
		{name: "account administrator", role: service.RoleAccountAdmin, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
				Ops: adminhandler.NewOpsHandler(nil),
			}}
			admin := router.Group("/api/v1/admin", func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUserRole), tc.role)
				c.Next()
			})
			registerReliabilityRoutes(admin, handlers)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reliability/status", nil)
			router.ServeHTTP(recorder, request)

			require.Equal(t, tc.wantStatus, recorder.Code)
			if tc.role == service.RoleAdmin {
				require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			} else {
				require.Empty(t, recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestReliabilityTurnStateSettingsRoutesAreSuperAdministratorOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "super administrator", role: service.RoleAdmin, wantStatus: http.StatusServiceUnavailable},
		{name: "account administrator", role: service.RoleAccountAdmin, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
				Ops: adminhandler.NewOpsHandler(nil),
			}}
			admin := router.Group("/api/v1/admin", func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyUserRole), tc.role)
				c.Next()
			})
			registerReliabilityRoutes(admin, handlers)

			for _, method := range []string{http.MethodGet, http.MethodPut} {
				recorder := httptest.NewRecorder()
				request := httptest.NewRequest(method, "/api/v1/admin/reliability/turn-state-settings", nil)
				router.ServeHTTP(recorder, request)
				require.Equal(t, tc.wantStatus, recorder.Code, method)
				if tc.role == service.RoleAdmin {
					require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"), method)
				}
			}
		})
	}
}

func TestReliabilityTurnStateHarvestRouteIsNotRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Ops: adminhandler.NewOpsHandler(nil),
	}}
	admin := router.Group("/api/v1/admin", func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	})
	registerReliabilityRoutes(admin, handlers)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reliability/turn-state-harvest", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}
