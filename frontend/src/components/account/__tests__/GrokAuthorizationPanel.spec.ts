import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
const { api } = vi.hoisted(() => ({ api: {
  getCapabilities: vi.fn(), generateAuthUrl: vi.fn(), exchangeCode: vi.fn(),
  refreshGrokToken: vi.fn(), validateSSOToken: vi.fn(), authorizePassword: vi.fn()
} }))
vi.mock('@/api/admin', () => ({ adminAPI: { grok: api } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
import GrokAuthorizationPanel from '../GrokAuthorizationPanel.vue'

describe('Grok authorization inputs', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.getCapabilities.mockResolvedValue({ password_auth_enabled: false })
    api.generateAuthUrl.mockResolvedValue({ auth_url: 'https://accounts.x.ai/authorize', session_id: 'session', state: 'generated-state' })
    api.exchangeCode.mockResolvedValue({ access_token: 'access', refresh_token: 'refresh' })
  })
  it('exchanges a callback URL with its original state and selected proxy', async () => {
    const wrapper = mount(GrokAuthorizationPanel, { props: { proxyId: 7 } })
    await wrapper.find('button').trigger('click')
    await flushPromises()
    await wrapper.get('textarea').setValue('http://localhost/callback?code=code%2B1&state=callback-state')
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await flushPromises()
    expect(api.exchangeCode).toHaveBeenCalledWith({ session_id: 'session', code: 'code+1', state: 'callback-state', proxy_id: 7 })
    expect(wrapper.emitted('authorized')?.[0]).toEqual([{ access_token: 'access', refresh_token: 'refresh' }])
  })
  it('exposes password authorization only when the server advertises it', async () => {
    const wrapper = mount(GrokAuthorizationPanel)
    await flushPromises()
    expect(wrapper.find('option[value="password"]').exists()).toBe(false)
    api.getCapabilities.mockResolvedValue({ password_auth_enabled: true })
    const enabled = mount(GrokAuthorizationPanel)
    await flushPromises()
    expect(enabled.find('option[value="password"]').exists()).toBe(true)
  })
  it('drops an SSO response when the selected proxy changes during authorization', async () => {
    let complete!: (value: { access_token: string }) => void
    api.validateSSOToken.mockImplementationOnce(() => new Promise(resolve => { complete = resolve }))
    const wrapper = mount(GrokAuthorizationPanel, { props: { proxyId: 1 } })
    await wrapper.get('select').setValue('sso')
    await wrapper.get('input').setValue('private-sso')
    await wrapper.findAll('button').at(-1)!.trigger('click')
    await wrapper.setProps({ proxyId: 2 })
    complete({ access_token: 'stale-access' })
    await flushPromises()
    expect(wrapper.emitted('authorized')).toBeUndefined()
    expect((wrapper.get('input').element as HTMLInputElement).value).toBe('')
  })
})
