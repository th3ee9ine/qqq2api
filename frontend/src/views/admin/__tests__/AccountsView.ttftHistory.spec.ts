import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import AccountsView from '../AccountsView.vue'
import type { OpenAIOAuthTTFTHistory } from '@/types'

const { list, listWithEtag, getById, getSettings, getBatchTodayStats, getBatchLifetimeStats, getUpstreamBillingProbeSettings, getAllProxies, getAllGroups } = vi.hoisted(() => ({
  list: vi.fn(), listWithEtag: vi.fn(), getById: vi.fn(), getSettings: vi.fn(),
  getBatchTodayStats: vi.fn(), getBatchLifetimeStats: vi.fn(),
  getUpstreamBillingProbeSettings: vi.fn(), getAllProxies: vi.fn(), getAllGroups: vi.fn()
}))

vi.mock('@/api/admin', () => ({ adminAPI: {
  accounts: {
    list, listWithEtag, getById, getBatchTodayStats, getBatchLifetimeStats, getUpstreamBillingProbeSettings
  },
  settings: { getOpenAIOAuthTTFTGuardSettings: getSettings },
  proxies: { getAll: getAllProxies },
  groups: { getAll: getAllGroups }
} }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ token: 'test-token', isSimpleMode: false }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => key === 'admin.accounts.ttftHistory.seconds' ? `${params?.value}s` : key
  }) }
})

enableAutoUnmount(afterEach)

const baseRow = {
  id: 42, name: 'subscription', platform: 'openai', type: 'oauth', status: 'active',
  schedulable: true, concurrency: 2, priority: 1, group_ids: [], extra: {}, credentials: {},
  updated_at: '2026-10-11T10:00:00Z', created_at: '2026-10-11T10:00:00Z', last_used_at: '2026-10-11T10:00:00Z'
}
const history = (milliseconds: number, threshold = 15_000): OpenAIOAuthTTFTHistory => ({
  threshold_ms: threshold,
  recent_requests: [{ first_token_ms: milliseconds, observed_at: '2026-10-11T12:00:00Z', model: 'gpt-5.5' }]
})
const page = (items: unknown[]) => ({ items, total: items.length, page: 1, page_size: 20, pages: 1 })

function mountView(desktop = true) {
  vi.spyOn(window, 'matchMedia').mockImplementation((query: string) => ({
    matches: desktop, media: query, onchange: null,
    addEventListener: vi.fn(), removeEventListener: vi.fn(),
    addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn()
  }))
  return mount(AccountsView, { global: { stubs: {
    AppLayout: { template: '<div><slot /></div>' },
    TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
    AccountTableActions: { template: '<div><slot name="after" /></div>' },
    AccountTableFilters: true, AccountBulkActionsBar: true, Pagination: true,
    ConfirmDialog: true, AccountActionMenu: true, ImportDataModal: true,
    ReAuthAccountModal: true, AccountTestModal: true, AccountStatsModal: true,
    ScheduledTestsPanel: true, SyncFromCrsModal: true, TempUnschedStatusModal: true,
    ErrorPassthroughRulesModal: true, TLSFingerprintProfilesModal: true,
    CreateAccountModal: true, EditAccountModal: true, BulkEditAccountModal: true,
    PlatformTypeBadge: true, AccountCapacityCell: true, AccountStatusIndicator: true,
    AccountTodayStatsCell: true, AccountGroupsCell: true, AccountUsageCell: true,
    UpstreamBillingRateCell: true, Icon: true, Teleport: true
  } } })
}

describe('AccountsView TTFT history', () => {
  beforeEach(() => {
    localStorage.clear()
    list.mockReset().mockResolvedValue(page([{ ...baseRow, openai_oauth_ttft: history(15_001) }]))
    listWithEtag.mockReset().mockResolvedValue({ notModified: true, etag: 'unchanged', data: null })
    getById.mockReset()
    getSettings.mockReset()
    getBatchTodayStats.mockReset().mockResolvedValue({ stats: {} })
    getBatchLifetimeStats.mockReset().mockResolvedValue({ stats: {} })
    getUpstreamBillingProbeSettings.mockReset().mockResolvedValue({ enabled: false })
    getAllProxies.mockReset().mockResolvedValue([])
    getAllGroups.mockReset().mockResolvedValue([])
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  it.each([true, false])('renders enabled history and empty history in the real DataTable desktop=%s layout', async (desktop) => {
    list.mockResolvedValue(page([
      { ...baseRow, openai_oauth_ttft: history(15_001) },
      { ...baseRow, id: 43, name: 'protection-off' },
      { ...baseRow, id: 44, name: 'setup-token', type: 'setup-token', openai_oauth_ttft: { threshold_ms: 15_000, recent_requests: [] } }
    ]))
    const wrapper = mountView(desktop)
    await flushPromises()
    expect(wrapper.find('table').exists()).toBe(desktop)
    if (!desktop) expect(wrapper.find('[data-field="status"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-testid="account-ttft-history"]')).toHaveLength(2)
    expect(wrapper.get('[data-testid="account-ttft-sample"]').text()).toBe('15.001s')
    expect(wrapper.get('[data-testid="account-ttft-empty"]').text()).toBe('admin.accounts.ttftHistory.empty')
    expect(list).toHaveBeenCalledWith(1, 20, expect.objectContaining({ lite: '1' }), expect.any(Object))
    expect(getById).not.toHaveBeenCalled()
    expect(getSettings).not.toHaveBeenCalled()
  })

  it('refreshes history and thresholds even without a changed account updated_at, and hides after disable', async () => {
    vi.useFakeTimers()
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false)
    localStorage.setItem('account-auto-refresh', JSON.stringify({ enabled: true, interval_seconds: 5 }))
    listWithEtag
      .mockResolvedValueOnce({ notModified: false, etag: 'new-history', data: page([{ ...baseRow, openai_oauth_ttft: history(16_000) }]) })
      .mockResolvedValueOnce({ notModified: false, etag: 'new-threshold', data: page([{ ...baseRow, openai_oauth_ttft: history(16_000, 20_000) }]) })
      .mockResolvedValueOnce({ notModified: false, etag: 'disabled', data: page([{ ...baseRow }]) })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-testid="account-ttft-sample"]').text()).toBe('15.001s')
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(wrapper.get('[data-testid="account-ttft-sample"]').text()).toBe('16s')
    expect(wrapper.get('[data-testid="account-ttft-sample"]').attributes('data-slow')).toBe('true')
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(wrapper.get('[data-testid="account-ttft-sample"]').attributes('data-slow')).toBe('false')
    await vi.advanceTimersByTimeAsync(6000)
    await flushPromises()
    expect(wrapper.find('[data-testid="account-ttft-history"]').exists()).toBe(false)
    expect(listWithEtag).toHaveBeenCalledTimes(3)
    expect(getById).not.toHaveBeenCalled()
    expect(getSettings).not.toHaveBeenCalled()
  })
})
