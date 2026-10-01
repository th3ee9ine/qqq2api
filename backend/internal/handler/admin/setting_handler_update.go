package admin

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/th3ee9ine/qqq2api/internal/config"
	"github.com/th3ee9ine/qqq2api/internal/handler/dto"
	"github.com/th3ee9ine/qqq2api/internal/pkg/response"
	"github.com/th3ee9ine/qqq2api/internal/server/middleware"
	"github.com/th3ee9ine/qqq2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// UpdateSettingsRequest 更新设置请求
type UpdateSettingsRequest struct {
	FrontendURL string `json:"frontend_url"`

	TotpEnabled bool `json:"totp_enabled"` // TOTP 双因素认证

	SessionBindingEnabled   *bool                        `json:"session_binding_enabled"`  // 会话 IP/UA 绑定（省略=保持现值）
	StepUpEnabled           *bool                        `json:"step_up_enabled"`          // 敏感操作 step-up 2FA（省略=保持现值）
	AuditLogRetentionDays   int                          `json:"audit_log_retention_days"` // 审计日志保留天数
	LoginAgreementEnabled   bool                         `json:"login_agreement_enabled"`
	LoginAgreementMode      string                       `json:"login_agreement_mode"`
	LoginAgreementUpdatedAt string                       `json:"login_agreement_updated_at"`
	LoginAgreementDocuments []dto.LoginAgreementDocument `json:"login_agreement_documents"`

	// Cloudflare Turnstile 设置
	TurnstileEnabled   bool   `json:"turnstile_enabled"`
	TurnstileSiteKey   string `json:"turnstile_site_key"`
	TurnstileSecretKey string `json:"turnstile_secret_key"`

	// 腾讯天御验证码设置
	TencentCaptchaEnabled        bool   `json:"tencent_captcha_enabled"`
	TencentCaptchaAppID          string `json:"tencent_captcha_app_id"`
	TencentCaptchaAppSecretKey   string `json:"tencent_captcha_app_secret_key"`
	TencentCaptchaCloudSecretID  string `json:"tencent_captcha_cloud_secret_id"`
	TencentCaptchaCloudSecretKey string `json:"tencent_captcha_cloud_secret_key"`
	TencentCaptchaRegion         string `json:"tencent_captcha_region"`

	// 阿里云验证码 2.0 设置
	AliyunCaptchaEnabled         bool   `json:"aliyun_captcha_enabled"`
	AliyunCaptchaAccessKeyID     string `json:"aliyun_captcha_access_key_id"`
	AliyunCaptchaAccessKeySecret string `json:"aliyun_captcha_access_key_secret"`
	AliyunCaptchaSceneID         string `json:"aliyun_captcha_scene_id"`
	AliyunCaptchaPrefix          string `json:"aliyun_captcha_prefix"`
	AliyunCaptchaRegion          string `json:"aliyun_captcha_region"`

	// API Key IP 访问控制设置
	APIKeyACLTrustForwardedIP *bool     `json:"api_key_acl_trust_forwarded_ip"`
	ForwardedClientIPHeaders  *[]string `json:"forwarded_client_ip_headers"`

	// OEM设置
	SiteName            string `json:"site_name"`
	SiteLogo            string `json:"site_logo"`
	SiteSubtitle        string `json:"site_subtitle"`
	APIBaseURL          string `json:"api_base_url"`
	ContactInfo         string `json:"contact_info"`
	DocURL              string `json:"doc_url"`
	HomeContent         string `json:"home_content"`
	CompactHomeEnabled  bool   `json:"compact_home_enabled"`
	HideCcsImportButton bool   `json:"hide_ccs_import_button"`

	TableDefaultPageSize int   `json:"table_default_page_size"`
	TablePageSizeOptions []int `json:"table_page_size_options"`

	CustomEndpoints *[]dto.CustomEndpoint `json:"custom_endpoints"`

	// Model fallback configuration
	EnableModelFallback    bool   `json:"enable_model_fallback"`
	FallbackModelAnthropic string `json:"fallback_model_anthropic"`
	FallbackModelOpenAI    string `json:"fallback_model_openai"`

	// Ops monitoring (vNext)
	OpsMonitoringEnabled         *bool   `json:"ops_monitoring_enabled"`
	OpsRealtimeMonitoringEnabled *bool   `json:"ops_realtime_monitoring_enabled"`
	OpsQueryModeDefault          *string `json:"ops_query_mode_default"`
	OpsMetricsIntervalSeconds    *int    `json:"ops_metrics_interval_seconds"`

	MinClaudeCodeVersion string `json:"min_claude_code_version"`
	MaxClaudeCodeVersion string `json:"max_claude_code_version"`

	// 分组隔离
	AllowUngroupedKeyScheduling bool `json:"allow_ungrouped_key_scheduling"`

	// Backend Mode
	BackendModeEnabled bool `json:"backend_mode_enabled"`

	// Gateway forwarding behavior
	OpenAITTFTMode                         *string `json:"openai_ttft_mode"`
	EnableFingerprintUnification           *bool   `json:"enable_fingerprint_unification"`
	EnableMetadataPassthrough              *bool   `json:"enable_metadata_passthrough"`
	EnableCCHSigning                       *bool   `json:"enable_cch_signing"`
	EnableClaudeOAuthSystemPromptInjection *bool   `json:"enable_claude_oauth_system_prompt_injection"`
	ClaudeOAuthSystemPrompt                *string `json:"claude_oauth_system_prompt"`
	ClaudeOAuthSystemPromptBlocks          *string `json:"claude_oauth_system_prompt_blocks"`
	EnableAnthropicCacheTTL1hInjection     *bool   `json:"enable_anthropic_cache_ttl_1h_injection"`
	RewriteMessageCacheControl             *bool   `json:"rewrite_message_cache_control"`
	EnableClientDatelineNormalization      *bool   `json:"enable_client_dateline_normalization"`

	OpenAICodexOriginator                  *string `json:"openai_codex_originator"`
	OpenAICodexUserAgent                   *string `json:"openai_codex_user_agent"`
	OpenAICodexClientVersion               *string `json:"openai_codex_client_version"`
	OpenAICodexClientVersionMode           *string `json:"openai_codex_client_version_mode"`
	OpenAICodexVersionAutoSyncEnabled      *bool   `json:"openai_codex_version_auto_sync_enabled"`
	EnableOpenAIAccountLocalDeviceIdentity *bool   `json:"enable_openai_account_local_device_identity"`
	ClaudeCodeClientVersion                *string `json:"claude_code_client_version"`
	ClaudeCodeVersionAutoSyncEnabled       *bool   `json:"claude_code_version_auto_sync_enabled"`

	// codex_cli_only 加固（global-only）
	MinCodexVersion                      string `json:"min_codex_version"`
	MaxCodexVersion                      string `json:"max_codex_version"`
	CodexCLIOnlyBlacklist                string `json:"codex_cli_only_blacklist"`
	CodexCLIOnlyWhitelist                string `json:"codex_cli_only_whitelist"`
	CodexCLIOnlyAllowAppServerClients    *bool  `json:"codex_cli_only_allow_app_server_clients"`
	CodexCLIOnlyEngineFingerprintSignals string `json:"codex_cli_only_engine_fingerprint_signals"`

	// OpenAI account scheduling
	OpenAILowUpstreamRatePriorityEnabled               *bool    `json:"openai_low_upstream_rate_priority_enabled"`
	OpenAIOAuthSchedulingRateMultiplier                *float64 `json:"openai_oauth_scheduling_rate_multiplier"`
	OpenAIAdvancedSchedulerEnabled                     *bool    `json:"openai_advanced_scheduler_enabled"`
	OpenAIAdvancedSchedulerStickyWeightedEnabled       *bool    `json:"openai_advanced_scheduler_sticky_weighted_enabled"`
	OpenAIAdvancedSchedulerSubscriptionPriorityEnabled *bool    `json:"openai_advanced_scheduler_subscription_priority_enabled"`
	OpenAIAdvancedSchedulerLBTopK                      *string  `json:"openai_advanced_scheduler_lb_top_k"`
	OpenAIAdvancedSchedulerWeightPriority              *string  `json:"openai_advanced_scheduler_weight_priority"`
	OpenAIAdvancedSchedulerWeightLoad                  *string  `json:"openai_advanced_scheduler_weight_load"`
	OpenAIAdvancedSchedulerWeightQueue                 *string  `json:"openai_advanced_scheduler_weight_queue"`
	OpenAIAdvancedSchedulerWeightErrorRate             *string  `json:"openai_advanced_scheduler_weight_error_rate"`
	OpenAIAdvancedSchedulerWeightTTFT                  *string  `json:"openai_advanced_scheduler_weight_ttft"`
	OpenAIAdvancedSchedulerWeightReset                 *string  `json:"openai_advanced_scheduler_weight_reset"`
	OpenAIAdvancedSchedulerWeightQuotaHeadroom         *string  `json:"openai_advanced_scheduler_weight_quota_headroom"`
	OpenAIAdvancedSchedulerWeightUpstreamCost          *string  `json:"openai_advanced_scheduler_weight_upstream_cost"`
	OpenAIAdvancedSchedulerWeightPreviousResponse      *string  `json:"openai_advanced_scheduler_weight_previous_response"`
	OpenAIAdvancedSchedulerWeightSessionSticky         *string  `json:"openai_advanced_scheduler_weight_session_sticky"`

	AccountQuotaNotifyEnabled *bool                   `json:"account_quota_notify_enabled"`
	AccountQuotaNotifyEmails  *[]dto.NotifyEmailEntry `json:"account_quota_notify_emails"`

	// Channel Monitor feature switch
	ChannelMonitorEnabled                *bool   `json:"channel_monitor_enabled"`
	ChannelMonitorMode                   *string `json:"channel_monitor_mode"`
	ChannelMonitorDefaultIntervalSeconds *int    `json:"channel_monitor_default_interval_seconds"`
	ChannelMonitorHideThroughput         *bool   `json:"channel_monitor_hide_throughput"`
	ChannelMonitorShowQuota              *bool   `json:"channel_monitor_show_quota"`

	// Grok model mapping policy
	GrokDefaultTextModel           *string `json:"grok_default_text_model"`
	GrokCrossClientModelMapEnabled *bool   `json:"grok_cross_client_model_map_enabled"`
	GrokDefaultBaseURLMode         *string `json:"grok_default_base_url_mode"`

	// Available Channels feature switch (user-facing)
	AvailableChannelsEnabled *bool `json:"available_channels_enabled"`

	// Model Plaza feature switches + description
	ModelPlazaEnabled     *bool   `json:"model_plaza_enabled"`
	ModelPlazaRequireAuth *bool   `json:"model_plaza_require_auth"`
	ModelPlazaDescription *string `json:"model_plaza_description"`

	// Plugin management menu visibility switch; plugin runtime is unaffected.
	PluginManagementEnabled *bool `json:"plugin_management_enabled"`

	// 风控中心功能开关
	RiskControlEnabled *bool `json:"risk_control_enabled"`

	// cyber 会话屏蔽开关 + TTL
	CyberSessionBlockEnabled    *bool   `json:"cyber_session_block_enabled"`
	CyberPolicyUserAllowlist    *string `json:"cyber_policy_user_allowlist"`
	CyberSessionBlockTTLSeconds *int    `json:"cyber_session_block_ttl_seconds"`

	// OpenAI fast/flex policy (optional, only updated when provided)
	OpenAIFastPolicySettings *dto.OpenAIFastPolicySettings `json:"openai_fast_policy_settings,omitempty"`

	// 各平台账号自动停调阈值（整体替换语义：nil = 不修改，non-nil = 整体覆盖）。
	AccountSchedulingThresholds map[string]int `json:"account_scheduling_thresholds"`

	AllowUserViewErrorRequests *bool `json:"allow_user_view_error_requests"`
}

