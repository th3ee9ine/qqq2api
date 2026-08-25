import { defineComponent, h, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  applyOAuthCredentialsMock,
  exchangeCodeMock,
  openAIValidateRefreshTokenMock,
  openAIExchangeAuthCodeMock,
  openAIBuildCredentialsMock,
  openAIBuildExtraInfoMock,
  claudeCookieAuthMock,
  claudeBuildExtraInfoMock,
  showErrorMock,
  openAISessionID,
  claudeSessionID,
} = vi.hoisted(() => ({
  applyOAuthCredentialsMock: vi.fn(),
  exchangeCodeMock: vi.fn(),
  openAIValidateRefreshTokenMock: vi.fn(),
  openAIExchangeAuthCodeMock: vi.fn(),
  openAIBuildCredentialsMock: vi.fn((tokenInfo) => ({
    access_token: tokenInfo.access_token,
    refresh_token: tokenInfo.refresh_token,
    client_id: tokenInfo.client_id,
  })),
  openAIBuildExtraInfoMock: vi.fn(() => ({ account_id: 'openai-account' })),
  claudeCookieAuthMock: vi.fn(),
  claudeBuildExtraInfoMock: vi.fn(() => ({ account_uuid: 'claude-account' })),
  showErrorMock: vi.fn(),
  openAISessionID: { value: 'openai-session' },
  claudeSessionID: { value: 'claude-session' },
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      applyOAuthCredentials: applyOAuthCredentialsMock,
      exchangeCode: exchangeCodeMock,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: vi.fn(),
    showError: showErrorMock,
  }),
}))

vi.mock('@/composables/useOpenAIOAuth', () => ({
  useOpenAIOAuth: () => ({
    authUrl: ref('https://openai.example/authorize'),
    sessionId: ref(openAISessionID.value),
    oauthState: ref('openai-state'),
    loading: ref(false),
    error: ref(''),
    generateAuthUrl: vi.fn(),
    exchangeAuthCode: openAIExchangeAuthCodeMock,
    validateRefreshToken: openAIValidateRefreshTokenMock,
    buildCredentials: openAIBuildCredentialsMock,
    buildExtraInfo: openAIBuildExtraInfoMock,
    resetState: vi.fn(),
  }),
}))

