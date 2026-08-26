package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountAdminManagementRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		AccountAdmin: adminhandler.NewAccountAdminHandler(nil),
	}}
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
		http.MethodGet + " /api/v1/admin/account-admins",
		http.MethodPost + " /api/v1/admin/account-admins",
		http.MethodPut + " /api/v1/admin/account-admins/:id",
		http.MethodDelete + " /api/v1/admin/account-admins/:id",
	} {
		require.True(t, registered[route], route)
	}
}
