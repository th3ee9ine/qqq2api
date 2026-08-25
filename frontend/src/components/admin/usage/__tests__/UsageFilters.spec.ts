import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

import UsageFilters from '../UsageFilters.vue'

// --- i18n messages (only what UsageFilters needs) ---
const messages: Record<string, string> = {
  'usage.apiKeyFilter': 'API Key',
  'admin.usage.searchApiKeyPlaceholder': 'Search API key...',
  'usage.model': 'Model',
  'admin.usage.allModels': 'All Models',
  'admin.usage.account': 'Account',
  'admin.usage.searchAccountPlaceholder': 'Search account...',
  'usage.type': 'Type',
  'admin.usage.allTypes': 'All Types',
  'usage.ws': 'WS',
  'usage.stream': 'Stream',
  'usage.sync': 'Sync',
  'admin.usage.billingMode': 'Billing Mode',
  'admin.usage.allBillingModes': 'All Billing Modes',
  'admin.usage.billingModeToken': 'Token',
  'admin.usage.billingModePerRequest': 'Per Request',
  'admin.usage.billingModeImage': 'Image',
	'admin.usage.upstreamModelAudit': 'Upstream model audit',
	'admin.usage.allUpstreamModelAudit': 'All response model states',
	'admin.usage.upstreamModelMismatchOnly': 'Mismatched only',
	'admin.usage.upstreamModelMatchedOnly': 'Matched only',
  'admin.usage.group': 'Group',
  'admin.usage.allGroups': 'All Groups',
  'common.refresh': 'Refresh',
  'common.reset': 'Reset',
  'admin.usage.cleanup.button': 'Cleanup',
  'usage.exportExcel': 'Export',
}

// Mock vue-i18n
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const mockSearchApiKeys = vi.fn().mockResolvedValue([])
const mockGroupsList = vi.fn().mockResolvedValue({ items: [] })
const mockGetModelStats = vi.fn().mockResolvedValue({ models: [] })
const mockAccountsList = vi.fn().mockResolvedValue({ items: [] })

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      searchApiKeys: (...args: any[]) => mockSearchApiKeys(...args),
    },
    groups: { list: (...args: any[]) => mockGroupsList(...args) },
    dashboard: { getModelStats: (...args: any[]) => mockGetModelStats(...args) },
    accounts: { list: (...args: any[]) => mockAccountsList(...args) },
  },
}))

// Default props helper
const defaultFilters = () => ({
  api_key_id: undefined,
  account_id: undefined,
  model: null,
  request_type: null,
  billing_type: null,
  billing_mode: null,
	upstream_model_mismatch: null,
  group_id: null,
  start_date: '',
  end_date: '',
})

function mountFilters(filters = defaultFilters()) {
  return mount(UsageFilters, {
    props: {
      modelValue: filters,
      exporting: false,
      startDate: '2026-05-01',
      endDate: '2026-05-28',
      showActions: false,
      modelOptions: [],
    },
    global: {
      stubs: {
        Select: true,
        Teleport: true,
      },
    },
  })
}

describe('UsageFilters — global API key search', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockSearchApiKeys.mockReset().mockResolvedValue([{ id: 7, name: 'system-key' }])
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('searches the global key set and selects by api_key_id without a user filter', async () => {
    const wrapper = mountFilters()
    const input = wrapper.find('input[type="text"]')
    await input.trigger('focus')
    await input.setValue('system')
    vi.advanceTimersByTime(300)
    await flushPromises()

    expect(mockSearchApiKeys).toHaveBeenCalledWith('system')
    const keyButton = wrapper.findAll('.usage-filter-dropdown button[type="button"]')
      .find((button) => button.text().includes('system-key'))
    expect(keyButton).toBeDefined()
    await keyButton!.trigger('click')
    await flushPromises()

    expect(wrapper.props('modelValue').api_key_id).toBe(7)
    expect(wrapper.props('modelValue')).not.toHaveProperty('user_id')
    expect(wrapper.text()).not.toContain('Search user')
  })
})

describe('UsageFilters — model options come from prop (no dup request)', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    mockGetModelStats.mockClear()
    mockGroupsList.mockClear()
  })
  afterEach(() => { vi.useRealTimers() })

  it('does not call dashboard.getModelStats on mount and renders model options from prop', async () => {
    const wrapper = mount(UsageFilters, {
      props: {
        modelValue: defaultFilters(),
        exporting: false,
        startDate: '2026-05-01',
        endDate: '2026-05-28',
        showActions: false,
        modelOptions: ['claude-3', 'gpt-4o'],
      },
      global: { stubs: { Select: true, Teleport: true } },
    })
    await flushPromises()

    expect(mockGetModelStats).not.toHaveBeenCalled()

    const opts = (wrapper.vm as any).modelOptions as Array<{ value: string | null; label: string }>
    expect(opts.map((o) => o.value)).toEqual([null, 'claude-3', 'gpt-4o'])
  })
})

describe('UsageFilters — retained system-wide dimensions', () => {
  it('does not expose retired subscription billing or video modes', () => {
    const wrapper = mountFilters()

    expect(wrapper.text()).not.toContain('Billing Type')
    expect(wrapper.text()).not.toContain('Subscription')
    expect((wrapper.vm as any).billingModeOptions.map((option: { value: string | null }) => option.value))
      .toEqual([null, 'token', 'per_request', 'image'])
  })
})
