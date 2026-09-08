import { apiClient } from '../client'

export interface UpstreamTestDefaults {
  endpoint: string
  source: string
  account_type: string
  body: Record<string, unknown>
  upstream_body?: Record<string, unknown>
  headers: Record<string, string>
  url?: string
  notes?: string[]
}

/** Returns the exact redacted request template used by the backend account test service. */
export async function getUpstreamTestDefaults(endpoint: string, accountId?: string, prompt?: string) {
  const { data } = await apiClient.get<UpstreamTestDefaults>('/admin/accounts/test-defaults', {
    params: {
      endpoint,
      ...(accountId && /^\d+$/.test(accountId) ? { account_id: accountId } : {}),
      ...(prompt ? { prompt } : {})
    }
  })
  return data
}
