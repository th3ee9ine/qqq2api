package handler

import (
	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/handler/admin"
	"github.com/th3ee9ine/qqq2api/internal/securityaudit"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/google/wire"
)

// ProvideOpenAIOAuthHandler is the fixed-arity Wire provider for the OpenAI
// OAuth admin handler.  NewOpenAIOAuthHandler intentionally accepts an
// optional cleanup runner for source compatibility with integrations that
// construct it directly; Wire cannot resolve a variadic dependency, so the
// provider graph uses this four-argument wrapper and injects the cleanup
// worker through SetSessionCleanupService in ProvideAdminHandlersWithSessionCleanup.
func ProvideOpenAIOAuthHandler(
	openaiOAuthService *service.OpenAIOAuthService,
	adminService service.AdminService,
	quotaService *service.OpenAIQuotaService,
	rateLimitService *service.RateLimitService,
) *admin.OpenAIOAuthHandler {
	return admin.NewOpenAIOAuthHandler(openaiOAuthService, adminService, quotaService, rateLimitService)
}

// ProvideAdminHandlers creates the AdminHandlers struct.  The optional cleanup
// argument keeps the historical constructor source-compatible for integrations
// that build handlers directly; the Wire provider below uses the explicit
// WithSessionCleanup variant so dependency injection remains deterministic.
func ProvideAdminHandlers(
	dashboardHandler *admin.DashboardHandler,
	groupHandler *admin.GroupHandler,
	modelPricingHandler *admin.ModelPricingHandler,
	accountHandler *admin.AccountHandler,
	accountAdminHandler *admin.AccountAdminHandler,
	imageStorageHandler *admin.ImageStorageHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	usageHandler *admin.UsageHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	pluginHandler *admin.PluginHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	promptAuditHandler *securityaudit.PromptAdminHandler,
	auditLogHandler *admin.AuditLogHandler,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	ollamaCloudUsage *service.OllamaCloudUsageService,
	optionalSessionCleanup ...*service.OpenAISessionCleanupService,
) *AdminHandlers {
	var openAISessionCleanup *service.OpenAISessionCleanupService
	if len(optionalSessionCleanup) > 0 {
		openAISessionCleanup = optionalSessionCleanup[0]
	}
	return provideAdminHandlersWithSessionCleanup(
		dashboardHandler,
		groupHandler,
		modelPricingHandler,
		accountHandler,
		accountAdminHandler,
		imageStorageHandler,
		oauthHandler,
		openaiOAuthHandler,
		proxyHandler,
		settingHandler,
		opsHandler,
		systemHandler,
		usageHandler,
		errorPassthroughHandler,
		tlsFingerprintProfileHandler,
		pluginHandler,
		apiKeyHandler,
		scheduledTestHandler,
		contentModerationHandler,
		promptAuditHandler,
		auditLogHandler,
		upstreamBillingProbe,
		ollamaCloudUsage,
		openAISessionCleanup,
	)
}

// ProvideAdminHandlersWithSessionCleanup is the fixed-arity provider used by
// Wire.  Keep it separate from the compatibility wrapper above because Wire
// does not infer optional variadic dependencies.
func ProvideAdminHandlersWithSessionCleanup(
	dashboardHandler *admin.DashboardHandler,
	groupHandler *admin.GroupHandler,
	modelPricingHandler *admin.ModelPricingHandler,
	accountHandler *admin.AccountHandler,
	accountAdminHandler *admin.AccountAdminHandler,
	imageStorageHandler *admin.ImageStorageHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	usageHandler *admin.UsageHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	pluginHandler *admin.PluginHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	promptAuditHandler *securityaudit.PromptAdminHandler,
	auditLogHandler *admin.AuditLogHandler,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	ollamaCloudUsage *service.OllamaCloudUsageService,
	openAISessionCleanup *service.OpenAISessionCleanupService,
) *AdminHandlers {
	return provideAdminHandlersWithSessionCleanup(
		dashboardHandler,
		groupHandler,
		modelPricingHandler,
		accountHandler,
		accountAdminHandler,
		imageStorageHandler,
		oauthHandler,
		openaiOAuthHandler,
		proxyHandler,
		settingHandler,
		opsHandler,
		systemHandler,
		usageHandler,
		errorPassthroughHandler,
		tlsFingerprintProfileHandler,
		pluginHandler,
		apiKeyHandler,
		scheduledTestHandler,
		contentModerationHandler,
		promptAuditHandler,
		auditLogHandler,
		upstreamBillingProbe,
		ollamaCloudUsage,
		openAISessionCleanup,
	)
}

