/**
 * Reliability status API.
 *
 * This endpoint returns only bounded turn-state capability and collector
 * health. Opaque state values and identity-bearing fields never cross it.
 */
import { apiClient } from '../client'

export interface ReliabilityTurnStateSummary {
  supported?: boolean
  http_enabled?: boolean
  websocket_enabled?: boolean
  cross_account_protection?: boolean
  http_cross_account_protection?: boolean
  websocket_cross_account_protection?: boolean
  collector?: ReliabilityTurnStateCollectorSummary
  /** Optional aliases accepted from older status responses. */
  http_supported?: boolean
  websocket_supported?: boolean
  cross_account_protected?: boolean
  [key: string]: unknown
}

/** A configured collector egress URL, exposed without credentials. */
export interface ReliabilityTurnStateProxyPoolEntry {
  protocol?: string
  host?: string
  port?: number | string
  /** Optional aggregate fields returned by newer gateway builds. */
  configured?: boolean
  usable?: boolean
  status?: string
  [key: string]: unknown
}

export interface ReliabilityTurnStateIPRegionSummary {
  region?: string
  country?: string
  country_code?: string
  count?: number
  /** Egress diagnostic observations, sampled after successful collections. */
  successes?: number
  [key: string]: unknown
}

export interface ReliabilityTurnStateSuccessfulIP {
  ip?: string
  address?: string
  ip_address?: string
  region?: string
  area?: string
  country?: string
  country_code?: string
  /** Egress diagnostic observations, sampled after successful collections. */
  successes?: number
  count?: number
  last_success_at?: string
  [key: string]: unknown
}

export interface ReliabilityTurnStateCandidateBreakdown {
  reason?: string
  code?: string
  cause?: string
  count?: number
  [key: string]: unknown
}

/** Credential-free health for one configured collector node. */
export interface ReliabilityTurnStateCollectorNodeSummary {
  node_id?: string
  label?: string
  successes?: number
  failures?: number
  consecutive_failures?: number
  cooldown_remaining_seconds?: number
  /** A bounded result code. Free-form values are not rendered by the UI. */
  last_result?: string
  [key: string]: unknown
}

/** Aggregate collector health only; the opaque state value is never returned. */
export interface ReliabilityTurnStateCollectorSummary {
  enabled?: boolean
  injection_enabled?: boolean
  status?: string
  ready?: boolean
  collecting?: boolean
  active_entries?: number
  ready_candidates?: number
  observations?: number
  successes?: number
  failures?: number
  last_success_at?: string
  last_failure_at?: string
  last_error_code?: string
  cookie_count?: number
  cookie_active_count?: number
  cookie_expired_count?: number
  cookie_remaining_seconds?: number
  budget_used?: number
  budget_limit?: number
  budget_reset_at?: string
  nodes?: ReliabilityTurnStateCollectorNodeSummary[]
  /** Configured egress pool, with credentials removed by the backend. */
  proxy_pool?: ReliabilityTurnStateProxyPoolEntry[]
  successful_ip_regions?: ReliabilityTurnStateIPRegionSummary[]
  successful_ips?: ReliabilityTurnStateSuccessfulIP[]
  candidate_breakdown?: ReliabilityTurnStateCandidateBreakdown[]
  /** Compatibility aliases accepted while rolling out the projection. */
  ip_regions?: ReliabilityTurnStateIPRegionSummary[] | Record<string, number>
  successful_ip_addresses?: string[]
  candidate_reasons?: ReliabilityTurnStateCandidateBreakdown[] | Record<string, number>
  /** Optional aggregate aliases used by older/newer gateways. */
  proxy_pool_count?: number
  successful_ip_count?: number
  [key: string]: unknown
}

export const reliabilityTurnStateSpeedPresets = ['slow', 'standard', 'fast', 'burst'] as const

export type ReliabilityTurnStateSpeedPreset = (typeof reliabilityTurnStateSpeedPresets)[number]

export interface ReliabilityTurnStateNumberBounds {
  min: number
  max: number
  step?: number
}

