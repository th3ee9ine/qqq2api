import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { Account } from '@/types'

const { listSessionsMock, revokeSessionMock, showSuccessMock, showErrorMock } = vi.hoisted(() => ({
  listSessionsMock: vi.fn(),
  revokeSessionMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      listOpenAISessions: listSessionsMock,
      revokeOpenAISession: revokeSessionMock,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: showSuccessMock, showError: showErrorMock }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params?.device ? `${key}:${String(params.device)}` : key,
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
    })
    revokeSessionMock.mockResolvedValue({ message: 'ok' })
  })

  it('打开时查询会话，并在确认后退出选中会话', async () => {
    const wrapper = mount(OpenAISessionsModal, {
      props: { show: true, account },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })
    await flushPromises()

    expect(listSessionsMock).toHaveBeenCalledWith(42)
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
})
