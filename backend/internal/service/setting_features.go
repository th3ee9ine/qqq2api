package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// IsTotpEnabled 检查是否启用 TOTP 双因素认证功能
func (s *SettingService) IsTotpEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyTotpEnabled)
	if err != nil {
		return false // 默认关闭
	}
	return value == "true"
}

// IsTotpEncryptionKeyConfigured 检查 TOTP 加密密钥是否已手动配置
// 只有手动配置了密钥才允许在管理后台启用 TOTP 功能
func (s *SettingService) IsTotpEncryptionKeyConfigured() bool {
	return s.cfg.Totp.EncryptionKeyConfigured
}

// IsSessionBindingEnabled 检查会话 IP/UA 绑定是否启用（默认关闭）。
// 开启时会话与登录时的 IP/User-Agent 绑定，任一变化立即失效并撤销该会话。
// 默认关闭：移动网络/多出口 IP 场景下 IP 频繁变化会导致登录后立即掉线。
func (s *SettingService) IsSessionBindingEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeySessionBindingEnabled)
	if err != nil {
		return false // 默认关闭
	}
	return value == "true"
}

// IsStepUpEnabled 检查敏感操作 step-up 2FA 门控是否启用（默认关闭）。
// 开启时账号/代理导出、提升管理员等敏感操作要求当前会话在有效期内
// 完成过 TOTP step-up 验证。
func (s *SettingService) IsStepUpEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyStepUpEnabled)
	if err != nil {
		return false // 默认关闭
	}
	return value == "true"
}

// defaultAuditLogRetentionDays 审计日志默认保留天数。
const defaultAuditLogRetentionDays = 180

// GetAuditLogRetentionDays 审计日志保留天数（<=0 表示永久保留，仅支持手动清空）。
func (s *SettingService) GetAuditLogRetentionDays(ctx context.Context) int {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAuditLogRetentionDays)
	if err != nil {
		return defaultAuditLogRetentionDays
	}
	return parseAuditLogRetentionDays(value)
}

// parseAuditLogRetentionDays 解析保留天数配置，空/非法值回退默认值。
func parseAuditLogRetentionDays(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultAuditLogRetentionDays
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return defaultAuditLogRetentionDays
	}
	if n < 0 {
		return 0
	}
	return n
}

// GetSiteName 获取网站名称
func (s *SettingService) GetSiteName(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeySiteName)
	if err != nil || value == "" {
		return "QQQ2API"
	}
	return value
}

// IsTurnstileEnabled 检查是否启用 Turnstile 验证
func (s *SettingService) IsTurnstileEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyTurnstileEnabled)
	if err != nil {
		return false
	}
	return value == "true"
}

// GetTurnstileSecretKey 获取 Turnstile Secret Key
func (s *SettingService) GetTurnstileSecretKey(ctx context.Context) string {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyTurnstileSecretKey)
	if err != nil {
		return ""
	}
	return value
}

// TencentCaptchaConfig contains the credentials required by Tencent Cloud's
// ticket verification API. It must never be returned by a public handler.
type TencentCaptchaConfig struct {
	Enabled        bool
	AppID          string
	AppSecretKey   string
	CloudSecretID  string
	CloudSecretKey string
	Region         string
}

// AliyunCaptchaConfig contains the credentials required by Aliyun Captcha 2.0's
// server-side verification API. It must never be returned by a public handler.
type AliyunCaptchaConfig struct {
	Enabled         bool
	AccessKeyID     string
	AccessKeySecret string
	SceneID         string
	Region          string
}

type CaptchaProviderConfig struct {
	TurnstileEnabled   bool
	TurnstileSecretKey string
	Tencent            TencentCaptchaConfig
	Aliyun             AliyunCaptchaConfig
}