export interface ReliabilityTurnStateSettingsBounds {
  max_requests_per_round?: ReliabilityTurnStateNumberBounds
  failure_cooldown_seconds?: ReliabilityTurnStateNumberBounds
}

export interface ReliabilityTurnStateSpeedPresetValues {
  max_requests_per_round?: number
  failure_cooldown_seconds?: number
  /** Compatibility with the source project's generic cooldown field. */
  cooldown_seconds?: number
  [key: string]: unknown
}

export interface ReliabilityStatusResponse {
  /** Compatibility wrapper used by early versions of the status endpoint. */
  summary?: ReliabilityStatusSummary
  turn_state?: ReliabilityTurnStateSummary
}

export interface ReliabilityStatusSummary {
  turn_state?: ReliabilityTurnStateSummary
}

/** Runtime controls for bounded turn-state collection and reuse. */
export interface ReliabilityTurnStateSettings {
  probe_enabled: boolean
  injection_enabled: boolean
  speed_preset?: ReliabilityTurnStateSpeedPreset
  max_requests_per_round?: number
  failure_cooldown_seconds?: number
  /** Server-advertised controls keep the UI aligned with deployed policy. */
  presets?: ReliabilityTurnStateSpeedPreset[] | Partial<Record<ReliabilityTurnStateSpeedPreset, ReliabilityTurnStateSpeedPresetValues>>
  bounds?: ReliabilityTurnStateSettingsBounds
  /** Compatibility with an internal nested projection during rolling deploys. */
  harvest?: {
    speed_preset?: ReliabilityTurnStateSpeedPreset
    max_requests_per_round?: number
    failure_cooldown_seconds?: number
  }
  /** The pool is write-only; credentials are never returned by the backend. */
  proxy_pool_urls?: string[]
  /** Credential-free metadata returned by the settings endpoint. */
  proxy_pool_configured?: boolean
  proxy_pool_count?: number
}

export interface ReliabilityTurnStateSettingsUpdate {
  probe_enabled: boolean
  injection_enabled: boolean
  speed_preset?: ReliabilityTurnStateSpeedPreset
  max_requests_per_round?: number
  failure_cooldown_seconds?: number
  proxy_pool_urls?: string[]
}

export interface ReliabilityTurnStateHarvestRequest {
  account_id: number
  model: string
}

export interface ReliabilityTurnStateHarvestResponse {
  accepted: boolean
  /** Informational backend text; callers should not render it directly. */
  message?: string
}

/** Keep compatibility with both nested and flat Turn State responses. */
export function normalizeReliabilityStatus(response: ReliabilityStatusResponse): ReliabilityStatusSummary {
  return {
    turn_state: response.summary?.turn_state ?? response.turn_state,
  }
}

export async function getReliabilityStatus(): Promise<ReliabilityStatusResponse> {
  const { data } = await apiClient.get<ReliabilityStatusResponse>('/admin/reliability/status')
  return data
}

export async function getReliabilityTurnStateSettings(): Promise<ReliabilityTurnStateSettings> {
  const { data } = await apiClient.get<ReliabilityTurnStateSettings>('/admin/reliability/turn-state-settings')
  return data
}

export async function updateReliabilityTurnStateSettings(
  settings: ReliabilityTurnStateSettingsUpdate,
): Promise<ReliabilityTurnStateSettings> {
  const { data } = await apiClient.put<ReliabilityTurnStateSettings>(
    '/admin/reliability/turn-state-settings',
    settings,
  )
  return data
}

export async function startReliabilityTurnStateHarvest(
  request: ReliabilityTurnStateHarvestRequest,
): Promise<ReliabilityTurnStateHarvestResponse> {
  const { data } = await apiClient.post<ReliabilityTurnStateHarvestResponse>(
    '/admin/reliability/turn-state-harvest',
    request,
  )
  return data
}

export const reliabilityAPI = {
  getStatus: getReliabilityStatus,
  getTurnStateSettings: getReliabilityTurnStateSettings,
  updateTurnStateSettings: updateReliabilityTurnStateSettings,
  startTurnStateHarvest: startReliabilityTurnStateHarvest,
}

export default reliabilityAPI