// UpdateSettings 更新系统设置
// PUT /api/v1/admin/settings
// ensureActorTotpForStepUp 校验当前操作者具备开启 step-up 门控的条件：
// 必须是真人管理员会话（admin API key 无法完成 TOTP step-up，拒绝）且本人已启用 TOTP。
// 校验失败时写入错误响应并返回 false。
func (h *SettingHandler) ensureActorTotpForStepUp(c *gin.Context) bool {
	if c.GetString("auth_method") == service.AuditAuthMethodAdminAPIKey {
		response.ErrorWithDetails(c, http.StatusForbidden,
			"Admin API key cannot enable step-up verification; use an admin session with TOTP enabled",
			"STEP_UP_ADMIN_API_KEY_FORBIDDEN", nil)
		return false
	}
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.ErrorWithDetails(c, http.StatusForbidden,
			"Enabling step-up verification requires an authenticated admin session",
			"STEP_UP_ENABLE_REQUIRES_TOTP", nil)
		return false
	}
	if h.userService == nil {
		response.InternalError(c, "Step-up precondition check unavailable")
		return false
	}
	user, err := h.userService.GetByID(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return false
	}
	if !user.TotpEnabled {
		response.ErrorWithDetails(c, http.StatusBadRequest,
			"Enable two-factor authentication (TOTP) for your account before turning on step-up verification",
			"STEP_UP_ENABLE_REQUIRES_TOTP", nil)
		return false
	}
	return true
}

// settingKeyJSONAliases covers the request fields whose JSON name differs from
// the setting key they persist to. Every other field of UpdateSettingsRequest
// is named after its setting key.
var settingKeyJSONAliases = map[string]string{}

// settingOmittablePointerKeys opts selected pointer fields into the same
// storage-level "omitted means do not write" behavior as value fields. Most
// legacy pointer fields are deliberately merged from the handler's prior
// snapshot for compatibility, but doing that for the independently editable
// Codex identity controls can roll back a concurrent save: a request that did
// not carry Originator could otherwise write the stale Originator it read
// before another request committed. Dropping these absent keys lets the
// repository's current value win, and refreshCachedSettingsAfterWrite reloads
// the resulting snapshot before publishing caches.
var settingOmittablePointerKeys = map[string]string{
	"openai_codex_originator":                     service.SettingKeyOpenAICodexOriginator,
	"openai_codex_user_agent":                     service.SettingKeyOpenAICodexUserAgent,
	"openai_codex_client_version":                 service.SettingKeyOpenAICodexClientVersion,
	"openai_codex_client_version_mode":            service.SettingKeyOpenAICodexClientVersionMode,
	"openai_codex_version_auto_sync_enabled":      service.SettingKeyOpenAICodexVersionAutoSyncEnabled,
	"enable_openai_account_local_device_identity": service.SettingKeyEnableOpenAIAccountLocalDeviceIdentity,
}

