package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
)

// AdminHandlers contains all admin-related HTTP handlers
type AdminHandlers struct {
	Dashboard             *admin.DashboardHandler
	Group                 *admin.GroupHandler
	ModelPricing          *admin.ModelPricingHandler
	Account               *admin.AccountHandler
	ImageStorage          *admin.ImageStorageHandler
	OAuth                 *admin.OAuthHandler
	OpenAIOAuth           *admin.OpenAIOAuthHandler
	Proxy                 *admin.ProxyHandler
	Setting               *admin.SettingHandler
	Ops                   *admin.OpsHandler
	System                *admin.SystemHandler
	Usage                 *admin.UsageHandler
	ErrorPassthrough      *admin.ErrorPassthroughHandler
	TLSFingerprintProfile *admin.TLSFingerprintProfileHandler
	APIKey                *admin.AdminAPIKeyHandler
	ScheduledTest         *admin.ScheduledTestHandler
	ContentModeration     *admin.ContentModerationHandler
	PromptAudit           *securityaudit.PromptAdminHandler
	Compliance            *admin.ComplianceHandler
	AuditLog              *admin.AuditLogHandler
}

// Handlers contains all HTTP handlers
type Handlers struct {
	Auth          *AuthHandler
	APIKey        *APIKeyHandler
	Usage         *UsageHandler
	Admin         *AdminHandlers
	Gateway       *GatewayHandler
	OpenAIGateway *OpenAIGatewayHandler
	Setting       *SettingHandler
	Totp          *TotpHandler
	AsyncImage    *AsyncImageHandler
}

// BuildInfo contains build-time information
type BuildInfo struct {
	Version   string
	BuildType string // "source" for manual builds, "release" for CI builds
}
