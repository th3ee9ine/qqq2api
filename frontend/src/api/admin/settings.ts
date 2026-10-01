/**
 * Admin Settings API endpoints
 * Handles system settings management for administrators
 */

import { apiClient } from "../client";
import type {
  CustomEndpoint,
  LoginAgreementDocument,
  NotifyEmailEntry,
} from "@/types";

export interface OpenAISessionCleanupGlobalSettings {
  enabled: boolean
  interval_minutes: number
}

export type SchedulingThresholdPlatformType =
  | "openai"
  | "anthropic"
  | "grok"

export type AccountSchedulingThresholdsMap = Record<SchedulingThresholdPlatformType, number>

export const SCHEDULING_THRESHOLD_PLATFORMS: SchedulingThresholdPlatformType[] = [
  "openai",
  "anthropic",
  "grok",
]

export function normalizeAccountSchedulingThresholdsMap(
  input?: Partial<Record<SchedulingThresholdPlatformType, number>> | null,
): AccountSchedulingThresholdsMap {
  const result = {} as AccountSchedulingThresholdsMap
  for (const platform of SCHEDULING_THRESHOLD_PLATFORMS) {
    const value = input?.[platform]
    result[platform] = typeof value === "number" && Number.isFinite(value)
      ? Math.min(100, Math.max(1, Math.trunc(value)))
      : 100
  }
  return result
}

export function sanitizeAccountSchedulingThresholdsMap(
  input?: Partial<Record<SchedulingThresholdPlatformType, number>> | null,
): AccountSchedulingThresholdsMap {
  return normalizeAccountSchedulingThresholdsMap(input)
}

export interface SystemSettings {
  totp_enabled: boolean; // TOTP 双因素认证
  totp_encryption_key_configured: boolean;
  session_binding_enabled: boolean; // 会话 IP/UA 绑定
  step_up_enabled: boolean; // 敏感操作 step-up 2FA
  audit_log_retention_days: number; // 审计日志保留天数
  login_agreement_enabled: boolean;
  login_agreement_mode: "modal" | "checkbox" | string;
  login_agreement_updated_at: string;
  login_agreement_documents: LoginAgreementDocument[];
  // OEM settings
  site_name: string;
  site_logo: string;
  site_subtitle: string;
  api_base_url: string;
  contact_info: string;
  doc_url: string;
  home_content: string;
  compact_home_enabled: boolean;
  hide_ccs_import_button: boolean;
  table_default_page_size: number;
  table_page_size_options: number[];
  backend_mode_enabled: boolean;
  custom_endpoints: CustomEndpoint[];
  // Cloudflare Turnstile settings
  turnstile_enabled: boolean;
  turnstile_site_key: string;
  turnstile_secret_key_configured: boolean;
  tencent_captcha_enabled: boolean;
  tencent_captcha_app_id: string;
  tencent_captcha_app_secret_key_configured: boolean;
  tencent_captcha_cloud_secret_id_configured: boolean;
  tencent_captcha_cloud_secret_key_configured: boolean;
  tencent_captcha_region: string;
  aliyun_captcha_enabled: boolean;
  aliyun_captcha_access_key_id: string;
  aliyun_captcha_access_key_secret_configured: boolean;
  aliyun_captcha_scene_id: string;
  aliyun_captcha_prefix: string;
  aliyun_captcha_region: string;
  api_key_acl_trust_forwarded_ip: boolean;
  forwarded_client_ip_headers: string[];

  // Model fallback configuration
  enable_model_fallback: boolean;
  fallback_model_anthropic: string;
  fallback_model_openai: string;
  grok_default_text_model: string;
  grok_cross_client_model_map_enabled: boolean;
  grok_default_base_url_mode: string;
  // Per-platform account auto-pause thresholds (100 = disabled)
  account_scheduling_thresholds: AccountSchedulingThresholdsMap;

  // Ops Monitoring (vNext)
  ops_monitoring_enabled: boolean;
  ops_realtime_monitoring_enabled: boolean;
  ops_query_mode_default: "auto" | "raw" | "preagg" | string;
  ops_metrics_interval_seconds: number;

  // Claude Code version check
  min_claude_code_version: string;
  max_claude_code_version: string;

  // 分组隔离
  allow_ungrouped_key_scheduling: boolean;

