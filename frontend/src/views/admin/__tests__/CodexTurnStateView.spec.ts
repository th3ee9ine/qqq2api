import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTurnStateView from '../CodexTurnStateView.vue'
import type { AccountListItem, CodexTurnStateAutoInfo, Proxy } from '@/types'

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  listProxies: vi.fn(),
  listAccounts: vi.fn(),
  collectCodexTurnState: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getSettings: mocks.getSettings,
      updateSettings: mocks.updateSettings,
    },
    proxies: { list: mocks.listProxies },
    accounts: {
      list: mocks.listAccounts,
      collectCodexTurnState: mocks.collectCodexTurnState,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: mocks.showError }),
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    locale: { value: 'en' },
    t: (key: string, values?: Record<string, unknown>) => {
      const suffix = values ? ` ${Object.values(values).join(' ')}` : ''
      return `${key}${suffix}`
    },
  }),
}))

function proxy(id: number): Proxy {
  return {
    id,
    name: `Route ${id}`,
    protocol: 'http',
    host: `proxy-${id}.example.com`,
    port: 8080,
    username: 'private-user',
    password: 'private-password',
    status: 'active',
    max_accounts: 0,
    expires_at: null,
    fallback_mode: 'direct',
    expiry_warn_days: 7,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
  }
}

function state(overrides: Partial<CodexTurnStateAutoInfo> = {}): CodexTurnStateAutoInfo {
  return {
    configured: true,
    due: false,
    recovery_pending: false,
    expires_at_ms: Date.now() + 60_000,
    ...overrides,
  }
}

function account(id: number, overrides: Partial<AccountListItem> = {}): AccountListItem {
  return {
    id,
    name: `Account ${id}`,
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 0,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    codex_turn_state_auto: state(),
    ...overrides,
  } as AccountListItem
}

function mountView() {
  return mount(CodexTurnStateView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        AdminPageHeader: { template: '<header><slot /><slot name="actions" /></header>' },
        AdminOverviewStrip: { props: ['items'], template: '<div data-testid="overview">{{ JSON.stringify(items) }}</div>' },
        Icon: true,
      },
    },
  })
}