vi.mock('@/composables/useAccountOAuth', () => ({
  useAccountOAuth: () => ({
    authUrl: ref('https://claude.example/authorize'),
    sessionId: ref(claudeSessionID.value),
    loading: ref(false),
    error: ref(''),
    generateAuthUrl: vi.fn(),
    cookieAuth: claudeCookieAuthMock,
    buildExtraInfo: claudeBuildExtraInfoMock,
    resetState: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import ReAuthAccountModal from '../ReAuthAccountModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const OAuthAuthorizationFlowStub = defineComponent({
  props: { error: { type: String, default: '' } },
  emits: [
    'generate-url',
    'cookie-auth',
    'validate-refresh-token',
    'validate-mobile-refresh-token'
  ],
  setup(props, { emit, expose }) {
    expose({
      authCode: 'claude-auth-code',
      oauthState: 'openai-state',
      sessionKey: '',
      inputMethod: 'manual',
      reset: vi.fn(),
    })
    return () => h('div', [
      props.error ? h('span', { class: 'oauth-error' }, props.error) : null,
      h('button', {
        'data-testid': 'mobile-refresh',
        onClick: () => emit('validate-mobile-refresh-token', 'mobile-refresh-token'),
      }, 'mobile'),
      h('button', {
        'data-testid': 'standard-refresh',
        onClick: () => emit('validate-refresh-token', '\nstandard-refresh-token\nignored-token'),
      }, 'refresh'),
      h('button', {
        'data-testid': 'cookie-auth',
        onClick: () => emit('cookie-auth', 'claude-session-key'),
      }, 'cookie')
    ])
  },
})

function mountModal(account: Record<string, unknown>) {
  return mount(ReAuthAccountModal, {
    props: {
      show: false,
      account,
    } as any,
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        Icon: true,
      },
    },
  })
}

describe('ReAuthAccountModal', () => {
  beforeEach(() => {
    applyOAuthCredentialsMock.mockReset()
    exchangeCodeMock.mockReset()
    openAIValidateRefreshTokenMock.mockReset()
    openAIExchangeAuthCodeMock.mockReset()
    claudeCookieAuthMock.mockReset()
    openAIBuildCredentialsMock.mockClear()
    openAIBuildExtraInfoMock.mockClear()
    claudeBuildExtraInfoMock.mockClear()
    showErrorMock.mockReset()
    applyOAuthCredentialsMock.mockResolvedValue({ id: 42 })
  })

  it('OpenAI mobile refresh token uses the mobile client id and persists it', async () => {
    openAIValidateRefreshTokenMock.mockResolvedValue({
      access_token: 'access-token',
      refresh_token: 'rotated-refresh-token',
    })
    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      proxy_id: 7,
    })

    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="mobile-refresh"]').trigger('click')
    await flushPromises()

    expect(openAIValidateRefreshTokenMock).toHaveBeenCalledWith(
      'mobile-refresh-token',
      7,
      'app_LlGpXReQgckcGGUo2JrYvtJK',
    )
    expect(openAIBuildCredentialsMock).toHaveBeenCalledWith(expect.objectContaining({
      client_id: 'app_LlGpXReQgckcGGUo2JrYvtJK',
    }))
    expect(applyOAuthCredentialsMock).toHaveBeenCalledWith(42, expect.objectContaining({
      type: 'oauth',
      credentials: expect.objectContaining({
        client_id: 'app_LlGpXReQgckcGGUo2JrYvtJK',
      }),
    }))
  })

  it('Claude setup-token reauthorization uses the dedicated exchange endpoint', async () => {
    exchangeCodeMock.mockResolvedValue({
      access_token: 'claude-access-token',
      refresh_token: 'claude-refresh-token',
    })
    const wrapper = mountModal({
      id: 9,
      name: 'Claude Setup Token',
      platform: 'anthropic',
      type: 'setup-token',
      proxy_id: 3,
    })

    await wrapper.setProps({ show: true })
    await flushPromises()
    const submit = wrapper.findAll('button').find((button) =>
      button.text().includes('admin.accounts.oauth.completeAuth'),
    )
    expect(submit).toBeDefined()
    await submit?.trigger('click')
    await flushPromises()

    expect(exchangeCodeMock).toHaveBeenCalledWith('/admin/accounts/exchange-setup-token-code', {
      session_id: 'claude-session',
      code: 'claude-auth-code',
      proxy_id: 3,
    })
    expect(applyOAuthCredentialsMock).toHaveBeenCalledWith(9, {
      type: 'setup-token',
      credentials: {
        access_token: 'claude-access-token',
        refresh_token: 'claude-refresh-token',
      },
      extra: { account_uuid: 'claude-account' },
    })
  })

  it('OpenAI standard refresh token keeps the existing client semantics', async () => {
    openAIValidateRefreshTokenMock.mockResolvedValue({
      access_token: 'access-token',
      refresh_token: 'rotated-refresh-token',
      client_id: 'existing-client-id'
    })
    const wrapper = mountModal({
      id: 43,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      proxy_id: null
    })

    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="standard-refresh"]').trigger('click')
    await flushPromises()

    expect(openAIValidateRefreshTokenMock).toHaveBeenCalledWith(
      'standard-refresh-token',
      null,
      undefined
    )
    expect(applyOAuthCredentialsMock).toHaveBeenCalledWith(43, expect.objectContaining({
      type: 'oauth',
      credentials: expect.objectContaining({ client_id: 'existing-client-id' })
    }))
  })

  it('OpenAI callback-code reauthorization exchanges state and applies rotated credentials', async () => {
    openAIExchangeAuthCodeMock.mockResolvedValue({
      access_token: 'new-access-token',
      refresh_token: 'new-refresh-token',
      client_id: 'oauth-client'
    })
    const wrapper = mountModal({
      id: 44,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      proxy_id: 8
    })

    await wrapper.setProps({ show: true })
    await flushPromises()
    const submit = wrapper.findAll('button').find(button =>
      button.text().includes('admin.accounts.oauth.completeAuth')
    )
    await submit?.trigger('click')
    await flushPromises()

    expect(openAIExchangeAuthCodeMock).toHaveBeenCalledWith(
      'claude-auth-code',
      'openai-session',
      'openai-state',
      8
    )
    expect(applyOAuthCredentialsMock).toHaveBeenCalledWith(44, expect.objectContaining({
      type: 'oauth',
      credentials: expect.objectContaining({ access_token: 'new-access-token' })
    }))
  })

  it('surfaces an OpenAI credential-apply failure inside the authorization flow', async () => {
    openAIValidateRefreshTokenMock.mockResolvedValue({
      access_token: 'access-token',
      refresh_token: 'refresh-token'
    })
    applyOAuthCredentialsMock.mockRejectedValue({
      response: { data: { detail: 'credentials rejected' } }
    })
    const wrapper = mountModal({
      id: 45,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      proxy_id: null
    })

    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="standard-refresh"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('.oauth-error').text()).toBe('credentials rejected')
    expect(showErrorMock).toHaveBeenCalledWith('credentials rejected')
    expect(wrapper.emitted('reauthorized')).toBeUndefined()
  })

  it('Claude cookie reauthorization applies credentials to the existing account', async () => {
    claudeCookieAuthMock.mockResolvedValue({
      access_token: 'claude-access',
      refresh_token: 'claude-refresh'
    })
    const wrapper = mountModal({
      id: 10,
      name: 'Claude OAuth',
      platform: 'anthropic',
      type: 'oauth',
      proxy_id: 5
    })

    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="cookie-auth"]').trigger('click')
    await flushPromises()

    expect(claudeCookieAuthMock).toHaveBeenCalledWith('oauth', 'claude-session-key', 5)
    expect(applyOAuthCredentialsMock).toHaveBeenCalledWith(10, {
      type: 'oauth',
      credentials: {
        access_token: 'claude-access',
        refresh_token: 'claude-refresh'
      },
      extra: { account_uuid: 'claude-account' }
    })
  })

  it('closes immediately for a retired platform account', async () => {
    const wrapper = mountModal({
      id: 99,
      name: 'Retired',
      platform: 'grok',
      type: 'oauth',
      proxy_id: null
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(applyOAuthCredentialsMock).not.toHaveBeenCalled()
  })
})
