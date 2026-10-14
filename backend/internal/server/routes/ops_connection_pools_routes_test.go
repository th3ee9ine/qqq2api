package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/th3ee9ine/qqq2api/internal/handler"
	adminhandler "github.com/th3ee9ine/qqq2api/internal/handler/admin"
	servermiddleware "github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

func TestOpsConnectionPoolStatsRouteUsesAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	ops := service.NewOpsService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Ops: adminhandler.NewOpsHandler(ops)}}
	auth := servermiddleware.AdminAuthMiddleware(func(c *gin.Context) {
		switch c.GetHeader("Authorization") {
		case "":
			servermiddleware.AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authorization required")
		case "Bearer admin":
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 1})
			c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
			c.Next()
		default:
			servermiddleware.AbortWithError(c, http.StatusForbidden, "FORBIDDEN", "Admin access required")
		}
	})
	audit := servermiddleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := servermiddleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), handlers, auth, audit, stepUp, nil)
	for _, tc := range []struct {
		auth string
		code int
	}{
		{code: http.StatusUnauthorized},
		{auth: "Bearer user", code: http.StatusForbidden},
		{auth: "Bearer admin", code: http.StatusOK},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ops/runtime/connection-pools", nil)
		request.Header.Set("Authorization", tc.auth)
		router.ServeHTTP(recorder, request)
		require.Equal(t, tc.code, recorder.Code)
		if tc.code == http.StatusOK {
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Contains(t, recorder.Body.String(), `"scope":"instance"`)
		}
	}
}
