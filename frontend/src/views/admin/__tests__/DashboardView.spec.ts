import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, RouterLinkStub } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import type { DashboardStats } from '@/types'
import DashboardView from '../DashboardView.vue'

const { getSnapshotV2 } = vi.hoisted(() => ({
  getSnapshotV2: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getSnapshotV2
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createDashboardStats = (): DashboardStats => ({
  total_users: 0,
  today_new_users: 0,
  active_users: 0,
  hourly_active_users: 0,
  stats_updated_at: '',
  stats_stale: false,
  total_api_keys: 0,
  active_api_keys: 0,
  total_accounts: 0,
  normal_accounts: 0,
  error_accounts: 0,
  ratelimit_accounts: 0,
  overload_accounts: 0,
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_creation_tokens: 0,
  total_cache_read_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  today_requests: 0,
  today_input_tokens: 0,
  today_output_tokens: 0,
  today_cache_creation_tokens: 0,
  today_cache_read_tokens: 0,
  today_tokens: 0,
  today_cost: 0,
  today_actual_cost: 0,
  average_duration_ms: 0,
  uptime: 0,
  rpm: 0,
  tpm: 0
})

const mountDashboard = () => mount(DashboardView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      RouterLink: RouterLinkStub,
      LoadingSpinner: true,
      Icon: true,
      DateRangePicker: true,
      Select: true,
      ModelDistributionChart: {
        template: '<div data-test="model-chart" />'
      },
      TokenUsageTrend: true
    }
  }
})

describe('admin DashboardView', () => {
  beforeEach(() => {
    setActivePinia(createPinia())

    getSnapshotV2.mockReset()

    getSnapshotV2.mockResolvedValue({
      stats: createDashboardStats(),
      trend: [],
      models: []
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('uses last 24 hours as default dashboard range', async () => {
    mountDashboard()

    await flushPromises()

    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))
  })

  it('does not render or request user-specific dashboard data', async () => {
    const wrapper = mountDashboard()

    await flushPromises()

    expect(wrapper.text()).not.toContain('admin.dashboard.users')
    expect(wrapper.text()).not.toContain('admin.dashboard.activeUsers')
    expect(wrapper.text()).not.toContain('admin.dashboard.recentUsage')
    expect(wrapper.find('[data-test="model-chart"]').attributes()).not.toHaveProperty('enable-breakdown')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    const snapshotParams = getSnapshotV2.mock.calls[0][0]
    expect(snapshotParams).not.toHaveProperty('user_id')
    expect(snapshotParams).not.toHaveProperty('include_users_trend')
  })

  it('offers only supported management shortcuts without extra data requests', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const shortcuts = wrapper.findAllComponents(RouterLinkStub)
    expect(shortcuts.map(shortcut => shortcut.props('to'))).toEqual([
      '/admin/groups', '/admin/accounts', '/keys'
    ])
    expect(shortcuts.map(shortcut => shortcut.text())).toEqual([
      expect.stringContaining('admin.dashboard.groupPricing'),
      expect.stringContaining('nav.accounts'),
      expect.stringContaining('nav.apiKeys')
    ])
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
  })

  it('shows a retry action after the initial snapshot fails and recovers on retry', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    getSnapshotV2.mockRejectedValueOnce(new Error('Network unavailable'))
    const wrapper = mountDashboard()
    await flushPromises()

    const errorState = wrapper.get('[role="alert"]')
    expect(errorState.text()).toContain('admin.dashboard.failedToLoad')
    expect(wrapper.findAllComponents(RouterLinkStub)).toHaveLength(3)
    expect(wrapper.find('[data-test="model-chart"]').exists()).toBe(false)

    await errorState.get('button').trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(2)
    expect(getSnapshotV2.mock.calls[1][0]).toEqual(getSnapshotV2.mock.calls[0][0])
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="model-chart"]').exists()).toBe(true)
  })

  it('keeps the last successful statistics visible when refresh fails', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const wrapper = mountDashboard()
    await flushPromises()
    getSnapshotV2.mockRejectedValueOnce(new Error('Refresh unavailable'))

    await wrapper.findAll('button').find(button => button.text().includes('common.refresh'))!.trigger('click')
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain('admin.dashboard.todayRequests')
    expect(wrapper.find('[data-test="model-chart"]').exists()).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
})
