package admin

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"

	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
)

// semverPattern 预编译 semver 格式校验正则
var semverPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// SettingHandler 系统设置处理器
type SettingHandler struct {
	codexVersionManager codexVersionManager
	settingService      *service.SettingService

	turnstileService     *service.TurnstileService
	aliyunCaptchaService *service.AliyunCaptchaService
	opsService           *service.OpsService

	totpService *service.TotpService
	userService *service.UserService
}

// NewSettingHandler 创建系统设置处理器
func NewSettingHandler(settingService *service.SettingService, turnstileService *service.TurnstileService, opsService *service.OpsService) *SettingHandler {
	return &SettingHandler{
		settingService: settingService,

		turnstileService: turnstileService,
		opsService:       opsService,
	}
}

// SetAliyunCaptchaService attaches the Aliyun captcha credential validator without
// changing the constructor signature used by existing unit tests.
func (h *SettingHandler) SetAliyunCaptchaService(aliyunCaptchaService *service.AliyunCaptchaService) {
	h.aliyunCaptchaService = aliyunCaptchaService
}

// SetStepUpDeps attaches the services backing the step-up switch preconditions
// (enable requires the acting admin to have TOTP enabled; disable is itself a
// step-up gated operation), without changing the constructor signature used by
// existing unit tests.
func (h *SettingHandler) SetStepUpDeps(totpService *service.TotpService, userService *service.UserService) {
	h.totpService = totpService
	h.userService = userService
}

