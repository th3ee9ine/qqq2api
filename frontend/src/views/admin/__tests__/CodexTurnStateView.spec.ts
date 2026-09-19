import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTurnStateView from '../CodexTurnStateView.vue'
import type { AccountListItem, CodexTurnStateAutoInfo } from '@/types'

const mocks = vi.hoisted(() => ({
  getSettings: vi.fn(),
  updateSettings: vi.fn(),
  listAccounts: vi.fn(),
  getAccountById: vi.fn(),
  collectCodexTurnState: vi.fn(),
  listCodexTurnStateTasks: vi.fn(),
  cancelCodexTurnStateTask: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: {
      getSettings: mocks.getSettings,
      updateSettings: mocks.updateSettings,
    },
    accounts: {
      list: mocks.listAccounts,
      getById: mocks.getAccountById,
      collectCodexTurnState: mocks.collectCodexTurnState,
      listCodexTurnStateTasks: mocks.listCodexTurnStateTasks,
      cancelCodexTurnStateTask: mocks.cancelCodexTurnStateTask,
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

function state(overrides: Partial<CodexTurnStateAutoInfo> = {}): CodexTurnStateAutoInfo {
  return {
    configured: true,
    due: false,
    recovery_pending: false,
    expires_at_ms: Date.now() + 60_000,
    ...overrides,
  }
}

function failedScopedState(probeAt: number, error = 'transport_failed'): CodexTurnStateAutoInfo {
  return state({
    configured: false,
    due: true,
    expires_at_ms: undefined,
    collection_succeeded: false,
    last_error: error,
    probe_at_ms: probeAt,
    models: {
      'gpt-5.5': state({
        configured: false,
        due: true,
        expires_at_ms: undefined,
        collection_succeeded: false,
        last_error: error,
        probe_at_ms: probeAt,
      }),
    },
  })
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
    codex_turn_state_auto: state({ verified_model: 'gpt-5.5' }),
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
        RouterLink: { props: ['to'], template: '<a data-testid="router-link"><slot /></a>' },
        ConfirmDialog: {
          props: ['show'],
          emits: ['confirm', 'cancel'],
          template: '<div v-if="show" data-testid="confirm-dialog"><button data-testid="confirm-dialog-confirm" @click="$emit(\'confirm\')">confirm</button></div>',
        },
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
      openai_codex_turn_state_auto_interval_minutes: 50,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: ['socks5://existing:secret@proxy.example:1080'],
      openai_codex_turn_state_proxy_urls_valid: true,
      openai_codex_turn_state_proxy_pool_configured: true,
      openai_codex_turn_state_proxy_pool_count: 1,
    })
    mocks.updateSettings.mockResolvedValue({})
    mocks.listAccounts.mockResolvedValue({
      items: [
        account(11),
        account(12, { type: 'setup-token', codex_turn_state_auto: state({ configured: false, due: true, expires_at_ms: undefined }) }),
        account(13, { type: 'apikey' }),
      ],
      pages: 1,
    })
    mocks.getAccountById.mockResolvedValue(account(12))
    mocks.listCodexTurnStateTasks.mockResolvedValue([])
    mocks.cancelCodexTurnStateTask.mockResolvedValue({})
    mocks.collectCodexTurnState.mockResolvedValue({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.5'],
      queued_models: ['gpt-5.5'],
      model_targets: [{ model: 'gpt-5.5', owner: 'gpt-5.5' }],
      codex_turn_state_auto: state({ configured: false, recovery_pending: true, expires_at_ms: undefined }),
    })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('loads healthy accounts and appends to the reloaded URL pool without using proxy inventory', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.listAccounts).toHaveBeenCalledWith(1, 200, { platform: 'openai', status: 'active' })
    expect(wrapper.find('[data-testid="turn-state-account-11"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-13"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-0"]').attributes('type')).toBe('password')

    await wrapper.get('[data-testid="turn-state-auto-toggle"]').setValue(true)
    await wrapper.get('[data-testid="turn-state-auto-interval"]').setValue(45)
    await wrapper.get('[data-testid="turn-state-default-model"]').setValue(' custom/probe ')
    await wrapper.get('[data-testid="turn-state-models"]').setValue(' GPT-5, gpt-5, Codex/* ')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-add"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-1"]').setValue('socks5://new:secret@NEW.EXAMPLE:1081')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateSettings).toHaveBeenCalledWith({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_auto_interval_minutes: 45,
      openai_codex_turn_state_models: 'gpt-5,codex/*',
      openai_codex_turn_state_default_model: 'custom/probe',
      openai_codex_turn_state_proxy_urls: [
        'socks5://existing:secret@proxy.example:1080',
        'socks5://new:secret@new.example:1081',
      ],
    })
    expect(mocks.updateSettings.mock.calls[0]?.[0]).not.toHaveProperty('openai_codex_turn_state_proxy_ids')
    expect(mocks.updateSettings.mock.calls[0]?.[0]).not.toHaveProperty('openai_codex_turn_state_proxy_id')
  })

  it('batch-appends through the real dialog and waits for the settings form submission to persist', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-batch-open"]').trigger('click')
    await flushPromises()
    const input = document.body.querySelector<HTMLTextAreaElement>('[data-testid="codex-turn-state-proxy-url-batch-input"]')
    expect(input).not.toBeNull()
    input!.value = [
      'socks5://existing:secret@PROXY.EXAMPLE:1080',
      'socks5://batch-one:secret@ONE.EXAMPLE:1081',
      'socks5://batch-two:secret@two.example:1082',
    ].join('\n')
    input!.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()

    const apply = document.body.querySelector<HTMLButtonElement>('[data-testid="codex-turn-state-proxy-url-batch-apply"]')
    expect(apply?.disabled).toBe(false)
    apply!.click()
    await flushPromises()

    expect(mocks.updateSettings).not.toHaveBeenCalled()
    await vi.waitFor(() => {
      expect(document.body.querySelector('[data-testid="codex-turn-state-proxy-url-batch-input"]')).toBeNull()
    })

    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      openai_codex_turn_state_proxy_urls: [
        'socks5://existing:secret@proxy.example:1080',
        'socks5://batch-one:secret@one.example:1081',
        'socks5://batch-two:secret@two.example:1082',
      ],
    }))

    wrapper.unmount()
  })

  it('rejects an automatic renewal interval outside the 1 to 60 minute range', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-auto-interval"]').setValue('1.5')
    await wrapper.get('form').trigger('submit')

    expect(mocks.updateSettings).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.settings.invalidAutoInterval')
  })

  it('allows an empty dedicated pool and saves the global IP management fallback', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
      openai_codex_turn_state_proxy_pool_configured: false,
      openai_codex_turn_state_proxy_pool_count: 0,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="overview"]').text()).toContain('admin.codexTurnState.overview.globalPool')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_urls: [] }))
  })

  it('blocks a malformed stored pool until the administrator explicitly clears it', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: false,
      openai_codex_turn_state_proxy_pool_configured: false,
      openai_codex_turn_state_proxy_pool_count: 0,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-proxy-pool-invalid"]').text()).toContain('admin.codexTurnState.proxyPool.invalidStored')
    expect(wrapper.get('[data-testid="turn-state-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateSettings).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="turn-state-proxy-pool-clear-invalid"]').trigger('click')
    expect(wrapper.find('[data-testid="turn-state-proxy-pool-invalid"]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({ openai_codex_turn_state_proxy_urls: [] }))
  })

  it('shows row-level URL errors and does not submit invalid entries', async () => {
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: false,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="codex-turn-state-proxy-url-add"]').trigger('click')
    await wrapper.get('[data-testid="codex-turn-state-proxy-url-0"]').setValue('http://user:pass@proxy.example:1080')
    expect(wrapper.get('[data-testid="codex-turn-state-proxy-url-error-0"]').text()).toContain('errors.scheme')
    expect(wrapper.get('[data-testid="turn-state-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    expect(mocks.updateSettings).not.toHaveBeenCalled()
  })

  it('client-filters every account-management state that is not currently normal', async () => {
    const future = new Date(Date.now() + 60_000).toISOString()
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(20),
        account(21, { status: 'inactive', codex_turn_state_auto: null }),
        account(22, { status: 'error', codex_turn_state_auto: null }),
        account(23, { schedulable: false, codex_turn_state_auto: null }),
        account(24, { rate_limit_reset_at: future, codex_turn_state_auto: null }),
        account(25, { overload_until: future, codex_turn_state_auto: null }),
        account(26, { temp_unschedulable_until: future, codex_turn_state_auto: null }),
        account(27, { auto_pause_on_expired: true, expires_at: Math.floor(Date.now() / 1000) - 1, codex_turn_state_auto: null }),
        account(28, { type: 'apikey', quota_limit: 10, quota_used: 10 }),
      ],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="turn-state-account-20"]').exists()).toBe(true)
    for (let id = 21; id <= 28; id += 1) {
      expect(wrapper.find(`[data-testid="turn-state-account-${id}"]`).exists()).toBe(false)
    }
  })

  it('renders the independent expiry time reported by each collected model', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    const firstExpiry = Date.now() + 30 * 60_000
    const secondExpiry = Date.now() + 90 * 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(60, {
        codex_turn_state_auto: state({
          collection_succeeded: true,
          successful_models: ['model-first', 'model-second'],
          models: {
            'model-first': state({
              collection_succeeded: true,
              verified_model: 'model-first',
              expires_at_ms: firstExpiry,
            }),
            'model-second': state({
              collection_succeeded: true,
              verified_model: 'model-second',
              expires_at_ms: secondExpiry,
            }),
          },
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    const formatter = new Intl.DateTimeFormat('en', { dateStyle: 'short', timeStyle: 'short' })
    const firstDetail = wrapper.get('[data-testid="turn-state-model-detail-60-0"]').text()
    const secondDetail = wrapper.get('[data-testid="turn-state-model-detail-60-1"]').text()
    expect(firstDetail).toContain(`admin.codexTurnState.accounts.details.validUntil ${formatter.format(new Date(firstExpiry))}`)
    expect(secondDetail).toContain(`admin.codexTurnState.accounts.details.validUntil ${formatter.format(new Date(secondExpiry))}`)
    expect(firstDetail).not.toBe(secondDetail)
    wrapper.unmount()
  })

  it('renders recent task status, progress, source, and details navigation', async () => {
    mocks.listCodexTurnStateTasks.mockResolvedValueOnce([{
      id: 'task-recent-1',
      account_id: 11,
      account_name: 'Account 11',
      request_model: 'gpt-5.6-sol',
      owner_model: 'gpt-5.6',
      source: 'bulk',
      status: 'succeeded',
      stage: 'completed',
      progress: 100,
      progress_current: 4,
      progress_total: 4,
      created_at_ms: Date.now(),
      updated_at_ms: Date.now(),
      finished_at_ms: Date.now(),
      can_cancel: false,
      can_retry: false,
    }])
    const wrapper = mountView()
    await flushPromises()

    const row = wrapper.get('[data-testid="turn-state-task-task-recent-1"]')
    expect(row.text()).toContain('Account 11')
    expect(row.text()).toContain('gpt-5.6-sol')
    expect(row.text()).toContain('gpt-5.6')
    expect(row.text()).toContain('admin.codexTurnState.tasks.sources.bulk')
    expect(row.text()).toContain('admin.codexTurnState.tasks.statuses.succeeded')
    expect(row.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')
    expect(row.get('[data-testid="router-link"]').text()).toContain('admin.codexTurnState.tasks.viewDetails')
    wrapper.unmount()
  })

  it('shows unfinished tasks before newer terminal tasks while preserving newest-first order within each group', async () => {
    const task = (id: string, status: string, createdAt: number) => ({
      id,
      account_id: 11,
      account_name: 'Account 11',
      request_model: 'gpt-5.6-sol',
      owner_model: 'gpt-5.6-sol',
      source: 'manual',
      status,
      stage: status === 'queued' ? 'queued' : status === 'running' ? 'collecting' : status === 'succeeded' ? 'completed' : 'failed',
      progress: status === 'succeeded' ? 100 : 20,
      progress_current: status === 'succeeded' ? 1 : 0,
      progress_total: 1,
      created_at_ms: createdAt,
      updated_at_ms: createdAt,
      can_cancel: status === 'queued' || status === 'running',
      can_retry: status === 'failed',
    })
    mocks.listCodexTurnStateTasks.mockResolvedValueOnce([
      task('terminal-newest', 'succeeded', 400),
      task('active-older', 'queued', 100),
      task('terminal-older', 'failed', 300),
      task('active-newer', 'running', 200),
    ])
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('.task-row').map(row => row.attributes('data-testid'))).toEqual([
      'turn-state-task-active-newer',
      'turn-state-task-active-older',
      'turn-state-task-terminal-newest',
      'turn-state-task-terminal-older',
    ])
    wrapper.unmount()
  })

  it('confirms and terminates an unfinished task once, then updates its row', async () => {
    let resolveCancellation!: (value: Record<string, unknown>) => void
    const activeTask = {
      id: 'task-running-1',
      account_id: 11,
      account_name: 'Account 11',
      request_model: 'gpt-5.6-sol',
      owner_model: 'gpt-5.6-sol',
      source: 'manual',
      status: 'running',
      stage: 'collecting',
      progress: 40,
      progress_current: 2,
      progress_total: 5,
      created_at_ms: Date.now(),
      updated_at_ms: Date.now(),
      can_cancel: true,
      can_retry: false,
    }
    mocks.listCodexTurnStateTasks.mockResolvedValueOnce([activeTask])
    mocks.cancelCodexTurnStateTask.mockReturnValueOnce(new Promise(resolve => {
      resolveCancellation = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    const terminateButton = wrapper.get('[data-testid="turn-state-task-cancel-task-running-1"]')
    expect(terminateButton.element.closest('[data-testid="router-link"]')).toBeNull()
    await terminateButton.trigger('click')
    const confirmButton = wrapper.get('[data-testid="confirm-dialog-confirm"]')
    const firstConfirm = confirmButton.trigger('click')
    const duplicateConfirm = confirmButton.trigger('click')
    await Promise.all([firstConfirm, duplicateConfirm])
    expect(mocks.cancelCodexTurnStateTask).toHaveBeenCalledTimes(1)
    expect(mocks.cancelCodexTurnStateTask).toHaveBeenCalledWith('task-running-1')
    expect(wrapper.get('[data-testid="turn-state-task-cancel-task-running-1"]').attributes('disabled')).toBeDefined()

    resolveCancellation({
      ...activeTask,
      status: 'canceled',
      stage: 'canceled',
      can_cancel: false,
      can_retry: true,
    })
    await flushPromises()

    const row = wrapper.get('[data-testid="turn-state-task-task-running-1"]')
    expect(row.text()).toContain('admin.codexTurnState.tasks.statuses.canceled')
    expect(row.find('[data-testid="turn-state-task-cancel-task-running-1"]').exists()).toBe(false)
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.codexTurnState.tasks.cancelSucceeded')
    wrapper.unmount()
  })

  it('keeps termination available and reports an API failure', async () => {
    const activeTask = {
      id: 'task-queued-1',
      account_id: 12,
      account_name: 'Account 12',
      request_model: 'gpt-5.5',
      owner_model: 'gpt-5.5',
      source: 'bulk',
      status: 'queued',
      stage: 'queued',
      progress: 0,
      progress_current: 0,
      progress_total: 1,
      created_at_ms: Date.now(),
      updated_at_ms: Date.now(),
      can_cancel: true,
      can_retry: false,
    }
    mocks.listCodexTurnStateTasks.mockResolvedValueOnce([activeTask])
    mocks.cancelCodexTurnStateTask.mockRejectedValueOnce({})
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-task-cancel-task-queued-1"]').trigger('click')
    await wrapper.get('[data-testid="confirm-dialog-confirm"]').trigger('click')
    await flushPromises()

    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.tasks.cancelFailed')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-task-cancel-task-queued-1"]').element.disabled).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-task-task-queued-1"]').text()).toContain('admin.codexTurnState.tasks.statuses.queued')
    wrapper.unmount()
  })

  it('closes a stale termination confirmation when the task reaches a terminal state', async () => {
    const activeTask = {
      id: 'task-finishes-during-confirm',
      account_id: 12,
      account_name: 'Account 12',
      request_model: 'gpt-5.5',
      owner_model: 'gpt-5.5',
      source: 'automatic',
      status: 'running',
      stage: 'collecting',
      progress: 60,
      progress_current: 3,
      progress_total: 5,
      created_at_ms: Date.now(),
      updated_at_ms: Date.now(),
      can_cancel: true,
      can_retry: false,
    }
    mocks.listCodexTurnStateTasks
      .mockResolvedValueOnce([activeTask])
      .mockResolvedValueOnce([{
        ...activeTask,
        status: 'succeeded',
        stage: 'completed',
        progress: 100,
        progress_current: 5,
        can_cancel: false,
      }])
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-task-cancel-task-finishes-during-confirm"]').trigger('click')
    const confirmation = wrapper.getComponent('[data-testid="confirm-dialog"]')
    expect(confirmation.props('show')).toBe(true)

    await wrapper.get('[data-testid="turn-state-tasks-refresh"]').trigger('click')
    await flushPromises()
    expect(confirmation.props('show')).toBe(false)

    confirmation.vm.$emit('confirm')
    await flushPromises()
    expect(mocks.cancelCodexTurnStateTask).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('collects every loaded eligible account even when search and status filters hide rows', async () => {
    const expiredAt = Date.now() - 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(61),
        account(62, {
          codex_turn_state_auto: state({ configured: false, due: true, expires_at_ms: undefined }),
        }),
        account(63, {
          codex_turn_state_auto: state({
            collection_succeeded: true,
            verified_model: 'expired-model',
            expires_at_ms: expiredAt,
          }),
        }),
      ],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockImplementation(async (accountId: number) => {
      const model = `model-${accountId}`
      return {
        status: 'already_valid',
        account_id: accountId,
        target_models: [model],
        queued_models: [],
        already_valid_models: [model],
        model_targets: [{ model, owner: model }],
        codex_turn_state_auto: state({
          collection_succeeded: true,
          successful_models: [model],
          verified_model: model,
        }),
      }
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-account-search"]').setValue('Account 61')
    await wrapper.get('[data-testid="turn-state-status-filter"]').setValue('valid')
    expect(wrapper.find('[data-testid="turn-state-account-61"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-62"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="turn-state-account-63"]').exists()).toBe(false)

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()

    expect(mocks.collectCodexTurnState.mock.calls.map(([accountId]) => accountId).sort()).toEqual([61, 62, 63])
    expect(mocks.collectCodexTurnState.mock.calls.every(([, source]) => source === 'bulk')).toBe(true)
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkAccounts 3 3')
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 3 0 0')
    wrapper.unmount()
  })

  it('limits bulk collection to three active account rounds and waits for a terminal poll before starting another', async () => {
    vi.useFakeTimers()
    const accountIds = [70, 71, 72, 73, 74]
    const finished = new Set<number>()
    mocks.listAccounts.mockResolvedValueOnce({
      items: accountIds.map(id => account(id)),
      pages: 1,
    })
    mocks.collectCodexTurnState.mockImplementation(async (accountId: number) => {
      const model = `model-${accountId}`
      if (accountId >= 73) {
        return {
          status: 'already_valid',
          account_id: accountId,
          target_models: [model],
          queued_models: [],
          already_valid_models: [model],
          model_targets: [{ model, owner: model }],
          codex_turn_state_auto: state({
            collection_succeeded: true,
            successful_models: [model],
            verified_model: model,
          }),
        }
      }
      return {
        status: 'queued',
        account_id: accountId,
        target_models: [model],
        queued_models: [model],
        model_targets: [{ model, owner: model }],
        codex_turn_state_auto: state({
          configured: false,
          due: true,
          recovery_pending: true,
          expires_at_ms: undefined,
          collection_succeeded: false,
        }),
      }
    })
    mocks.getAccountById.mockImplementation(async (accountId: number) => {
      const model = `model-${accountId}`
      return account(accountId, {
        codex_turn_state_auto: finished.has(accountId)
          ? state({
              collection_succeeded: true,
              successful_models: [model],
              verified_model: model,
              verified_at_ms: Date.now(),
            })
          : state({
              configured: false,
              due: true,
              recovery_pending: true,
              expires_at_ms: undefined,
              collection_succeeded: false,
            }),
      })
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()
    expect(mocks.collectCodexTurnState.mock.calls.map(([accountId]) => accountId)).toEqual([70, 71, 72])
    expect(wrapper.get('[data-testid="turn-state-account-progress-73"]').text()).toContain('admin.codexTurnState.accounts.progressQueued')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(3)
    expect(mocks.collectCodexTurnState).toHaveBeenCalledTimes(3)

    finished.add(70)
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.collectCodexTurnState.mock.calls.map(([accountId]) => accountId)).toEqual([70, 71, 72, 73, 74])

    finished.add(71)
    finished.add(72)
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkAccounts 5 5')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-all"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('settles submitting workers when a refresh removes their accounts and does not count an unsubmitted skip', async () => {
    const accountIds = [75, 76, 77, 78]
    mocks.listAccounts
      .mockResolvedValueOnce({ items: accountIds.map(id => account(id)), pages: 1 })
      .mockResolvedValueOnce({ items: [], pages: 1 })
    mocks.collectCodexTurnState.mockImplementation(() => new Promise(() => {}))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()
    expect(mocks.collectCodexTurnState).toHaveBeenCalledTimes(3)
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkSubmitted 3 4')

    await wrapper.get('[data-testid="turn-state-refresh"]').trigger('click')
    await flushPromises()
    await flushPromises()

    expect(mocks.collectCodexTurnState).toHaveBeenCalledTimes(3)
    const progress = wrapper.get('[data-testid="turn-state-bulk-progress"]')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkComplete')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkSubmitted 3 4')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkAccounts 4 4')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 0 0 4')
    wrapper.unmount()
  })

  it('keeps a submission skipped when its API result wins the race immediately before refresh removal', async () => {
    vi.useFakeTimers()
    let resolveCollection!: (value: {
      status: string
      account_id: number
      target_models: string[]
      queued_models: string[]
      model_targets: Array<{ model: string; owner: string }>
      codex_turn_state_auto: CodexTurnStateAutoInfo
    }) => void
    let resolveRefresh!: (value: { items: AccountListItem[]; pages: number }) => void
    mocks.listAccounts
      .mockResolvedValueOnce({ items: [account(79)], pages: 1 })
      .mockReturnValueOnce(new Promise((resolve) => {
        resolveRefresh = resolve
      }))
    mocks.collectCodexTurnState.mockReturnValueOnce(new Promise((resolve) => {
      resolveCollection = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="turn-state-refresh"]').trigger('click')
    expect(mocks.listAccounts).toHaveBeenCalledTimes(2)

    resolveCollection({
      status: 'queued',
      account_id: 79,
      target_models: ['model-race'],
      queued_models: ['model-race'],
      model_targets: [{ model: 'model-race', owner: 'model-race' }],
      codex_turn_state_auto: state({
        configured: false,
        due: true,
        recovery_pending: true,
        expires_at_ms: undefined,
        collection_succeeded: false,
      }),
    })
    resolveRefresh({ items: [], pages: 1 })
    await flushPromises()
    await flushPromises()

    const progress = wrapper.get('[data-testid="turn-state-bulk-progress"]')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkComplete')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 0 0 1')
    expect(mocks.getAccountById).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.getAccountById).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps a completed bulk summary stable while one account is collected again', async () => {
    const collected = state({
      collection_succeeded: true,
      successful_models: ['model-stable'],
      verified_model: 'model-stable',
    })
    mocks.listAccounts.mockResolvedValueOnce({ items: [account(79)], pages: 1 })
    mocks.collectCodexTurnState
      .mockResolvedValueOnce({
        status: 'already_valid',
        account_id: 79,
        target_models: ['model-stable'],
        queued_models: [],
        already_valid_models: ['model-stable'],
        model_targets: [{ model: 'model-stable', owner: 'model-stable' }],
        codex_turn_state_auto: collected,
      })
      .mockReturnValueOnce(new Promise(() => {}))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()
    const progress = wrapper.get('[data-testid="turn-state-bulk-progress"]')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkAccounts 1 1')
    expect(progress.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')

    await wrapper.get('[data-testid="turn-state-collect-79"]').trigger('click')
    await flushPromises()

    expect(mocks.collectCodexTurnState).toHaveBeenCalledTimes(2)
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkComplete')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkAccounts 1 1')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 1 0 0')
    expect(progress.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')
    wrapper.unmount()
  })

  it('does not invent a model when the backend explicitly reports an empty target list', async () => {
    mocks.listAccounts.mockResolvedValueOnce({ items: [account(80)], pages: 1 })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'rejected',
      account_id: 80,
      target_models: [],
      queued_models: [],
      codex_turn_state_auto: null,
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()

    const progress = wrapper.get('[data-testid="turn-state-bulk-progress"]')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkModels 0 0')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 0 1 0')
    wrapper.unmount()
  })

  it('polls a queued collection and renders the backend verified model name', async () => {
    vi.useFakeTimers()
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.6-sol'],
      queued_models: ['gpt-5.6-sol'],
      model_targets: [{ model: 'gpt-5.6-sol', owner: 'gpt-5.6-sol' }],
      codex_turn_state_auto: state({ configured: false, recovery_pending: true, expires_at_ms: undefined }),
    })
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({
        successful_models: ['gpt-5.6-sol'],
        collection_succeeded: true,
        verified_model: 'gpt-5.6-sol',
        models: {
          'fallback-map-key': state({
            verified_model: 'gpt-5.6-sol',
            collection_succeeded: true,
          }),
        },
      }),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="turn-state-model-12"]').exists()).toBe(false)
    expect(mocks.collectCodexTurnState).toHaveBeenCalledWith(12, 'manual', expect.anything())
    expect(wrapper.get('[data-testid="turn-state-collect-12"]').text()).toContain('admin.codexTurnState.accounts.checking')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledWith(12)
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('gpt-5.6-sol')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).not.toContain('fallback-map-key')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('admin.codexTurnState.accounts.modelStates.success')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('keeps polling after a transient account detail failure and backs off before retrying', async () => {
    vi.useFakeTimers()
    mocks.getAccountById
      .mockRejectedValueOnce(new Error('temporary detail failure'))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['gpt-5.5'],
          collection_succeeded: true,
          verified_model: 'gpt-5.5',
          verified_at_ms: Date.now(),
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="turn-state-collect-12"]').text()).toContain('admin.codexTurnState.accounts.checking')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(3_999)
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(mocks.showSuccess).toHaveBeenLastCalledWith('admin.codexTurnState.accounts.collectSucceeded')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('admin.codexTurnState.accounts.modelStates.success')
    wrapper.unmount()
  })

  it('reports a transient bulk polling retry without incrementing the failed account total', async () => {
    vi.useFakeTimers()
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(80, {
        type: 'setup-token',
        codex_turn_state_auto: state({ configured: false, due: true, expires_at_ms: undefined }),
      })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 80,
      target_models: ['model-retry'],
      queued_models: ['model-retry'],
      model_targets: [{ model: 'model-retry', owner: 'model-retry' }],
      codex_turn_state_auto: state({
        configured: false,
        due: true,
        recovery_pending: true,
        expires_at_ms: undefined,
        collection_succeeded: false,
      }),
    })
    mocks.getAccountById
      .mockRejectedValueOnce(new Error('temporary https://private.example/retry?token=secret'))
      .mockResolvedValueOnce(account(80, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          collection_succeeded: true,
          successful_models: ['model-retry'],
          verified_model: 'model-retry',
          verified_at_ms: Date.now(),
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-all"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="turn-state-account-progress-80"]').text()).toContain('admin.codexTurnState.accounts.progressRetrying 1')
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 0 0 0')
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkAccounts 0 1')
    expect(wrapper.text()).not.toContain('private.example')
    expect(wrapper.text()).not.toContain('token=secret')
    expect(mocks.showError).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(3_999)
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(1)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-testid="turn-state-bulk-progress"]').text()).toContain('admin.codexTurnState.accounts.bulkOutcomes 1 0 0')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-all"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('removes an account instead of retrying forever when detail polling returns not found', async () => {
    vi.useFakeTimers()
    mocks.getAccountById.mockRejectedValueOnce({ status: 404, message: 'account not found' })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(false)
    expect(mocks.showError).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(30_000)
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('does not let an older account list response overwrite a newer polled success', async () => {
    vi.useFakeTimers()
    const pending = account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({ configured: false, due: true, recovery_pending: true, expires_at_ms: undefined }),
    })
    let resolveRefresh!: (value: { items: AccountListItem[]; pages: number }) => void
    mocks.listAccounts
      .mockResolvedValueOnce({ items: [pending], pages: 1 })
      .mockReturnValueOnce(new Promise((resolve) => {
        resolveRefresh = resolve
      }))
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({
        successful_models: ['gpt-5.5'],
        collection_succeeded: true,
        verified_model: 'gpt-5.5',
        verified_at_ms: Date.now(),
      }),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="turn-state-refresh"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(mocks.showSuccess).toHaveBeenLastCalledWith('admin.codexTurnState.accounts.collectSucceeded')

    resolveRefresh({ items: [pending], pages: 1 })
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).not.toContain('admin.codexTurnState.accounts.states.pending')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('admin.codexTurnState.accounts.modelStates.success')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('does not let an older account list response restore an account removed by detail polling', async () => {
    vi.useFakeTimers()
    const eligible = account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({ configured: false, due: true, recovery_pending: true, expires_at_ms: undefined }),
    })
    let resolveRefresh!: (value: { items: AccountListItem[]; pages: number }) => void
    mocks.listAccounts
      .mockResolvedValueOnce({ items: [eligible], pages: 1 })
      .mockReturnValueOnce(new Promise((resolve) => {
        resolveRefresh = resolve
      }))
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: null,
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="turn-state-refresh"]').trigger('click')
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    resolveRefresh({ items: [eligible], pages: 1 })
    await flushPromises()

    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(false)
    expect(mocks.showError).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps polling queued model B when model A was already successful', async () => {
    vi.useFakeTimers()
    const existing = state({
      successful_models: ['model-a'],
      collection_succeeded: true,
      models: {
        'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
      },
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(12, { type: 'setup-token', codex_turn_state_auto: existing })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['model-a', 'model-b'],
      queued_models: ['model-b'],
      already_valid_models: ['model-a'],
      model_targets: [
        { model: 'model-a', owner: 'model-a' },
        { model: 'model-b', owner: 'model-b' },
      ],
      codex_turn_state_auto: existing,
    })
    mocks.getAccountById
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['model-a'],
          collection_succeeded: true,
          recovery_pending: true,
          models: {
            'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
            'model-b': state({ configured: false, due: true, recovery_pending: true, expires_at_ms: undefined, collection_succeeded: false }),
          },
        }),
      }))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['model-a', 'model-b'],
          collection_succeeded: true,
          models: {
            'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
            'model-b': state({ verified_model: 'model-b', collection_succeeded: true, verified_at_ms: Date.now() }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="turn-state-collect-12"]').text()).toContain('admin.codexTurnState.accounts.checking')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('model-b')
    wrapper.unmount()
  })

  it('waits for every queued owner before completing a mixed success and failure round', async () => {
    vi.useFakeTimers()
    const baselineProbeAt = Date.now() - 60_000
    const initial = state({
      configured: false,
      recovery_pending: true,
      expires_at_ms: undefined,
      models: {
        'gpt-6-astra': state({
          configured: false,
          due: true,
          expires_at_ms: undefined,
          collection_succeeded: false,
          last_error: 'transport_failed',
          probe_at_ms: baselineProbeAt,
        }),
        'model-b': state({
          configured: false,
          due: true,
          recovery_pending: true,
          expires_at_ms: undefined,
          collection_succeeded: false,
        }),
      },
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(12, { type: 'setup-token', codex_turn_state_auto: initial })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-6-astra-preview', 'model-b'],
      queued_models: ['model-b', 'gpt-6-astra'],
      already_valid_models: [],
      model_targets: [
        { model: 'gpt-6-astra-preview', owner: 'gpt-6-astra' },
        { model: 'model-b', owner: 'model-b' },
      ],
      codex_turn_state_auto: initial,
    })
    const failedProbeAt = Date.now()
    mocks.getAccountById
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          configured: false,
          recovery_pending: true,
          expires_at_ms: undefined,
          models: {
            'gpt-6-astra': state({
              configured: false,
              due: true,
              expires_at_ms: undefined,
              collection_succeeded: false,
            }),
            'model-b': state({
              configured: false,
              due: true,
              recovery_pending: true,
              expires_at_ms: undefined,
              collection_succeeded: false,
            }),
          },
        }),
      }))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['actual-model-b'],
          collection_succeeded: true,
          models: {
            'gpt-6-astra': state({
              configured: false,
              due: true,
              expires_at_ms: undefined,
              collection_succeeded: false,
              last_error: 'transport_failed',
              probe_at_ms: failedProbeAt,
            }),
            'model-b': state({
              verified_model: 'actual-model-b',
              collection_succeeded: true,
              verified_at_ms: Date.now(),
            }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('gpt-6-astra-preview')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)
    expect(mocks.showError).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.accounts.collectFailed')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('actual-model-b')
    wrapper.unmount()
  })

  it('keeps a mixed model round active at partial progress until the remaining model fails', async () => {
    vi.useFakeTimers()
    const verifiedAt = Date.now()
    const successfulModel = state({
      collection_succeeded: true,
      verified_model: 'model-success',
      verified_at_ms: verifiedAt,
    })
    const pendingModel = state({
      configured: false,
      due: true,
      recovery_pending: true,
      expires_at_ms: undefined,
      collection_succeeded: false,
    })
    const initial = state({
      configured: false,
      due: true,
      recovery_pending: true,
      expires_at_ms: undefined,
      collection_succeeded: false,
      models: {
        'model-success': pendingModel,
        'model-failure': pendingModel,
      },
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(81, { type: 'setup-token', codex_turn_state_auto: initial })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 81,
      target_models: ['model-success', 'model-failure'],
      queued_models: ['model-success', 'model-failure'],
      model_targets: [
        { model: 'model-success', owner: 'model-success' },
        { model: 'model-failure', owner: 'model-failure' },
      ],
      codex_turn_state_auto: initial,
    })
    mocks.getAccountById
      .mockResolvedValueOnce(account(81, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          configured: false,
          recovery_pending: true,
          expires_at_ms: undefined,
          collection_succeeded: false,
          successful_models: ['model-success'],
          models: {
            'model-success': successfulModel,
            'model-failure': pendingModel,
          },
        }),
      }))
      .mockResolvedValueOnce(account(81, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          configured: false,
          expires_at_ms: undefined,
          collection_succeeded: false,
          successful_models: ['model-success'],
          models: {
            'model-success': successfulModel,
            'model-failure': state({
              configured: false,
              due: true,
              expires_at_ms: undefined,
              collection_succeeded: false,
              last_error: 'https://private.example/token?authorization=secret',
              probe_at_ms: Date.now(),
            }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-81"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    const progress = wrapper.get('[data-testid="turn-state-account-progress-81"]')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.progressModels 1 2')
    expect(progress.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('50')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-81"]').element.disabled).toBe(true)
    expect(mocks.showError).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(progress.text()).toContain('admin.codexTurnState.accounts.progressPartial')
    expect(progress.text()).toContain('admin.codexTurnState.accounts.progressModels 2 2')
    expect(progress.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-81"]').element.disabled).toBe(false)
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.accounts.collectFailed')
    expect(wrapper.text()).not.toContain('private.example')
    expect(wrapper.text()).not.toContain('authorization=secret')
    wrapper.unmount()
  })

  it('does not finish target polling when only another model gains a new success', async () => {
    vi.useFakeTimers()
    const queued = state({
      successful_models: ['model-a'],
      collection_succeeded: true,
      recovery_pending: true,
      models: {
        'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
        'model-b': state({ configured: false, due: true, recovery_pending: true, expires_at_ms: undefined, collection_succeeded: false }),
      },
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(12, { type: 'setup-token', codex_turn_state_auto: queued })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['model-b'],
      queued_models: ['model-b'],
      model_targets: [{ model: 'model-b', owner: 'model-b' }],
      codex_turn_state_auto: queued,
    })
    mocks.getAccountById
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['model-a', 'model-c'],
          collection_succeeded: true,
          recovery_pending: true,
          models: {
            'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
            'model-b': state({ configured: false, due: true, recovery_pending: true, expires_at_ms: undefined, collection_succeeded: false }),
            'model-c': state({ verified_model: 'model-c', collection_succeeded: true, verified_at_ms: Date.now() }),
          },
        }),
      }))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['model-a', 'model-b', 'model-c'],
          collection_succeeded: true,
          models: {
            'model-a': state({ verified_model: 'model-a', collection_succeeded: true }),
            'model-b': state({ verified_model: 'model-b', collection_succeeded: true, verified_at_ms: Date.now() + 1 }),
            'model-c': state({ verified_model: 'model-c', collection_succeeded: true, verified_at_ms: Date.now() }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('keeps polling a queued retry while the target slot still exposes its baseline error', async () => {
    vi.useFakeTimers()
    const baselineProbeAt = Date.now() - 60_000
    const baselineFailure = failedScopedState(baselineProbeAt)
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(12, { type: 'setup-token', codex_turn_state_auto: baselineFailure })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.5'],
      queued_models: ['gpt-5.5'],
      model_targets: [{ model: 'gpt-5.5', owner: 'gpt-5.5' }],
      codex_turn_state_auto: baselineFailure,
    })
    mocks.getAccountById
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: failedScopedState(baselineProbeAt),
      }))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          successful_models: ['gpt-5.5'],
          collection_succeeded: true,
          models: {
            'gpt-5.5': state({ verified_model: 'gpt-5.5', collection_succeeded: true, verified_at_ms: Date.now() }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    wrapper.unmount()
  })

  it('finishes a due retry when only the probe timestamp changes on the same persisted state', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    const setAt = Date.now() - 51 * 60_000
    const verifiedAt = setAt - 1_000
    const expiresAt = Date.now() + 60 * 60_000
    const baselineProbeAt = Date.now() - 60_000
    const finalProbeAt = Date.now()
    const baseline = state({
      due: true,
      set_at_ms: setAt,
      verified_at_ms: verifiedAt,
      state_length: 128,
      collection_succeeded: false,
      last_error: 'transport_failed',
      probe_at_ms: baselineProbeAt,
      expires_at_ms: expiresAt,
      models: {
        'gpt-5.5': state({
          due: true,
          set_at_ms: setAt,
          verified_at_ms: verifiedAt,
          verified_model: 'gpt-5.5',
          state_length: 128,
          collection_succeeded: false,
          last_error: 'transport_failed',
          probe_at_ms: baselineProbeAt,
          expires_at_ms: expiresAt,
        }),
      },
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(12, { type: 'setup-token', codex_turn_state_auto: baseline })],
      pages: 1,
    })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.5'],
      queued_models: ['gpt-5.5'],
      model_targets: [{ model: 'gpt-5.5', owner: 'gpt-5.5' }],
      codex_turn_state_auto: baseline,
    })
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({
        due: true,
        set_at_ms: setAt,
        verified_at_ms: verifiedAt,
        state_length: 128,
        collection_succeeded: true,
        successful_models: ['gpt-5.5'],
        probe_at_ms: finalProbeAt,
        expires_at_ms: expiresAt,
        models: {
          'gpt-5.5': state({
            due: true,
            set_at_ms: setAt,
            verified_at_ms: verifiedAt,
            verified_model: 'gpt-5.5',
            state_length: 128,
            collection_succeeded: true,
            probe_at_ms: finalProbeAt,
            expires_at_ms: expiresAt,
          }),
        },
      }),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(mocks.showSuccess).toHaveBeenLastCalledWith('admin.codexTurnState.accounts.collectSucceeded')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('admin.codexTurnState.accounts.modelStates.renewal')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).not.toContain('admin.codexTurnState.accounts.modelStates.success')
    wrapper.unmount()
  })

  it('treats the same error code with a newer target probe timestamp as this round failure', async () => {
    vi.useFakeTimers()
    const baselineProbeAt = Date.now() - 60_000
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.5'],
      queued_models: ['gpt-5.5'],
      model_targets: [{ model: 'gpt-5.5', owner: 'gpt-5.5' }],
      codex_turn_state_auto: failedScopedState(baselineProbeAt),
    })
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: failedScopedState(Date.now()),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.accounts.collectFailed')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)

    await vi.advanceTimersByTimeAsync(2_000)
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('reenables manual collection after a polled failure without exposing last_error', async () => {
    vi.useFakeTimers()
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({
        configured: false,
        collection_succeeded: false,
        expires_at_ms: undefined,
        last_error: 'https://private.example/token?authorization=secret',
      }),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(mocks.showError).toHaveBeenCalledWith('admin.codexTurnState.accounts.collectFailed')
    expect(wrapper.text()).not.toContain('private.example')
    expect(wrapper.text()).not.toContain('authorization=secret')
    wrapper.unmount()
  })

  it('keeps polling through an intermediate IP cooldown until another IP succeeds', async () => {
    vi.useFakeTimers()
    const boundary = Date.now() + 60_000
    mocks.getAccountById
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          configured: false,
          collection_succeeded: false,
          expires_at_ms: undefined,
          probe_not_before_ms: boundary,
        }),
      }))
      .mockResolvedValueOnce(account(12, {
        type: 'setup-token',
        codex_turn_state_auto: state({
          collection_succeeded: true,
          successful_models: ['gpt-5.5'],
          verified_model: 'gpt-5.5',
          probe_not_before_ms: boundary,
          models: {
            'gpt-5.5': state({
              collection_succeeded: true,
              verified_model: 'gpt-5.5',
              verified_at_ms: Date.now(),
              probe_not_before_ms: boundary,
            }),
          },
        }),
      }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).toContain('admin.codexTurnState.accounts.states.cooldown')

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(2)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    expect(wrapper.get('[data-testid="turn-state-account-12"]').text()).not.toContain('admin.codexTurnState.accounts.states.cooldown')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('admin.codexTurnState.accounts.modelStates.success')
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).not.toContain('admin.codexTurnState.accounts.modelStates.cooldown')
    wrapper.unmount()
  })

  it('does not disable manual collection merely because the current Turn State is cooling down or failed', async () => {
    const future = Date.now() + 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(31, { codex_turn_state_auto: state({ configured: false, expires_at_ms: undefined, probe_not_before_ms: future }) }),
        account(32, { codex_turn_state_auto: state({ configured: false, expires_at_ms: undefined, last_error: 'transport_failed' }) }),
      ],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-31"]').element.disabled).toBe(false)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-32"]').element.disabled).toBe(false)
  })

  it('continues queued polling beyond thirty seconds while the account stays eligible', async () => {
    vi.useFakeTimers()
    mocks.getAccountById.mockResolvedValue(account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({ configured: false, recovery_pending: true, expires_at_ms: undefined }),
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    for (let attempt = 0; attempt < 16; attempt += 1) {
      await vi.advanceTimersByTimeAsync(2_000)
      await flushPromises()
    }

    expect(mocks.getAccountById).toHaveBeenCalledTimes(16)
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="turn-state-collect-12"]').element.disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getAccountById).toHaveBeenCalledTimes(17)
    wrapper.unmount()
  })

  it.each(['account_not_schedulable', 'account_not_eligible'])(
    'removes an account when manual collection is rejected with null diagnostics: %s',
    async (reason) => {
      vi.useFakeTimers()
      mocks.collectCodexTurnState.mockResolvedValueOnce({
        status: 'rejected',
        reason,
        message: 'account is no longer eligible',
        account_id: 12,
        target_models: [],
        queued_models: [],
        codex_turn_state_auto: null,
      })
      const wrapper = mountView()
      await flushPromises()

      expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(true)
      await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(false)
      expect(mocks.showError).toHaveBeenCalledWith('account is no longer eligible')

      await vi.advanceTimersByTimeAsync(2_000)
      expect(mocks.getAccountById).not.toHaveBeenCalled()
      wrapper.unmount()
    },
  )

  it('cancels queued polling when the page unmounts', async () => {
    vi.useFakeTimers()
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(2_000)

    expect(mocks.getAccountById).not.toHaveBeenCalled()
  })

  it('ignores a manual collection response that arrives after the page unmounts', async () => {
    vi.useFakeTimers()
    let resolveCollection!: (value: {
      status: string
      account_id: number
      target_models: string[]
      queued_models: string[]
      codex_turn_state_auto: CodexTurnStateAutoInfo
    }) => void
    mocks.collectCodexTurnState.mockReturnValueOnce(new Promise((resolve) => {
      resolveCollection = resolve
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    expect(mocks.collectCodexTurnState).toHaveBeenCalledTimes(1)
    wrapper.unmount()
    resolveCollection({
      status: 'queued',
      account_id: 12,
      target_models: ['gpt-5.5'],
      queued_models: ['gpt-5.5'],
      codex_turn_state_auto: state({ configured: false, recovery_pending: true, expires_at_ms: undefined }),
    })
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)

    expect(mocks.showSuccess).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(mocks.getAccountById).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('removes an account and stops polling when refreshed eligibility becomes false', async () => {
    vi.useFakeTimers()
    mocks.getAccountById.mockResolvedValueOnce(account(12, {
      type: 'setup-token',
      schedulable: false,
      codex_turn_state_auto: null,
    }))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(true)
    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()

    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="turn-state-account-12"]').exists()).toBe(false)
    expect(mocks.showError).not.toHaveBeenCalled()

    await vi.advanceTimersByTimeAsync(2_000)
    expect(mocks.getAccountById).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('classifies due unexpired states as renewal while keeping expired states expired', async () => {
    const future = Date.now() + 60_000
    const past = Date.now() - 60_000
    mocks.listAccounts.mockResolvedValueOnce({
      items: [
        account(40, { codex_turn_state_auto: state({ due: true, expires_at_ms: future }) }),
        account(41, {
          codex_turn_state_auto: state({
            due: true,
            expires_at_ms: past,
            verified_model: 'gpt-expired',
            collection_succeeded: true,
            successful_models: ['gpt-expired'],
          }),
        }),
        account(42, {
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

    expect(wrapper.get('[data-testid="turn-state-account-40"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-account-41"]').text()).toContain('admin.codexTurnState.accounts.states.expired')
    expect(wrapper.get('[data-testid="turn-state-model-results-41"]').text()).toContain('admin.codexTurnState.accounts.modelStates.expired')
    expect(wrapper.get('[data-testid="turn-state-model-results-41"]').text()).not.toContain('admin.codexTurnState.accounts.modelStates.success')
    expect(wrapper.get('[data-testid="turn-state-account-42"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-account-42"]').text()).toContain('gpt-renewal')

    await wrapper.get('[data-testid="turn-state-status-filter"]').setValue('renewal')
    expect(wrapper.find('[data-testid="turn-state-account-40"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="turn-state-account-41"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="turn-state-account-42"]').exists()).toBe(true)
  })

  it('updates expiry classifications while the page remains open', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(50, {
        codex_turn_state_auto: state({
          collection_succeeded: true,
          verified_model: 'gpt-clock',
          expires_at_ms: Date.now() + 5_000,
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-50"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    await vi.advanceTimersByTimeAsync(30_000)
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-50"]').text()).toContain('admin.codexTurnState.accounts.states.expired')
    wrapper.unmount()
  })

  it('uses the latest verification as the renewal baseline without overruling hard expiry', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    const verifiedAt = Date.now()
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_auto_interval_minutes: 5,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(54, {
        codex_turn_state_auto: state({
          set_at_ms: verifiedAt - 51 * 60_000,
          verified_at_ms: verifiedAt,
          due: true,
          collection_succeeded: true,
          successful_models: ['gpt-verified-baseline'],
          verified_model: 'gpt-verified-baseline',
          expires_at_ms: verifiedAt + 9 * 60_000,
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-54"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    expect(wrapper.get('[data-testid="turn-state-model-results-54"]').text()).toContain('admin.codexTurnState.accounts.modelStates.success')

    await vi.advanceTimersByTimeAsync(5 * 60_000)
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-54"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-model-results-54"]').text()).toContain('admin.codexTurnState.accounts.modelStates.renewal')

    await vi.advanceTimersByTimeAsync(4 * 60_000)
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-54"]').text()).toContain('admin.codexTurnState.accounts.states.expired')
    expect(wrapper.get('[data-testid="turn-state-model-results-54"]').text()).toContain('admin.codexTurnState.accounts.modelStates.expired')
    wrapper.unmount()
  })

  it('lets a configured 60 minute interval override a stale 50 minute due flag', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_auto_interval_minutes: 60,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(51, {
        codex_turn_state_auto: state({
          set_at_ms: Date.now(),
          due: true,
          collection_succeeded: true,
          verified_model: 'gpt-interval',
          expires_at_ms: Date.now() + 2 * 60 * 60_000,
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-51"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    await vi.advanceTimersByTimeAsync(55 * 60_000)
    await flushPromises()
    expect(wrapper.get('[data-testid="turn-state-account-51"]').text()).toContain('admin.codexTurnState.accounts.states.valid')

    await vi.advanceTimersByTimeAsync(5 * 60_000)
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-51"]').text()).toContain('admin.codexTurnState.accounts.states.renewal')
    expect(wrapper.get('[data-testid="turn-state-model-results-51"]').text()).toContain('admin.codexTurnState.accounts.modelStates.renewal')
    wrapper.unmount()
  })

  it('keeps account classifications on the applied interval while the edited value is unsaved', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_auto_interval_minutes: 60,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(52, {
        codex_turn_state_auto: state({
          set_at_ms: Date.now() - 55 * 60_000,
          due: true,
          collection_succeeded: true,
          verified_model: 'gpt-unsaved-interval',
          expires_at_ms: Date.now() + 60 * 60_000,
        }),
      })],
      pages: 1,
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-account-52"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    await wrapper.get('[data-testid="turn-state-auto-interval"]').setValue(50)

    expect(wrapper.get('[data-testid="turn-state-account-52"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    expect(wrapper.get('[data-testid="turn-state-account-52"]').text()).not.toContain('admin.codexTurnState.accounts.states.renewal')
    expect(mocks.updateSettings).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('keeps account classifications on the previous interval when saving the edited value fails', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-19T00:00:00Z'))
    mocks.getSettings.mockResolvedValueOnce({
      openai_codex_turn_state_auto_enabled: true,
      openai_codex_turn_state_auto_interval_minutes: 60,
      openai_codex_turn_state_models: '',
      openai_codex_turn_state_default_model: 'gpt-5.5',
      openai_codex_turn_state_proxy_urls: [],
      openai_codex_turn_state_proxy_urls_valid: true,
    })
    mocks.listAccounts.mockResolvedValueOnce({
      items: [account(53, {
        codex_turn_state_auto: state({
          set_at_ms: Date.now() - 55 * 60_000,
          due: true,
          collection_succeeded: true,
          verified_model: 'gpt-failed-save-interval',
          expires_at_ms: Date.now() + 60 * 60_000,
        }),
      })],
      pages: 1,
    })
    mocks.updateSettings.mockRejectedValueOnce(new Error('save failed'))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-auto-interval"]').setValue(50)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.updateSettings).toHaveBeenCalledWith(expect.objectContaining({
      openai_codex_turn_state_auto_interval_minutes: 50,
    }))
    expect(wrapper.get('[data-testid="turn-state-account-53"]').text()).toContain('admin.codexTurnState.accounts.states.valid')
    expect(wrapper.get('[data-testid="turn-state-account-53"]').text()).not.toContain('admin.codexTurnState.accounts.states.renewal')
    expect(mocks.showError).toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reconciles locally reported successes with the next authoritative account snapshot', async () => {
    const initial = account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({ configured: false, due: true, expires_at_ms: undefined, collection_succeeded: false }),
    })
    const authoritative = account(12, {
      type: 'setup-token',
      codex_turn_state_auto: state({
        successful_models: ['fresh-model'],
        collection_succeeded: true,
        verified_model: 'fresh-model',
      }),
    })
    mocks.listAccounts
      .mockResolvedValueOnce({ items: [initial], pages: 1 })
      .mockResolvedValueOnce({ items: [authoritative], pages: 1 })
    mocks.collectCodexTurnState.mockResolvedValueOnce({
      status: 'already_valid',
      account_id: 12,
      target_models: ['stale-model'],
      queued_models: [],
      already_valid_models: ['stale-model'],
      codex_turn_state_auto: state({
        successful_models: ['stale-model'],
        collection_succeeded: true,
        verified_model: 'stale-model',
      }),
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-collect-12"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="turn-state-model-results-12"]').text()).toContain('stale-model')

    await wrapper.get('[data-testid="turn-state-refresh"]').trigger('click')
    await flushPromises()

    const results = wrapper.get('[data-testid="turn-state-model-results-12"]').text()
    expect(results).toContain('fresh-model')
    expect(results).not.toContain('stale-model')
  })
})
