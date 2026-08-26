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

func TestAccountAdminScope_AccountAdminRequestMatrix(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		// Account and proxy/IP maintenance surfaces.
		{name: "list accounts", method: http.MethodGet, path: "/api/v1/admin/accounts", wantStatus: http.StatusNoContent},
		{name: "update account", method: http.MethodPut, path: "/api/v1/admin/accounts/42", wantStatus: http.StatusNoContent},
		{name: "account child action", method: http.MethodPost, path: "/api/v1/admin/accounts/42/test", wantStatus: http.StatusNoContent},
		{name: "read account probe policy", method: http.MethodGet, path: "/api/v1/admin/accounts/upstream-billing-probe/settings", wantStatus: http.StatusNoContent},
		{name: "read ollama account policy", method: http.MethodGet, path: "/api/v1/admin/accounts/ollama-cloud-usage/settings", wantStatus: http.StatusNoContent},
		{name: "openai oauth account flow", method: http.MethodPost, path: "/api/v1/admin/openai/oauth/start", wantStatus: http.StatusNoContent},
		{name: "create proxy IP", method: http.MethodPost, path: "/api/v1/admin/proxies", wantStatus: http.StatusNoContent},
		{name: "clear ollama session", method: http.MethodDelete, path: "/api/v1/admin/accounts/42/ollama-cloud-usage/session", wantStatus: http.StatusNoContent},
		{name: "clear temporary account state", method: http.MethodDelete, path: "/api/v1/admin/accounts/42/temp-unschedulable", wantStatus: http.StatusNoContent},
		{name: "scheduled account test", method: http.MethodPost, path: "/api/v1/admin/scheduled-test-plans/9/run", wantStatus: http.StatusNoContent},

		// Account administrators may maintain resources but cannot remove them.
		{name: "delete account", method: http.MethodDelete, path: "/api/v1/admin/accounts/42", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_DELETE_FORBIDDEN"},
		{name: "batch delete accounts", method: http.MethodPost, path: "/api/v1/admin/accounts/batch-delete", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_DELETE_FORBIDDEN"},
		{name: "delete proxy IP", method: http.MethodDelete, path: "/api/v1/admin/proxies/7", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_DELETE_FORBIDDEN"},
		{name: "batch delete proxy IPs", method: http.MethodPost, path: "/api/v1/admin/proxies/batch-delete", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_DELETE_FORBIDDEN"},

		// Read-only support data needed by account forms.
		{name: "read all groups", method: http.MethodGet, path: "/api/v1/admin/groups/all", wantStatus: http.StatusNoContent},
		{name: "read TLS profiles", method: http.MethodGet, path: "/api/v1/admin/tls-fingerprint-profiles", wantStatus: http.StatusNoContent},
		{name: "read one TLS profile", method: http.MethodGet, path: "/api/v1/admin/tls-fingerprint-profiles/3", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "read sanitized web search setting", method: http.MethodGet, path: "/api/v1/admin/settings/web-search-emulation", wantStatus: http.StatusNoContent},

		// Everything outside the explicit capability list is denied.
		{name: "account admin management", method: http.MethodGet, path: "/api/v1/admin/account-admins", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "dashboard", method: http.MethodGet, path: "/api/v1/admin/dashboard", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "global API keys", method: http.MethodGet, path: "/api/v1/admin/api-keys", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "mutate groups", method: http.MethodPost, path: "/api/v1/admin/groups/all", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "mutate TLS profiles", method: http.MethodPut, path: "/api/v1/admin/tls-fingerprint-profiles/3", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "mutate web search setting", method: http.MethodPut, path: "/api/v1/admin/settings/web-search-emulation", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "mutate account probe global policy", method: http.MethodPut, path: "/api/v1/admin/accounts/upstream-billing-probe/settings", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "mutate ollama global policy", method: http.MethodPut, path: "/api/v1/admin/accounts/ollama-cloud-usage/settings", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "system settings", method: http.MethodGet, path: "/api/v1/admin/settings", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "account prefix lookalike", method: http.MethodGet, path: "/api/v1/admin/accounts-export", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "proxy prefix lookalike", method: http.MethodGet, path: "/api/v1/admin/proxies-archive", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
		{name: "account dot segment traversal", method: http.MethodGet, path: "/api/v1/admin/accounts/../settings", wantStatus: http.StatusForbidden, wantCode: "ACCOUNT_ADMIN_SCOPE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			reached := false
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyUserRole), service.RoleAccountAdmin)
				c.Next()
			})
			router.Use(AccountAdminScope())
			router.Any("/*path", func(c *gin.Context) {
				reached = true
				c.Status(http.StatusNoContent)
			})

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))

			require.Equal(t, tt.wantStatus, response.Code)
			require.Equal(t, tt.wantStatus == http.StatusNoContent, reached)
			if tt.wantCode != "" {
				require.Contains(t, response.Body.String(), `"code":"`+tt.wantCode+`"`)
				if tt.wantCode == "ACCOUNT_ADMIN_DELETE_FORBIDDEN" {
					require.Contains(t, response.Body.String(), "Account administrators cannot delete accounts or IPs")
				}
			}
		})
	}
}

func TestAccountAdminScope_RoleMatrix(t *testing.T) {
	for _, tt := range []struct {
		name       string
		role       string
		setRole    bool
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "super administrator may manage account administrators", role: service.RoleAdmin, setRole: true, path: "/api/v1/admin/account-admins", wantStatus: http.StatusNoContent},
		{name: "account administrator may access scoped route", role: service.RoleAccountAdmin, setRole: true, path: "/api/v1/admin/accounts", wantStatus: http.StatusNoContent},
		{name: "ordinary user denied", role: service.RoleUser, setRole: true, path: "/api/v1/admin/accounts", wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
		{name: "unknown role denied", role: "operator", setRole: true, path: "/api/v1/admin/accounts", wantStatus: http.StatusForbidden, wantCode: "FORBIDDEN"},
		{name: "missing identity rejected", path: "/api/v1/admin/accounts", wantStatus: http.StatusUnauthorized, wantCode: "UNAUTHORIZED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()
			if tt.setRole {
				router.Use(func(c *gin.Context) {
					c.Set(string(ContextKeyUserRole), tt.role)
					c.Next()
				})
			}
			router.Use(AccountAdminScope())
			router.Any("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))

			require.Equal(t, tt.wantStatus, response.Code)
			if tt.wantCode != "" {
				require.Contains(t, response.Body.String(), `"code":"`+tt.wantCode+`"`)
			}
		})
	}
}

func TestAccountAdminScope_SuperAdminCanDeleteAccountsAndIPs(t *testing.T) {
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
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyUserRole), service.RoleAdmin)
				c.Next()
			})
			router.Use(AccountAdminScope())
			router.Any("/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))

			require.Equal(t, http.StatusNoContent, response.Code)
		})
	}
}
