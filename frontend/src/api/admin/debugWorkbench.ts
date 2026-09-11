import { apiClient } from '../client'

export interface UpstreamHeaderDetail {
  name: string
  purpose: string
  requirement: 'required' | 'recommended' | 'conditional' | 'transport'
  condition: string
  source: string
  default_included: boolean
}

export interface UpstreamTestDefaults {
  endpoint: string
  source: string
  account_type: string
  body: Record<string, unknown>
  upstream_body?: Record<string, unknown>
  proxy_id?: number | null
  proxy_url?: string
  proxy_name?: string
  headers: Record<string, string>
  url?: string
  notes?: string[]
  header_details?: UpstreamHeaderDetail[]
}

/** Returns the exact redacted request template used by the backend account test service. */
export async function getUpstreamTestDefaults(endpoint: string, accountId?: string, prompt?: string, proxyId?: number | null) {
  const { data } = await apiClient.get<UpstreamTestDefaults>('/admin/accounts/test-defaults', {
    params: {
      endpoint,
      ...(accountId && /^\d+$/.test(accountId) ? { account_id: accountId } : {}),
      ...(prompt ? { prompt } : {}),
      ...(proxyId != null ? { proxy_id: proxyId } : {})
    }
  })
  return data
}

export type DebugEndpoint = 'responses' | 'chat/completions' | 'images/generations'
export type DebugSessionAction = 'new_session' | 'new_turn' | 'continue_turn'
export interface DebugWorkbenchRequest {
  endpoint: DebugEndpoint
  headers: Record<string, string>
  body: Record<string, unknown>
  proxy_id?: number
  session: { id?: string; action: DebugSessionAction }
}
export interface DebugSnapshot {
  method?: string
  url?: string
  status_code?: number
  headers: Record<string, string[]>
  body?: unknown
  body_text?: string
  body_encoding?: 'base64'
  body_bytes: number
  captured_bytes: number
  truncated: boolean
  complete: boolean
}
export interface DebugHeaderChange {
  name: string
  action: 'preserved' | 'rewritten' | 'generated' | 'filtered'
  input_values?: string[]
  output_values?: string[]
  reason: string
}
export interface DebugAttempt {
  index: number
  transport: string
  account_id: number
  proxy: string
  request: DebugSnapshot
  response?: DebugSnapshot
  error?: string
  duration_ms: number
  ttft_ms?: number
  header_changes: DebugHeaderChange[]
}
export interface DebugSession {
  id: string
  session_id: string
  thread_id: string
  turn_id: string
  window_id: string
  turn_index: number
  turn_state_available: boolean
}
export interface DebugWorkbenchResult {
  request_id: string
  success: boolean
  endpoint: DebugEndpoint
  transport: string
  duration_ms: number
  session: DebugSession
  inbound: DebugSnapshot
  outbound: DebugSnapshot
  attempts: DebugAttempt[]
  warnings?: string[]
  error?: string
}

/** Sends the complete editor payload through the actual gateway and returns redacted execution traces. */
export async function runDebugWorkbench(accountId: string, payload: DebugWorkbenchRequest, signal?: AbortSignal) {
  const { data } = await apiClient.post<DebugWorkbenchResult>(`/admin/accounts/${accountId}/debug`, payload, {
    timeout: 180000,
    signal
  })
  return data
}
