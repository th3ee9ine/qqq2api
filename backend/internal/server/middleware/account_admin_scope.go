package middleware

import (
	"context"
	"net/http"
	pathpkg "path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/th3ee9ine/qqq2api/internal/pkg/ctxkey"
	"github.com/th3ee9ine/qqq2api/internal/service"
)

// AccountAdminScope constrains restricted account administrators to the
// account/proxy maintenance surface. It must run after AdminAuthMiddleware.
// Super administrators keep the complete existing management API.
func AccountAdminScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := GetUserRoleFromContext(c)
		if !ok {
			AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Panel administrator identity is missing")
			return
		}

		switch role {
		case service.RoleAdmin:
			c.Next()
			return
		case service.RoleAccountAdmin:
			subject, ok := GetAuthSubjectFromContext(c)
			if !ok || subject.UserID <= 0 {
				AbortWithError(c, http.StatusUnauthorized, "UNAUTHORIZED", "Panel administrator identity is missing")
				return
			}
			requestContext := context.WithValue(c.Request.Context(), ctxkey.AccountAdminID, subject.UserID)
			c.Request = c.Request.WithContext(requestContext)

			requestPath := adminRelativePath(c.Request.URL.Path)
			if accountAdminResourceDelete(c.Request.Method, requestPath) {
				AbortWithError(c, http.StatusForbidden, "ACCOUNT_ADMIN_DELETE_FORBIDDEN", "Account administrators cannot delete accounts or IPs")
				return
			}
			if accountAdminRequestAllowed(c.Request.Method, requestPath) {
				c.Next()
				return
			}
			AbortWithError(c, http.StatusForbidden, "ACCOUNT_ADMIN_SCOPE", "Account administrators may only manage accounts and IPs")
			return
		default:
			AbortWithError(c, http.StatusForbidden, "FORBIDDEN", "Panel administrator access required")
		}
	}
}

func adminRelativePath(path string) string {
	if index := strings.Index(path, "/admin"); index >= 0 {
		path = path[index:]
	}
	// Canonicalize dot segments before applying prefix checks so a path such as
	// /admin/accounts/../settings cannot be mistaken for an account endpoint.
	cleaned := pathpkg.Clean("/" + strings.TrimPrefix(path, "/"))
	return strings.TrimSuffix(cleaned, "/")
}

// accountAdminResourceDelete identifies only deletion of the account or proxy
// resource itself. DELETE child actions such as clearing a temporary state or
// an Ollama session remain part of normal account maintenance.
func accountAdminResourceDelete(method, path string) bool {
	if method == http.MethodPost {
		return path == "/admin/accounts/batch-delete" || path == "/admin/proxies/batch-delete"
	}
	if method != http.MethodDelete {
		return false
	}

	for _, prefix := range []string{"/admin/accounts/", "/admin/proxies/"} {
		if suffix, ok := strings.CutPrefix(path, prefix); ok && suffix != "" && !strings.Contains(suffix, "/") {
			return true
		}
	}
	return false
}

func accountAdminRequestAllowed(method, path string) bool {
	// These workflows are global synchronization surfaces whose records are not
	// partitioned by account owner. Keep them super-administrator only until
	// their own persistence models carry an owner-aware authorization boundary.
	for _, restrictedPrefix := range []string{
		"/admin/accounts/sync/crs",
		"/admin/accounts/upstream-billing-rates",
		"/admin/scheduled-test-plans",
		"/admin/grok/oauth/reconcile",
		"/admin/grok/runtime-sanity",
	} {
		if path == restrictedPrefix || strings.HasPrefix(path, restrictedPrefix+"/") {
			return false
		}
	}

	// Upstream billing probes expose the provider-declared multiplier and can
	// write it back to account.rate_multiplier.  Restricted account
	// administrators use a centrally managed supply multiplier, so allowing
	// either the per-account or batch probe endpoint would leak the internal
	// billing basis and could overwrite that managed value.  The read-only
	// global settings endpoint below remains available for the account form's
	// capability discovery; all probe actions stay super-administrator only.
	if path == "/admin/accounts/upstream-billing-probe/batch" ||
		(strings.HasPrefix(path, "/admin/accounts/") && strings.HasSuffix(path, "/upstream-billing-probe")) {
		return false
	}

	// These endpoints live under /accounts for historical reasons but mutate
	// global runtime policy. Restricted operators only need their read side to
	// render account rows and editors.
	for _, readOnlyPath := range []string{
		"/admin/accounts/upstream-billing-probe/settings",
	} {
		if path == readOnlyPath {
			return method == http.MethodGet
		}
	}

	for _, prefix := range []string{
		"/admin/accounts",
		"/admin/openai",
		"/admin/grok",
		"/admin/proxies",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	// Read-only support data used by the account create/edit dialogs. These
	// endpoints do not grant permission to mutate groups or global settings.
	if method == http.MethodGet {
		if path == "/admin/groups/all" ||
			path == "/admin/tls-fingerprint-profiles" ||
			path == "/admin/settings/web-search-emulation" {
			return true
		}
	}

	return false
}