// settingKeyByJSONName maps the value-typed top-level JSON fields of
// UpdateSettingsRequest, plus the explicitly opted-in pointer fields above, to
// the setting key each one writes. Value fields are resolved once from struct
// tags so new fields are covered without touching this file.
//
// Other pointer-typed fields remain excluded: they already carry their own
// "omitted = keep the prior snapshot" merge in UpdateSettings, and some rely on
// being rewritten on every save to re-normalize fail-closed security state (see
// TestUpdateSettingsMalformedForwardedClientIPHeadersRemainFailClosedWhenOmitted).
// Value-typed fields are otherwise indistinguishable from a deliberate clear.
var settingKeyByJSONName = buildSettingKeyByJSONName()

func buildSettingKeyByJSONName() map[string]string {
	t := reflect.TypeOf(UpdateSettingsRequest{})
	out := make(map[string]string, t.NumField()+len(settingOmittablePointerKeys))
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Pointer {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		if alias, ok := settingKeyJSONAliases[name]; ok {
			out[name] = alias
			continue
		}
		out[name] = name
	}
	for jsonName, settingKey := range settingOmittablePointerKeys {
		out[jsonName] = settingKey
	}
	return out
}

// omittedSettingKeys reports the setting keys this payload never mentioned.
// Saving settings is a whole-document PUT, so without this a client that sends
// only the one field it cares about resets every other field to a zero value.
func omittedSettingKeys(sentFields map[string]json.RawMessage) service.OmittedSettingKeys {
	omitted := make(service.OmittedSettingKeys, len(settingKeyByJSONName))
	for jsonName, settingKey := range settingKeyByJSONName {
		raw, sent := sentFields[jsonName]
		if !sent {
			omitted[settingKey] = struct{}{}
			continue
		}
		// These pointer fields use nil as "leave unchanged" for both absent
		// and explicit JSON null values. Keep null out of the write too, so
		// it cannot re-publish a stale value merged by the handler.
		if _, pointer := settingOmittablePointerKeys[jsonName]; pointer && strings.TrimSpace(string(raw)) == "null" {
			omitted[settingKey] = struct{}{}
		}
	}
	return omitted
}

func settingsAuditRequest(req UpdateSettingsRequest) UpdateSettingsRequest {
	req.TencentCaptchaAppSecretKey = strings.TrimSpace(req.TencentCaptchaAppSecretKey)
	req.TencentCaptchaCloudSecretID = strings.TrimSpace(req.TencentCaptchaCloudSecretID)
	req.TencentCaptchaCloudSecretKey = strings.TrimSpace(req.TencentCaptchaCloudSecretKey)
	req.AliyunCaptchaAccessKeySecret = strings.TrimSpace(req.AliyunCaptchaAccessKeySecret)
	return req
}

