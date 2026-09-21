import { describe, expect, it, vi } from 'vitest'

vi.mock('@/api/client', () => ({
  apiClient: { get: vi.fn() },
}))

import { normalizeReliabilityStatus } from '@/api/admin/reliability'

describe('normalizeReliabilityStatus', () => {
  it('normalizes the nested aggregate response and keeps nested values authoritative', () => {
    const normalized = normalizeReliabilityStatus({
      summary: {
        account_availability: { total: 7, available: 5 },
        traffic: { current_concurrency: 3 },
        connection: { dial_timeout: 10, openai_ws: { dial_timeout_seconds: 8 } },
      },
      account_availability: { total: 99, available: 99 },
      traffic: { current_concurrency: 99 },
    })

    expect(normalized.account_availability).toEqual({ total: 7, available: 5 })
    expect(normalized.traffic).toEqual({ current_concurrency: 3 })
    expect(normalized.connection).toEqual({ dial_timeout: 10, openai_ws: { dial_timeout_seconds: 8 } })
  })

  it('normalizes the flat aggregate response used by earlier backends', () => {
    const normalized = normalizeReliabilityStatus({
      enabled: true,
      account_availability: { total_accounts: 4, available_count: 2 },
      traffic: { concurrency: 6 },
      connection: { status: 'unknown' },
      diagnostics: { warnings: ['no_schedulable_accounts'] },
    })

    expect(normalized.account_availability).toEqual({ total_accounts: 4, available_count: 2 })
    expect(normalized.traffic).toEqual({ concurrency: 6 })
    expect(normalized.connection).toEqual({ status: 'unknown' })
    expect(normalized.diagnostics).toEqual({ warnings: ['no_schedulable_accounts'] })
  })
})
