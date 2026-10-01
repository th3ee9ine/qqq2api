import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAdminSettingsStore } from '@/stores/adminSettings'

const mockGetSettings = vi.fn()
const mockGetPaymentConfig = vi.fn()

vi.mock('@/api', () => ({
  adminAPI: {
    settings: {
      getSettings: (...args: unknown[]) => mockGetSettings(...args),
    },
    payment: {
      getConfig: (...args: unknown[]) => mockGetPaymentConfig(...args),
    },
  },
}))

describe('useAdminSettingsStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('loads retained settings without requesting the removed payment endpoint', async () => {

    mockGetPaymentConfig.mockRejectedValue(new Error('404 Not Found'))
    mockGetSettings.mockResolvedValue({
      ops_monitoring_enabled: false,
      ops_realtime_monitoring_enabled: true,
      ops_query_mode_default: 'preagg',
    })

    const store = useAdminSettingsStore()

    await store.fetch()

    expect(mockGetSettings).toHaveBeenCalledTimes(1)
    expect(mockGetPaymentConfig).not.toHaveBeenCalled()
    expect(store.loaded).toBe(true)
    expect(store.loading).toBe(false)
    expect(store.opsMonitoringEnabled).toBe(false)
    expect(store.opsRealtimeMonitoringEnabled).toBe(true)
    expect(store.opsQueryModeDefault).toBe('preagg')
    expect(localStorage.getItem('ops_monitoring_enabled_cached')).toBe('false')
    expect(localStorage.getItem('ops_realtime_monitoring_enabled_cached')).toBe('true')
    expect(localStorage.getItem('ops_query_mode_default_cached')).toBe('preagg')
  })
})