  // Gateway forwarding behavior
  openai_ttft_mode: string;
  enable_fingerprint_unification: boolean;
  enable_metadata_passthrough: boolean;
  enable_cch_signing: boolean;
  enable_claude_oauth_system_prompt_injection: boolean;
  claude_oauth_system_prompt: string;
  claude_oauth_system_prompt_blocks: string;
  enable_anthropic_cache_ttl_1h_injection: boolean;
  rewrite_message_cache_control: boolean;
  enable_client_dateline_normalization: boolean;
  openai_codex_originator: string;
  openai_codex_user_agent: string;
  openai_codex_client_version: string;
  openai_codex_client_version_mode: "auto" | "pinned";
  openai_codex_originator_default: string;
  openai_codex_user_agent_default: string;
  openai_codex_client_version_default: string;
  openai_codex_client_version_synced: string;
  openai_codex_version_auto_sync_enabled: boolean;
  enable_openai_account_local_device_identity: boolean;
  claude_code_client_version: string;
  claude_code_client_version_synced: string;
  claude_code_version_auto_sync_enabled: boolean;
  // codex_cli_only 加固
  min_codex_version: string;
  max_codex_version: string;
  codex_cli_only_blacklist: string;
  codex_cli_only_whitelist: string;
  codex_cli_only_allow_app_server_clients: boolean;
  codex_cli_only_engine_fingerprint_signals: string;
  web_search_emulation_enabled?: boolean;
  risk_control_enabled: boolean;

  // Cyber session block
  cyber_session_block_enabled: boolean;
  cyber_session_block_ttl_seconds: number;
  openai_low_upstream_rate_priority_enabled?: boolean;
  /** null means OAuth accounts use their individual account rates. */
  openai_oauth_scheduling_rate_multiplier?: number | null;
  openai_advanced_scheduler_enabled?: boolean;
  openai_advanced_scheduler_sticky_weighted_enabled?: boolean;
  openai_advanced_scheduler_subscription_priority_enabled?: boolean;
  openai_advanced_scheduler_lb_top_k?: string;
  openai_advanced_scheduler_weight_priority?: string;
  openai_advanced_scheduler_weight_load?: string;
  openai_advanced_scheduler_weight_queue?: string;
  openai_advanced_scheduler_weight_error_rate?: string;
  openai_advanced_scheduler_weight_ttft?: string;
  openai_advanced_scheduler_weight_reset?: string;
  openai_advanced_scheduler_weight_quota_headroom?: string;
  openai_advanced_scheduler_weight_upstream_cost?: string;
  openai_advanced_scheduler_weight_previous_response?: string;
  openai_advanced_scheduler_weight_session_sticky?: string;
  openai_advanced_scheduler_effective_lb_top_k?: string;
  openai_advanced_scheduler_effective_weight_priority?: string;
  openai_advanced_scheduler_effective_weight_load?: string;
  openai_advanced_scheduler_effective_weight_queue?: string;
  openai_advanced_scheduler_effective_weight_error_rate?: string;
  openai_advanced_scheduler_effective_weight_ttft?: string;
  openai_advanced_scheduler_effective_weight_reset?: string;
  openai_advanced_scheduler_effective_weight_quota_headroom?: string;
  openai_advanced_scheduler_effective_weight_upstream_cost?: string;
  openai_advanced_scheduler_effective_weight_previous_response?: string;
  openai_advanced_scheduler_effective_weight_session_sticky?: string;

  // 账号限额通知
  account_quota_notify_enabled: boolean;
  account_quota_notify_emails: NotifyEmailEntry[];

  // Channel Monitor feature switch
  channel_monitor_enabled: boolean;
  channel_monitor_mode?: 'v1' | 'v2';
  channel_monitor_default_interval_seconds: number;
  channel_monitor_hide_throughput?: boolean;
  channel_monitor_show_quota?: boolean;

  // Available Channels feature switch
  available_channels_enabled: boolean;


  // Model Plaza feature switches + description
  model_plaza_enabled: boolean;
  model_plaza_require_auth: boolean;
  model_plaza_description: string;
  /** @deprecated Plugin management is no longer exposed in the admin panel. */
  plugin_management_enabled?: boolean;

  // OpenAI fast/flex policy
  openai_fast_policy_settings?: OpenAIFastPolicySettings;

  // Allow user view error requests
  allow_user_view_error_requests: boolean;
}

