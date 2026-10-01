package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/th3ee9ine/qqq2api/internal/config"
	infraerrors "github.com/th3ee9ine/qqq2api/internal/pkg/errors"
	"github.com/th3ee9ine/qqq2api/internal/pkg/xai"
)

// OmittedSettingKeys marks setting keys the caller's payload never carried.
// SystemSettings is a plain struct, so a field the caller omitted arrives as a
// zero value and is indistinguishable from a deliberate clear. Listing the key
// here drops it from the write, leaving the stored value in place.
//
// A nil or empty set keeps whole-document semantics: every key is written.
type OmittedSettingKeys map[string]struct{}

func (o OmittedSettingKeys) dropFrom(updates map[string]string) {
	for key := range o {
		delete(updates, key)
	}
}

// UpdateSettings 更新系统设置
func (s *SettingService) UpdateSettings(ctx context.Context, settings *SystemSettings) error {
	return s.UpdateSettingsOmitting(ctx, settings, nil)
}

// UpdateSettingsOmitting persists system settings, leaving the keys in omitted
// at their stored value.
func (s *SettingService) UpdateSettingsOmitting(ctx context.Context, settings *SystemSettings, omitted OmittedSettingKeys) error {
	updates, err := s.buildSystemSettingsUpdates(ctx, settings)
	if err != nil {
		return err
	}
	omitted.dropFrom(updates)

	revision, err := s.persistSystemSettingsAndRefresh(ctx, updates, settings, omitted)
	if err != nil {
		return err
	}
	s.notifySettingsUpdated(revision)
	return nil
}

// persistSystemSettingsAndRefresh serializes a repository commit through the
// publication of every corresponding runtime cache. Keeping both operations in
// one helper prevents either update entrypoint from accidentally narrowing the
// lock to SetMultiple and reintroducing an older-write/newer-cache inversion.
func (s *SettingService) persistSystemSettingsAndRefresh(
	ctx context.Context,
	updates map[string]string,
	settings *SystemSettings,
	omitted OmittedSettingKeys,
) (uint64, error) {
	s.settingsUpdateMu.Lock()
	defer s.settingsUpdateMu.Unlock()
	// Validate the actual merged pair under the write lock, not the handler's
	// potentially stale settings snapshot. Omitted fields stay in storage.
	if err := s.validateOpenAICodexVersionUpdates(ctx, updates); err != nil {
		return 0, err
	}
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return 0, err
	}
	s.refreshCachedSettingsAfterWrite(ctx, settings, omitted)
	s.settingsUpdateRevision++
	return s.settingsUpdateRevision, nil
}

// refreshCachedSettingsAfterWrite keeps the in-process caches in step with the
// write that just landed. A partial payload carries zero values for the fields
// it omitted, so in that case the caches are rebuilt from storage rather than
// from the request struct.
func (s *SettingService) refreshCachedSettingsAfterWrite(ctx context.Context, settings *SystemSettings, omitted OmittedSettingKeys) {
	if len(omitted) == 0 {
		s.refreshCachedSettings(settings)
		return
	}
	stored, err := s.GetAllSettings(ctx)
	if err != nil {
		slog.Warn("refresh cached settings after partial update failed", "error", err)
		return
	}
	s.refreshCachedSettings(stored)
}

