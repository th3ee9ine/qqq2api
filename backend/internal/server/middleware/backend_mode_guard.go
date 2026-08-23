package middleware

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// BackendModeUserGuard is kept as the compatibility name used by the route
// wiring, but the application is now single-admin: legacy user sessions must
// not reach any panel/self-service endpoint even when the old backend-mode
// setting is disabled. Must be placed AFTER JWT auth middleware.
func BackendModeUserGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := GetUserRoleFromContext(c)
		if role == service.RoleAdmin {
			c.Next()
			return
		}
		response.Forbidden(c, "User self-service is disabled. Administrator access is required.")
		c.Abort()
	}
}

func backendModeAllowsAuthPath(path string) bool {
	path = strings.ToLower(strings.TrimSpace(path))
	for _, suffix := range []string{
		"/auth/login",
		"/auth/login/2fa",
		"/auth/logout",
		"/auth/refresh",
	} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}

	return false
}

// BackendModeAuthGuard keeps the compatibility middleware name used by route
// wiring while exposing only the administrator sign-in/session endpoints.
// Registration, OAuth/passkey callbacks, and other self-service auth flows
// are intentionally unavailable in the single-admin deployment.
func BackendModeAuthGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if backendModeAllowsAuthPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		response.Forbidden(c, "Registration and self-service authentication are disabled.")
		c.Abort()
	}
}