// GetSettings 获取所有系统设置
// GET /api/v1/admin/settings
func (h *SettingHandler) GetSettings(c *gin.Context) {
	settings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// Check if ops monitoring is enabled (respects config.ops.enabled)
	opsEnabled := h.opsService != nil && h.opsService.IsMonitoringEnabled(c.Request.Context())

	codexHeaderDefaults := service.ResolveOpenAICodexHeaderDefaults(settings.OpenAICodexClientVersionSynced)

	payload := dto.SystemSettings{

		FrontendURL: settings.FrontendURL,

		TotpEnabled:                 settings.TotpEnabled,
		TotpEncryptionKeyConfigured: h.settingService.IsTotpEncryptionKeyConfigured(),

		SessionBindingEnabled:   settings.SessionBindingEnabled,
		StepUpEnabled:           settings.StepUpEnabled,
		AuditLogRetentionDays:   settings.AuditLogRetentionDays,
		LoginAgreementEnabled:   settings.LoginAgreementEnabled,
		LoginAgreementMode:      settings.LoginAgreementMode,
		LoginAgreementUpdatedAt: settings.LoginAgreementUpdatedAt,
		LoginAgreementDocuments: loginAgreementDocumentsToDTO(settings.LoginAgreementDocuments),

		TurnstileEnabled:                       settings.TurnstileEnabled,
		TurnstileSiteKey:                       settings.TurnstileSiteKey,
		TurnstileSecretKeyConfigured:           settings.TurnstileSecretKeyConfigured,
		TencentCaptchaEnabled:                  settings.TencentCaptchaEnabled,
		TencentCaptchaAppID:                    settings.TencentCaptchaAppID,
		TencentCaptchaAppSecretKeyConfigured:   settings.TencentCaptchaAppSecretKeyConfigured,
		TencentCaptchaCloudSecretIDConfigured:  settings.TencentCaptchaCloudSecretIDConfigured,
		TencentCaptchaCloudSecretKeyConfigured: settings.TencentCaptchaCloudSecretKeyConfigured,
		TencentCaptchaRegion:                   settings.TencentCaptchaRegion,
		AliyunCaptchaEnabled:                   settings.AliyunCaptchaEnabled,
		AliyunCaptchaAccessKeyID:               settings.AliyunCaptchaAccessKeyID,
		AliyunCaptchaAccessKeySecretConfigured: settings.AliyunCaptchaAccessKeySecretConfigured,
		AliyunCaptchaSceneID:                   settings.AliyunCaptchaSceneID,
		AliyunCaptchaPrefix:                    settings.AliyunCaptchaPrefix,
		AliyunCaptchaRegion:                    settings.AliyunCaptchaRegion,
		APIKeyACLTrustForwardedIP:              settings.APIKeyACLTrustForwardedIP,
		ForwardedClientIPHeaders:               settings.ForwardedClientIPHeaders,

		SiteName:            settings.SiteName,
		SiteLogo:            settings.SiteLogo,
		SiteSubtitle:        settings.SiteSubtitle,
		APIBaseURL:          settings.APIBaseURL,
		ContactInfo:         settings.ContactInfo,
		DocURL:              settings.DocURL,
		HomeContent:         settings.HomeContent,
		CompactHomeEnabled:  settings.CompactHomeEnabled,
		HideCcsImportButton: settings.HideCcsImportButton,

		TableDefaultPageSize: settings.TableDefaultPageSize,
		TablePageSizeOptions: settings.TablePageSizeOptions,

		CustomEndpoints: dto.ParseCustomEndpoints(settings.CustomEndpoints),

		RiskControlEnabled:          settings.RiskControlEnabled,
		CyberSessionBlockEnabled:    settings.CyberSessionBlockEnabled,
		CyberPolicyUserAllowlist:    settings.CyberPolicyUserAllowlist,
		CyberSessionBlockTTLSeconds: settings.CyberSessionBlockTTLSeconds,

		EnableModelFallback:    settings.EnableModelFallback,
		FallbackModelAnthropic: settings.FallbackModelAnthropic,
		FallbackModelOpenAI:    settings.FallbackModelOpenAI,

		OpsMonitoringEnabled:                   opsEnabled && settings.OpsMonitoringEnabled,
		OpsRealtimeMonitoringEnabled:           settings.OpsRealtimeMonitoringEnabled,
		OpsQueryModeDefault:                    settings.OpsQueryModeDefault,
		OpsMetricsIntervalSeconds:              settings.OpsMetricsIntervalSeconds,
		MinClaudeCodeVersion:                   settings.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:                   settings.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling:            settings.AllowUngroupedKeyScheduling,
		BackendModeEnabled:                     settings.BackendModeEnabled,
		OpenAITTFTMode:                         settings.OpenAITTFTMode,
		EnableFingerprintUnification:           settings.EnableFingerprintUnification,
		EnableMetadataPassthrough:              settings.EnableMetadataPassthrough,
		EnableCCHSigning:                       settings.EnableCCHSigning,
		EnableClaudeOAuthSystemPromptInjection: settings.EnableClaudeOAuthSystemPromptInjection,
		ClaudeOAuthSystemPrompt:                settings.ClaudeOAuthSystemPrompt,
		ClaudeOAuthSystemPromptBlocks:          settings.ClaudeOAuthSystemPromptBlocks,
		EnableAnthropicCacheTTL1hInjection:     settings.EnableAnthropicCacheTTL1hInjection,
		RewriteMessageCacheControl:             settings.RewriteMessageCacheControl,
		EnableClientDatelineNormalization:      settings.EnableClientDatelineNormalization,

		OpenAICodexOriginator:                  settings.OpenAICodexOriginator,
		OpenAICodexUserAgent:                   settings.OpenAICodexUserAgent,
		OpenAICodexClientVersion:               settings.OpenAICodexClientVersion,
		OpenAICodexClientVersionMode:           settings.OpenAICodexClientVersionMode,
		OpenAICodexClientVersionSynced:         settings.OpenAICodexClientVersionSynced,
		OpenAICodexVersionAutoSyncEnabled:      settings.OpenAICodexVersionAutoSyncEnabled,
		OpenAICodexOriginatorDefault:           codexHeaderDefaults.Originator,
		OpenAICodexUserAgentDefault:            codexHeaderDefaults.UserAgent,
		OpenAICodexClientVersionDefault:        codexHeaderDefaults.ClientVersion,
		EnableOpenAIAccountLocalDeviceIdentity: settings.EnableOpenAIAccountLocalDeviceIdentity,
		ClaudeCodeClientVersion:                settings.ClaudeCodeClientVersion,
		ClaudeCodeClientVersionSynced:          settings.ClaudeCodeClientVersionSynced,
		ClaudeCodeVersionAutoSyncEnabled:       settings.ClaudeCodeVersionAutoSyncEnabled,
		MinCodexVersion:                        settings.MinCodexVersion,
		MaxCodexVersion:                        settings.MaxCodexVersion,
		CodexCLIOnlyBlacklist:                  settings.CodexCLIOnlyBlacklist,
		CodexCLIOnlyWhitelist:                  settings.CodexCLIOnlyWhitelist,
		CodexCLIOnlyAllowAppServerClients:      settings.CodexCLIOnlyAllowAppServerClients,
		CodexCLIOnlyEngineFingerprintSignals:   settings.CodexCLIOnlyEngineFingerprintSignals,
		WebSearchEmulationEnabled:              settings.WebSearchEmulationEnabled,

		OpenAILowUpstreamRatePriorityEnabled:                   settings.OpenAILowUpstreamRatePriorityEnabled,
		OpenAIOAuthSchedulingRateMultiplier:                    settings.OpenAIOAuthSchedulingRateMultiplier,
		OpenAIAdvancedSchedulerEnabled:                         settings.OpenAIAdvancedSchedulerEnabled,
		OpenAIAdvancedSchedulerStickyWeightedEnabled:           settings.OpenAIAdvancedSchedulerStickyWeightedEnabled,
		OpenAIAdvancedSchedulerSubscriptionPriorityEnabled:     settings.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled,
		OpenAIAdvancedSchedulerLBTopK:                          settings.OpenAIAdvancedSchedulerLBTopK,
		OpenAIAdvancedSchedulerWeightPriority:                  settings.OpenAIAdvancedSchedulerWeightPriority,
		OpenAIAdvancedSchedulerWeightLoad:                      settings.OpenAIAdvancedSchedulerWeightLoad,
		OpenAIAdvancedSchedulerWeightQueue:                     settings.OpenAIAdvancedSchedulerWeightQueue,
		OpenAIAdvancedSchedulerWeightErrorRate:                 settings.OpenAIAdvancedSchedulerWeightErrorRate,
		OpenAIAdvancedSchedulerWeightTTFT:                      settings.OpenAIAdvancedSchedulerWeightTTFT,
		OpenAIAdvancedSchedulerWeightReset:                     settings.OpenAIAdvancedSchedulerWeightReset,
		OpenAIAdvancedSchedulerWeightQuotaHeadroom:             settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom,
		OpenAIAdvancedSchedulerWeightUpstreamCost:              settings.OpenAIAdvancedSchedulerWeightUpstreamCost,
		OpenAIAdvancedSchedulerWeightPreviousResponse:          settings.OpenAIAdvancedSchedulerWeightPreviousResponse,
		OpenAIAdvancedSchedulerWeightSessionSticky:             settings.OpenAIAdvancedSchedulerWeightSessionSticky,
		OpenAIAdvancedSchedulerEffectiveLBTopK:                 settings.OpenAIAdvancedSchedulerEffectiveLBTopK,
		OpenAIAdvancedSchedulerEffectiveWeightPriority:         settings.OpenAIAdvancedSchedulerEffectiveWeightPriority,
		OpenAIAdvancedSchedulerEffectiveWeightLoad:             settings.OpenAIAdvancedSchedulerEffectiveWeightLoad,
		OpenAIAdvancedSchedulerEffectiveWeightQueue:            settings.OpenAIAdvancedSchedulerEffectiveWeightQueue,
		OpenAIAdvancedSchedulerEffectiveWeightErrorRate:        settings.OpenAIAdvancedSchedulerEffectiveWeightErrorRate,
		OpenAIAdvancedSchedulerEffectiveWeightTTFT:             settings.OpenAIAdvancedSchedulerEffectiveWeightTTFT,
		OpenAIAdvancedSchedulerEffectiveWeightReset:            settings.OpenAIAdvancedSchedulerEffectiveWeightReset,
		OpenAIAdvancedSchedulerEffectiveWeightQuotaHeadroom:    settings.OpenAIAdvancedSchedulerEffectiveWeightQuotaHeadroom,
		OpenAIAdvancedSchedulerEffectiveWeightUpstreamCost:     settings.OpenAIAdvancedSchedulerEffectiveWeightUpstreamCost,
		OpenAIAdvancedSchedulerEffectiveWeightPreviousResponse: settings.OpenAIAdvancedSchedulerEffectiveWeightPreviousResponse,
		OpenAIAdvancedSchedulerEffectiveWeightSessionSticky:    settings.OpenAIAdvancedSchedulerEffectiveWeightSessionSticky,

		AccountQuotaNotifyEnabled: settings.AccountQuotaNotifyEnabled,
		AccountQuotaNotifyEmails:  dto.NotifyEmailEntriesFromService(settings.AccountQuotaNotifyEmails),

		ChannelMonitorEnabled:                settings.ChannelMonitorEnabled,
		ChannelMonitorMode:                   settings.ChannelMonitorMode,
		ChannelMonitorDefaultIntervalSeconds: settings.ChannelMonitorDefaultIntervalSeconds,
		ChannelMonitorHideThroughput:         settings.ChannelMonitorHideThroughput,
		ChannelMonitorShowQuota:              settings.ChannelMonitorShowQuota,

		GrokDefaultTextModel:           settings.GrokDefaultTextModel,
		GrokCrossClientModelMapEnabled: settings.GrokCrossClientModelMapEnabled,
		GrokDefaultBaseURLMode:         settings.GrokDefaultBaseURLMode,

		AvailableChannelsEnabled: settings.AvailableChannelsEnabled,

		ModelPlazaEnabled:       settings.ModelPlazaEnabled,
		ModelPlazaRequireAuth:   settings.ModelPlazaRequireAuth,
		PluginManagementEnabled: settings.PluginManagementEnabled,
		ModelPlazaDescription:   settings.ModelPlazaDescription,

		AccountSchedulingThresholds: settings.AccountSchedulingThresholds,
		AllowUserViewErrorRequests:  settings.AllowUserViewErrorRequests,
	}

	// OpenAI fast policy (stored under a dedicated setting key)
	if fastPolicy, err := h.settingService.GetOpenAIFastPolicySettings(c.Request.Context()); err != nil {
		slog.Error("openai_fast_policy_settings_get_failed", "error", err)
	} else if fastPolicy != nil {
		payload.OpenAIFastPolicySettings = openaiFastPolicySettingsToDTO(fastPolicy)
	}
	response.Success(c, systemSettingsResponseData(payload))

}