func (s *SettingService) buildSystemSettingsUpdates(ctx context.Context, settings *SystemSettings) (map[string]string, error) {

	normalizedForwardedClientIPHeaders, err := config.NormalizeForwardedClientIPHeaders(settings.ForwardedClientIPHeaders)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_FORWARDED_CLIENT_IP_HEADERS", err.Error())
	}
	settings.ForwardedClientIPHeaders = normalizedForwardedClientIPHeaders

	if err := s.normalizeOpenAIAdvancedSchedulerOverrides(settings); err != nil {
		return nil, err
	}

	updates := make(map[string]string)

	updates[SettingKeyFrontendURL] = settings.FrontendURL

	updates[SettingKeyTotpEnabled] = strconv.FormatBool(settings.TotpEnabled)

	updates[SettingKeySessionBindingEnabled] = strconv.FormatBool(settings.SessionBindingEnabled)
	updates[SettingKeyStepUpEnabled] = strconv.FormatBool(settings.StepUpEnabled)
	updates[SettingKeyAuditLogRetentionDays] = strconv.Itoa(settings.AuditLogRetentionDays)
	settings.LoginAgreementMode = normalizeLoginAgreementMode(settings.LoginAgreementMode)
	settings.LoginAgreementUpdatedAt = strings.TrimSpace(settings.LoginAgreementUpdatedAt)
	if settings.LoginAgreementUpdatedAt == "" {
		settings.LoginAgreementUpdatedAt = defaultLoginAgreementDate
	}
	loginAgreementDocumentsJSON, err := marshalLoginAgreementDocuments(settings.LoginAgreementDocuments)
	if err != nil {
		return nil, err
	}
	updates[SettingKeyLoginAgreementEnabled] = strconv.FormatBool(settings.LoginAgreementEnabled)
	updates[SettingKeyLoginAgreementMode] = settings.LoginAgreementMode
	updates[SettingKeyLoginAgreementUpdatedAt] = settings.LoginAgreementUpdatedAt
	updates[SettingKeyLoginAgreementDocuments] = loginAgreementDocumentsJSON

	// Cloudflare Turnstile 设置（只有非空才更新密钥）
	updates[SettingKeyTurnstileEnabled] = strconv.FormatBool(settings.TurnstileEnabled)
	updates[SettingKeyTurnstileSiteKey] = settings.TurnstileSiteKey
	if settings.TurnstileSecretKey != "" {
		updates[SettingKeyTurnstileSecretKey] = settings.TurnstileSecretKey
	}

	updates[SettingKeyTencentCaptchaEnabled] = strconv.FormatBool(settings.TencentCaptchaEnabled)
	updates[SettingKeyTencentCaptchaAppID] = settings.TencentCaptchaAppID
	if settings.TencentCaptchaAppSecretKey != "" {
		updates[SettingKeyTencentCaptchaAppSecretKey] = settings.TencentCaptchaAppSecretKey
	}
	if settings.TencentCaptchaCloudSecretID != "" {
		updates[SettingKeyTencentCaptchaCloudSecretID] = settings.TencentCaptchaCloudSecretID
	}
	if settings.TencentCaptchaCloudSecretKey != "" {
		updates[SettingKeyTencentCaptchaCloudSecretKey] = settings.TencentCaptchaCloudSecretKey
	}
	updates[SettingKeyTencentCaptchaRegion] = normalizeTencentCaptchaRegion(settings.TencentCaptchaRegion)
	// 阿里云验证码 2.0 设置（只有非空才更新密钥）
	updates[SettingKeyAliyunCaptchaEnabled] = strconv.FormatBool(settings.AliyunCaptchaEnabled)
	updates[SettingKeyAliyunCaptchaAccessKeyID] = settings.AliyunCaptchaAccessKeyID
	if settings.AliyunCaptchaAccessKeySecret != "" {
		updates[SettingKeyAliyunCaptchaAccessKeySecret] = settings.AliyunCaptchaAccessKeySecret
	}
	updates[SettingKeyAliyunCaptchaSceneID] = settings.AliyunCaptchaSceneID
	updates[SettingKeyAliyunCaptchaPrefix] = settings.AliyunCaptchaPrefix
	updates[SettingKeyAliyunCaptchaRegion] = normalizeAliyunCaptchaRegion(settings.AliyunCaptchaRegion)
	updates[SettingKeyAPIKeyACLTrustForwardedIP] = strconv.FormatBool(settings.APIKeyACLTrustForwardedIP)
	forwardedClientIPHeadersJSON, err := json.Marshal(settings.ForwardedClientIPHeaders)
	if err != nil {
		return nil, fmt.Errorf("marshal forwarded client IP headers: %w", err)
	}
	updates[SettingKeyForwardedClientIPHeaders] = string(forwardedClientIPHeadersJSON)

	// OEM设置
	updates[SettingKeySiteName] = settings.SiteName
	updates[SettingKeySiteLogo] = settings.SiteLogo
	updates[SettingKeySiteSubtitle] = settings.SiteSubtitle
	updates[SettingKeyAPIBaseURL] = settings.APIBaseURL
	updates[SettingKeyContactInfo] = settings.ContactInfo
	updates[SettingKeyDocURL] = settings.DocURL
	updates[SettingKeyHomeContent] = settings.HomeContent
	updates[SettingKeyCompactHomeEnabled] = strconv.FormatBool(settings.CompactHomeEnabled)
	updates[SettingKeyHideCcsImportButton] = strconv.FormatBool(settings.HideCcsImportButton)

	tableDefaultPageSize, tablePageSizeOptions := normalizeTablePreferences(
		settings.TableDefaultPageSize,
		settings.TablePageSizeOptions,
	)
	updates[SettingKeyTableDefaultPageSize] = strconv.Itoa(tableDefaultPageSize)
	tablePageSizeOptionsJSON, err := json.Marshal(tablePageSizeOptions)
	if err != nil {
		return nil, fmt.Errorf("marshal table page size options: %w", err)
	}
	updates[SettingKeyTablePageSizeOptions] = string(tablePageSizeOptionsJSON)

	updates[SettingKeyCustomEndpoints] = settings.CustomEndpoints

	// Model fallback configuration
	updates[SettingKeyEnableModelFallback] = strconv.FormatBool(settings.EnableModelFallback)
	updates[SettingKeyFallbackModelAnthropic] = settings.FallbackModelAnthropic
	updates[SettingKeyFallbackModelOpenAI] = settings.FallbackModelOpenAI

	// Ops monitoring (vNext)
	updates[SettingKeyOpsMonitoringEnabled] = strconv.FormatBool(settings.OpsMonitoringEnabled)
	updates[SettingKeyOpsRealtimeMonitoringEnabled] = strconv.FormatBool(settings.OpsRealtimeMonitoringEnabled)
	updates[SettingKeyOpsQueryModeDefault] = string(ParseOpsQueryMode(settings.OpsQueryModeDefault))
	if settings.OpsMetricsIntervalSeconds > 0 {
		updates[SettingKeyOpsMetricsIntervalSeconds] = strconv.Itoa(settings.OpsMetricsIntervalSeconds)
	}

	// Channel monitor feature switch
	updates[SettingKeyChannelMonitorEnabled] = strconv.FormatBool(settings.ChannelMonitorEnabled)
	updates[SettingKeyChannelMonitorMode] = normalizeChannelMonitorMode(settings.ChannelMonitorMode)
	if v := clampChannelMonitorInterval(settings.ChannelMonitorDefaultIntervalSeconds); v > 0 {
		updates[SettingKeyChannelMonitorDefaultIntervalSeconds] = strconv.Itoa(v)
	}
	updates[SettingKeyChannelMonitorHideThroughput] = strconv.FormatBool(settings.ChannelMonitorHideThroughput)
	updates[SettingKeyChannelMonitorShowQuota] = strconv.FormatBool(settings.ChannelMonitorShowQuota)

	// Grok model mapping and upstream endpoint defaults.
	if model := strings.TrimSpace(settings.GrokDefaultTextModel); model != "" {
		updates[SettingKeyGrokDefaultTextModel] = model
	} else {
		updates[SettingKeyGrokDefaultTextModel] = xai.DefaultTextModel
	}
	updates[SettingKeyGrokCrossClientModelMapEnabled] = strconv.FormatBool(settings.GrokCrossClientModelMapEnabled)
	updates[SettingKeyGrokDefaultBaseURLMode] = normalizeGrokDefaultBaseURLMode(settings.GrokDefaultBaseURLMode)

	// Available channels feature switch
	updates[SettingKeyAvailableChannelsEnabled] = strconv.FormatBool(settings.AvailableChannelsEnabled)

	// Model plaza feature switches + description
	updates[SettingKeyModelPlazaEnabled] = strconv.FormatBool(settings.ModelPlazaEnabled)
	updates[SettingKeyModelPlazaRequireAuth] = strconv.FormatBool(settings.ModelPlazaRequireAuth)
	updates[SettingKeyModelPlazaDescription] = settings.ModelPlazaDescription
	updates[SettingKeyPluginManagementEnabled] = strconv.FormatBool(settings.PluginManagementEnabled)

	// 风控中心功能开关
	updates[SettingKeyRiskControlEnabled] = strconv.FormatBool(settings.RiskControlEnabled)

	// cyber 会话屏蔽开关 + TTL
	updates[SettingKeyCyberSessionBlockEnabled] = strconv.FormatBool(settings.CyberSessionBlockEnabled)
	if _, err := ParseCyberPolicyUserAllowlist(settings.CyberPolicyUserAllowlist); err != nil {
		return nil, err
	}
	updates[SettingKeyCyberPolicyUserAllowlist] = settings.CyberPolicyUserAllowlist
	if settings.CyberSessionBlockTTLSeconds > 0 {
		updates[SettingKeyCyberSessionBlockTTLSeconds] = strconv.Itoa(settings.CyberSessionBlockTTLSeconds)
	}

	// Claude Code version check
	updates[SettingKeyMinClaudeCodeVersion] = settings.MinClaudeCodeVersion
	updates[SettingKeyMaxClaudeCodeVersion] = settings.MaxClaudeCodeVersion

	// 分组隔离
	updates[SettingKeyAllowUngroupedKeyScheduling] = strconv.FormatBool(settings.AllowUngroupedKeyScheduling)

	// Backend Mode
	updates[SettingKeyBackendModeEnabled] = strconv.FormatBool(settings.BackendModeEnabled)

	// Gateway forwarding behavior
	mode := normalizeOpenAITTFTMode(settings.OpenAITTFTMode)
	if strings.TrimSpace(settings.OpenAITTFTMode) != "" && strings.ToLower(strings.TrimSpace(settings.OpenAITTFTMode)) != OpenAITTFTModeSemantic && strings.ToLower(strings.TrimSpace(settings.OpenAITTFTMode)) != OpenAITTFTModeVisible {
		return nil, fmt.Errorf("%s must be one of: %s/%s", SettingKeyOpenAITTFTMode, OpenAITTFTModeSemantic, OpenAITTFTModeVisible)
	}
	updates[SettingKeyOpenAITTFTMode] = mode
	updates[SettingKeyEnableFingerprintUnification] = strconv.FormatBool(settings.EnableFingerprintUnification)
	updates[SettingKeyEnableMetadataPassthrough] = strconv.FormatBool(settings.EnableMetadataPassthrough)
	updates[SettingKeyEnableCCHSigning] = strconv.FormatBool(settings.EnableCCHSigning)
	updates[SettingKeyEnableClaudeOAuthSystemPromptInjection] = strconv.FormatBool(settings.EnableClaudeOAuthSystemPromptInjection)
	updates[SettingKeyClaudeOAuthSystemPrompt] = settings.ClaudeOAuthSystemPrompt
	if err := ValidateClaudeOAuthSystemPromptBlocksConfig(settings.ClaudeOAuthSystemPromptBlocks); err != nil {
		return nil, err
	}
	updates[SettingKeyClaudeOAuthSystemPromptBlocks] = settings.ClaudeOAuthSystemPromptBlocks
	updates[SettingKeyEnableAnthropicCacheTTL1hInjection] = strconv.FormatBool(settings.EnableAnthropicCacheTTL1hInjection)
	updates[SettingKeyRewriteMessageCacheControl] = strconv.FormatBool(settings.RewriteMessageCacheControl)
	updates[SettingKeyEnableClientDatelineNormalization] = strconv.FormatBool(settings.EnableClientDatelineNormalization)
	rawOriginator := settings.OpenAICodexOriginator
	originator := NormalizeCodexOriginatorHeader(rawOriginator)
	trimmedOriginator := strings.Trim(rawOriginator, " ")
	if trimmedOriginator != "" && (originator == "" || originator != trimmedOriginator) {
		return nil, fmt.Errorf("%s must be printable ASCII, contain no slash, and be at most 64 characters", SettingKeyOpenAICodexOriginator)
	}
	updates[SettingKeyOpenAICodexOriginator] = originator
	rawCodexUA := settings.OpenAICodexUserAgent
	if !IsValidCodexUserAgentHeader(rawCodexUA) {
		return nil, fmt.Errorf("%s must be empty or a printable ASCII client/version value at most 512 characters", SettingKeyOpenAICodexUserAgent)
	}
	codexUA := NormalizeCodexUserAgentHeader(rawCodexUA)
	updates[SettingKeyOpenAICodexUserAgent] = codexUA
	versionMode := NormalizeOpenAICodexClientVersionMode(settings.OpenAICodexClientVersionMode)
	if versionMode == "" {
		return nil, infraerrors.BadRequest("INVALID_OPENAI_CODEX_CLIENT_VERSION_MODE", "openai_codex_client_version_mode must be auto or pinned")
	}
	version := normalizeStableCodexClientVersion(settings.OpenAICodexClientVersion)
	if strings.TrimSpace(settings.OpenAICodexClientVersion) != "" && version == "" {
		return nil, infraerrors.BadRequest("INVALID_OPENAI_CODEX_CLIENT_VERSION", "openai_codex_client_version must be empty or a stable X.Y.Z version")
	}
	updates[SettingKeyOpenAICodexClientVersion] = version
	updates[SettingKeyOpenAICodexClientVersionMode] = versionMode
	updates[SettingKeyOpenAICodexVersionAutoSyncEnabled] = strconv.FormatBool(settings.OpenAICodexVersionAutoSyncEnabled)
	updates[SettingKeyEnableOpenAIAccountLocalDeviceIdentity] = strconv.FormatBool(settings.EnableOpenAIAccountLocalDeviceIdentity)
	// SettingKeyOpenAICodexClientVersionSynced 由自动同步任务独占写入，此处不得覆盖，
	// 否则面板保存会把同步结果清空。
	updates[SettingKeyClaudeCodeClientVersion] = NormalizeClaudeCodeClientVersion(settings.ClaudeCodeClientVersion)
	updates[SettingKeyClaudeCodeVersionAutoSyncEnabled] = strconv.FormatBool(settings.ClaudeCodeVersionAutoSyncEnabled)
	// SettingKeyClaudeCodeClientVersionSynced 由自动同步任务独占写入，此处不得覆盖，
	// 否则面板保存会把同步结果清空。
	// codex_cli_only 加固
	updates[SettingKeyMinCodexVersion] = strings.TrimSpace(settings.MinCodexVersion)
	updates[SettingKeyMaxCodexVersion] = strings.TrimSpace(settings.MaxCodexVersion)
	updates[SettingKeyCodexCLIOnlyBlacklist] = strings.TrimSpace(settings.CodexCLIOnlyBlacklist)
	updates[SettingKeyCodexCLIOnlyWhitelist] = strings.TrimSpace(settings.CodexCLIOnlyWhitelist)
	updates[SettingKeyCodexCLIOnlyAllowAppServerClients] = strconv.FormatBool(settings.CodexCLIOnlyAllowAppServerClients)
	updates[SettingKeyCodexCLIOnlyEngineFingerprintSignals] = strings.TrimSpace(settings.CodexCLIOnlyEngineFingerprintSignals)

	updates[SettingKeyOpenAILowUpstreamRatePriorityEnabled] = strconv.FormatBool(settings.OpenAILowUpstreamRatePriorityEnabled)
	updates[SettingKeyOpenAIOAuthSchedulingRateMultiplier] = strconv.FormatFloat(settings.OpenAIOAuthSchedulingRateMultiplier, 'f', -1, 64)
	updates[openAIAdvancedSchedulerSettingKey] = strconv.FormatBool(settings.OpenAIAdvancedSchedulerEnabled)
	updates[SettingKeyOpenAIAdvancedSchedulerStickyWeightedEnabled] = strconv.FormatBool(settings.OpenAIAdvancedSchedulerStickyWeightedEnabled)
	updates[SettingKeyOpenAIAdvancedSchedulerSubscriptionPriorityEnabled] = strconv.FormatBool(settings.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled)
	updates[SettingKeyOpenAIAdvancedSchedulerLBTopK] = settings.OpenAIAdvancedSchedulerLBTopK
	updates[SettingKeyOpenAIAdvancedSchedulerWeightPriority] = settings.OpenAIAdvancedSchedulerWeightPriority
	updates[SettingKeyOpenAIAdvancedSchedulerWeightLoad] = settings.OpenAIAdvancedSchedulerWeightLoad
	updates[SettingKeyOpenAIAdvancedSchedulerWeightQueue] = settings.OpenAIAdvancedSchedulerWeightQueue
	updates[SettingKeyOpenAIAdvancedSchedulerWeightErrorRate] = settings.OpenAIAdvancedSchedulerWeightErrorRate
	updates[SettingKeyOpenAIAdvancedSchedulerWeightTTFT] = settings.OpenAIAdvancedSchedulerWeightTTFT
	updates[SettingKeyOpenAIAdvancedSchedulerWeightReset] = settings.OpenAIAdvancedSchedulerWeightReset
	updates[SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom] = settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom
	updates[SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost] = settings.OpenAIAdvancedSchedulerWeightUpstreamCost
	updates[SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse] = settings.OpenAIAdvancedSchedulerWeightPreviousResponse
	updates[SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky] = settings.OpenAIAdvancedSchedulerWeightSessionSticky

	// 账号限额通知
	updates[SettingKeyAccountQuotaNotifyEnabled] = strconv.FormatBool(settings.AccountQuotaNotifyEnabled)
	updates[SettingKeyAccountQuotaNotifyEmails] = MarshalNotifyEmails(settings.AccountQuotaNotifyEmails)

	if settings.AccountSchedulingThresholds != nil {
		normalized, err := validateAndNormalizeAccountSchedulingThresholds(settings.AccountSchedulingThresholds)
		if err != nil {
			return nil, err
		}
		blob, err := json.Marshal(normalized)
		if err != nil {
			return nil, fmt.Errorf("marshal account scheduling thresholds: %w", err)
		}
		updates[SettingKeyAccountSchedulingThresholds] = string(blob)
	}

	updates[SettingKeyAllowUserViewErrorRequests] = strconv.FormatBool(settings.AllowUserViewErrorRequests)

	return updates, nil
}

