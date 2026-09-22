import { beforeEach, describe, expect, it, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get: mocks.get, put: mocks.put },
}))

import {
  getReliabilityStatus,
  getReliabilityTurnStateSettings,
  normalizeReliabilityStatus,
  updateReliabilityTurnStateSettings,
} from '@/api/admin/reliability'

beforeEach(() => {
  mocks.get.mockReset()
  mocks.put.mockReset()
})

describe('turn-state reliability status API', () => {
  it('keeps a nested Turn State response authoritative', () => {
    const normalized = normalizeReliabilityStatus({
      summary: { turn_state: { supported: true, collector: { status: 'ready' } } },
      turn_state: { supported: false },
    })

    expect(normalized).toEqual({
      turn_state: { supported: true, collector: { status: 'ready' } },
    })
  })

  it('accepts the current flat Turn State response', () => {
    expect(normalizeReliabilityStatus({
      turn_state: { supported: true, collector: { status: 'degraded' } },
    })).toEqual({
      turn_state: { supported: true, collector: { status: 'degraded' } },
    })
  })

  it('loads only the Turn State projection endpoint', async () => {
    const status = { turn_state: { supported: true, collector: { status: 'ready' } } }
    mocks.get.mockResolvedValue({ data: status })

    await expect(getReliabilityStatus()).resolves.toEqual(status)
    expect(mocks.get).toHaveBeenCalledWith('/admin/reliability/status')
  })
})

describe('turn-state reliability settings API', () => {
  it('loads the active probe and injection settings', async () => {
    const settings = {
      probe_enabled: true,
      injection_enabled: false,
      proxy_pool_urls: ['https://proxy.example.com:443'],
    }
    mocks.get.mockResolvedValue({ data: settings })

    await expect(getReliabilityTurnStateSettings()).resolves.toEqual(settings)
    expect(mocks.get).toHaveBeenCalledWith('/admin/reliability/turn-state-settings')
  })

  it('persists both settings together', async () => {
    const settings = {
      probe_enabled: false,
      injection_enabled: true,
      proxy_pool_urls: ['socks5://proxy.example.com:1080'],
    }
    mocks.put.mockResolvedValue({ data: settings })

    await expect(updateReliabilityTurnStateSettings(settings)).resolves.toEqual(settings)
    expect(mocks.put).toHaveBeenCalledWith('/admin/reliability/turn-state-settings', settings)
  })
})