func (s *SettingService) GetCaptchaProviderConfig(ctx context.Context) (CaptchaProviderConfig, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyTurnstileEnabled,
		SettingKeyTurnstileSecretKey,
		SettingKeyTencentCaptchaEnabled,
		SettingKeyTencentCaptchaAppID,
		SettingKeyTencentCaptchaAppSecretKey,
		SettingKeyTencentCaptchaCloudSecretID,
		SettingKeyTencentCaptchaCloudSecretKey,
		SettingKeyTencentCaptchaRegion,
		SettingKeyAliyunCaptchaEnabled,
		SettingKeyAliyunCaptchaAccessKeyID,
		SettingKeyAliyunCaptchaAccessKeySecret,
		SettingKeyAliyunCaptchaSceneID,
		SettingKeyAliyunCaptchaRegion,
	})
	if err != nil {
		return CaptchaProviderConfig{}, fmt.Errorf("read captcha provider settings: %w", err)
	}
	return CaptchaProviderConfig{
		TurnstileEnabled:   values[SettingKeyTurnstileEnabled] == "true",
		TurnstileSecretKey: values[SettingKeyTurnstileSecretKey],
		Tencent: TencentCaptchaConfig{
			Enabled:        values[SettingKeyTencentCaptchaEnabled] == "true",
			AppID:          values[SettingKeyTencentCaptchaAppID],
			AppSecretKey:   values[SettingKeyTencentCaptchaAppSecretKey],
			CloudSecretID:  values[SettingKeyTencentCaptchaCloudSecretID],
			CloudSecretKey: values[SettingKeyTencentCaptchaCloudSecretKey],
			Region:         normalizeTencentCaptchaRegion(values[SettingKeyTencentCaptchaRegion]),
		},
		Aliyun: AliyunCaptchaConfig{
			Enabled:         values[SettingKeyAliyunCaptchaEnabled] == "true",
			AccessKeyID:     values[SettingKeyAliyunCaptchaAccessKeyID],
			AccessKeySecret: values[SettingKeyAliyunCaptchaAccessKeySecret],
			SceneID:         values[SettingKeyAliyunCaptchaSceneID],
			Region:          normalizeAliyunCaptchaRegion(values[SettingKeyAliyunCaptchaRegion]),
		},
	}, nil
}

func (s *SettingService) IsTencentCaptchaEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyTencentCaptchaEnabled)
	return err == nil && value == "true"
}

func (s *SettingService) GetTencentCaptchaConfig(ctx context.Context) TencentCaptchaConfig {
	config, err := s.GetCaptchaProviderConfig(ctx)
	if err != nil {
		return TencentCaptchaConfig{}
	}
	return config.Tencent
}

// GenerateAdminAPIKey 生成新的管理员 API Key
func (s *SettingService) GenerateAdminAPIKey(ctx context.Context) (string, error) {
	// 生成 32 字节随机数 = 64 位十六进制字符
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}

	key := AdminAPIKeyPrefix + hex.EncodeToString(bytes)

	// 存储到 settings 表
	if err := s.settingRepo.Set(ctx, SettingKeyAdminAPIKey, key); err != nil {
		return "", fmt.Errorf("save admin api key: %w", err)
	}

	return key, nil
}

// GetAdminAPIKeyStatus 获取管理员 API Key 状态
// 返回脱敏的 key、是否存在、错误
func (s *SettingService) GetAdminAPIKeyStatus(ctx context.Context) (maskedKey string, exists bool, err error) {
	key, err := s.settingRepo.GetValue(ctx, SettingKeyAdminAPIKey)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if key == "" {
		return "", false, nil
	}

	// 脱敏：显示前 10 位和后 4 位
	if len(key) > 14 {
		maskedKey = key[:10] + "..." + key[len(key)-4:]
	} else {
		maskedKey = key
	}

	return maskedKey, true, nil
}

// GetAdminAPIKey 获取完整的管理员 API Key（仅供内部验证使用）
// 如果未配置返回空字符串和 nil 错误，只有数据库错误时才返回 error
func (s *SettingService) GetAdminAPIKey(ctx context.Context) (string, error) {
	key, err := s.settingRepo.GetValue(ctx, SettingKeyAdminAPIKey)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return "", nil // 未配置，返回空字符串
		}
		return "", err // 数据库错误
	}
	return key, nil
}

// DeleteAdminAPIKey 删除管理员 API Key
func (s *SettingService) DeleteAdminAPIKey(ctx context.Context) error {
	return s.settingRepo.Delete(ctx, SettingKeyAdminAPIKey)
}

// IsModelFallbackEnabled 检查是否启用模型兜底机制
func (s *SettingService) IsModelFallbackEnabled(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyEnableModelFallback)
	if err != nil {
		return false // Default: disabled
	}
	return value == "true"
}