func defaultAccountSchedulingThresholds() map[string]int {
	return map[string]int{
		PlatformOpenAI:    100,
		PlatformAnthropic: 100,
		PlatformGrok:      100,
	}
}

func validateAndNormalizeAccountSchedulingThresholds(input map[string]int) (map[string]int, error) {
	normalized := defaultAccountSchedulingThresholds()
	for platform, value := range input {
		allowed := false
		for _, item := range AllowedSchedulingThresholdPlatforms {
			if item == platform {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, infraerrors.BadRequest("INVALID_ACCOUNT_SCHEDULING_THRESHOLDS", fmt.Sprintf("unknown platform %q", platform))
		}
		if value < 1 || value > 100 {
			return nil, infraerrors.BadRequest("INVALID_ACCOUNT_SCHEDULING_THRESHOLDS", "platform scheduling threshold must be between 1 and 100")
		}
		normalized[platform] = value
	}
	return normalized, nil
}

func parseAccountSchedulingThresholdsSetting(raw string) (map[string]int, error) {
	thresholds := defaultAccountSchedulingThresholds()
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return thresholds, nil
	}
	parsed := map[string]int{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return thresholds, err
	}
	for _, platform := range AllowedSchedulingThresholdPlatforms {
		if value, ok := parsed[platform]; ok {
			thresholds[platform] = boundedIntOrDefault(value, 1, 100, 100)
		}
	}
	return thresholds, nil
}

