/**
 * Reliability and connection health API.
 *
 * This endpoint intentionally returns operational state rather than secrets or
 * request payloads. Most fields are optional so older backends can be used
 * while the aggregate response is rolled out.
 */
import { apiClient } from '../client'

export interface ReliabilityAccountAvailability {
  total?: number
  total_accounts?: number
  available?: number
  available_count?: number
  limited?: number
  rate_limit_count?: number
  overload_count?: number
  temp_unschedulable_count?: number
  error_count?: number
  cooling_down?: number
  error?: number
  unavailable?: number
  by_platform?: Record<string, ReliabilityAccountAvailability>
}

export interface ReliabilityTrafficSummary {
  current_concurrency?: number
  concurrency?: number
  queued_requests?: number
  qps?: number | { current?: number; peak?: number; avg?: number }
  qps_summary?: { current?: number; peak?: number; avg?: number }
  tps?: number | { current?: number; peak?: number; avg?: number }
  tps_summary?: { current?: number; peak?: number; avg?: number }
  requests?: number
  error_rate?: number
  upstream_429?: number
  upstream_529?: number
  connection_failures?: number
  stream_timeouts?: number
  start_time?: string
  end_time?: string
}

export interface ReliabilityWebSocketConnectionSummary {
  enabled?: boolean
  force_http?: boolean
  oauth_enabled?: boolean
  apikey_enabled?: boolean
  mode_router_v2_enabled?: boolean
  ingress_mode_default?: string
  client_first_message_timeout_seconds?: number
  ingress_inter_turn_idle_timeout_seconds?: number
  max_ingress_connections_per_api_key?: number
  dial_timeout_seconds?: number
  read_timeout_seconds?: number
  write_timeout_seconds?: number
  queue_limit_per_connection?: number
  fallback_cooldown_seconds?: number
  retry_total_budget_ms?: number
  http_bridge_enabled?: boolean
}

export interface ReliabilityHTTP2ConnectionSummary {
  enabled?: boolean
  allow_proxy_fallback_to_http1?: boolean
  fallback_error_threshold?: number
  fallback_window_seconds?: number
  fallback_ttl_seconds?: number
}

export interface ReliabilityConnectionSummary {
  status?: 'healthy' | 'degraded' | 'unavailable' | 'unknown' | string
  reachable?: boolean
  checked_at?: string
  active_proxies?: number
  failed_proxies?: number
  last_error?: string
  message?: string
  configured?: boolean
  response_header_timeout?: number
  openai_response_header_timeout?: number
  first_output_timeout?: number
  stream_interval?: number
  idle_conn_timeout?: number
  dial_timeout?: number
  tls_handshake_timeout?: number
  pool_isolation?: string
  connection_pool_isolation?: string
  max_idle_conns?: number
  max_idle_conns_per_host?: number
  max_conns_per_host?: number
  idle_conn_timeout_seconds?: number
  max_upstream_clients?: number
  client_idle_ttl_seconds?: number
  response_header_timeout_seconds?: number
  openai_response_header_timeout_seconds?: number
  openai_first_output_timeout_seconds?: number
  stream_data_interval_timeout_seconds?: number
  stream_keepalive_interval_seconds?: number
  codex_identity_enforcement?: boolean
  openai_ws?: ReliabilityWebSocketConnectionSummary
  openai_http2?: ReliabilityHTTP2ConnectionSummary
}

export interface ReliabilityLimitSummary {
  rate_limited_accounts?: number
  cooling_accounts?: number
  longest_cooldown_seconds?: number
  next_retry_at?: string
  overload_cooldown_minutes?: number
  oauth_401_cooldown_minutes?: number
  max_account_switches?: number
  max_account_switches_gemini?: number
  max_ingress_connections_per_api_key?: number
  max_conns_per_host?: number
  max_upstream_clients?: number
  connection_pool_isolation?: string
  stream_data_interval_timeout_seconds?: number
  stream_keepalive_interval_seconds?: number
  panel_enabled?: boolean
  panel_user_rpm?: number
  panel_heavy_rpm?: number
  panel_public_ip_rpm?: number
  panel_exempt_admin?: boolean
}

export interface ReliabilityFallbackSummary {
  enabled?: boolean
  models?: Record<string, string>
}

