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
  /** The pool is write-only; credentials are never returned by the backend. */
  proxy_pool_urls?: string[]
  /** Credential-free metadata returned by the settings endpoint. */
  proxy_pool_configured?: boolean
  proxy_pool_count?: number
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
  settings: ReliabilityTurnStateSettings,
): Promise<ReliabilityTurnStateSettings> {
  const { data } = await apiClient.put<ReliabilityTurnStateSettings>(
    '/admin/reliability/turn-state-settings',
    settings,
  )
  return data
}

export const reliabilityAPI = {
  getStatus: getReliabilityStatus,
  getTurnStateSettings: getReliabilityTurnStateSettings,
  updateTurnStateSettings: updateReliabilityTurnStateSettings,
}

export default reliabilityAPI