func (h *SettingHandler) UpdateSettings(c *gin.Context) {
	var sentFields map[string]json.RawMessage
	if err := c.ShouldBindBodyWith(&sentFields, binding.JSON); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	for field := range sentFields {
		if retiredSettingField(field) {
			response.BadRequest(c, "Setting is no longer supported: "+field)
			return
		}
	}
	var req UpdateSettingsRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	auditReq := settingsAuditRequest(req)
	omitted := omittedSettingKeys(sentFields)

	previousSettings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// 两个安全开关的请求字段为指针：省略字段=保持现值，避免旧客户端/脚本
	// 用不含新字段的全量 payload 保存设置时把安全开关静默重置。
	sessionBindingEnabled := previousSettings.SessionBindingEnabled
	if req.SessionBindingEnabled != nil {
		sessionBindingEnabled = *req.SessionBindingEnabled
	}
	stepUpEnabled := previousSettings.StepUpEnabled
	if req.StepUpEnabled != nil {
		stepUpEnabled = *req.StepUpEnabled
	}

	forwardedClientIPHeaders := append([]string(nil), previousSettings.ForwardedClientIPHeaders...)
	if req.ForwardedClientIPHeaders != nil {
		forwardedClientIPHeaders = append([]string(nil), (*req.ForwardedClientIPHeaders)...)
	}

	// 开启敏感操作 step-up 门控属自锁风险操作：仅允许本人已启用 TOTP 的管理员会话开启，
	// 否则开启后操作者立即被挡在所有敏感操作之外。仅在 false→true 的开启瞬间校验，
	// 保持开启状态的常规设置保存不受影响。
	if stepUpEnabled && !previousSettings.StepUpEnabled {
		if !h.ensureActorTotpForStepUp(c) {
			return
		}
	}
	// 关闭 step-up 门控本身就是敏感操作：防止拿到管理员会话的攻击者先关闸再执行导出等敏感操作。
	// previousSettings 已证实开关处于开启状态，使用无条件门控变体，
	// 避免门控内部二次读取开关时因存储故障 fail-open（前端捕获 STEP_UP_REQUIRED 弹码重试）。
	if !stepUpEnabled && previousSettings.StepUpEnabled {
		if !middleware.EnforceStepUpAlways(c, h.totpService, h.userService) {
			return
		}
	}

	// 通用表格配置：兼容旧客户端未传字段时保留当前值。
	if req.TableDefaultPageSize <= 0 {
		req.TableDefaultPageSize = previousSettings.TableDefaultPageSize
	}
	if req.TablePageSizeOptions == nil {
		req.TablePageSizeOptions = previousSettings.TablePageSizeOptions
	}

	req.TencentCaptchaAppID = strings.TrimSpace(req.TencentCaptchaAppID)
	req.TencentCaptchaAppSecretKey = strings.TrimSpace(req.TencentCaptchaAppSecretKey)
	req.TencentCaptchaCloudSecretID = strings.TrimSpace(req.TencentCaptchaCloudSecretID)
	req.TencentCaptchaCloudSecretKey = strings.TrimSpace(req.TencentCaptchaCloudSecretKey)

	turnstileEnabled := req.TurnstileEnabled
	if _, sent := sentFields["turnstile_enabled"]; !sent {
		turnstileEnabled = previousSettings.TurnstileEnabled
	}
	tencentCaptchaEnabled := req.TencentCaptchaEnabled
	if _, sent := sentFields["tencent_captcha_enabled"]; !sent {
		tencentCaptchaEnabled = previousSettings.TencentCaptchaEnabled
	}
	aliyunCaptchaEnabled := req.AliyunCaptchaEnabled
	if _, sent := sentFields["aliyun_captcha_enabled"]; !sent {
		aliyunCaptchaEnabled = previousSettings.AliyunCaptchaEnabled
	}
	enabledCaptchaProviders := 0
	for _, enabled := range []bool{turnstileEnabled, tencentCaptchaEnabled, aliyunCaptchaEnabled} {
		if enabled {
			enabledCaptchaProviders++
		}
	}
	if enabledCaptchaProviders > 1 {
		response.BadRequest(c, "Multiple captcha providers (Cloudflare Turnstile / Tencent Captcha / Aliyun Captcha) cannot be enabled at the same time")
		return
	}
	// 阿里云地域 normalize：未发送保留已存值，非法值一律按中国内地落库
	if _, sent := sentFields["aliyun_captcha_region"]; !sent {
		req.AliyunCaptchaRegion = previousSettings.AliyunCaptchaRegion
	}
	if req.AliyunCaptchaRegion != service.AliyunCaptchaRegionSGP {
		req.AliyunCaptchaRegion = service.AliyunCaptchaRegionCN
	}
	// 天御站点 normalize：未发送保留已存值，非法值一律按中国站落库
	if _, sent := sentFields["tencent_captcha_region"]; !sent {
		req.TencentCaptchaRegion = previousSettings.TencentCaptchaRegion
	}
	if req.TencentCaptchaRegion != service.TencentCaptchaRegionINTL {
		req.TencentCaptchaRegion = service.TencentCaptchaRegionCN
	}

	// Turnstile 参数验证
	if req.TurnstileEnabled {
		// 检查必填字段
		if req.TurnstileSiteKey == "" {
			response.BadRequest(c, "Turnstile Site Key is required when enabled")
			return
		}
		// 如果未提供 secret key，使用已保存的值（留空保留当前值）
		if req.TurnstileSecretKey == "" {
			if previousSettings.TurnstileSecretKey == "" {
				response.BadRequest(c, "Turnstile Secret Key is required when enabled")
				return
			}
			req.TurnstileSecretKey = previousSettings.TurnstileSecretKey
		}

		// 当 site_key 或 secret_key 任一变化时验证（避免配置错误导致无法登录）
		siteKeyChanged := previousSettings.TurnstileSiteKey != req.TurnstileSiteKey
		secretKeyChanged := previousSettings.TurnstileSecretKey != req.TurnstileSecretKey
		if siteKeyChanged || secretKeyChanged {
			if err := h.turnstileService.ValidateSecretKey(c.Request.Context(), req.TurnstileSecretKey); err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
	}

	if tencentCaptchaEnabled {
		if _, sent := sentFields["tencent_captcha_app_id"]; !sent {
			req.TencentCaptchaAppID = previousSettings.TencentCaptchaAppID
		}
		appID, err := strconv.ParseUint(req.TencentCaptchaAppID, 10, 64)
		if err != nil || appID == 0 {
			response.BadRequest(c, "Tencent Captcha CaptchaAppId must be a positive integer when enabled")
			return
		}
		if req.TencentCaptchaAppSecretKey == "" {
			req.TencentCaptchaAppSecretKey = previousSettings.TencentCaptchaAppSecretKey
		}
		if req.TencentCaptchaCloudSecretID == "" {
			req.TencentCaptchaCloudSecretID = previousSettings.TencentCaptchaCloudSecretID
		}
		if req.TencentCaptchaCloudSecretKey == "" {
			req.TencentCaptchaCloudSecretKey = previousSettings.TencentCaptchaCloudSecretKey
		}
		if req.TencentCaptchaAppSecretKey == "" {
			response.BadRequest(c, "Tencent Captcha AppSecretKey is required when enabled")
			return
		}
		if req.TencentCaptchaCloudSecretID == "" {
			response.BadRequest(c, "Tencent Cloud SecretId is required when Tencent Captcha is enabled")
			return
		}
		if req.TencentCaptchaCloudSecretKey == "" {
			response.BadRequest(c, "Tencent Cloud SecretKey is required when Tencent Captcha is enabled")
			return
		}
	}

	// 阿里云验证码 2.0 参数验证
	if aliyunCaptchaEnabled {
		if _, sent := sentFields["aliyun_captcha_scene_id"]; !sent {
			req.AliyunCaptchaSceneID = previousSettings.AliyunCaptchaSceneID
		}
		if _, sent := sentFields["aliyun_captcha_prefix"]; !sent {
			req.AliyunCaptchaPrefix = previousSettings.AliyunCaptchaPrefix
		}
		if _, sent := sentFields["aliyun_captcha_access_key_id"]; !sent {
			req.AliyunCaptchaAccessKeyID = previousSettings.AliyunCaptchaAccessKeyID
		}
		if req.AliyunCaptchaSceneID == "" {
			response.BadRequest(c, "Aliyun Captcha Scene ID is required when enabled")
			return
		}
		if req.AliyunCaptchaPrefix == "" {
			response.BadRequest(c, "Aliyun Captcha Prefix is required when enabled")
			return
		}
		if req.AliyunCaptchaAccessKeyID == "" {
			response.BadRequest(c, "Aliyun Captcha AccessKey ID is required when enabled")
			return
		}
		// 如果未提供 AccessKey Secret，使用已保存的值（留空保留当前值）
		if req.AliyunCaptchaAccessKeySecret == "" {
			if previousSettings.AliyunCaptchaAccessKeySecret == "" {
				response.BadRequest(c, "Aliyun Captcha AccessKey Secret is required when enabled")
				return
			}
			req.AliyunCaptchaAccessKeySecret = previousSettings.AliyunCaptchaAccessKeySecret
		}

		// 凭证任一变化时真实调用一次阿里云校验（避免配置错误导致无法登录）
		credentialsChanged := previousSettings.AliyunCaptchaAccessKeyID != req.AliyunCaptchaAccessKeyID ||
			previousSettings.AliyunCaptchaAccessKeySecret != req.AliyunCaptchaAccessKeySecret ||
			previousSettings.AliyunCaptchaSceneID != req.AliyunCaptchaSceneID ||
			previousSettings.AliyunCaptchaRegion != req.AliyunCaptchaRegion
		if credentialsChanged {
			if err := h.aliyunCaptchaService.ValidateCredentials(c.Request.Context(), req.AliyunCaptchaAccessKeyID, req.AliyunCaptchaAccessKeySecret, req.AliyunCaptchaSceneID, req.AliyunCaptchaRegion); err != nil {
				response.ErrorFrom(c, err)
				return
			}
		}
	}

	// TOTP 双因素认证参数验证
	// 只有手动配置了加密密钥才允许启用 TOTP 功能
	if req.TotpEnabled && !previousSettings.TotpEnabled {
		// 尝试启用 TOTP，检查加密密钥是否已手动配置
		if !h.settingService.IsTotpEncryptionKeyConfigured() {
			response.BadRequest(c, "Cannot enable TOTP: TOTP_ENCRYPTION_KEY environment variable must be configured first. Generate a key with 'openssl rand -hex 32' and set it in your environment.")
			return
		}
	}
	loginAgreementMode := strings.ToLower(strings.TrimSpace(req.LoginAgreementMode))
	if loginAgreementMode == "" {
		loginAgreementMode = strings.ToLower(strings.TrimSpace(previousSettings.LoginAgreementMode))
	}
	switch loginAgreementMode {
	case "", "modal":
		loginAgreementMode = "modal"
	case "checkbox":
	default:
		response.BadRequest(c, "Login agreement mode must be modal or checkbox")
		return
	}
	loginAgreementUpdatedAt := strings.TrimSpace(req.LoginAgreementUpdatedAt)
	if loginAgreementUpdatedAt == "" {
		loginAgreementUpdatedAt = strings.TrimSpace(previousSettings.LoginAgreementUpdatedAt)
	}
	loginAgreementDocuments := loginAgreementDocumentsToService(req.LoginAgreementDocuments)
	if len(loginAgreementDocuments) == 0 {
		loginAgreementDocuments = previousSettings.LoginAgreementDocuments
	}
	for _, doc := range loginAgreementDocuments {
		if strings.TrimSpace(doc.Title) == "" {
			response.BadRequest(c, "Login agreement document title is required")
			return
		}
		if len(doc.Title) > 80 {
			response.BadRequest(c, "Login agreement document title is too long (max 80 characters)")
			return
		}
		if len(doc.ContentMD) > 200*1024 {
			response.BadRequest(c, "Login agreement document content is too large (max 200KB)")
			return
		}
	}
	if req.LoginAgreementEnabled && len(loginAgreementDocuments) == 0 {
		response.BadRequest(c, "Login agreement documents are required when enabled")
		return
	}

	// Frontend URL 验证
	req.FrontendURL = strings.TrimSpace(req.FrontendURL)
	if req.FrontendURL != "" {
		if err := config.ValidateAbsoluteHTTPURL(req.FrontendURL); err != nil {
			response.BadRequest(c, "Frontend URL must be an absolute http(s) URL")
			return
		}
	}

	// 自定义端点验证
	const (
		maxCustomEndpoints        = 10
		maxEndpointNameLen        = 50
		maxEndpointURLLen         = 2048
		maxEndpointDescriptionLen = 200
	)

	customEndpointsJSON := previousSettings.CustomEndpoints
	if req.CustomEndpoints != nil {
		endpoints := *req.CustomEndpoints
		if len(endpoints) > maxCustomEndpoints {
			response.BadRequest(c, "Too many custom endpoints (max 10)")
			return
		}
		for _, ep := range endpoints {
			if strings.TrimSpace(ep.Name) == "" {
				response.BadRequest(c, "Custom endpoint name is required")
				return
			}
			if len(ep.Name) > maxEndpointNameLen {
				response.BadRequest(c, "Custom endpoint name is too long (max 50 characters)")
				return
			}
			if strings.TrimSpace(ep.Endpoint) == "" {
				response.BadRequest(c, "Custom endpoint URL is required")
				return
			}
			if len(ep.Endpoint) > maxEndpointURLLen {
				response.BadRequest(c, "Custom endpoint URL is too long (max 2048 characters)")
				return
			}
			if err := config.ValidateAbsoluteHTTPURL(strings.TrimSpace(ep.Endpoint)); err != nil {
				response.BadRequest(c, "Custom endpoint URL must be an absolute http(s) URL")
				return
			}
			if len(ep.Description) > maxEndpointDescriptionLen {
				response.BadRequest(c, "Custom endpoint description is too long (max 200 characters)")
				return
			}
		}
		endpointBytes, err := json.Marshal(endpoints)
		if err != nil {
			response.BadRequest(c, "Failed to serialize custom endpoints")
			return
		}
		customEndpointsJSON = string(endpointBytes)
	}

	// Ops metrics collector interval validation (seconds).
	if req.OpsMetricsIntervalSeconds != nil {
		v := *req.OpsMetricsIntervalSeconds
		if v < 60 {
			v = 60
		}
		if v > 3600 {
			v = 3600
		}
		req.OpsMetricsIntervalSeconds = &v
	}

	// 验证最低版本号格式（空字符串=禁用，或合法 semver）
	if req.MinClaudeCodeVersion != "" {
		if !semverPattern.MatchString(req.MinClaudeCodeVersion) {
			response.Error(c, http.StatusBadRequest, "min_claude_code_version must be empty or a valid semver (e.g. 2.1.63)")
			return
		}
	}

	// 验证最高版本号格式（空字符串=禁用，或合法 semver）
	if req.MaxClaudeCodeVersion != "" {
		if !semverPattern.MatchString(req.MaxClaudeCodeVersion) {
			response.Error(c, http.StatusBadRequest, "max_claude_code_version must be empty or a valid semver (e.g. 3.0.0)")
			return
		}
	}

	if req.OpenAICodexOriginator != nil {
		raw := *req.OpenAICodexOriginator
		normalized := service.NormalizeCodexOriginatorHeader(raw)
		// NormalizeCodexOriginatorHeader trims Unicode whitespace for reuse by
		// runtime reads. At the write boundary, only surrounding ASCII spaces
		// are allowed; CR/LF, tabs, controls, and non-ASCII must be rejected.
		trimmedASCII := strings.Trim(raw, " ")
		if trimmedASCII != "" && (normalized == "" || normalized != trimmedASCII) {
			response.Error(c, http.StatusBadRequest, "openai_codex_originator must be printable ASCII, contain no slash, and be at most 64 characters")
			return
		}
		req.OpenAICodexOriginator = &normalized
	}
	if req.OpenAICodexUserAgent != nil {
		raw := *req.OpenAICodexUserAgent
		if !service.IsValidCodexUserAgentHeader(raw) {
			response.Error(c, http.StatusBadRequest, "openai_codex_user_agent must be empty or a printable ASCII client/version value at most 512 characters")
			return
		}
		normalized := service.NormalizeCodexUserAgentHeader(raw)
		req.OpenAICodexUserAgent = &normalized
	}
	if req.OpenAICodexClientVersionMode != nil {
		normalized := service.NormalizeOpenAICodexClientVersionMode(*req.OpenAICodexClientVersionMode)
		if normalized == "" {
			response.Error(c, http.StatusBadRequest, "openai_codex_client_version_mode must be auto or pinned")
			return
		}
		req.OpenAICodexClientVersionMode = &normalized
	}
	if req.OpenAICodexClientVersion != nil {
		// 该值是 UA engine 与 Responses/WS Version 共用的稳定版，支持自动下限和固定版本；
		// 与后续持久化共用同一严格 X.Y.Z 校验，避免 API 接受后又被服务层静默清空。
		normalized := strings.TrimSpace(*req.OpenAICodexClientVersion)
		if normalized != "" && service.NormalizeStableCodexClientVersion(normalized) == "" {
			response.Error(c, http.StatusBadRequest, "openai_codex_client_version must be empty or a stable X.Y.Z version (e.g. 0.154.0)")
			return
		}
		req.OpenAICodexClientVersion = &normalized
	}
	if req.ClaudeCodeClientVersion != nil {
		// 该值会被拼进出站 User-Agent 与 billing attribution，必须是合法版本号；空串表示跟随自动同步。
		normalized := strings.TrimSpace(*req.ClaudeCodeClientVersion)
		if normalized != "" && service.NormalizeClaudeCodeClientVersion(normalized) == "" {
			response.Error(c, http.StatusBadRequest, "claude_code_client_version must be empty or a valid version (e.g. 2.1.258)")
			return
		}
		req.ClaudeCodeClientVersion = &normalized
	}

	// codex_cli_only 加固：最低/最高 Codex 版本（空=禁用，或合法 semver；max>=min）
	if req.MinCodexVersion != "" && !semverPattern.MatchString(req.MinCodexVersion) {
		response.Error(c, http.StatusBadRequest, "min_codex_version must be empty or a valid semver (e.g. 0.141.0)")
		return
	}
	if req.MaxCodexVersion != "" && !semverPattern.MatchString(req.MaxCodexVersion) {
		response.Error(c, http.StatusBadRequest, "max_codex_version must be empty or a valid semver (e.g. 0.200.0)")
		return
	}
	if req.MinCodexVersion != "" && req.MaxCodexVersion != "" && service.CompareVersions(req.MaxCodexVersion, req.MinCodexVersion) < 0 {
		response.Error(c, http.StatusBadRequest, "max_codex_version must be greater than or equal to min_codex_version")
		return
	}
	// codex_cli_only 黑/白名单：非空须为合法 []AllowedClientEntry JSON。
	// 黑名单 OR 宽 deny（允许 originator-only）；白名单双因子 AND，额外要求每条可命中（非空 originator + ua_contains）。
	if err := service.ValidateCodexClientEntriesJSON(req.CodexCLIOnlyBlacklist); err != nil {
		response.Error(c, http.StatusBadRequest, "codex_cli_only_blacklist "+err.Error())
		return
	}
	if err := service.ValidateCodexWhitelistEntriesJSON(req.CodexCLIOnlyWhitelist); err != nil {
		response.Error(c, http.StatusBadRequest, "codex_cli_only_whitelist "+err.Error())
		return
	}
	if err := service.ValidateEngineFingerprintSignalsJSON(req.CodexCLIOnlyEngineFingerprintSignals); err != nil {
		response.Error(c, http.StatusBadRequest, "codex_cli_only_engine_fingerprint_signals "+err.Error())
		return
	}

	// 交叉验证：如果同时设置了最低和最高版本号，最高版本号必须 >= 最低版本号
	if req.MinClaudeCodeVersion != "" && req.MaxClaudeCodeVersion != "" {
		if service.CompareVersions(req.MaxClaudeCodeVersion, req.MinClaudeCodeVersion) < 0 {
			response.Error(c, http.StatusBadRequest, "max_claude_code_version must be greater than or equal to min_claude_code_version")
			return
		}
	}

	if req.CyberPolicyUserAllowlist != nil {
		if _, err := service.ParseCyberPolicyUserAllowlist(*req.CyberPolicyUserAllowlist); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	// cyber 会话屏蔽 TTL 校验：提供时必须 > 0
	if req.CyberSessionBlockTTLSeconds != nil && *req.CyberSessionBlockTTLSeconds <= 0 {
		response.BadRequest(c, "cyber_session_block_ttl_seconds must be > 0")
		return
	}

	settings := &service.SystemSettings{

		AccountSchedulingThresholds: req.AccountSchedulingThresholds,

		FrontendURL: req.FrontendURL,

		TotpEnabled: req.TotpEnabled,

		SessionBindingEnabled:   sessionBindingEnabled,
		StepUpEnabled:           stepUpEnabled,
		AuditLogRetentionDays:   req.AuditLogRetentionDays,
		LoginAgreementEnabled:   req.LoginAgreementEnabled,
		LoginAgreementMode:      loginAgreementMode,
		LoginAgreementUpdatedAt: loginAgreementUpdatedAt,
		LoginAgreementDocuments: loginAgreementDocuments,

		TurnstileEnabled:             req.TurnstileEnabled,
		TurnstileSiteKey:             req.TurnstileSiteKey,
		TurnstileSecretKey:           req.TurnstileSecretKey,
		TencentCaptchaEnabled:        req.TencentCaptchaEnabled,
		TencentCaptchaAppID:          req.TencentCaptchaAppID,
		TencentCaptchaAppSecretKey:   req.TencentCaptchaAppSecretKey,
		TencentCaptchaCloudSecretID:  req.TencentCaptchaCloudSecretID,
		TencentCaptchaCloudSecretKey: req.TencentCaptchaCloudSecretKey,
		TencentCaptchaRegion:         req.TencentCaptchaRegion,
		AliyunCaptchaEnabled:         req.AliyunCaptchaEnabled,
		AliyunCaptchaAccessKeyID:     req.AliyunCaptchaAccessKeyID,
		AliyunCaptchaAccessKeySecret: req.AliyunCaptchaAccessKeySecret,
		AliyunCaptchaSceneID:         req.AliyunCaptchaSceneID,
		AliyunCaptchaPrefix:          req.AliyunCaptchaPrefix,
		AliyunCaptchaRegion:          req.AliyunCaptchaRegion,
		APIKeyACLTrustForwardedIP: func() bool {
			if req.APIKeyACLTrustForwardedIP != nil {
				return *req.APIKeyACLTrustForwardedIP
			}
			return previousSettings.APIKeyACLTrustForwardedIP
		}(),
		ForwardedClientIPHeaders: forwardedClientIPHeaders,

		SiteName:            req.SiteName,
		SiteLogo:            req.SiteLogo,
		SiteSubtitle:        req.SiteSubtitle,
		APIBaseURL:          req.APIBaseURL,
		ContactInfo:         req.ContactInfo,
		DocURL:              req.DocURL,
		HomeContent:         req.HomeContent,
		CompactHomeEnabled:  req.CompactHomeEnabled,
		HideCcsImportButton: req.HideCcsImportButton,

		TableDefaultPageSize: req.TableDefaultPageSize,
		TablePageSizeOptions: req.TablePageSizeOptions,

		CustomEndpoints: customEndpointsJSON,

		EnableModelFallback:    req.EnableModelFallback,
		FallbackModelAnthropic: req.FallbackModelAnthropic,
		FallbackModelOpenAI:    req.FallbackModelOpenAI,

		MinClaudeCodeVersion:        req.MinClaudeCodeVersion,
		MaxClaudeCodeVersion:        req.MaxClaudeCodeVersion,
		AllowUngroupedKeyScheduling: req.AllowUngroupedKeyScheduling,
		BackendModeEnabled:          req.BackendModeEnabled,
		AllowUserViewErrorRequests: func() bool {
			if req.AllowUserViewErrorRequests != nil {
				return *req.AllowUserViewErrorRequests
			}
			return previousSettings.AllowUserViewErrorRequests
		}(),
		OpsMonitoringEnabled: func() bool {
			if req.OpsMonitoringEnabled != nil {
				return *req.OpsMonitoringEnabled
			}
			return previousSettings.OpsMonitoringEnabled
		}(),
		OpsRealtimeMonitoringEnabled: func() bool {
			if req.OpsRealtimeMonitoringEnabled != nil {
				return *req.OpsRealtimeMonitoringEnabled
			}
			return previousSettings.OpsRealtimeMonitoringEnabled
		}(),
		OpsQueryModeDefault: func() string {
			if req.OpsQueryModeDefault != nil {
				return *req.OpsQueryModeDefault
			}
			return previousSettings.OpsQueryModeDefault
		}(),
		OpsMetricsIntervalSeconds: func() int {
			if req.OpsMetricsIntervalSeconds != nil {
				return *req.OpsMetricsIntervalSeconds
			}
			return previousSettings.OpsMetricsIntervalSeconds
		}(),
		EnableFingerprintUnification: func() bool {
			if req.EnableFingerprintUnification != nil {
				return *req.EnableFingerprintUnification
			}
			return previousSettings.EnableFingerprintUnification
		}(),
		OpenAITTFTMode: func() string {
			if req.OpenAITTFTMode != nil {
				return *req.OpenAITTFTMode
			}
			return previousSettings.OpenAITTFTMode
		}(),
		EnableMetadataPassthrough: func() bool {
			if req.EnableMetadataPassthrough != nil {
				return *req.EnableMetadataPassthrough
			}
			return previousSettings.EnableMetadataPassthrough
		}(),
		EnableCCHSigning: func() bool {
			if req.EnableCCHSigning != nil {
				return *req.EnableCCHSigning
			}
			return previousSettings.EnableCCHSigning
		}(),
		EnableClaudeOAuthSystemPromptInjection: func() bool {
			if req.EnableClaudeOAuthSystemPromptInjection != nil {
				return *req.EnableClaudeOAuthSystemPromptInjection
			}
			return previousSettings.EnableClaudeOAuthSystemPromptInjection
		}(),
		ClaudeOAuthSystemPrompt: func() string {
			if req.ClaudeOAuthSystemPrompt != nil {
				return *req.ClaudeOAuthSystemPrompt
			}
			return previousSettings.ClaudeOAuthSystemPrompt
		}(),
		ClaudeOAuthSystemPromptBlocks: func() string {
			if req.ClaudeOAuthSystemPromptBlocks != nil {
				return *req.ClaudeOAuthSystemPromptBlocks
			}
			return previousSettings.ClaudeOAuthSystemPromptBlocks
		}(),
		EnableAnthropicCacheTTL1hInjection: func() bool {
			if req.EnableAnthropicCacheTTL1hInjection != nil {
				return *req.EnableAnthropicCacheTTL1hInjection
			}
			return previousSettings.EnableAnthropicCacheTTL1hInjection
		}(),
		RewriteMessageCacheControl: func() bool {
			if req.RewriteMessageCacheControl != nil {
				return *req.RewriteMessageCacheControl
			}
			return previousSettings.RewriteMessageCacheControl
		}(),
		EnableClientDatelineNormalization: func() bool {
			if req.EnableClientDatelineNormalization != nil {
				return *req.EnableClientDatelineNormalization
			}
			return previousSettings.EnableClientDatelineNormalization
		}(),

		OpenAICodexOriginator: func() string {
			if req.OpenAICodexOriginator != nil {
				return *req.OpenAICodexOriginator
			}
			return previousSettings.OpenAICodexOriginator
		}(),
		OpenAICodexUserAgent: func() string {
			if req.OpenAICodexUserAgent != nil {
				return *req.OpenAICodexUserAgent
			}
			return previousSettings.OpenAICodexUserAgent
		}(),
		OpenAICodexClientVersion: func() string {
			if req.OpenAICodexClientVersion != nil {
				return *req.OpenAICodexClientVersion
			}
			return previousSettings.OpenAICodexClientVersion
		}(),
		// 同步值由自动同步任务独占写入，面板保存时原样带回，避免被清空。
		OpenAICodexClientVersionMode: func() string {
			if req.OpenAICodexClientVersionMode != nil {
				return *req.OpenAICodexClientVersionMode
			}
			return previousSettings.OpenAICodexClientVersionMode
		}(),
		OpenAICodexClientVersionSynced: previousSettings.OpenAICodexClientVersionSynced,
		OpenAICodexVersionAutoSyncEnabled: func() bool {
			if req.OpenAICodexVersionAutoSyncEnabled != nil {
				return *req.OpenAICodexVersionAutoSyncEnabled
			}
			return previousSettings.OpenAICodexVersionAutoSyncEnabled
		}(),
		EnableOpenAIAccountLocalDeviceIdentity: func() bool {
			if req.EnableOpenAIAccountLocalDeviceIdentity != nil {
				return *req.EnableOpenAIAccountLocalDeviceIdentity
			}
			return previousSettings.EnableOpenAIAccountLocalDeviceIdentity
		}(),
		ClaudeCodeClientVersion: func() string {
			if req.ClaudeCodeClientVersion != nil {
				return *req.ClaudeCodeClientVersion
			}
			return previousSettings.ClaudeCodeClientVersion
		}(),
		// 同步值由自动同步任务独占写入，面板保存时原样带回，避免被清空。
		ClaudeCodeClientVersionSynced: previousSettings.ClaudeCodeClientVersionSynced,
		ClaudeCodeVersionAutoSyncEnabled: func() bool {
			if req.ClaudeCodeVersionAutoSyncEnabled != nil {
				return *req.ClaudeCodeVersionAutoSyncEnabled
			}
			return previousSettings.ClaudeCodeVersionAutoSyncEnabled
		}(),
		MinCodexVersion:       strings.TrimSpace(req.MinCodexVersion),
		MaxCodexVersion:       strings.TrimSpace(req.MaxCodexVersion),
		CodexCLIOnlyBlacklist: strings.TrimSpace(req.CodexCLIOnlyBlacklist),
		CodexCLIOnlyWhitelist: strings.TrimSpace(req.CodexCLIOnlyWhitelist),
		CodexCLIOnlyAllowAppServerClients: func() bool {
			if req.CodexCLIOnlyAllowAppServerClients != nil {
				return *req.CodexCLIOnlyAllowAppServerClients
			}
			return previousSettings.CodexCLIOnlyAllowAppServerClients
		}(),
		CodexCLIOnlyEngineFingerprintSignals: strings.TrimSpace(req.CodexCLIOnlyEngineFingerprintSignals),

		OpenAILowUpstreamRatePriorityEnabled: func() bool {
			if req.OpenAILowUpstreamRatePriorityEnabled != nil {
				return *req.OpenAILowUpstreamRatePriorityEnabled
			}
			return previousSettings.OpenAILowUpstreamRatePriorityEnabled
		}(),
		OpenAIOAuthSchedulingRateMultiplier: func() float64 {
			if req.OpenAIOAuthSchedulingRateMultiplier != nil {
				return *req.OpenAIOAuthSchedulingRateMultiplier
			}
			return previousSettings.OpenAIOAuthSchedulingRateMultiplier
		}(),
		OpenAIAdvancedSchedulerEnabled: func() bool {
			if req.OpenAIAdvancedSchedulerEnabled != nil {
				return *req.OpenAIAdvancedSchedulerEnabled
			}
			return previousSettings.OpenAIAdvancedSchedulerEnabled
		}(),
		OpenAIAdvancedSchedulerStickyWeightedEnabled: func() bool {
			if req.OpenAIAdvancedSchedulerStickyWeightedEnabled != nil {
				return *req.OpenAIAdvancedSchedulerStickyWeightedEnabled
			}
			return previousSettings.OpenAIAdvancedSchedulerStickyWeightedEnabled
		}(),
		OpenAIAdvancedSchedulerSubscriptionPriorityEnabled: func() bool {
			if req.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled != nil {
				return *req.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled
			}
			return previousSettings.OpenAIAdvancedSchedulerSubscriptionPriorityEnabled
		}(),
		OpenAIAdvancedSchedulerLBTopK:                 stringSetting(req.OpenAIAdvancedSchedulerLBTopK, previousSettings.OpenAIAdvancedSchedulerLBTopK),
		OpenAIAdvancedSchedulerWeightPriority:         stringSetting(req.OpenAIAdvancedSchedulerWeightPriority, previousSettings.OpenAIAdvancedSchedulerWeightPriority),
		OpenAIAdvancedSchedulerWeightLoad:             stringSetting(req.OpenAIAdvancedSchedulerWeightLoad, previousSettings.OpenAIAdvancedSchedulerWeightLoad),
		OpenAIAdvancedSchedulerWeightQueue:            stringSetting(req.OpenAIAdvancedSchedulerWeightQueue, previousSettings.OpenAIAdvancedSchedulerWeightQueue),
		OpenAIAdvancedSchedulerWeightErrorRate:        stringSetting(req.OpenAIAdvancedSchedulerWeightErrorRate, previousSettings.OpenAIAdvancedSchedulerWeightErrorRate),
		OpenAIAdvancedSchedulerWeightTTFT:             stringSetting(req.OpenAIAdvancedSchedulerWeightTTFT, previousSettings.OpenAIAdvancedSchedulerWeightTTFT),
		OpenAIAdvancedSchedulerWeightReset:            stringSetting(req.OpenAIAdvancedSchedulerWeightReset, previousSettings.OpenAIAdvancedSchedulerWeightReset),
		OpenAIAdvancedSchedulerWeightQuotaHeadroom:    stringSetting(req.OpenAIAdvancedSchedulerWeightQuotaHeadroom, previousSettings.OpenAIAdvancedSchedulerWeightQuotaHeadroom),
		OpenAIAdvancedSchedulerWeightUpstreamCost:     stringSetting(req.OpenAIAdvancedSchedulerWeightUpstreamCost, previousSettings.OpenAIAdvancedSchedulerWeightUpstreamCost),
		OpenAIAdvancedSchedulerWeightPreviousResponse: stringSetting(req.OpenAIAdvancedSchedulerWeightPreviousResponse, previousSettings.OpenAIAdvancedSchedulerWeightPreviousResponse),
		OpenAIAdvancedSchedulerWeightSessionSticky:    stringSetting(req.OpenAIAdvancedSchedulerWeightSessionSticky, previousSettings.OpenAIAdvancedSchedulerWeightSessionSticky),

		AccountQuotaNotifyEnabled: func() bool {
			if req.AccountQuotaNotifyEnabled != nil {
				return *req.AccountQuotaNotifyEnabled
			}
			return previousSettings.AccountQuotaNotifyEnabled
		}(),
		AccountQuotaNotifyEmails: func() []service.NotifyEmailEntry {
			if req.AccountQuotaNotifyEmails != nil {
				return dto.NotifyEmailEntriesToService(*req.AccountQuotaNotifyEmails)
			}
			return previousSettings.AccountQuotaNotifyEmails
		}(),
		ChannelMonitorEnabled: func() bool {
			if req.ChannelMonitorEnabled != nil {
				return *req.ChannelMonitorEnabled
			}
			return previousSettings.ChannelMonitorEnabled
		}(),
		ChannelMonitorMode: func() string {
			if req.ChannelMonitorMode != nil {
				return *req.ChannelMonitorMode
			}
			return previousSettings.ChannelMonitorMode
		}(),
		ChannelMonitorDefaultIntervalSeconds: func() int {
			if req.ChannelMonitorDefaultIntervalSeconds != nil {
				return *req.ChannelMonitorDefaultIntervalSeconds
			}
			return previousSettings.ChannelMonitorDefaultIntervalSeconds
		}(),
		ChannelMonitorHideThroughput: func() bool {
			if req.ChannelMonitorHideThroughput != nil {
				return *req.ChannelMonitorHideThroughput
			}
			return previousSettings.ChannelMonitorHideThroughput
		}(),
		ChannelMonitorShowQuota: func() bool {
			if req.ChannelMonitorShowQuota != nil {
				return *req.ChannelMonitorShowQuota
			}
			return previousSettings.ChannelMonitorShowQuota
		}(),

		GrokDefaultTextModel: func() string {
			if req.GrokDefaultTextModel != nil {
				return *req.GrokDefaultTextModel
			}
			return previousSettings.GrokDefaultTextModel
		}(),
		GrokCrossClientModelMapEnabled: func() bool {
			if req.GrokCrossClientModelMapEnabled != nil {
				return *req.GrokCrossClientModelMapEnabled
			}
			return previousSettings.GrokCrossClientModelMapEnabled
		}(),
		GrokDefaultBaseURLMode: func() string {
			if req.GrokDefaultBaseURLMode != nil {
				return strings.TrimSpace(*req.GrokDefaultBaseURLMode)
			}
			return previousSettings.GrokDefaultBaseURLMode
		}(),
		AvailableChannelsEnabled: func() bool {
			if req.AvailableChannelsEnabled != nil {
				return *req.AvailableChannelsEnabled
			}
			return previousSettings.AvailableChannelsEnabled
		}(),

		ModelPlazaEnabled: func() bool {
			if req.ModelPlazaEnabled != nil {
				return *req.ModelPlazaEnabled
			}
			return previousSettings.ModelPlazaEnabled
		}(),
		ModelPlazaRequireAuth: func() bool {
			if req.ModelPlazaRequireAuth != nil {
				return *req.ModelPlazaRequireAuth
			}
			return previousSettings.ModelPlazaRequireAuth
		}(),
		ModelPlazaDescription: func() string {
			if req.ModelPlazaDescription != nil {
				return *req.ModelPlazaDescription
			}
			return previousSettings.ModelPlazaDescription
		}(),
		PluginManagementEnabled: func() bool {
			if req.PluginManagementEnabled != nil {
				return *req.PluginManagementEnabled
			}
			return previousSettings.PluginManagementEnabled
		}(),

		RiskControlEnabled: func() bool {
			if req.RiskControlEnabled != nil {
				return *req.RiskControlEnabled
			}
			return previousSettings.RiskControlEnabled
		}(),
		CyberPolicyUserAllowlist: func() string {
			if req.CyberPolicyUserAllowlist != nil {
				return *req.CyberPolicyUserAllowlist
			}
			return previousSettings.CyberPolicyUserAllowlist
		}(),
		CyberSessionBlockEnabled: func() bool {
			if req.CyberSessionBlockEnabled != nil {
				return *req.CyberSessionBlockEnabled
			}
			return previousSettings.CyberSessionBlockEnabled
		}(),
		CyberSessionBlockTTLSeconds: func() int {
			if req.CyberSessionBlockTTLSeconds != nil {
				return *req.CyberSessionBlockTTLSeconds
			}
			return previousSettings.CyberSessionBlockTTLSeconds
		}(),
	}

	if err := h.settingService.UpdateSettingsOmitting(c.Request.Context(), settings, omitted); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	if h.opsService != nil {
		h.opsService.SetMonitoringEnabled(settings.OpsMonitoringEnabled)
	}

	// Update OpenAI fast policy (stored under dedicated key, only when provided).
	if req.OpenAIFastPolicySettings != nil {
		if err := h.settingService.SetOpenAIFastPolicySettings(c.Request.Context(), openaiFastPolicySettingsFromDTO(req.OpenAIFastPolicySettings)); err != nil {
			response.BadRequest(c, err.Error())
			return
		}
	}

	updatedSettings, err := h.settingService.GetAllSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.auditSettingsUpdate(c, previousSettings, updatedSettings, auditReq)
	h.GetSettings(c)
}

// Reject stale clients explicitly so a successful save never implies that a
// removed feature can be re-enabled. Unrelated response-only fields remain
// compatible with older clients.
func retiredSettingField(field string) bool {
	for _, prefix := range []string{
		"registration_", "email_verify_", "password_reset_", "passkey_",
		"linuxdo_", "wechat_", "dingtalk_", "oidc_", "github_oauth_", "google_oauth_",
		"auth_source_default_", "payment_", "smtp_", "email_template_",
		"promo_code_", "invitation_code_", "purchase_subscription_", "affiliate_", "balance_low_notify_",
		"default_subscriptions", "default_balance", "default_concurrency",
		"default_user_rpm_limit", "default_platform_quotas", "custom_menu_",
		"subscription_expiry_", "gemini_", "antigravity_", "fallback_model_gemini", "fallback_model_antigravity",
	} {
		if strings.HasPrefix(field, prefix) {
			return true
		}
	}
	return field == "channel_monitor_hide_user_ranking" || field == "subscription_enabled" || field == "force_email_on_third_party_signup"
}
