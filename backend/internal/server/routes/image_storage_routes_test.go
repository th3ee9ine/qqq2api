package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminImageStorageRoutesAreStandaloneAndUpdateRequiresStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		ImageStorage: adminhandler.NewImageStorageHandler(nil),
	}}
	pass := func(c *gin.Context) {
		c.Set(string(servermiddleware.ContextKeyUserRole), service.RoleAdmin)
		c.Next()
	}
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
	)

	registered := make(map[string]bool)
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	require.True(t, registered["GET /api/v1/admin/settings/image-storage"])
	require.True(t, registered["PUT /api/v1/admin/settings/image-storage"])
	require.True(t, registered["POST /api/v1/admin/settings/image-storage/test"])
	require.False(t, registered["GET /api/v1/admin/backups/image-storage"])
	require.False(t, registered["PUT /api/v1/admin/backups/image-storage"])
	require.False(t, registered["POST /api/v1/admin/backups/image-storage/test"])

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/image-storage", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusPreconditionRequired, recorder.Code)
	require.Equal(t, 1, stepUpCalls)
}
