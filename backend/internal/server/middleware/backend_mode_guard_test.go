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

func TestBackendModeUserGuardRequiresAdministrator(t *testing.T) {
	for _, tc := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "administrator allowed", role: service.RoleAdmin, wantStatus: http.StatusOK},
		{name: "account administrator allowed", role: service.RoleAccountAdmin, wantStatus: http.StatusOK},
		{name: "ordinary user blocked", role: service.RoleUser, wantStatus: http.StatusForbidden},
		{name: "missing role blocked", wantStatus: http.StatusForbidden},
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
			router.Use(BackendModeUserGuard(nil))
			router.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

func TestBackendModeAuthGuardOnlyAllowsAdministratorSessionEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "password login", path: "/api/v1/auth/login", wantStatus: http.StatusOK},
		{name: "totp login", path: "/api/v1/auth/login/2fa", wantStatus: http.StatusOK},
		{name: "refresh", path: "/api/v1/auth/refresh", wantStatus: http.StatusOK},
		{name: "logout", path: "/api/v1/auth/logout", wantStatus: http.StatusOK},
		{name: "registration blocked", path: "/api/v1/auth/register", wantStatus: http.StatusForbidden},
		{name: "password reset blocked", path: "/api/v1/auth/forgot-password", wantStatus: http.StatusForbidden},
		{name: "oauth start blocked", path: "/api/v1/auth/oauth/github/start", wantStatus: http.StatusForbidden},
		{name: "oauth callback blocked", path: "/api/v1/auth/oauth/github/callback", wantStatus: http.StatusForbidden},
		{name: "oauth account creation blocked", path: "/api/v1/auth/oauth/dingtalk/create-account", wantStatus: http.StatusForbidden},
		{name: "pending oauth blocked", path: "/api/v1/auth/oauth/pending/exchange", wantStatus: http.StatusForbidden},
		{name: "passkey blocked", path: "/api/v1/auth/passkey/login/start", wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(BackendModeAuthGuard(nil))
			router.Any("/*path", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
			require.Equal(t, tc.wantStatus, w.Code)
		})
	}
}

func TestBackendModeAllowsAuthPathDoesNotAcceptLookalikes(t *testing.T) {
	require.True(t, backendModeAllowsAuthPath("/api/v1/auth/login"))
	require.True(t, backendModeAllowsAuthPath(" /API/V1/AUTH/REFRESH "))
	require.False(t, backendModeAllowsAuthPath("/api/v1/auth/login/extra"))
	require.False(t, backendModeAllowsAuthPath("/api/v1/auth/oauth/login"))
}