func provideAdminHandlersWithSessionCleanup(
	dashboardHandler *admin.DashboardHandler,
	groupHandler *admin.GroupHandler,
	modelPricingHandler *admin.ModelPricingHandler,
	accountHandler *admin.AccountHandler,
	accountAdminHandler *admin.AccountAdminHandler,
	imageStorageHandler *admin.ImageStorageHandler,
	oauthHandler *admin.OAuthHandler,
	openaiOAuthHandler *admin.OpenAIOAuthHandler,
	proxyHandler *admin.ProxyHandler,
	settingHandler *admin.SettingHandler,
	opsHandler *admin.OpsHandler,
	systemHandler *admin.SystemHandler,
	usageHandler *admin.UsageHandler,
	errorPassthroughHandler *admin.ErrorPassthroughHandler,
	tlsFingerprintProfileHandler *admin.TLSFingerprintProfileHandler,
	pluginHandler *admin.PluginHandler,
	apiKeyHandler *admin.AdminAPIKeyHandler,
	scheduledTestHandler *admin.ScheduledTestHandler,
	contentModerationHandler *admin.ContentModerationHandler,
	promptAuditHandler *securityaudit.PromptAdminHandler,
	auditLogHandler *admin.AuditLogHandler,
	upstreamBillingProbe *service.UpstreamBillingProbeService,
	ollamaCloudUsage *service.OllamaCloudUsageService,
	openAISessionCleanup *service.OpenAISessionCleanupService,
) *AdminHandlers {
	// Keep the provider usable in reduced test/development graphs where one of
	// the optional handlers is deliberately omitted.  The generated production
	// graph supplies all three values, while these guards avoid a nil-pointer
	// panic for compatibility callers that only need the remaining handlers.
	if accountHandler != nil {
		accountHandler.SetUpstreamBillingProbeService(upstreamBillingProbe)
		accountHandler.SetOllamaCloudUsageService(ollamaCloudUsage)
	}
	if openaiOAuthHandler != nil {
		openaiOAuthHandler.SetSessionCleanupService(openAISessionCleanup)
	}
	return &AdminHandlers{
		Dashboard:             dashboardHandler,
		Group:                 groupHandler,
		ModelPricing:          modelPricingHandler,
		Account:               accountHandler,
		AccountAdmin:          accountAdminHandler,
		ImageStorage:          imageStorageHandler,
		OAuth:                 oauthHandler,
		OpenAIOAuth:           openaiOAuthHandler,
		Proxy:                 proxyHandler,
		Setting:               settingHandler,
		Ops:                   opsHandler,
		System:                systemHandler,
		Usage:                 usageHandler,
		ErrorPassthrough:      errorPassthroughHandler,
		TLSFingerprintProfile: tlsFingerprintProfileHandler,
		Plugin:                pluginHandler,
		APIKey:                apiKeyHandler,
		ScheduledTest:         scheduledTestHandler,
		ContentModeration:     contentModerationHandler,
		PromptAudit:           promptAuditHandler,
		AuditLog:              auditLogHandler,
	}
}

func ProvideGatewayHandler(
	gatewayService *service.GatewayService,
	openAIGatewayService *service.OpenAIGatewayService,
	userService *service.UserService,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	usageService *service.UsageService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	userMsgQueueService *service.UserMessageQueueService,
	cfg *config.Config,
	settingService *service.SettingService,
	coordinator *securityaudit.Coordinator,
) *GatewayHandler {
	h := NewGatewayHandler(gatewayService, openAIGatewayService, nil, nil,
		userService, concurrencyService, billingCacheService, usageService, apiKeyService, usageRecordWorkerPool,
		errorPassthroughService, contentModerationService, userMsgQueueService, cfg, settingService)
	h.securityAuditCoordinator = coordinator
	return h
}

func ProvideOpenAIGatewayHandler(
	gatewayService *service.OpenAIGatewayService,
	pluginManager *service.PluginManager,
	concurrencyService *service.ConcurrencyService,
	billingCacheService *service.BillingCacheService,
	apiKeyService *service.APIKeyService,
	usageRecordWorkerPool *service.UsageRecordWorkerPool,
	errorPassthroughService *service.ErrorPassthroughService,
	contentModerationService *service.ContentModerationService,
	opsService *service.OpsService,
	cfg *config.Config,
	coordinator *securityaudit.Coordinator,
) *OpenAIGatewayHandler {
	gatewayService.SetPluginManager(pluginManager)
	h := NewOpenAIGatewayHandler(gatewayService, concurrencyService, billingCacheService, apiKeyService,
		usageRecordWorkerPool, errorPassthroughService, contentModerationService, opsService, cfg)
	h.securityAuditCoordinator = coordinator
	return h
}

