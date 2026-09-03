import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'

const {
  listSessionsMock,
  revokeSessionMock,
  revokeSessionsMock,
  trustSessionMock,
  getCleanupMock,
  updateCleanupMock,
  runCleanupMock,
  showSuccessMock,
  showErrorMock,
  showWarningMock
} = vi.hoisted(() => ({
  listSessionsMock: vi.fn(),
  revokeSessionMock: vi.fn(),
  revokeSessionsMock: vi.fn(),
  trustSessionMock: vi.fn(),
  getCleanupMock: vi.fn(),
  updateCleanupMock: vi.fn(),
  runCleanupMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      listOpenAISessions: listSessionsMock,
      revokeOpenAISession: revokeSessionMock,
      revokeOpenAISessions: revokeSessionsMock,
      trustOpenAISession: trustSessionMock,
      getOpenAISessionCleanup: getCleanupMock,
      updateOpenAISessionCleanup: updateCleanupMock,
      runOpenAISessionCleanup: runCleanupMock,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: showSuccessMock, showError: showErrorMock, showWarning: showWarningMock }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${Object.values(params).join(':')}` : key,
    }),
  }
})

import OpenAISessionsModal from '../OpenAISessionsModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  template: '<section v-if="show"><h1>{{ title }}</h1><slot /></section>',
})

const account: Account = {
  id: 42,
  name: 'GPT Account',
  platform: 'openai',
  type: 'oauth',
  proxy_id: null,
  concurrency: 3,
  priority: 50,
  status: 'active',
  error_message: null,
  last_used_at: null,
  expires_at: null,
  auto_pause_on_expired: false,
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  schedulable: true,
  rate_limited_at: null,
  rate_limit_reset_at: null,
  overload_until: null,
  temp_unschedulable_until: null,
  temp_unschedulable_reason: null,
  session_window_start: null,
  session_window_end: null,
  session_window_status: null,
}

describe('OpenAISessionsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    listSessionsMock.mockResolvedValue({
      sessions: [{
        id: 'sess-1',
        device_name: 'MacBook Pro',
        app_name: 'ChatGPT',
        browser: 'Chrome',
        location: 'Shanghai, China',
        current: false,
        trusted: true,
        status_available: true,
        can_revoke: true,
      }],
      fetched_at: 1,
      current_known: true,
    })
    getCleanupMock.mockResolvedValue({
      enabled: true,
      interval_minutes: 60,
      state: {
        status: 'success',
        last_run_at: '2026-09-02T00:00:00Z',
        last_success_at: '2026-09-02T00:00:00Z',
        revoked_count: 2,
        failed_count: 0,
        current_session_known: true,
      },
    })
    updateCleanupMock.mockImplementation(async (_id: number, updates: Record<string, unknown>) => ({
      enabled: Boolean(updates.enabled),
      interval_minutes: Number(updates.interval_minutes),
      state: null,
    }))
    runCleanupMock.mockResolvedValue({ message: 'ok' })
    revokeSessionMock.mockResolvedValue({ message: 'ok' })
    revokeSessionsMock.mockResolvedValue({
      requested_count: 1,
      success_count: 1,
      failed_count: 0,
      revoked_session_ids: ['sess-1'],
      failures: [],
    })
    trustSessionMock.mockResolvedValue({ message: 'ok' })
  })

  it('打开时查询会话，并在确认后退出选中会话', async () => {
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    expect(listSessionsMock).toHaveBeenCalledWith(42)
    expect(getCleanupMock).toHaveBeenCalledWith(42)
    expect(wrapper.text()).toContain('MacBook Pro')
    expect(wrapper.text()).toContain('Shanghai, China')
    expect(wrapper.text()).toContain('admin.accounts.sessions.trusted')

    const logoutButton = wrapper.get('[data-testid="session-logout"]')
    await logoutButton.trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.sessions.logoutConfirm:MacBook Pro')

    await wrapper.get('[data-testid="confirm-session-logout"]').trigger('click')
    await flushPromises()

    expect(revokeSessionMock).toHaveBeenCalledWith(42, 'sess-1')
    expect(wrapper.text()).not.toContain('MacBook Pro')
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.sessions.logoutSuccess')
    expect(showErrorMock).not.toHaveBeenCalled()
  })

  it('支持选择会话并批量退出', async () => {
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    await wrapper.get('[data-testid="session-select"]').setValue(true)
    await wrapper.get('[data-testid="bulk-session-logout"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.sessions.logoutSelectedConfirm')

    await wrapper.get('[data-testid="confirm-bulk-session-logout"]').trigger('click')
    await flushPromises()

    expect(revokeSessionsMock).toHaveBeenCalledWith(42, ['sess-1'])
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.sessions.logoutSelectedSuccess:1')
    expect(wrapper.text()).not.toContain('MacBook Pro')
  })

  it('加载、保存并立即执行定时会话清理', async () => {
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="session-cleanup-settings"]').text()).toContain('admin.accounts.sessions.cleanup.status.success')
    const interval = wrapper.get('[data-testid="session-cleanup-interval"]')
    await interval.setValue(15)
    await wrapper.get('[data-testid="session-cleanup-save"]').trigger('click')
    await flushPromises()
    expect(updateCleanupMock).toHaveBeenCalledWith(42, { enabled: true, interval_minutes: 15 })
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.sessions.cleanup.saveSuccess')

    await wrapper.get('[data-testid="session-cleanup-run-now"]').trigger('click')
    await flushPromises()
    expect(runCleanupMock).toHaveBeenCalledWith(42)
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.sessions.cleanup.runSuccess')
  })

  it('拒绝超出范围的清理间隔', async () => {
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    await wrapper.get('[data-testid="session-cleanup-interval"]').setValue(4)
    await wrapper.get('[data-testid="session-cleanup-save"]').trigger('click')
    expect(updateCleanupMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.sessions.cleanup.invalidInterval')
  })

  it('支持将当前设备会话设为受信任', async () => {
    listSessionsMock.mockResolvedValueOnce({
      sessions: [{
        id: 'sess-current',
        device_name: 'Local Mac',
        current: true,
        trusted: false,
        status_available: true,
        can_revoke: false,
      }],
      fetched_at: 1,
      current_known: true,
    })
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    await wrapper.get('[data-testid="session-trust"]').trigger('click')
    await flushPromises()

    expect(trustSessionMock).toHaveBeenCalledWith(42, 'sess-current')
    expect(showSuccessMock).toHaveBeenCalledWith('admin.accounts.sessions.trust.success')
    expect(wrapper.text()).toContain('admin.accounts.sessions.trusted')
    expect(wrapper.find('[data-testid="session-trust"]').exists()).toBe(false)
  })
})