// openaiFastPolicySettingsToDTO converts service -> dto for OpenAI fast policy.
func openaiFastPolicySettingsToDTO(s *service.OpenAIFastPolicySettings) *dto.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]dto.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = dto.OpenAIFastPolicyRule(r)
	}
	return &dto.OpenAIFastPolicySettings{Rules: rules}
}

// openaiFastPolicySettingsFromDTO converts dto -> service for OpenAI fast policy.
//
// 规范化 ServiceTier：在 DTO 进入 service 层之前统一把空字符串归一为
// service.OpenAIFastTierAny ("all")，避免管理员保存时空串与 "all" 同时
// 表达"匹配任意 tier"造成数据库取值的二义性。其它非空值原样透传，由
// service.SetOpenAIFastPolicySettings 负责合法值校验。
func openaiFastPolicySettingsFromDTO(s *dto.OpenAIFastPolicySettings) *service.OpenAIFastPolicySettings {
	if s == nil {
		return nil
	}
	rules := make([]service.OpenAIFastPolicyRule, len(s.Rules))
	for i, r := range s.Rules {
		rules[i] = service.OpenAIFastPolicyRule(r)
		tier := strings.ToLower(strings.TrimSpace(rules[i].ServiceTier))
		if tier == "" {
			tier = service.OpenAIFastTierAny
		}
		rules[i].ServiceTier = tier
	}
	return &service.OpenAIFastPolicySettings{Rules: rules}
}

func loginAgreementDocumentsToDTO(items []service.LoginAgreementDocument) []dto.LoginAgreementDocument {
	result := make([]dto.LoginAgreementDocument, 0, len(items))
	for _, item := range items {
		result = append(result, dto.LoginAgreementDocument{
			ID:        item.ID,
			Title:     item.Title,
			ContentMD: item.ContentMD,
		})
	}
	return result
}

func loginAgreementDocumentsToService(items []dto.LoginAgreementDocument) []service.LoginAgreementDocument {
	result := make([]service.LoginAgreementDocument, 0, len(items))
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		content := strings.TrimSpace(item.ContentMD)
		if title == "" && content == "" {
			continue
		}
		result = append(result, service.LoginAgreementDocument{
			ID:        strings.TrimSpace(item.ID),
			Title:     title,
			ContentMD: content,
		})
	}
	return result
}

func systemSettingsResponseData(settings dto.SystemSettings) map[string]any {
	data := make(map[string]any)
	raw, err := json.Marshal(settings)
	if err == nil {
		_ = json.Unmarshal(raw, &data)
	}

	return data
}
