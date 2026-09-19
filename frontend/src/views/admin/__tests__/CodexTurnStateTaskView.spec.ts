import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import CodexTurnStateTaskView from '../CodexTurnStateTaskView.vue'
import type { CodexTurnStateTask } from '@/api/admin/accounts'

const mocks = vi.hoisted(() => ({
  route: { params: { taskId: 'task-1' } },
  push: vi.fn(),
  getTask: vi.fn(),
  cancelTask: vi.fn(),
  retryTask: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('vue-router', () => ({
  RouterLink: {
    name: 'RouterLink',
    props: ['to'],
    template: '<a data-testid="router-link"><slot /></a>',
  },
  useRoute: () => mocks.route,
  useRouter: () => ({ push: mocks.push }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getCodexTurnStateTask: mocks.getTask,
      cancelCodexTurnStateTask: mocks.cancelTask,
      retryCodexTurnStateTask: mocks.retryTask,
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

function task(overrides: Partial<CodexTurnStateTask> = {}): CodexTurnStateTask {
  return {
    id: 'task-1',
    account_id: 17,
    account_name: 'Codex Account',
    request_model: 'gpt-5.6-sol',
    owner_model: 'gpt-5.6',
    source: 'manual',
    status: 'failed',
    stage: 'failed',
    progress: 72,
    progress_current: 3,
    progress_total: 4,
    created_at_ms: Date.parse('2026-09-19T00:00:00Z'),
    started_at_ms: Date.parse('2026-09-19T00:00:01Z'),
    updated_at_ms: Date.parse('2026-09-19T00:00:04Z'),
    finished_at_ms: Date.parse('2026-09-19T00:00:04Z'),
    error: 'verification_failed',
    can_cancel: false,
    can_retry: true,
    events: [
      {
        at_ms: Date.parse('2026-09-19T00:00:03Z'),
        status: 'failed',
        stage: 'failed',
        progress: 72,
        error: 'late_safe_code',
      },
      {
        at_ms: Date.parse('2026-09-19T00:00:01Z'),
        status: 'running',
        stage: 'collecting',
        progress: 30,
        error: 'early_safe_code',
      },
    ],
    ...overrides,
  }
}

function mountView() {
  return mount(CodexTurnStateTaskView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        AdminPageHeader: { template: '<header><slot /><slot name="actions" /></header>' },
        AdminOverviewStrip: { props: ['items'], template: '<div data-testid="overview">{{ JSON.stringify(items) }}</div>' },
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

describe('CodexTurnStateTaskView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.route.params.taskId = 'task-1'
    mocks.getTask.mockResolvedValue(task())
    mocks.cancelTask.mockResolvedValue(task({ status: 'canceled', stage: 'canceled', can_cancel: false, can_retry: true }))
    mocks.retryTask.mockResolvedValue(task({ id: 'task-2', status: 'queued', stage: 'queued', progress: 0, retry_of: 'task-1', can_cancel: true, can_retry: false }))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders account, models, source, timing, safe error, progress, and a chronological event timeline', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.getTask).toHaveBeenCalledWith('task-1')
    const summary = wrapper.get('[data-testid="turn-state-task-summary"]')
    expect(summary.text()).toContain('Codex Account')
    expect(summary.text()).toContain('#17')
    expect(summary.text()).toContain('gpt-5.6-sol')
    expect(summary.text()).toContain('gpt-5.6')
    expect(summary.text()).toContain('admin.codexTurnState.tasks.sources.manual')
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('72')
    expect(wrapper.get('[data-testid="turn-state-task-error"]').text()).toContain('verification_failed')

    const timeline = wrapper.get('[data-testid="turn-state-task-events"]').text()
    expect(timeline.indexOf('early_safe_code')).toBeLessThan(timeline.indexOf('late_safe_code'))
    expect(timeline).toContain('admin.codexTurnState.tasks.stages.collecting')
    expect(timeline).toContain('admin.codexTurnState.tasks.stages.failed')
    wrapper.unmount()
  })

  it('polls a running task and stops after it reaches a terminal state', async () => {
    vi.useFakeTimers()
    mocks.getTask
      .mockResolvedValueOnce(task({ status: 'running', stage: 'collecting', progress: 40, finished_at_ms: undefined, can_cancel: true, can_retry: false }))
      .mockResolvedValueOnce(task({ status: 'succeeded', stage: 'completed', progress: 100, error: undefined, can_cancel: false, can_retry: false }))
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.getTask).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getTask).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('100')

    await vi.advanceTimersByTimeAsync(10_000)
    await flushPromises()
    expect(mocks.getTask).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('confirms and cancels a queued or running task', async () => {
    mocks.getTask.mockResolvedValueOnce(task({ status: 'running', stage: 'collecting', can_cancel: true, can_retry: false }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-task-cancel"]').trigger('click')
    await wrapper.get('[data-testid="confirm-dialog-confirm"]').trigger('click')
    await flushPromises()

    expect(mocks.cancelTask).toHaveBeenCalledWith('task-1')
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.codexTurnState.tasks.cancelSucceeded')
    expect(wrapper.text()).toContain('admin.codexTurnState.tasks.statuses.canceled')
    wrapper.unmount()
  })

  it('pauses task polling while cancellation is pending and resumes after a failed action', async () => {
    vi.useFakeTimers()
    let rejectCancellation!: (reason?: unknown) => void
    mocks.getTask.mockResolvedValue(task({
      status: 'running',
      stage: 'collecting',
      finished_at_ms: undefined,
      can_cancel: true,
      can_retry: false,
    }))
    mocks.cancelTask.mockImplementationOnce(() => new Promise((_, reject) => {
      rejectCancellation = reject
    }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-task-cancel"]').trigger('click')
    await wrapper.get('[data-testid="confirm-dialog-confirm"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(4_000)
    await flushPromises()
    expect(mocks.getTask).toHaveBeenCalledTimes(1)

    rejectCancellation(new Error('cancel failed'))
    await flushPromises()
    await vi.advanceTimersByTimeAsync(2_000)
    await flushPromises()
    expect(mocks.getTask).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('confirms a terminal retry and opens the new task', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-testid="turn-state-task-retry"]').trigger('click')
    await wrapper.get('[data-testid="confirm-dialog-confirm"]').trigger('click')
    await flushPromises()

    expect(mocks.retryTask).toHaveBeenCalledWith('task-1')
    expect(mocks.showSuccess).toHaveBeenCalledWith('admin.codexTurnState.tasks.retrySucceeded')
    expect(mocks.push).toHaveBeenCalledWith({ name: 'AdminCodexTurnStateTask', params: { taskId: 'task-2' } })
    wrapper.unmount()
  })
})