// GetFallbackModel 获取指定平台的兜底模型
func (s *SettingService) GetFallbackModel(ctx context.Context, platform string) string {
	var key string
	var defaultModel string

	switch platform {
	case PlatformAnthropic:
		key = SettingKeyFallbackModelAnthropic
		defaultModel = "claude-3-5-sonnet-20241022"
	case PlatformOpenAI:
		key = SettingKeyFallbackModelOpenAI
		defaultModel = "gpt-4o"

	default:
		return ""
	}

	value, err := s.settingRepo.GetValue(ctx, key)
	if err != nil || value == "" {
		return defaultModel
	}
	return value
}

// GetOverloadCooldownSettings 获取529过载冷却配置
func (s *SettingService) GetOverloadCooldownSettings(ctx context.Context) (*OverloadCooldownSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOverloadCooldownSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultOverloadCooldownSettings(), nil
		}
		return nil, fmt.Errorf("get overload cooldown settings: %w", err)
	}
	if value == "" {
		return DefaultOverloadCooldownSettings(), nil
	}

	var settings OverloadCooldownSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultOverloadCooldownSettings(), nil
	}

	// 修正配置值范围
	if settings.CooldownMinutes < 1 {
		settings.CooldownMinutes = 1
	}
	if settings.CooldownMinutes > 120 {
		settings.CooldownMinutes = 120
	}

	return &settings, nil
}

// SetOverloadCooldownSettings 设置529过载冷却配置
func (s *SettingService) SetOverloadCooldownSettings(ctx context.Context, settings *OverloadCooldownSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// 禁用时修正为合法值即可，不拒绝请求
	if settings.CooldownMinutes < 1 || settings.CooldownMinutes > 120 {
		if settings.Enabled {
			return fmt.Errorf("cooldown_minutes must be between 1-120")
		}
		settings.CooldownMinutes = 10 // 禁用状态下归一化为默认值
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal overload cooldown settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyOverloadCooldownSettings, string(data))
}

// GetRateLimit429CooldownSettings 获取429默认回避配置
func (s *SettingService) GetRateLimit429CooldownSettings(ctx context.Context) (*RateLimit429CooldownSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyRateLimit429CooldownSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultRateLimit429CooldownSettings(), nil
		}
		return nil, fmt.Errorf("get 429 cooldown settings: %w", err)
	}
	if value == "" {
		return DefaultRateLimit429CooldownSettings(), nil
	}

	var settings RateLimit429CooldownSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultRateLimit429CooldownSettings(), nil
	}

	if settings.CooldownSeconds < 1 {
		settings.CooldownSeconds = 1
	}
	if settings.CooldownSeconds > 7200 {
		settings.CooldownSeconds = 7200
	}

	return &settings, nil
}

// SetRateLimit429CooldownSettings 设置429默认回避配置
func (s *SettingService) SetRateLimit429CooldownSettings(ctx context.Context, settings *RateLimit429CooldownSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	if settings.CooldownSeconds < 1 || settings.CooldownSeconds > 7200 {
		if settings.Enabled {
			return fmt.Errorf("cooldown_seconds must be between 1-7200")
		}
		settings.CooldownSeconds = 5
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal 429 cooldown settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyRateLimit429CooldownSettings, string(data))
}

func (s *SettingService) GetOpenAIImagesOAuthUnavailableCooldownSettings(ctx context.Context) (*OpenAIImagesOAuthUnavailableCooldownSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIImagesOAuthUnavailableCooldownSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultOpenAIImagesOAuthUnavailableCooldownSettings(), nil
		}
		return nil, fmt.Errorf("get OpenAI images OAuth unavailable cooldown settings: %w", err)
	}
	if value == "" {
		return DefaultOpenAIImagesOAuthUnavailableCooldownSettings(), nil
	}

	var settings OpenAIImagesOAuthUnavailableCooldownSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil ||
		settings.CooldownMinutes <= 0 || settings.CooldownMinutes > openAIImagesOAuthUnavailableMaxCooldownMinutes {
		return DefaultOpenAIImagesOAuthUnavailableCooldownSettings(), nil
	}
	return &settings, nil
}

func (s *SettingService) SetOpenAIImagesOAuthUnavailableCooldownSettings(ctx context.Context, settings *OpenAIImagesOAuthUnavailableCooldownSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}
	if settings.CooldownMinutes <= 0 || settings.CooldownMinutes > openAIImagesOAuthUnavailableMaxCooldownMinutes {
		return fmt.Errorf("cooldown_minutes must be between 1-%d", openAIImagesOAuthUnavailableMaxCooldownMinutes)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal OpenAI images OAuth unavailable cooldown settings: %w", err)
	}
	return s.settingRepo.Set(ctx, SettingKeyOpenAIImagesOAuthUnavailableCooldownSettings, string(data))
}

