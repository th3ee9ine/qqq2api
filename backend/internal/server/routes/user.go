package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterUserRoutes registers the small authenticated surface that remains
// after removing user self-service.  The function name is retained so the
// router wiring and rolling upgrades do not need a flag day: API Keys are
// global system resources, while TOTP endpoints are kept for panel operators'
// own security setup.
func RegisterUserRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	jwtAuth middleware.JWTAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	authenticated := v1.Group("")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	// 管理面全局限流：防止管理员会话高频请求打爆数据库。
	authenticated.Use(panelRateLimiter.Global())
	// 管理面变更类操作入审计（含 TOTP 启用/禁用及 step-up 验证）。
	authenticated.Use(gin.HandlerFunc(auditLog))
	{
		// Global API Key management.  There is deliberately no user filter or
		// ownership check in the service/repository layer.
		keys := authenticated.Group("/keys")
		keys.Use(middleware.AdminOnly())
		{
			keys.GET("", h.APIKey.List)
			keys.GET("/:id", h.APIKey.GetByID)
			keys.POST("", h.APIKey.Create)
			keys.PUT("/:id", h.APIKey.Update)
			keys.DELETE("/:id", h.APIKey.Delete)
		}

		// The group picker is needed by the global API Key page.  It returns all
		// active groups rather than a per-user entitlement list.
		groups := authenticated.Group("/groups")
		groups.Use(middleware.AdminOnly())
		groups.GET("/available", h.APIKey.GetAvailableGroups)

		// Keep the existing daily usage URL as a compatibility alias for the
		// global key page.  It is key-scoped, not user-scoped.
		user := authenticated.Group("/user")
		user.GET("/api-keys/:id/usage/daily", middleware.AdminOnly(), panelRateLimiter.Heavy(), h.Usage.GetMyAPIKeyDailyUsage)

		// TOTP is a panel operator security mechanism, not a user profile
		// feature.  The legacy URL is retained so existing admin settings and
		// step-up dialogs continue to work.
		totp := user.Group("/totp")
		{
			totp.GET("/status", h.Totp.GetStatus)
			totp.GET("/verification-method", h.Totp.GetVerificationMethod)
			totp.POST("/send-code", h.Totp.SendVerifyCode)
			totp.POST("/setup", h.Totp.InitiateSetup)
			totp.POST("/enable", h.Totp.Enable)
			totp.POST("/disable", h.Totp.Disable)
			totp.POST("/step-up", h.Totp.StepUp)
		}
	}
}
