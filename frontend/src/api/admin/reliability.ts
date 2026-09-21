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
