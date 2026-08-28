import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import AccountsView from '../AccountsView.vue'

const { listAccounts } = vi.hoisted(() => ({
  listAccounts: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      list: listAccounts,
      listWithEtag: vi.fn(),
      getBatchTodayStats: vi.fn().mockResolvedValue({ stats: {} }),
      getBatchLifetimeStats: vi.fn().mockResolvedValue({ stats: {} }),
      getUpstreamBillingProbeSettings: vi.fn().mockResolvedValue({ enabled: true, interval_minutes: 30 }),
      delete: vi.fn(),
      batchClearError: vi.fn(),
      batchRefresh: vi.fn(),
      toggleSchedulable: vi.fn()
    },
    proxies: { getAll: vi.fn().mockResolvedValue([]) },
    groups: { getAll: vi.fn().mockResolvedValue([]) }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showInfo: vi.fn() })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ token: 'test-token', isAdmin: true, isSimpleMode: false })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

const DataTableStub = {
  props: ['columns', 'data'],
  emits: ['sort'],
  template: `
    <div data-test="data-table">
      <span v-for="column in columns" :key="column.key" :data-column="column.key" />
      <div v-for="row in data" :key="row.id" data-test="proxy-cell">
        <slot name="cell-proxy" :row="row" />
      </div>
    </div>
  `
}

function mountView() {
  return mount(AccountsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        AccountTableActions: { template: '<div><slot name="after" /></div>' },
        AccountTableFilters: true,
        AccountBulkActionsBar: true,
        Pagination: true,
        ConfirmDialog: true,
        AccountActionMenu: true,
        ImportDataModal: true,
        ReAuthAccountModal: true,
        AccountTestModal: true,
        AccountStatsModal: true,
        ScheduledTestsPanel: true,
        SyncFromCrsModal: true,
        TempUnschedStatusModal: true,
        ErrorPassthroughRulesModal: true,
        TLSFingerprintProfilesModal: true,
        CreateAccountModal: true,
        EditAccountModal: true,
        BulkEditAccountModal: true,
        PlatformTypeBadge: true,
        AccountCapacityCell: true,
        AccountStatusIndicator: true,
        AccountTodayStatsCell: true,
        AccountGroupsCell: true,
        AccountUsageCell: true,
        HelpTooltip: true,
        Icon: true,
        Teleport: true
      }
    }
  })
}

describe('admin AccountsView bound proxy column', () => {
  beforeEach(() => {
    localStorage.clear()
    listAccounts.mockReset().mockResolvedValue({
      items: [{
        id: 7,
        name: 'proxy-account',
        platform: 'openai',
        type: 'oauth',
        proxy_id: 19,
        proxy: {
          id: 19,
          name: 'Tokyo pool 1',
          protocol: 'socks5h',
          host: '2001:db8::10',
          port: 1080,
          username: 'agent',
          status: 'active',
          country_code: 'JP',
          max_accounts: 5,
          expires_at: null,
          fallback_mode: 'none',
          expiry_warn_days: 7,
          created_at: '2026-08-28T00:00:00Z',
          updated_at: '2026-08-28T00:00:00Z'
        },
        concurrency: 1,
        priority: 1,
        status: 'active',
        schedulable: true,
        error_message: null,
        last_used_at: null,
        expires_at: null,
        auto_pause_on_expired: false,
        created_at: '2026-08-28T00:00:00Z',
        updated_at: '2026-08-28T00:00:00Z'
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1
    })
  })

  it('shows the concrete bound proxy endpoint by default', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-column="proxy"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="proxy-cell"]').text()).toContain('Tokyo pool 1')
    expect(wrapper.get('[data-test="proxy-cell"]').text()).toContain('#19')
    expect(wrapper.get('[data-testid="account-proxy-endpoint"]').text()).toBe(
      'socks5h://agent@[2001:db8::10]:1080'
    )
  })

  it('reveals the proxy column once for older saved layouts', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['proxy', 'today_stats']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-column="proxy"]').exists()).toBe(true)
    expect(JSON.parse(localStorage.getItem('account-hidden-columns') || '[]')).not.toContain('proxy')
    expect(localStorage.getItem('account-proxy-column-version')).toBe('proxy-endpoint-visible-by-default')
  })

  it('keeps an explicit hide preference after the proxy-column migration', async () => {
    localStorage.setItem('account-hidden-columns', JSON.stringify(['proxy', 'today_stats']))
    localStorage.setItem('account-hidden-columns-version', 'scheduler-score-hidden-by-default')
    localStorage.setItem('account-proxy-column-version', 'proxy-endpoint-visible-by-default')

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-column="proxy"]').exists()).toBe(false)
  })
})