describe('CodexTurnStateView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getSettings.mockResolvedValue({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_ids: [2],
      openai_codex_turn_state_proxy_id: 0,
      openai_codex_turn_state_proxy_ids_valid: true,
    })
    mocks.updateSettings.mockResolvedValue({})
    mocks.listProxies.mockResolvedValue({ items: [proxy(1), proxy(2)], pages: 1 })
    mocks.listAccounts.mockResolvedValue({
      items: [
        account(11),
        account(12, { type: 'setup-token', codex_turn_state_auto: null }),
        account(13, { type: 'apikey' }),
      ],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValue({ status: 'queued', account_id: 12, codex_turn_state_auto: state({ recovery_pending: true }) })
  })

  it('loads eligible accounts and saves only the standalone Turn State settings', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.listAccounts).toHaveBeenCalledWith(1, 200, { platform: 'openai' })
    expect(wrapper.find('[data-testid="turn-state-account-11"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-13"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('private-user')
    expect(wrapper.text()).not.toContain('private-password')

    await wrapper.get('[data-testid="turn-state-auto-toggle"]').setValue(true)
    await wrapper.get('[data-testid="turn-state-default-model"]').setValue(' custom/probe ')
    await wrapper.get('[data-testid="turn-state-models"]').setValue(' GPT-5, gpt-5, Codex/* ')
    await wrapper.get('[data-testid="codex-turn-state-proxy-1"]').setValue(true)
    await wrapper.get('[data-testid="turn-state-save"]').trigger('submit')
    await flushPromises()

    expect(mocks.updateSettings).toHaveBeenCalledWith({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_models: 'gpt-5,codex/*',
      openai_codex_turn_state_default_model: 'custom/probe',
      openai_codex_turn_state_proxy_ids: [2, 1],
    })
    expect(mocks.updateSettings.mock.calls[0]?.[0]).not.toHaveProperty('openai_codex_turn_state_proxy_id')
  })

  it('uses the legacy proxy only when the array field is absent and degrades proxy loading independently', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_models: '*',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_id: 73,
    })
    mocks.listProxies.mockRejectedValueOnce(new Error('proxy inventory unavailable'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="turn-state-auto-toggle"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('proxy inventory unavailable')
    await wrapper.get('[data-testid="turn-state-save"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_ids: [73] }))
  })

  it('treats an explicit empty array as the compatible all-proxy pool instead of restoring the legacy proxy', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_ids: [],
      openai_codex_turn_state_proxy_id: 73,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="overview"]').text()).toContain('admin.codexTurnState.overview.compatiblePool')
    await wrapper.get('[data-testid="turn-state-save"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_ids: [] }))
  })

  it('blocks saving a malformed stored pool until the administrator explicitly clears it', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_ids: [],
      openai_codex_turn_state_proxy_id: 0,
      openai_codex_turn_state_proxy_ids_valid: false,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-proxy-pool-invalid"]').text()).toContain('admin.codexTurnState.proxyPool.invalidStored')
    expect(wrapper.get('[data-testid="turn-state-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.proxyPool.invalidStored')

    await wrapper.get('[data-testid="turn-state-proxy-pool-clear-invalid"]').trigger('click')
    expect(wrapper.find('[data-testid="turn-state-proxy-pool-invalid"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-save"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-testid="turn-state-save"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_ids: [] }))
  })

  it('accepts an explicit proxy reselection as repair for a malformed stored pool', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_ids: [],
      openai_codex_turn_state_proxy_id: 0,
      openai_codex_turn_state_proxy_ids_valid: false,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="codex-turn-state-proxy-1"]').setValue(true)
    expect(wrapper.find('[data-testid="turn-state-proxy-pool-invalid"]').exists()).toBe(false)
    await wrapper.get('[data-testid="turn-state-save"]').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_ids: [1] }))
  })

  it('passes an optional exact model to manual collection and merges redacted status', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-model-12"]').setValue('custom/manual-model')
    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()

    expect(mocks.collectCodexTurnState).toHaveBeenCalledWith(12, 'custom/manual-model')
    expect(mocks.showSuccess).toHaveBeenCalled()
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).toContain('admin.codexTurnState.accounts.states.pending')
  })

  it('never renders the backend last_error value', async () => {
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(14, {
        codex_turn_state_auto: state({
          configured: false,
          expires_at_ms: undefined,
          last_error: 'https://private.example/token?authorization=secret',
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-14"]').text()).toContain('admin.codexTurnState.accounts.details.error')
    expect(wrapper.text()).not.toContain('private.example')
    expect(wrapper.text()).not.toContain('authorization=secret')
  })

  it('classifies due unexpired states as renewal while keeping expired states expired', async () => {
    const future = Date.now() + 60_000
    const past = Date.now() - 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(20, { codex_turn_state_auto: state({ due: true, expires_at_ms: future }) }),
        account(21, { codex_turn_state_auto: state({ due: true, expires_at_ms: past }) }),
        account(22, {
          codex_turn_state_auto: state({
            due: false,
            expires_at_ms: future,
            models: {
              'gpt-renewal': state({ due: true, expires_at_ms: future }),
              'gpt-valid': state({ due: false, expires_at_ms: future }),
            },
          }),
        }),
      ],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-20"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-account-21"]').text()).toContain('admin.codexTurnState.accounts.states.expired')
    expect(wrapper.get('[data-testid="turn-state-account-22"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-account-22"]').text()).toContain('gpt-renewal')

    const overview = JSON.parse(wrapper.get('[data-testid="overview"]').text()) as Array<{ label: string; value: string | number }>
    expect(overview.find((item) => item.label === 'admin.codexTurnState.overview.valid')?.value).toBe(0)
    expect(overview.find((item) => item.label === 'admin.codexTurnState.overview.attention')?.value).toBe(3)

    await wrapper.get('[data-testid="turn-state-status-filter"]').setValue('renewal')
    expect(wrapper.find('[data-testid="turn-state-account-20"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-21"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="turn-state-account-22"]').exists()).toBe(true)
  })

  it('keeps recovery and cooldown states ahead of renewal', async () => {
    const future = Date.now() + 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(30, { codex_turn_state_auto: state({ due: true, recovery_pending: true, expires_at_ms: future }) }),
        account(31, { codex_turn_state_auto: state({ due: true, probe_not_before_ms: future, expires_at_ms: future }) }),
      ],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-30"]').text()).toContain('admin.codexTurnState.accounts.states.pending')
    expect(wrapper.get('[data-testid="turn-state-account-31"]').text()).toContain('admin.codexTurnState.accounts.states.cooldown')
    expect(wrapper.text()).not.toContain('admin.codexTurnState.accounts.states.renewal')
  })
})
