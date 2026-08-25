import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import OAuthAuthorizationFlow from '../OAuthAuthorizationFlow.vue'

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copied: false,
    copyToClipboard: vi.fn()
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

describe('OAuthAuthorizationFlow', () => {
  it('can regenerate an authorization URL after one has already been created', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        authUrl: 'https://auth.openai.example/authorize',
        showCookieOption: false,
        showManualOption: true
      },
      global: {
        stubs: { Icon: true }
      }
    })

    ;(wrapper.vm as any).authCode = 'old-code'
    ;(wrapper.vm as any).oauthState = 'old-state'
    await wrapper.get('[data-testid="regenerate-oauth-url"]').trigger('click')

    expect(wrapper.emitted('generate-url')).toHaveLength(1)
    expect((wrapper.vm as any).authCode).toBe('')
    expect((wrapper.vm as any).oauthState).toBe('old-state')
  })

  it('shows only the retained OpenAI authorization methods', () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true,
        showRefreshTokenOption: true,
        showMobileRefreshTokenOption: true,
        showCodexSessionImportOption: true,
        showAgentIdentityOption: true,
        showCodexPatOption: true
      },
      global: { stubs: { Icon: true } }
    })

    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.refreshTokenAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.mobileRefreshTokenAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.codexSessionAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.agentIdentityAuth')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.codexPatAuth')
    expect(wrapper.text()).not.toMatch(/gemini|antigravity|grok|kimi|zhipu|deepseek/i)
  })

  it('shows batch token count and emits the full OpenAI refresh-token payload', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true,
        showRefreshTokenOption: true,
        allowMultiple: true
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input[value="refresh_token"]').setValue(true)
    await wrapper.get('textarea').setValue('refresh-one\n\nrefresh-two')

    expect(wrapper.text()).toContain('admin.accounts.oauth.keysCount')
    expect(wrapper.text()).toContain('admin.accounts.oauth.batchCreateAccounts')
    await wrapper.get('button.btn-primary.w-full').trigger('click')
    expect(wrapper.emitted('validate-refresh-token')?.[0]).toEqual(['refresh-one\n\nrefresh-two'])
  })

  it('routes the mobile refresh-token method to its dedicated event', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true,
        showMobileRefreshTokenOption: true
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input[value="mobile_refresh_token"]').setValue(true)
    await wrapper.get('textarea').setValue('mobile-token')
    await wrapper.get('button.btn-primary.w-full').trigger('click')

    expect(wrapper.emitted('validate-mobile-refresh-token')?.[0]).toEqual(['mobile-token'])
    expect(wrapper.emitted('validate-refresh-token')).toBeUndefined()
  })

  it('keeps Agent Identity and Codex PAT descriptions and import events', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true,
        showAgentIdentityOption: true,
        showCodexPatOption: true
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input[value="agent_identity"]').setValue(true)
    await wrapper.get('textarea').setValue('{"auth":{"access_token":"token"}}')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.agentIdentityDesc')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.agentIdentityHint')
    await wrapper.get('button.btn-primary.w-full').trigger('click')
    expect(wrapper.emitted('import-codex-session')?.[0]).toEqual(['{"auth":{"access_token":"token"}}'])

    await wrapper.get('input[value="codex_pat"]').setValue(true)
    await wrapper.get('textarea').setValue('at-personal-token')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.codexPatDesc')
    expect(wrapper.text()).toContain('admin.accounts.oauth.openai.codexPatHint')
    await wrapper.get('button.btn-primary.w-full').trigger('click')
    expect(wrapper.emitted('import-codex-pat')?.[0]).toEqual(['at-personal-token'])
  })

  it('extracts OpenAI code and state from a pasted callback URL', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('textarea').setValue('http://localhost/callback?code=code-value&state=state-value')
    expect((wrapper.vm as any).authCode).toBe('code-value')
    expect((wrapper.vm as any).oauthState).toBe('state-value')
  })

  it('keeps manual authorization errors as a sibling below the callback input', () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'openai',
        showCookieOption: false,
        showManualOption: true,
        error: 'authorization failed'
      },
      global: { stubs: { Icon: true } }
    })

    const textarea = wrapper.get('textarea')
    const error = wrapper
      .findAll('.border-red-200')
      .find((candidate) => candidate.text().includes('authorization failed'))

    expect(error).toBeDefined()
    expect(textarea.element.parentElement?.nextElementSibling).toBe(error?.element)
    expect(error?.classes()).toEqual(
      expect.arrayContaining(['dark:border-red-700', 'dark:bg-red-900/30'])
    )
    expect(error?.get('p').classes()).toContain('dark:text-red-400')
  })

  it('renders Claude cookie help, count, loading, and error feedback', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'setup-token',
        platform: 'anthropic',
        showCookieOption: true,
        showManualOption: true,
        allowMultiple: true,
        loading: true,
        error: 'authorization failed'
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input[value="cookie"]').setValue(true)
    const textarea = wrapper.get('textarea')
    await textarea.setValue('  session-one\nsession-two  ')
    await wrapper.get('[data-testid="session-key-help"]').trigger('click')
    expect(wrapper.text()).toContain('admin.accounts.oauth.howToGetSessionKey')
    expect(wrapper.text()).toContain('admin.accounts.oauth.step6')
    expect(wrapper.text()).toContain('admin.accounts.oauth.sessionKeyFormat')
    expect(wrapper.text()).toContain('admin.accounts.oauth.keysCount')
    expect(wrapper.text()).toContain('authorization failed')
    expect(textarea.attributes('rows')).toBe('3')
    expect(wrapper.get('button.btn-primary.w-full').attributes('disabled')).toBeDefined()
  })

  it('preserves the original Claude cookie batch payload', async () => {
    const wrapper = mount(OAuthAuthorizationFlow, {
      props: {
        addMethod: 'oauth',
        platform: 'anthropic',
        showCookieOption: true,
        showManualOption: true,
        allowMultiple: true
      },
      global: { stubs: { Icon: true } }
    })

    await wrapper.get('input[value="cookie"]').setValue(true)
    await wrapper.get('textarea').setValue('  session-one\nsession-two  ')
    await wrapper.get('button.btn-primary.w-full').trigger('click')

    expect(wrapper.emitted('cookie-auth')?.[0]).toEqual(['  session-one\nsession-two  '])
  })
})