// GetStreamTimeoutSettings 获取流超时处理配置
func (s *SettingService) GetStreamTimeoutSettings(ctx context.Context) (*StreamTimeoutSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyStreamTimeoutSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultStreamTimeoutSettings(), nil
		}
		return nil, fmt.Errorf("get stream timeout settings: %w", err)
	}
	if value == "" {
		return DefaultStreamTimeoutSettings(), nil
	}

	var settings StreamTimeoutSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultStreamTimeoutSettings(), nil
	}

	// 验证并修正配置值
	if settings.TempUnschedMinutes < 1 {
		settings.TempUnschedMinutes = 1
	}
	if settings.TempUnschedMinutes > 60 {
		settings.TempUnschedMinutes = 60
	}
	if settings.ThresholdCount < 1 {
		settings.ThresholdCount = 1
	}
	if settings.ThresholdCount > 10 {
		settings.ThresholdCount = 10
	}
	if settings.ThresholdWindowMinutes < 1 {
		settings.ThresholdWindowMinutes = 1
	}
	if settings.ThresholdWindowMinutes > 60 {
		settings.ThresholdWindowMinutes = 60
	}

	// 验证 action
	switch settings.Action {
	case StreamTimeoutActionTempUnsched, StreamTimeoutActionError, StreamTimeoutActionNone:
		// valid
	default:
		settings.Action = StreamTimeoutActionTempUnsched
	}

	return &settings, nil
}

// IsUngroupedKeySchedulingAllowed 查询是否允许未分组 Key 调度
func (s *SettingService) IsUngroupedKeySchedulingAllowed(ctx context.Context) bool {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyAllowUngroupedKeyScheduling)
	if err != nil {
		return false // fail-closed: 查询失败时默认不允许
	}
	return value == "true"
}

// GetRectifierSettings 获取请求整流器配置
func (s *SettingService) GetRectifierSettings(ctx context.Context) (*RectifierSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyRectifierSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultRectifierSettings(), nil
		}
		return nil, fmt.Errorf("get rectifier settings: %w", err)
	}
	if value == "" {
		return DefaultRectifierSettings(), nil
	}

	var settings RectifierSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultRectifierSettings(), nil
	}

	return &settings, nil
}

// SetRectifierSettings 设置请求整流器配置
func (s *SettingService) SetRectifierSettings(ctx context.Context, settings *RectifierSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal rectifier settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyRectifierSettings, string(data))
}

// IsSignatureRectifierEnabled 判断签名整流是否启用（总开关 && 签名子开关）
func (s *SettingService) IsSignatureRectifierEnabled(ctx context.Context) bool {
	settings, err := s.GetRectifierSettings(ctx)
	if err != nil {
		return true // fail-open: 查询失败时默认启用
	}
	return settings.Enabled && settings.ThinkingSignatureEnabled
}

// IsBudgetRectifierEnabled 判断 Budget 整流是否启用（总开关 && Budget 子开关）
func (s *SettingService) IsBudgetRectifierEnabled(ctx context.Context) bool {
	settings, err := s.GetRectifierSettings(ctx)
	if err != nil {
		return true // fail-open: 查询失败时默认启用
	}
	return settings.Enabled && settings.ThinkingBudgetEnabled
}

// GetBetaPolicySettings 获取 Beta 策略配置
func (s *SettingService) GetBetaPolicySettings(ctx context.Context) (*BetaPolicySettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyBetaPolicySettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultBetaPolicySettings(), nil
		}
		return nil, fmt.Errorf("get beta policy settings: %w", err)
	}
	if value == "" {
		return DefaultBetaPolicySettings(), nil
	}

	var settings BetaPolicySettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultBetaPolicySettings(), nil
	}

	return &settings, nil
}

