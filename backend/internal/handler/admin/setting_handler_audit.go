package admin

import (
	"log/slog"

	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) auditSettingsUpdate(c *gin.Context, before *service.SystemSettings, after *service.SystemSettings, req UpdateSettingsRequest) {
	if before == nil || after == nil {
		return
	}

	changed := diffSettings(before, after, req)
	if len(changed) == 0 {
		return
	}

	subject, _ := middleware.GetAuthSubjectFromContext(c)
	role, _ := middleware.GetUserRoleFromContext(c)
	slog.Info("settings updated",
		"audit", true,
		"user_id", subject.UserID,
		"role", role,
		"changed", changed,
	)
}

func diffSettings(before *service.SystemSettings, after *service.SystemSettings, req UpdateSettingsRequest) []string {
	changed := make([]string, 0, 20)

	if before.FrontendURL != after.FrontendURL {
		changed = append(changed, "frontend_url")
	}
	if before.TotpEnabled != after.TotpEnabled {
		changed = append(changed, "totp_enabled")
	}

	if before.SessionBindingEnabled != after.SessionBindingEnabled {
		changed = append(changed, "session_binding_enabled")
	}
	if before.StepUpEnabled != after.StepUpEnabled {
		changed = append(changed, "step_up_enabled")
	}
	if before.LoginAgreementEnabled != after.LoginAgreementEnabled {
		changed = append(changed, "login_agreement_enabled")
	}
	if before.LoginAgreementMode != after.LoginAgreementMode {
		changed = append(changed, "login_agreement_mode")
	}
	if before.LoginAgreementUpdatedAt != after.LoginAgreementUpdatedAt {
		changed = append(changed, "login_agreement_updated_at")
	}
	if !equalLoginAgreementDocuments(before.LoginAgreementDocuments, after.LoginAgreementDocuments) {
		changed = append(changed, "login_agreement_documents")
	}

	if before.TurnstileEnabled != after.TurnstileEnabled {
		changed = append(changed, "turnstile_enabled")
	}
	if before.TurnstileSiteKey != after.TurnstileSiteKey {
		changed = append(changed, "turnstile_site_key")
	}
	if req.TurnstileSecretKey != "" {
		changed = append(changed, "turnstile_secret_key")
	}
	if before.TencentCaptchaEnabled != after.TencentCaptchaEnabled {
		changed = append(changed, "tencent_captcha_enabled")
	}
	if before.TencentCaptchaAppID != after.TencentCaptchaAppID {
		changed = append(changed, "tencent_captcha_app_id")
	}
	if req.TencentCaptchaAppSecretKey != "" {
		changed = append(changed, "tencent_captcha_app_secret_key")
	}
	if req.TencentCaptchaCloudSecretID != "" {
		changed = append(changed, "tencent_captcha_cloud_secret_id")
	}
	if req.TencentCaptchaCloudSecretKey != "" {
		changed = append(changed, "tencent_captcha_cloud_secret_key")
	}
	if before.TencentCaptchaRegion != after.TencentCaptchaRegion {
		changed = append(changed, "tencent_captcha_region")
	}
	if before.AliyunCaptchaEnabled != after.AliyunCaptchaEnabled {
		changed = append(changed, "aliyun_captcha_enabled")
	}
	if before.AliyunCaptchaAccessKeyID != after.AliyunCaptchaAccessKeyID {
		changed = append(changed, "aliyun_captcha_access_key_id")
	}
	if req.AliyunCaptchaAccessKeySecret != "" {
		changed = append(changed, "aliyun_captcha_access_key_secret")
	}
	if before.AliyunCaptchaSceneID != after.AliyunCaptchaSceneID {
		changed = append(changed, "aliyun_captcha_scene_id")
	}
	if before.AliyunCaptchaPrefix != after.AliyunCaptchaPrefix {
		changed = append(changed, "aliyun_captcha_prefix")
	}
	if before.AliyunCaptchaRegion != after.AliyunCaptchaRegion {
		changed = append(changed, "aliyun_captcha_region")
	}
	if before.APIKeyACLTrustForwardedIP != after.APIKeyACLTrustForwardedIP {
		changed = append(changed, "api_key_acl_trust_forwarded_ip")
	}
	if !equalStringSlice(before.ForwardedClientIPHeaders, after.ForwardedClientIPHeaders) {
		changed = append(changed, "forwarded_client_ip_headers")
	}

	if before.SiteName != after.SiteName {
		changed = append(changed, "site_name")
	}
	if before.SiteLogo != after.SiteLogo {
		changed = append(changed, "site_logo")
	}
	if before.SiteSubtitle != after.SiteSubtitle {
		changed = append(changed, "site_subtitle")
	}
	if before.APIBaseURL != after.APIBaseURL {
		changed = append(changed, "api_base_url")
	}
	if before.ContactInfo != after.ContactInfo {
		changed = append(changed, "contact_info")
	}
	if before.DocURL != after.DocURL {
		changed = append(changed, "doc_url")
	}
	if before.HomeContent != after.HomeContent {
		changed = append(changed, "home_content")
	}
	if before.CompactHomeEnabled != after.CompactHomeEnabled {
		changed = append(changed, "compact_home_enabled")
	}
	if before.HideCcsImportButton != after.HideCcsImportButton {
		changed = append(changed, "hide_ccs_import_button")
	}

	if before.EnableModelFallback != after.EnableModelFallback {
		changed = append(changed, "enable_model_fallback")
	}
	if before.FallbackModelAnthropic != after.FallbackModelAnthropic {
		changed = append(changed, "fallback_model_anthropic")
	}
	if before.FallbackModelOpenAI != after.FallbackModelOpenAI {
		changed = append(changed, "fallback_model_openai")
	}

	if before.OpsMonitoringEnabled != after.OpsMonitoringEnabled {
		changed = append(changed, "ops_monitoring_enabled")
	}
	if before.OpsRealtimeMonitoringEnabled != after.OpsRealtimeMonitoringEnabled {
		changed = append(changed, "ops_realtime_monitoring_enabled")
	}
	if before.OpsQueryModeDefault != after.OpsQueryModeDefault {
		changed = append(changed, "ops_query_mode_default")
	}
	if before.OpsMetricsIntervalSeconds != after.OpsMetricsIntervalSeconds {
		changed = append(changed, "ops_metrics_interval_seconds")
	}
	if before.MinClaudeCodeVersion != after.MinClaudeCodeVersion {
		changed = append(changed, "min_claude_code_version")
	}
	if before.MaxClaudeCodeVersion != after.MaxClaudeCodeVersion {
		changed = append(changed, "max_claude_code_version")
	}
	if before.MinCodexVersion != after.MinCodexVersion {
		changed = append(changed, "min_codex_version")
	}
	if before.MaxCodexVersion != after.MaxCodexVersion {
		changed = append(changed, "max_codex_version")
	}
	if before.CodexCLIOnlyAllowAppServerClients != after.CodexCLIOnlyAllowAppServerClients {
		changed = append(changed, "codex_cli_only_allow_app_server_clients")
	}
	if before.CodexCLIOnlyEngineFingerprintSignals != after.CodexCLIOnlyEngineFingerprintSignals {
		changed = append(changed, "codex_cli_only_engine_fingerprint_signals")
	}
	if before.CodexCLIOnlyBlacklist != after.CodexCLIOnlyBlacklist {
		changed = append(changed, "codex_cli_only_blacklist")
	}
	if before.CodexCLIOnlyWhitelist != after.CodexCLIOnlyWhitelist {
		changed = append(changed, "codex_cli_only_whitelist")
	}
	if before.AllowUngroupedKeyScheduling != after.AllowUngroupedKeyScheduling {
		changed = append(changed, "allow_ungrouped_key_scheduling")
	}
	if before.BackendModeEnabled != after.BackendModeEnabled {
		changed = append(changed, "backend_mode_enabled")
	}

	if before.TableDefaultPageSize != after.TableDefaultPageSize {
		changed = append(changed, "table_default_page_size")
	}
	if !equalIntSlice(before.TablePageSizeOptions, after.TablePageSizeOptions) {
		changed = append(changed, "table_page_size_options")
	}

	if before.CustomEndpoints != after.CustomEndpoints {
		changed = append(changed, "custom_endpoints")
	}
	if before.EnableFingerprintUnification != after.EnableFingerprintUnification {
		changed = append(changed, "enable_fingerprint_unification")
	}
	if before.OpenAITTFTMode != after.OpenAITTFTMode {
		changed = append(changed, "openai_ttft_mode")
	}
	if before.EnableMetadataPassthrough != after.EnableMetadataPassthrough {
		changed = append(changed, "enable_metadata_passthrough")
	}
	if before.EnableCCHSigning != after.EnableCCHSigning {
		changed = append(changed, "enable_cch_signing")
	}
	if before.EnableClaudeOAuthSystemPromptInjection != after.EnableClaudeOAuthSystemPromptInjection {
		changed = append(changed, "enable_claude_oauth_system_prompt_injection")
	}
	if before.ClaudeOAuthSystemPrompt != after.ClaudeOAuthSystemPrompt {
		changed = append(changed, "claude_oauth_system_prompt")
	}
	if before.ClaudeOAuthSystemPromptBlocks != after.ClaudeOAuthSystemPromptBlocks {
		changed = append(changed, "claude_oauth_system_prompt_blocks")
	}
	if before.EnableAnthropicCacheTTL1hInjection != after.EnableAnthropicCacheTTL1hInjection {
		changed = append(changed, "enable_anthropic_cache_ttl_1h_injection")
	}
	if before.RewriteMessageCacheControl != after.RewriteMessageCacheControl {
		changed = append(changed, "rewrite_message_cache_control")
	}
	if before.EnableClientDatelineNormalization != after.EnableClientDatelineNormalization {
		changed = append(changed, "enable_client_dateline_normalization")
	}

	if before.OpenAICodexOriginator != after.OpenAICodexOriginator {
		changed = append(changed, "openai_codex_originator")
	}
	if before.OpenAICodexUserAgent != after.OpenAICodexUserAgent {
		changed = append(changed, "openai_codex_user_agent")
	}
	if before.OpenAICodexClientVersionMode != after.OpenAICodexClientVersionMode {
		changed = append(changed, "openai_codex_client_version_mode")
	}
	if before.OpenAICodexClientVersion != after.OpenAICodexClientVersion {
		changed = append(changed, "openai_codex_client_version")
	}
	if before.OpenAICodexVersionAutoSyncEnabled != after.OpenAICodexVersionAutoSyncEnabled {
		changed = append(changed, "openai_codex_version_auto_sync_enabled")
	}
	if before.EnableOpenAIAccountLocalDeviceIdentity != after.EnableOpenAIAccountLocalDeviceIdentity {
		changed = append(changed, "enable_openai_account_local_device_identity")
	}
	if before.ClaudeCodeClientVersion != after.ClaudeCodeClientVersion {
		changed = append(changed, "claude_code_client_version")
	}
	if before.ClaudeCodeVersionAutoSyncEnabled != after.ClaudeCodeVersionAutoSyncEnabled {
		changed = append(changed, "claude_code_version_auto_sync_enabled")
	}

	if before.OpenAILowUpstreamRatePriorityEnabled != after.OpenAILowUpstreamRatePriorityEnabled {
		changed = append(changed, "openai_low_upstream_rate_priority_enabled")
	}
	if before.OpenAIOAuthSchedulingRateMultiplier != after.OpenAIOAuthSchedulingRateMultiplier {
		changed = append(changed, "openai_oauth_scheduling_rate_multiplier")
	}
	if before.OpenAIAdvancedSchedulerEnabled != after.OpenAIAdvancedSchedulerEnabled {
		changed = append(changed, "openai_advanced_scheduler_enabled")
	}
	if before.OpenAIAdvancedSchedulerStickyWeightedEnabled != after.OpenAIAdvancedSchedulerStickyWeightedEnabled {
		changed = append(changed, "openai_advanced_scheduler_sticky_weighted_enabled")
	}
	if before.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled != after.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled {
		changed = append(changed, "openai_advanced_scheduler_subscription_priority_enabled")
	}
	if before.OpenAIAdvancedSchedulerLBTopK != after.OpenAIAdvancedSchedulerLBTopK {
		changed = append(changed, "openai_advanced_scheduler_lb_top_k")
	}
	if before.OpenAIAdvancedSchedulerWeightPriority != after.OpenAIAdvancedSchedulerWeightPriority {
		changed = append(changed, "openai_advanced_scheduler_weight_priority")
	}
	if before.OpenAIAdvancedSchedulerWeightLoad != after.OpenAIAdvancedSchedulerWeightLoad {
		changed = append(changed, "openai_advanced_scheduler_weight_load")
	}
	if before.OpenAIAdvancedSchedulerWeightQueue != after.OpenAIAdvancedSchedulerWeightQueue {
		changed = append(changed, "openai_advanced_scheduler_weight_queue")
	}
	if before.OpenAIAdvancedSchedulerWeightErrorRate != after.OpenAIAdvancedSchedulerWeightErrorRate {
		changed = append(changed, "openai_advanced_scheduler_weight_error_rate")
	}
	if before.OpenAIAdvancedSchedulerWeightTTFT != after.OpenAIAdvancedSchedulerWeightTTFT {
		changed = append(changed, "openai_advanced_scheduler_weight_ttft")
	}
	if before.OpenAIAdvancedSchedulerWeightReset != after.OpenAIAdvancedSchedulerWeightReset {
		changed = append(changed, "openai_advanced_scheduler_weight_reset")
	}
	if before.OpenAIAdvancedSchedulerWeightQuotaHeadroom != after.OpenAIAdvancedSchedulerWeightQuotaHeadroom {
		changed = append(changed, "openai_advanced_scheduler_weight_quota_headroom")
	}
	if before.OpenAIAdvancedSchedulerWeightUpstreamCost != after.OpenAIAdvancedSchedulerWeightUpstreamCost {
		changed = append(changed, "openai_advanced_scheduler_weight_upstream_cost")
	}
	if before.OpenAIAdvancedSchedulerWeightPreviousResponse != after.OpenAIAdvancedSchedulerWeightPreviousResponse {
		changed = append(changed, "openai_advanced_scheduler_weight_previous_response")
	}
	if before.OpenAIAdvancedSchedulerWeightSessionSticky != after.OpenAIAdvancedSchedulerWeightSessionSticky {
		changed = append(changed, "openai_advanced_scheduler_weight_session_sticky")
	}
	// 账号限额通知

	if before.AccountQuotaNotifyEnabled != after.AccountQuotaNotifyEnabled {
		changed = append(changed, "account_quota_notify_enabled")
	}
	if !equalNotifyEmailEntries(before.AccountQuotaNotifyEmails, after.AccountQuotaNotifyEmails) {
		changed = append(changed, "account_quota_notify_emails")
	}
	if before.ChannelMonitorEnabled != after.ChannelMonitorEnabled {
		changed = append(changed, "channel_monitor_enabled")
	}
	if before.ChannelMonitorDefaultIntervalSeconds != after.ChannelMonitorDefaultIntervalSeconds {
		changed = append(changed, "channel_monitor_default_interval_seconds")
	}
	if before.AvailableChannelsEnabled != after.AvailableChannelsEnabled {
		changed = append(changed, "available_channels_enabled")
	}

	if before.ModelPlazaEnabled != after.ModelPlazaEnabled {
		changed = append(changed, "model_plaza_enabled")
	}
	if before.ModelPlazaRequireAuth != after.ModelPlazaRequireAuth {
		changed = append(changed, "model_plaza_require_auth")
	}
	if before.ModelPlazaDescription != after.ModelPlazaDescription {
		changed = append(changed, "model_plaza_description")
	}

	if before.RiskControlEnabled != after.RiskControlEnabled {
		changed = append(changed, "risk_control_enabled")
	}
	if before.CyberPolicyUserAllowlist != after.CyberPolicyUserAllowlist {
		changed = append(changed, "cyber_policy_user_allowlist")
	}
	if before.CyberSessionBlockEnabled != after.CyberSessionBlockEnabled {
		changed = append(changed, "cyber_session_block_enabled")
	}
	if before.CyberSessionBlockTTLSeconds != after.CyberSessionBlockTTLSeconds {
		changed = append(changed, "cyber_session_block_ttl_seconds")
	}

	if !equalAccountSchedulingThresholds(before.AccountSchedulingThresholds, after.AccountSchedulingThresholds) {
		changed = append(changed, service.SettingKeyAccountSchedulingThresholds)
	}

	return changed
}

func float64ValueOrDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func intValueOrDefault(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValueOrDefault(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalLoginAgreementDocuments(a, b []service.LoginAgreementDocument) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Title != b[i].Title || a[i].ContentMD != b[i].ContentMD {
			return false
		}
	}
	return true
}

func equalIntSlice(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInt64Slice(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalNotifyEmailEntries(a, b []service.NotifyEmailEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Email != b[i].Email || a[i].Verified != b[i].Verified || a[i].Disabled != b[i].Disabled {
			return false
		}
	}
	return true
}

// equalPlatformQuotaSettings reports whether two platform-quota maps are identical across all allowed slots.
func equalAccountSchedulingThresholds(before, after map[string]int) bool {
	for _, platform := range service.AllowedSchedulingThresholdPlatforms {
		beforeValue := 100
		if before != nil {
			if value, ok := before[platform]; ok {
				beforeValue = value
			}
		}
		afterValue := 100
		if after != nil {
			if value, ok := after[platform]; ok {
				afterValue = value
			}
		}
		if beforeValue != afterValue {
			return false
		}
	}
	return true
}

func stringSetting(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