// ProvideAuthHandler builds the administrator-only login handler. Optional
// registration, redemption, and user-profile services stay detached so their
// removed routes cannot keep those feature graphs alive at runtime.
func ProvideAuthHandler(
	cfg *config.Config,
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	totpService *service.TotpService,
) *AuthHandler {
	return NewAuthHandler(cfg, authService, userService, settingService, nil, nil, totpService, nil)
}

// ProvideSystemHandler creates admin.SystemHandler with UpdateService
func ProvideSystemHandler(updateService *service.UpdateService, lockService *service.SystemOperationLockService) *admin.SystemHandler {
	return admin.NewSystemHandler(updateService, lockService)
}

// ProvideSettingHandler creates SettingHandler with version from BuildInfo
func ProvideSettingHandler(settingService *service.SettingService, buildInfo BuildInfo, notificationEmailService *service.NotificationEmailService) *SettingHandler {
	h := NewSettingHandler(settingService, buildInfo.Version)
	h.SetNotificationEmailService(notificationEmailService)
	return h
}

// ProvideAdminSettingHandler creates admin.SettingHandler with notification template APIs.
func ProvideAdminSettingHandler(settingService *service.SettingService, emailService *service.EmailService, turnstileService *service.TurnstileService, aliyunCaptchaService *service.AliyunCaptchaService, opsService *service.OpsService, notificationEmailService *service.NotificationEmailService, totpService *service.TotpService, userService *service.UserService) *admin.SettingHandler {
	h := admin.NewSettingHandler(settingService, emailService, turnstileService, opsService, nil, nil, nil)
	h.SetNotificationEmailService(notificationEmailService)
	h.SetAliyunCaptchaService(aliyunCaptchaService)
	h.SetStepUpDeps(totpService, userService)
	return h
}

// ProvideHandlers creates the Handlers struct
func ProvideHandlers(
	authHandler *AuthHandler,
	apiKeyHandler *APIKeyHandler,
	usageHandler *UsageHandler,
	adminHandlers *AdminHandlers,
	gatewayHandler *GatewayHandler,
	openaiGatewayHandler *OpenAIGatewayHandler,
	settingHandler *SettingHandler,
	totpHandler *TotpHandler,
	asyncImageHandler *AsyncImageHandler,
	_ *service.IdempotencyCoordinator,
	_ *service.IdempotencyCleanupService,
	_ *service.OpenAIQuotaAutoResetService,
) *Handlers {
	return &Handlers{
		Auth:          authHandler,
		APIKey:        apiKeyHandler,
		Usage:         usageHandler,
		Admin:         adminHandlers,
		Gateway:       gatewayHandler,
		OpenAIGateway: openaiGatewayHandler,
		Setting:       settingHandler,
		Totp:          totpHandler,
		AsyncImage:    asyncImageHandler,
	}
}

// ProviderSet is the Wire provider set for all handlers
var ProviderSet = wire.NewSet(
	// Top-level handlers
	ProvideAuthHandler,
	NewAPIKeyHandler,
	NewUsageHandler,
	ProvideGatewayHandler,
	ProvideOpenAIGatewayHandler,
	NewTotpHandler,
	ProvideSettingHandler,
	NewAsyncImageHandler,

	// Admin handlers
	admin.NewDashboardHandler,
	admin.NewGroupHandler,
	admin.NewModelPricingHandler,
	admin.ProvideAccountHandler,
	admin.NewAccountAdminHandler,
	admin.NewImageStorageHandler,
	admin.NewOAuthHandler,
	ProvideOpenAIOAuthHandler,
	admin.NewProxyHandler,
	ProvideAdminSettingHandler,
	admin.NewOpsHandler,
	ProvideSystemHandler,
	admin.NewUsageHandler,
	admin.NewErrorPassthroughHandler,
	admin.NewTLSFingerprintProfileHandler,
	admin.NewPluginHandler,
	admin.NewAdminAPIKeyHandler,
	admin.NewScheduledTestHandler,
	admin.NewContentModerationHandler,
	admin.NewAuditLogHandler,

	// AdminHandlers and Handlers constructors
	ProvideAdminHandlersWithSessionCleanup,
	ProvideHandlers,
)