// SetBetaPolicySettings 设置 Beta 策略配置
func (s *SettingService) SetBetaPolicySettings(ctx context.Context, settings *BetaPolicySettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	validActions := map[string]bool{
		BetaPolicyActionPass: true, BetaPolicyActionFilter: true, BetaPolicyActionBlock: true,
	}
	validScopes := map[string]bool{
		BetaPolicyScopeAll: true, BetaPolicyScopeOAuth: true, BetaPolicyScopeAPIKey: true, BetaPolicyScopeBedrock: true,
	}

	for i, rule := range settings.Rules {
		if rule.BetaToken == "" {
			return fmt.Errorf("rule[%d]: beta_token cannot be empty", i)
		}
		if !validActions[rule.Action] {
			return fmt.Errorf("rule[%d]: invalid action %q", i, rule.Action)
		}
		if !validScopes[rule.Scope] {
			return fmt.Errorf("rule[%d]: invalid scope %q", i, rule.Scope)
		}
		// Validate model_whitelist patterns
		for j, pattern := range rule.ModelWhitelist {
			trimmed := strings.TrimSpace(pattern)
			if trimmed == "" {
				return fmt.Errorf("rule[%d]: model_whitelist[%d] cannot be empty", i, j)
			}
			settings.Rules[i].ModelWhitelist[j] = trimmed
		}
		// Validate fallback_action
		if rule.FallbackAction != "" && !validActions[rule.FallbackAction] {
			return fmt.Errorf("rule[%d]: invalid fallback_action %q", i, rule.FallbackAction)
		}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal beta policy settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyBetaPolicySettings, string(data))
}

// GetOpenAIFastPolicySettings 获取 OpenAI fast 策略配置
func (s *SettingService) GetOpenAIFastPolicySettings(ctx context.Context) (*OpenAIFastPolicySettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIFastPolicySettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultOpenAIFastPolicySettings(), nil
		}
		return nil, fmt.Errorf("get openai fast policy settings: %w", err)
	}
	if value == "" {
		return DefaultOpenAIFastPolicySettings(), nil
	}

	var settings OpenAIFastPolicySettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		// JSON 损坏时静默 fallback 到默认配置会让策略意外失效（管理员配
		// 置的 block/filter 规则被忽略）。记录 Warn 让运维能在出现异常
		// 行为时定位到 settings 表里的脏数据。
		slog.Warn("failed to unmarshal openai fast policy settings, falling back to defaults",
			"error", err,
			"key", SettingKeyOpenAIFastPolicySettings)
		return DefaultOpenAIFastPolicySettings(), nil
	}
	// API Keys are global resources. Legacy user_ids are intentionally ignored
	// so policy evaluation can no longer vary by the row that once owned a key.
	for i := range settings.Rules {
		settings.Rules[i].UserIDs = nil
	}

	return &settings, nil
}

// SetOpenAIFastPolicySettings 设置 OpenAI fast 策略配置
func (s *SettingService) SetOpenAIFastPolicySettings(ctx context.Context, settings *OpenAIFastPolicySettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	validActions := map[string]bool{
		BetaPolicyActionPass: true, BetaPolicyActionFilter: true, BetaPolicyActionBlock: true,
		OpenAIFastPolicyActionForcePriority: true,
	}
	validScopes := map[string]bool{
		BetaPolicyScopeAll: true, BetaPolicyScopeOAuth: true, BetaPolicyScopeAPIKey: true, BetaPolicyScopeBedrock: true,
	}
	validTiers := map[string]bool{
		OpenAIFastTierAny: true, OpenAIFastTierPriority: true, OpenAIFastTierUltrafast: true, OpenAIFastTierFlex: true,
		OpenAIFastTierMissing: true,
	}

	for i, rule := range settings.Rules {
		tier := strings.ToLower(strings.TrimSpace(rule.ServiceTier))
		if tier == "" {
			tier = OpenAIFastTierAny
		}
		if !validTiers[tier] {
			return fmt.Errorf("rule[%d]: invalid service_tier %q", i, rule.ServiceTier)
		}
		settings.Rules[i].ServiceTier = tier
		if !validActions[rule.Action] {
			return fmt.Errorf("rule[%d]: invalid action %q", i, rule.Action)
		}
		if !validScopes[rule.Scope] {
			return fmt.Errorf("rule[%d]: invalid scope %q", i, rule.Scope)
		}
		// Keep the JSON field for rolling compatibility, but never persist a
		// user-specific selector in the single-administrator/global-key model.
		settings.Rules[i].UserIDs = nil
		for j, pattern := range rule.ModelWhitelist {
			trimmed := strings.TrimSpace(pattern)
			if trimmed == "" {
				return fmt.Errorf("rule[%d]: model_whitelist[%d] cannot be empty", i, j)
			}
			settings.Rules[i].ModelWhitelist[j] = trimmed
		}
		if rule.FallbackAction != "" && !validActions[rule.FallbackAction] {
			return fmt.Errorf("rule[%d]: invalid fallback_action %q", i, rule.FallbackAction)
		}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal openai fast policy settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyOpenAIFastPolicySettings, string(data))
}