func boundedIntOrDefault(value, minValue, maxValue, defaultValue int) int {
	if value < minValue || value > maxValue {
		return defaultValue
	}
	return value
}

func cloneAccountSchedulingThresholds(input map[string]int) map[string]int {
	if len(input) == 0 {
		return defaultAccountSchedulingThresholds()
	}
	cloned := make(map[string]int, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func (s *SettingService) refreshCachedSettings(settings *SystemSettings) {
	if settings == nil {
		return
	}

	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{
		DefaultText:          strings.TrimSpace(settings.GrokDefaultTextModel),
		EnableCrossClientMap: settings.GrokCrossClientModelMapEnabled,
	})

	// 先使 inflight singleflight 失效，再刷新缓存，缩小旧值覆盖新值的竞态窗口
	versionBoundsSF.Forget("version_bounds")
	versionBoundsCache.Store(&cachedVersionBounds{
		min:       settings.MinClaudeCodeVersion,
		max:       settings.MaxClaudeCodeVersion,
		expiresAt: time.Now().Add(versionBoundsCacheTTL).UnixNano(),
	})
	backendModeSF.Forget("backend_mode")
	backendModeCache.Store(&cachedBackendMode{
		value:     settings.BackendModeEnabled,
		expiresAt: time.Now().Add(backendModeCacheTTL).UnixNano(),
	})
	gatewayForwardingSF.Forget("gateway_forwarding")
	gatewayForwardingCache.Store(&cachedGatewayForwardingSettings{
		openAITTFTMode:                   normalizeOpenAITTFTMode(settings.OpenAITTFTMode),
		fingerprintUnification:           settings.EnableFingerprintUnification,
		metadataPassthrough:              settings.EnableMetadataPassthrough,
		cchSigning:                       settings.EnableCCHSigning,
		claudeOAuthSystemPromptInjection: settings.EnableClaudeOAuthSystemPromptInjection,
		claudeOAuthSystemPrompt:          settings.ClaudeOAuthSystemPrompt,
		claudeOAuthSystemPromptBlocks:    settings.ClaudeOAuthSystemPromptBlocks,
		anthropicCacheTTL1hInjection:     settings.EnableAnthropicCacheTTL1hInjection,
		rewriteMessageCacheControl:       settings.RewriteMessageCacheControl,
		clientDatelineNormalization:      settings.EnableClientDatelineNormalization,
		accountLocalDeviceIdentity:       settings.EnableOpenAIAccountLocalDeviceIdentity,
		expiresAt:                        time.Now().Add(gatewayForwardingCacheTTL).UnixNano(),
	})
	SetCodexAccountLocalDeviceIdentityEnabled(settings.EnableOpenAIAccountLocalDeviceIdentity)
	// Publish the retained risk-control settings under the same lock used by
	// database refreshes, so a read started before this save cannot restore the
	// previous allowlist or session-block policy.
	allowlistedUsers, _ := ParseCyberPolicyUserAllowlist(settings.CyberPolicyUserAllowlist)
	cyberTTL := time.Duration(settings.CyberSessionBlockTTLSeconds) * time.Second
	if cyberTTL <= 0 {
		cyberTTL = time.Hour
	}
	s.cyberSessionBlockRuntimeMu.Lock()
	s.cyberSessionBlockRuntimeCache.Store(&cachedCyberSessionBlockRuntime{
		allowlistedUsers: allowlistedUsers,
		enabled:          settings.CyberSessionBlockEnabled,
		ttl:              cyberTTL,
		expiresAt:        time.Now().Add(cyberSessionBlockRuntimeCacheTTL).UnixNano(),
	})
	s.cyberSessionBlockRuntimeMu.Unlock()
	// Advance the Originator/UA generation before publishing either cache.
	// In-flight reads from the previous generation may still complete after
	// singleflight.Forget, but their epoch no longer permits them to return or
	// overwrite these freshly saved values.
	newHeaderEpoch := s.openAICodexHeaderOverridesEpoch.Add(1)
	s.openAICodexOriginatorSF.Forget(openAICodexHeaderOverrideSFKeyForEpoch(openAICodexOriginatorSFKey, newHeaderEpoch-1))
	s.openAICodexOriginatorCache.Store(&cachedOpenAICodexHeaderOverride{
		value:     NormalizeCodexOriginatorHeader(settings.OpenAICodexOriginator),
		epoch:     newHeaderEpoch,
		expiresAt: time.Now().Add(openAICodexOriginatorCacheTTL).UnixNano(),
	})
	s.openAICodexUASF.Forget(openAICodexHeaderOverrideSFKeyForEpoch(openAICodexUserAgentSFKey, newHeaderEpoch-1))
	codexUA := NormalizeCodexUserAgentHeader(settings.OpenAICodexUserAgent)
	if codexUA == "" {
		codexUA = DefaultOpenAICodexUserAgent
	}
	s.openAICodexUACache.Store(&cachedOpenAICodexHeaderOverride{
		value:     codexUA,
		epoch:     newHeaderEpoch,
		expiresAt: time.Now().Add(openAICodexUserAgentCacheTTL).UnixNano(),
	})
	// 版本号缓存只做失效，不在此重算：生效值还取决于自动同步写入的 synced 键，
	// 这里没有它的最新值，重算会把同步结果覆盖成陈旧值。
	s.InvalidateOpenAICodexClientVersionCache()
	s.InvalidateClaudeCodeClientVersionCache()
	openAIAdvancedSchedulerSettingSF.Forget(openAIAdvancedSchedulerSettingKey)
	openAIAdvancedSchedulerSettingCache.Store(&cachedOpenAIAdvancedSchedulerSetting{
		lowUpstreamRatePriorityEnabled: settings.OpenAILowUpstreamRatePriorityEnabled,
		oauthSchedulingRateMultiplier:  settings.OpenAIOAuthSchedulingRateMultiplier,
		enabled:                        settings.OpenAIAdvancedSchedulerEnabled,
		stickyWeightedEnabled:          settings.OpenAIAdvancedSchedulerStickyWeightedEnabled,
		subscriptionPriorityEnabled:    settings.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled,
		lbTopKOverride:                 parsePositiveIntOverride(settings.OpenAIAdvancedSchedulerLBTopK),
		weightOverrides: parseOpenAIAdvancedSchedulerWeightOverrides(map[string]string{
			SettingKeyOpenAIAdvancedSchedulerWeightPriority:         settings.OpenAIAdvancedSchedulerWeightPriority,
			SettingKeyOpenAIAdvancedSchedulerWeightLoad:             settings.OpenAIAdvancedSchedulerWeightLoad,
			SettingKeyOpenAIAdvancedSchedulerWeightQueue:            settings.OpenAIAdvancedSchedulerWeightQueue,
			SettingKeyOpenAIAdvancedSchedulerWeightErrorRate:        settings.OpenAIAdvancedSchedulerWeightErrorRate,
			SettingKeyOpenAIAdvancedSchedulerWeightTTFT:             settings.OpenAIAdvancedSchedulerWeightTTFT,
			SettingKeyOpenAIAdvancedSchedulerWeightReset:            settings.OpenAIAdvancedSchedulerWeightReset,
			SettingKeyOpenAIAdvancedSchedulerWeightQuotaHeadroom:    settings.OpenAIAdvancedSchedulerWeightQuotaHeadroom,
			SettingKeyOpenAIAdvancedSchedulerWeightUpstreamCost:     settings.OpenAIAdvancedSchedulerWeightUpstreamCost,
			SettingKeyOpenAIAdvancedSchedulerWeightPreviousResponse: settings.OpenAIAdvancedSchedulerWeightPreviousResponse,
			SettingKeyOpenAIAdvancedSchedulerWeightSessionSticky:    settings.OpenAIAdvancedSchedulerWeightSessionSticky,
		}),
		expiresAt: time.Now().Add(openAIAdvancedSchedulerSettingCacheTTL).UnixNano(),
	})
	// Invalidate the quota auto-pause cache and let the next read trigger a fresh load.
	// We can't know from here whether ops_advanced_settings was also touched, so be
	// defensive: store an expired entry — GetOpenAIQuotaAutoPauseSettings will serve
	// stale and kick off an async refresh, never blocking the request that follows.
	s.openAIQuotaAutoPauseSettingsSF.Forget(openAIQuotaAutoPauseSettingsRefreshKey)
	if cached, _ := s.openAIQuotaAutoPauseSettingsCache.Load().(*cachedOpenAIQuotaAutoPauseSettings); cached != nil {
		s.openAIQuotaAutoPauseSettingsCache.Store(&cachedOpenAIQuotaAutoPauseSettings{
			settings:  cached.settings,
			expiresAt: 0,
		})
	}
	accountSchedulingThresholdsSF.Forget(SettingKeyAccountSchedulingThresholds)
	if settings.AccountSchedulingThresholds != nil {
		normalizedThresholds, err := validateAndNormalizeAccountSchedulingThresholds(settings.AccountSchedulingThresholds)
		if err != nil {
			normalizedThresholds = defaultAccountSchedulingThresholds()
		}
		accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{
			thresholds: cloneAccountSchedulingThresholds(normalizedThresholds),
			expiresAt:  time.Now().Add(accountSchedulingThresholdsCacheTTL).UnixNano(),
		})
	} else {
		// Partial/omitted payload: clear cache so the next hot-path read reloads from DB.
		accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{})
	}
	if s.cfg != nil {
		s.cfg.SetForwardedClientIPSettings(settings.APIKeyACLTrustForwardedIP, settings.ForwardedClientIPHeaders)
	}
	// codex_cli_only 加固策略缓存：设置更新后强制下次重载（涉及 4 个键 + JSON 解析，直接置过期）。
	s.codexRestrictionPolicySF.Forget("codex_restriction_policy")
	s.codexRestrictionPolicyCache.Store(&cachedCodexRestrictionPolicy{expiresAt: 0})
}

