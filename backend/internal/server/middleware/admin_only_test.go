//go:build unit

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminOnlyRejectsRestrictedAccountAdministrators(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "super administrator", role: service.RoleAdmin, wantStatus: http.StatusNoContent},
		{name: "account administrator", role: service.RoleAccountAdmin, wantStatus: http.StatusForbidden},
		{name: "ordinary user", role: service.RoleUser, wantStatus: http.StatusForbidden},
		{name: "missing role", wantStatus: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			if tc.role != "" {
				router.Use(func(c *gin.Context) {
					c.Set(string(ContextKeyUserRole), tc.role)
					c.Next()
				})
			}
			router.Use(AdminOnly())
			router.GET("/keys", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/keys", nil))
			require.Equal(t, tc.wantStatus, response.Code)
		})
	}
}