// SetStreamTimeoutSettings 设置流超时处理配置
func (s *SettingService) SetStreamTimeoutSettings(ctx context.Context, settings *StreamTimeoutSettings) error {
	if settings == nil {
		return fmt.Errorf("settings cannot be nil")
	}

	// 验证配置值
	if settings.TempUnschedMinutes < 1 || settings.TempUnschedMinutes > 60 {
		return fmt.Errorf("temp_unsched_minutes must be between 1-60")
	}
	if settings.ThresholdCount < 1 || settings.ThresholdCount > 10 {
		return fmt.Errorf("threshold_count must be between 1-10")
	}
	if settings.ThresholdWindowMinutes < 1 || settings.ThresholdWindowMinutes > 60 {
		return fmt.Errorf("threshold_window_minutes must be between 1-60")
	}

	switch settings.Action {
	case StreamTimeoutActionTempUnsched, StreamTimeoutActionError, StreamTimeoutActionNone:
		// valid
	default:
		return fmt.Errorf("invalid action: %s", settings.Action)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal stream timeout settings: %w", err)
	}

	return s.settingRepo.Set(ctx, SettingKeyStreamTimeoutSettings, string(data))
}

// GetAccountSchedulingThresholds returns per-platform auto-pause thresholds (1..100).
// 100 disables the threshold for that platform. Hot-path cached with singleflight.
func (s *SettingService) GetAccountSchedulingThresholds(ctx context.Context) map[string]int {
	if s == nil || s.settingRepo == nil {
		return defaultAccountSchedulingThresholds()
	}
	if cached, ok := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds); ok {
		if cached != nil && len(cached.thresholds) > 0 && time.Now().UnixNano() < cached.expiresAt {
			return cloneAccountSchedulingThresholds(cached.thresholds)
		}
	}

	result, err, _ := accountSchedulingThresholdsSF.Do(SettingKeyAccountSchedulingThresholds, func() (any, error) {
		if cached, ok := accountSchedulingThresholdsCache.Load().(*cachedAccountSchedulingThresholds); ok {
			if cached != nil && len(cached.thresholds) > 0 && time.Now().UnixNano() < cached.expiresAt {
				return cloneAccountSchedulingThresholds(cached.thresholds), nil
			}
		}

		thresholds := defaultAccountSchedulingThresholds()
		dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountSchedulingThresholdsDBTimeout)
		defer cancel()

		raw, err := s.settingRepo.GetValue(dbCtx, SettingKeyAccountSchedulingThresholds)
		if err != nil {
			if errors.Is(err, ErrSettingNotFound) {
				accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{
					thresholds: cloneAccountSchedulingThresholds(thresholds),
					expiresAt:  time.Now().Add(accountSchedulingThresholdsCacheTTL).UnixNano(),
				})
				return cloneAccountSchedulingThresholds(thresholds), nil
			}
			slog.Warn("failed to get account scheduling thresholds, falling back to defaults", "error", err)
			accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{
				thresholds: cloneAccountSchedulingThresholds(thresholds),
				expiresAt:  time.Now().Add(accountSchedulingThresholdsErrorTTL).UnixNano(),
			})
			return cloneAccountSchedulingThresholds(thresholds), nil
		}

		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			if parsed, err := parseAccountSchedulingThresholdsSetting(trimmed); err != nil {
				slog.Warn("failed to parse account scheduling thresholds, falling back to defaults", "error", err)
			} else {
				thresholds = parsed
			}
		}

		accountSchedulingThresholdsCache.Store(&cachedAccountSchedulingThresholds{
			thresholds: cloneAccountSchedulingThresholds(thresholds),
			expiresAt:  time.Now().Add(accountSchedulingThresholdsCacheTTL).UnixNano(),
		})
		return cloneAccountSchedulingThresholds(thresholds), nil
	})
	if err != nil {
		return defaultAccountSchedulingThresholds()
	}
	if thresholds, ok := result.(map[string]int); ok {
		return cloneAccountSchedulingThresholds(thresholds)
	}
	return defaultAccountSchedulingThresholds()
}
