package routes

import (
	"time"

	"github.com/th3ee9ine/qqq2api/internal/handler"
	"github.com/th3ee9ine/qqq2api/internal/middleware"
	servermiddleware "github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// RegisterAuthRoutes registers the deliberately small authentication surface
// used by the administrator panel. Password login (plus its TOTP
// continuation), refresh, and logout remain available; registration,
// password recovery, passkeys, OAuth, promo/invitation validation, and all
// other self-service flows are no longer routable.
func RegisterAuthRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth servermiddleware.JWTAuthMiddleware,
	auditLog servermiddleware.AuditLogMiddleware,
	redisClient *redis.Client,
	settingService *service.SettingService,
	panelRateLimiter *servermiddleware.PanelRateLimiter,
) {
	rateLimiter := middleware.NewRateLimiter(redisClient)

	// Public authentication endpoints. BackendModeAuthGuard is retained as a
	// defense-in-depth check; the handler also rejects non-admin credentials.
	auth := v1.Group("/auth")
	auth.Use(servermiddleware.BackendModeAuthGuard(settingService))
	auth.Use(gin.HandlerFunc(auditLog))
	{
		auth.POST("/login", rateLimiter.LimitWithOptions("auth-login", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.Login)
		auth.POST("/login/2fa", rateLimiter.LimitWithOptions("auth-login-2fa", 20, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.Login2FA)
		auth.POST("/refresh", rateLimiter.LimitWithOptions("refresh-token", 30, time.Minute, middleware.RateLimitOptions{
			FailureMode: middleware.RateLimitFailClose,
		}), h.Auth.RefreshToken)
		// Logout is intentionally callable without a bearer token so a stale
		// client can still revoke its refresh token.
		auth.POST("/logout", h.Auth.Logout)
	}

	// Public settings (needed to render the admin login/setup shell).
	settings := v1.Group("/settings")
	settings.Use(panelRateLimiter.PublicIP())
	{
		settings.GET("/public", h.Setting.GetPublicSettings)
	}

	// Authenticated administrator session endpoints.
	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(servermiddleware.BackendModeUserGuard(settingService))
	authenticated.Use(panelRateLimiter.Global())
	{
		authenticated.GET("/auth/me", h.Auth.GetCurrentUser)
		authenticated.POST("/auth/revoke-all-sessions", h.Auth.RevokeAllSessions)
	}
}