export interface UpdateSettingsRequest {
  totp_enabled?: boolean;
  session_binding_enabled?: boolean; // 会话 IP/UA 绑定
  step_up_enabled?: boolean; // 敏感操作 step-up 2FA
  audit_log_retention_days?: number; // 审计日志保留天数
  login_agreement_enabled?: boolean;
  login_agreement_mode?: "modal" | "checkbox" | string;
  login_agreement_updated_at?: string;
  login_agreement_documents?: LoginAgreementDocument[];
  site_name?: string;
  site_logo?: string;
  site_subtitle?: string;
  api_base_url?: string;
  contact_info?: string;
  doc_url?: string;
  home_content?: string;
  compact_home_enabled?: boolean;
  hide_ccs_import_button?: boolean;
  table_default_page_size?: number;
  table_page_size_options?: number[];
  backend_mode_enabled?: boolean;
  custom_endpoints?: CustomEndpoint[];
  turnstile_enabled?: boolean;
  turnstile_site_key?: string;
  turnstile_secret_key?: string;
  tencent_captcha_enabled?: boolean;
  tencent_captcha_app_id?: string;
  tencent_captcha_app_secret_key?: string;
  tencent_captcha_cloud_secret_id?: string;
  tencent_captcha_cloud_secret_key?: string;
  tencent_captcha_region?: string;
  aliyun_captcha_enabled?: boolean;
  aliyun_captcha_access_key_id?: string;
  aliyun_captcha_access_key_secret?: string;
  aliyun_captcha_scene_id?: string;
  aliyun_captcha_prefix?: string;
  aliyun_captcha_region?: string;
  api_key_acl_trust_forwarded_ip?: boolean;
  forwarded_client_ip_headers?: string[];
  enable_model_fallback?: boolean;
  fallback_model_anthropic?: string;
  fallback_model_openai?: string;
  grok_default_text_model?: string;
  grok_cross_client_model_map_enabled?: boolean;
  grok_default_base_url_mode?: string;
  account_scheduling_thresholds?: AccountSchedulingThresholdsMap;
  ops_monitoring_enabled?: boolean;
  ops_realtime_monitoring_enabled?: boolean;
  ops_query_mode_default?: "auto" | "raw" | "preagg" | string;
  ops_metrics_interval_seconds?: number;
  min_claude_code_version?: string;
  max_claude_code_version?: string;
  allow_ungrouped_key_scheduling?: boolean;
  openai_ttft_mode?: string;
  enable_fingerprint_unification?: boolean;
  enable_metadata_passthrough?: boolean;
  enable_cch_signing?: boolean;
  enable_claude_oauth_system_prompt_injection?: boolean;
  claude_oauth_system_prompt?: string;
  claude_oauth_system_prompt_blocks?: string;
  enable_anthropic_cache_ttl_1h_injection?: boolean;
  rewrite_message_cache_control?: boolean;
  enable_client_dateline_normalization?: boolean;
  openai_codex_originator?: string;
  openai_codex_user_agent?: string;
  openai_codex_client_version?: string;
  openai_codex_client_version_mode?: "auto" | "pinned";
  openai_codex_version_auto_sync_enabled?: boolean;
  enable_openai_account_local_device_identity?: boolean;
  claude_code_client_version?: string;
  claude_code_version_auto_sync_enabled?: boolean;
  // codex_cli_only 加固
  min_codex_version?: string;
  max_codex_version?: string;
  codex_cli_only_blacklist?: string;
  codex_cli_only_whitelist?: string;
  codex_cli_only_allow_app_server_clients?: boolean;
  codex_cli_only_engine_fingerprint_signals?: string;
  risk_control_enabled?: boolean;

  // Cyber session block
  cyber_session_block_enabled?: boolean;
  cyber_session_block_ttl_seconds?: number;
  openai_low_upstream_rate_priority_enabled?: boolean;
  /** Omit to preserve the override; null clears it; zero is an explicit rate. */
  openai_oauth_scheduling_rate_multiplier?: number | null;
  openai_advanced_scheduler_enabled?: boolean;
  openai_advanced_scheduler_sticky_weighted_enabled?: boolean;
  openai_advanced_scheduler_subscription_priority_enabled?: boolean;
  openai_advanced_scheduler_lb_top_k?: string;
  openai_advanced_scheduler_weight_priority?: string;
  openai_advanced_scheduler_weight_load?: string;
  openai_advanced_scheduler_weight_queue?: string;
  openai_advanced_scheduler_weight_error_rate?: string;
  openai_advanced_scheduler_weight_ttft?: string;
  openai_advanced_scheduler_weight_reset?: string;
  openai_advanced_scheduler_weight_quota_headroom?: string;
  openai_advanced_scheduler_weight_upstream_cost?: string;
  openai_advanced_scheduler_weight_previous_response?: string;
  openai_advanced_scheduler_weight_session_sticky?: string;
  // 余额、订阅到期与账号限额通知
  account_quota_notify_enabled?: boolean;
  account_quota_notify_emails?: NotifyEmailEntry[];

