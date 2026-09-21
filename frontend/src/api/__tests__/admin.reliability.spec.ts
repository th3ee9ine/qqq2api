import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get: mocks.get, put: mocks.put },
}))

import {
  getReliabilityTurnStateSettings,
  normalizeReliabilityStatus,
  updateReliabilityTurnStateSettings,
} from '@/api/admin/reliability'

beforeEach(() => {
  mocks.get.mockReset()
  mocks.put.mockReset()
})

describe('normalizeReliabilityStatus', () => {
  it('normalizes the nested aggregate response and keeps nested values authoritative', () => {
    const normalized = normalizeReliabilityStatus({
      summary: {
        account_availability: { total: 7, available: 5 },
        traffic: { current_concurrency: 3 },
        connection: { dial_timeout: 10, openai_ws: { dial_timeout_seconds: 8 } },
        turn_state: { supported: true, collector: { injection_enabled: false, status: 'cooldown' } },
      },
      account_availability: { total: 99, available: 99 },
      traffic: { current_concurrency: 99 },
    })

    expect(normalized.account_availability).toEqual({ total: 7, available: 5 })
    expect(normalized.traffic).toEqual({ current_concurrency: 3 })
    expect(normalized.connection).toEqual({ dial_timeout: 10, openai_ws: { dial_timeout_seconds: 8 } })
    expect(normalized.turn_state).toEqual({ supported: true, collector: { injection_enabled: false, status: 'cooldown' } })
  })

  it('normalizes the flat aggregate response used by earlier backends', () => {
    const normalized = normalizeReliabilityStatus({
      enabled: true,
      account_availability: { total_accounts: 4, available_count: 2 },
      traffic: { concurrency: 6 },
      connection: { status: 'unknown' },
      turn_state: { supported: true, collector: { status: 'degraded' } },
      diagnostics: { warnings: ['no_schedulable_accounts'] },
    })

    expect(normalized.account_availability).toEqual({ total_accounts: 4, available_count: 2 })
    expect(normalized.traffic).toEqual({ concurrency: 6 })
    expect(normalized.connection).toEqual({ status: 'unknown' })
    expect(normalized.turn_state).toEqual({ supported: true, collector: { status: 'degraded' } })
    expect(normalized.diagnostics).toEqual({ warnings: ['no_schedulable_accounts'] })
  })
})

describe('turn-state reliability settings API', () => {
  it('loads the active probe and injection settings', async () => {
    const settings = { probe_enabled: true, injection_enabled: false }
    mocks.get.mockResolvedValue({ data: settings })

    await expect(getReliabilityTurnStateSettings()).resolves.toEqual(settings)
    expect(mocks.get).toHaveBeenCalledWith('/admin/reliability/turn-state-settings')
  })

  it('persists both settings together', async () => {
    const settings = { probe_enabled: false, injection_enabled: true }
    mocks.put.mockResolvedValue({ data: settings })

    await expect(updateReliabilityTurnStateSettings(settings)).resolves.toEqual(settings)
    expect(mocks.put).toHaveBeenCalledWith('/admin/reliability/turn-state-settings', settings)
  })
})