// notifySettingsUpdated queues an external-observer notification for a
// committed settings revision. Persistence and in-process cache publication are
// serialized by settingsUpdateMu, but observers stay outside that mutex because
// they may perform I/O or re-enter a settings update.
//
// A single drainer invokes observers without holding settingsNotificationMu.
// This keeps callbacks re-entrant while preventing concurrent callbacks from
// publishing an older DB snapshot after a newer one. If several saves arrive
// while an observer is running, their edge-triggered notifications are
// coalesced and the drainer runs once more for the newest pending revision.
func (s *SettingService) notifySettingsUpdated(revision uint64) {
	if s == nil || revision == 0 {
		return
	}

	s.settingsNotificationMu.Lock()
	if revision > s.settingsNotificationPending {
		s.settingsNotificationPending = revision
	}
	if s.settingsNotificationRunning || s.settingsNotificationPending <= s.settingsNotificationCompleted {
		s.settingsNotificationMu.Unlock()
		return
	}
	s.settingsNotificationRunning = true
	s.settingsNotificationMu.Unlock()

	for {
		s.settingsNotificationMu.Lock()
		targetRevision := s.settingsNotificationPending
		s.settingsNotificationMu.Unlock()

		s.invokeSettingsUpdatedObservers()

		s.settingsNotificationMu.Lock()
		if targetRevision > s.settingsNotificationCompleted {
			s.settingsNotificationCompleted = targetRevision
		}
		if s.settingsNotificationPending <= s.settingsNotificationCompleted {
			s.settingsNotificationRunning = false
			s.settingsNotificationMu.Unlock()
			return
		}
		s.settingsNotificationMu.Unlock()
	}
}

func (s *SettingService) invokeSettingsUpdatedObservers() {
	s.onUpdateMu.RLock()
	onUpdate := s.onUpdate
	s.onUpdateMu.RUnlock()
	if onUpdate != nil {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.Error("settings update callback panicked", "panic", recovered)
				}
			}()
			onUpdate() // Invalidate cache after settings update
		}()
	}
	s.notifyChannelMonitorRuntimeListeners()
}

func (s *SettingService) defaultRewriteMessageCacheControl() bool {
	return false
}