export interface ReliabilityCooldownSummary {
  enabled?: boolean
  cooldown_minutes?: number
  cooldown_seconds?: number
  action?: 'temp_unsched' | 'error' | 'none' | string
  temp_unsched_minutes?: number
  threshold_count?: number
  threshold_window_minutes?: number
}

export interface ReliabilityCooldownsSummary {
  overload_529?: ReliabilityCooldownSummary
  rate_limit_429?: ReliabilityCooldownSummary
  stream_timeout?: ReliabilityCooldownSummary
}

export interface ReliabilityRuntimeSummary {
  ops_enabled?: boolean
  error?: {
    available?: boolean
    total?: number
    rate_limited?: number
    overloaded?: number
    connection_failed?: number
    stream_timeouts?: number
    error_rate?: number
  }
}

export interface ReliabilityTurnStateSummary {
  supported?: boolean
  http_enabled?: boolean
  websocket_enabled?: boolean
  cross_account_protection?: boolean
  http_cross_account_protection?: boolean
  websocket_cross_account_protection?: boolean
  /** Optional aliases accepted while the aggregate projection is rolled out. */
  http_supported?: boolean
  websocket_supported?: boolean
  cross_account_protected?: boolean
  [key: string]: unknown
}

export interface ReliabilityDiagnostic {
  at?: string
  category?: '429' | '529' | 'connection' | 'stream_timeout' | 'upstream' | string
  status_code?: number
  message?: string
  count?: number
  next_step?: string
}

export interface ReliabilityStatusSummary {
  fallback?: ReliabilityFallbackSummary
  cooldowns?: ReliabilityCooldownsSummary
  account_availability?: ReliabilityAccountAvailability
  traffic?: ReliabilityTrafficSummary
  connection?: ReliabilityConnectionSummary
  limits?: ReliabilityLimitSummary
  runtime?: ReliabilityRuntimeSummary
  turn_state?: ReliabilityTurnStateSummary
  diagnostics?: ReliabilityDiagnostic[] | ReliabilityDiagnosticsSummary
  notes?: string[]
  /** Compatibility aliases accepted from early backend implementations. */
  accounts?: ReliabilityAccountAvailability
  errors?: ReliabilityDiagnostic[]
}

export interface ReliabilityDiagnosticsSummary {
  source_errors?: string[]
  warnings?: string[]
  [key: string]: unknown
}

export interface ReliabilityStatusResponse {
  enabled?: boolean
  timestamp?: string
  generated_at?: string
  summary?: ReliabilityStatusSummary
  fallback?: ReliabilityFallbackSummary
  cooldowns?: ReliabilityCooldownsSummary
  runtime?: ReliabilityRuntimeSummary
  turn_state?: ReliabilityTurnStateSummary
  notes?: string[]
  account_availability?: ReliabilityAccountAvailability
  traffic?: ReliabilityTrafficSummary
  connection?: ReliabilityConnectionSummary
  limits?: ReliabilityLimitSummary
  diagnostics?: ReliabilityDiagnosticsSummary
  message?: string
}

/** Normalize both the current nested response and the earlier flat projection. */
export function normalizeReliabilityStatus(response: ReliabilityStatusResponse): ReliabilityStatusSummary {
  const nested = response.summary ?? {}
  return {
    fallback: nested.fallback ?? response.fallback,
    cooldowns: nested.cooldowns ?? response.cooldowns,
    account_availability: nested.account_availability ?? nested.accounts ?? response.account_availability,
    traffic: nested.traffic ?? response.traffic,
    connection: nested.connection ?? response.connection,
    limits: nested.limits ?? response.limits,
    runtime: nested.runtime ?? response.runtime,
    turn_state: nested.turn_state ?? response.turn_state,
    diagnostics: nested.diagnostics ?? nested.errors ?? response.diagnostics,
    notes: nested.notes ?? response.notes,
  }
}

export async function getReliabilityStatus(): Promise<ReliabilityStatusResponse> {
  const { data } = await apiClient.get<ReliabilityStatusResponse>('/admin/reliability/status')
  return data
}

export const reliabilityAPI = {
  getStatus: getReliabilityStatus,
}

export default reliabilityAPI