  // Channel Monitor feature switch
  channel_monitor_enabled?: boolean;
  channel_monitor_mode?: 'v1' | 'v2';
  channel_monitor_default_interval_seconds?: number;
  channel_monitor_hide_throughput?: boolean;
  channel_monitor_show_quota?: boolean;

  // Available Channels feature switch
  available_channels_enabled?: boolean;

  // Subscription feature switch

  // Model Plaza feature switches + description
  model_plaza_enabled?: boolean;
  model_plaza_require_auth?: boolean;
  model_plaza_description?: string;
  plugin_management_enabled?: boolean;

  // OpenAI fast/flex policy
  openai_fast_policy_settings?: OpenAIFastPolicySettings;

  allow_user_view_error_requests?: boolean;
}

/**
 * Get all system settings
 * @returns System settings
 */
export async function getSettings(): Promise<SystemSettings> {
  const { data } = await apiClient.get<SystemSettings>("/admin/settings");
  return data;
}

/** Read the global OpenAI device-session cleanup policy. */
export async function getOpenAISessionCleanupSettings(): Promise<OpenAISessionCleanupGlobalSettings> {
  const { data } = await apiClient.get<OpenAISessionCleanupGlobalSettings>('/admin/settings/openai-session-cleanup')
  return data
}

/** Persist the global OpenAI device-session cleanup policy. */
export async function updateOpenAISessionCleanupSettings(
  settings: OpenAISessionCleanupGlobalSettings,
): Promise<OpenAISessionCleanupGlobalSettings> {
  const { data } = await apiClient.put<OpenAISessionCleanupGlobalSettings>(
    '/admin/settings/openai-session-cleanup',
    settings,
  )
  return data
}

/**
 * Update system settings
 * @param settings - Partial settings to update
 * @returns Updated settings
 */
export async function updateSettings(
  settings: UpdateSettingsRequest,
): Promise<SystemSettings> {
  const { data } = await apiClient.put<SystemSettings>(
    "/admin/settings",
    settings,
  );
  return data;
}

/**
 * Admin API Key status response
 */
export interface AdminApiKeyStatus {
  exists: boolean;
  masked_key: string;
}

/**
 * Get admin API key status
 * @returns Status indicating if key exists and masked version
 */
export async function getAdminApiKey(): Promise<AdminApiKeyStatus> {
  const { data } = await apiClient.get<AdminApiKeyStatus>(
    "/admin/settings/admin-api-key",
  );
  return data;
}

/**
 * Regenerate admin API key
 * @returns The new full API key (only shown once)
 */
export async function regenerateAdminApiKey(): Promise<{ key: string }> {
  const { data } = await apiClient.post<{ key: string }>(
    "/admin/settings/admin-api-key/regenerate",
  );
  return data;
}

/**
 * Delete admin API key
 * @returns Success message
 */
export async function deleteAdminApiKey(): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(
    "/admin/settings/admin-api-key",
  );
  return data;
}

// ==================== Overload Cooldown Settings ====================

/**
 * Overload cooldown settings interface (529 handling)
 */
export interface OverloadCooldownSettings {
  enabled: boolean;
  cooldown_minutes: number;
}

export async function getOverloadCooldownSettings(): Promise<OverloadCooldownSettings> {
  const { data } = await apiClient.get<OverloadCooldownSettings>(
    "/admin/settings/overload-cooldown",
  );
  return data;
}

export async function updateOverloadCooldownSettings(
  settings: OverloadCooldownSettings,
): Promise<OverloadCooldownSettings> {
  const { data } = await apiClient.put<OverloadCooldownSettings>(
    "/admin/settings/overload-cooldown",
    settings,
  );
  return data;
}

// ==================== 429 Rate Limit Cooldown Settings ====================

export interface RateLimit429CooldownSettings {
  enabled: boolean;
  cooldown_seconds: number;
}

export async function getRateLimit429CooldownSettings(): Promise<RateLimit429CooldownSettings> {
  const { data } = await apiClient.get<RateLimit429CooldownSettings>(
    "/admin/settings/rate-limit-429-cooldown",
  );
  return data;
}

export async function updateRateLimit429CooldownSettings(
  settings: RateLimit429CooldownSettings,
): Promise<RateLimit429CooldownSettings> {
  const { data } = await apiClient.put<RateLimit429CooldownSettings>(
    "/admin/settings/rate-limit-429-cooldown",
    settings,
  );
  return data;
}

// ==================== Panel Rate Limit Settings ====================

/**
 * Panel API rate limit settings.
 * Authenticated panel endpoints are limited per user account (reverse-proxy
 * safe); public endpoints are limited per publicly routable client IP.
 */
export interface PanelRateLimitSettings {
  enabled: boolean;
  user_rpm: number;
  heavy_rpm: number;
  exempt_admin: boolean;
  public_ip_rpm: number;
}

export async function getPanelRateLimitSettings(): Promise<PanelRateLimitSettings> {
  const { data } = await apiClient.get<PanelRateLimitSettings>(
    "/admin/settings/panel-rate-limit",
  );
  return data;
}

export async function updatePanelRateLimitSettings(
  settings: PanelRateLimitSettings,
): Promise<PanelRateLimitSettings> {
  const { data } = await apiClient.put<PanelRateLimitSettings>(
    "/admin/settings/panel-rate-limit",
    settings,
  );
  return data;
}

// ==================== Stream Timeout Settings ====================

/**
 * Stream timeout settings interface
 */
export interface StreamTimeoutSettings {
  enabled: boolean;
  action: "temp_unsched" | "error" | "none";
  temp_unsched_minutes: number;
  threshold_count: number;
  threshold_window_minutes: number;
}

/**
 * Get stream timeout settings
 * @returns Stream timeout settings
 */
export async function getStreamTimeoutSettings(): Promise<StreamTimeoutSettings> {
  const { data } = await apiClient.get<StreamTimeoutSettings>(
    "/admin/settings/stream-timeout",
  );
  return data;
}

/**
 * Update stream timeout settings
 * @param settings - Stream timeout settings to update
 * @returns Updated settings
 */
export async function updateStreamTimeoutSettings(
  settings: StreamTimeoutSettings,
): Promise<StreamTimeoutSettings> {
  const { data } = await apiClient.put<StreamTimeoutSettings>(
    "/admin/settings/stream-timeout",
    settings,
  );
  return data;
}

// ==================== Rectifier Settings ====================

/**
 * Rectifier settings interface
 */
export interface RectifierSettings {
  enabled: boolean;
  thinking_signature_enabled: boolean;
  thinking_budget_enabled: boolean;
  apikey_signature_enabled: boolean;
  apikey_signature_patterns: string[];
}

/**
 * Get rectifier settings
 * @returns Rectifier settings
 */
export async function getRectifierSettings(): Promise<RectifierSettings> {
  const { data } = await apiClient.get<RectifierSettings>(
    "/admin/settings/rectifier",
  );
  return data;
}

/**
 * Update rectifier settings
 * @param settings - Rectifier settings to update
 * @returns Updated settings
 */
export async function updateRectifierSettings(
  settings: RectifierSettings,
): Promise<RectifierSettings> {
  const { data } = await apiClient.put<RectifierSettings>(
    "/admin/settings/rectifier",
    settings,
  );
  return data;
}

// ==================== OpenAI Fast Policy Settings ====================

/**
 * OpenAI fast/flex policy rule interface.
 * Matches backend dto.OpenAIFastPolicyRule.
 */
export interface OpenAIFastPolicyRule {
  service_tier: "all" | "priority" | "flex" | "ultrafast" | "missing";
  action: "pass" | "filter" | "block" | "force_priority";
  scope: "all" | "oauth" | "apikey" | "bedrock";
  user_ids?: number[];
  error_message?: string;
  model_whitelist?: string[];
  fallback_action?: "pass" | "filter" | "block" | "force_priority";
  fallback_error_message?: string;
}

/**
 * OpenAI fast/flex policy settings interface.
 */
export interface OpenAIFastPolicySettings {
  rules: OpenAIFastPolicyRule[];
}

// ==================== Beta Policy Settings ====================

/**
 * Beta policy rule interface
 */
export interface BetaPolicyRule {
  beta_token: string;
  action: "pass" | "filter" | "block";
  scope: "all" | "oauth" | "apikey" | "bedrock";
  error_message?: string;
  model_whitelist?: string[];
  fallback_action?: "pass" | "filter" | "block";
  fallback_error_message?: string;
}

/**
 * Beta policy settings interface
 */
export interface BetaPolicySettings {
  rules: BetaPolicyRule[];
}

/**
 * Get beta policy settings
 * @returns Beta policy settings
 */
export async function getBetaPolicySettings(): Promise<BetaPolicySettings> {
  const { data } = await apiClient.get<BetaPolicySettings>(
    "/admin/settings/beta-policy",
  );
  return data;
}

/**
 * Update beta policy settings
 * @param settings - Beta policy settings to update
 * @returns Updated settings
 */
export async function updateBetaPolicySettings(
  settings: BetaPolicySettings,
): Promise<BetaPolicySettings> {
  const { data } = await apiClient.put<BetaPolicySettings>(
    "/admin/settings/beta-policy",
    settings,
  );
  return data;
}

// --- Web Search Emulation Config ---

export interface WebSearchProviderConfig {
  type: "brave" | "tavily";
  api_key: string;
  api_key_configured: boolean;
  quota_limit: number | null;
  subscribed_at: number | null;
  quota_used?: number;
  proxy_id: number | null;
  expires_at: number | null;
}

export interface WebSearchEmulationConfig {
  enabled: boolean;
  providers: WebSearchProviderConfig[];
}

export interface WebSearchTestResult {
  provider: string;
  results: { url: string; title: string; snippet: string; page_age?: string }[];
  query: string;
}

export async function getWebSearchEmulationConfig(): Promise<WebSearchEmulationConfig> {
  const { data } = await apiClient.get<WebSearchEmulationConfig>(
    "/admin/settings/web-search-emulation",
  );
  return data;
}

export async function updateWebSearchEmulationConfig(
  config: WebSearchEmulationConfig,
): Promise<WebSearchEmulationConfig> {
  const { data } = await apiClient.put<WebSearchEmulationConfig>(
    "/admin/settings/web-search-emulation",
    config,
  );
  return data;
}

export async function testWebSearchEmulation(
  query: string,
): Promise<WebSearchTestResult> {
  const { data } = await apiClient.post<WebSearchTestResult>(
    "/admin/settings/web-search-emulation/test",
    { query },
  );
  return data;
}

export async function resetWebSearchUsage(payload: {
  provider_type: string;
}): Promise<void> {
  await apiClient.post(
    "/admin/settings/web-search-emulation/reset-usage",
    payload,
  );
}

export interface OpenAICodexVersionRelease {
  version: string;
  tag_name: string;
  published_at: string;
  html_url: string;
}

export interface OpenAICodexVersionsResponse {
  versions: OpenAICodexVersionRelease[];
  latest_version: string;
  has_more: boolean;
  next_page: number | null;
}

export interface OpenAICodexSyncResponse {
  latest_version: string;
  synced_version: string;
  updated: boolean;
  defaults: {
    originator: string;
    user_agent: string;
    client_version: string;
  };
}

export async function getOpenAICodexVersions(
  page = 1,
): Promise<OpenAICodexVersionsResponse> {
  const { data } = await apiClient.get<OpenAICodexVersionsResponse>(
    "/admin/settings/openai-codex/versions", { params: { page } },
  );
  return data;
}

export async function syncOpenAICodexVersion(): Promise<OpenAICodexSyncResponse> {
  const { data } = await apiClient.post<OpenAICodexSyncResponse>(
    "/admin/settings/openai-codex/sync",
  );
  return data;
}

export const settingsAPI = {
  getOpenAICodexVersions,
  syncOpenAICodexVersion,
  getSettings,
  updateSettings,
  getOpenAISessionCleanupSettings,
  updateOpenAISessionCleanupSettings,
  getAdminApiKey,
  regenerateAdminApiKey,
  deleteAdminApiKey,
  getOverloadCooldownSettings,
  updateOverloadCooldownSettings,
  getRateLimit429CooldownSettings,
  updateRateLimit429CooldownSettings,
  getPanelRateLimitSettings,
  updatePanelRateLimitSettings,
  getStreamTimeoutSettings,
  updateStreamTimeoutSettings,
  getRectifierSettings,
  updateRectifierSettings,
  getBetaPolicySettings,
  updateBetaPolicySettings,
  getWebSearchEmulationConfig,
  updateWebSearchEmulationConfig,
  testWebSearchEmulation,
  resetWebSearchUsage,
};

export default settingsAPI;
