import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CodexTurnStateSettings from '../CodexTurnStateSettings.vue'
import en from '@/i18n/locales/en/admin/settings'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, values: Record<string, unknown> = {}) => {
    let value: unknown = { admin: en }
    for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
    return String(value || key).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? ''))
  },
}) }))

describe('CodexTurnStateSettings', () => {
  const proxy: Proxy = {
    id: 17,
    name: 'Dedicated state route',
    protocol: 'socks5',
    host: 'proxy.example.com',
    port: 1080,
    username: 'collector',
    password: 'super-secret',
    status: 'inactive',
    max_accounts: 0,
    expires_at: null,
    fallback_mode: 'direct',
    expiry_warn_days: 7,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
  }

  it('provides automatic collection and a redacted dedicated proxy selector', async () => {
    const wrapper = mount(CodexTurnStateSettings, {
      props: {
        autoEnabled: false,
        models: 'gpt-5.5',
        defaultModel: 'gpt-5.5',
        proxyId: proxy.id,
        proxies: [proxy],
      },
    })
    expect(wrapper.text()).toContain('clients do not enter it manually')
    expect(wrapper.text()).toContain('upstream validity guarantee')
    expect(wrapper.text()).toContain('response.created and response.completed models')
    expect(wrapper.text()).toContain('Retry-After backoff')
    expect(wrapper.text()).toContain('Dedicated state route (socks5://proxy.example.com:1080)')
    expect(wrapper.text()).not.toContain('super-secret')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="openai-codex-turn-state-clear"]').exists()).toBe(false)
    await wrapper.get('[data-testid="openai-codex-turn-state-auto-toggle"]').trigger('click')
    expect(wrapper.emitted('update:autoEnabled')).toEqual([[true]])
    wrapper.unmount()
  })

  it('emits the selected dedicated proxy', async () => {
    const wrapper = mount(CodexTurnStateSettings, {
      props: {
        autoEnabled: false,
        models: '',
        defaultModel: 'gpt-5.5',
        proxies: [proxy],
      },
    })
    const selector = wrapper.get('[data-testid="openai-codex-turn-state-proxy"]')
    await selector.get('button').trigger('click')
    const option = selector.findAll('.select-option').find(node => node.text().includes(proxy.name))
    expect(option).toBeDefined()
    await option?.trigger('click')
    expect(wrapper.emitted('update:proxyId')).toEqual([[proxy.id]])
    wrapper.unmount()
  })

  it('lets model scope be prepared while automatic mode is off', async () => {
    const wrapper = mount(CodexTurnStateSettings, {
      props: { autoEnabled: false, defaultModel: 'gpt-5.5', models: 'gpt-5.5' },
    })
    const input = wrapper.get('[data-testid="openai-codex-turn-state-models"]')
    expect((input.element as HTMLInputElement).value).toBe('gpt-5.5')
    await input.setValue('gpt-5*')
    expect(wrapper.emitted('update:models')).toEqual([['gpt-5*']])
    wrapper.unmount()
  })

  it('allows editing the default probe model without changing the model scope', async () => {
    const wrapper = mount(CodexTurnStateSettings, {
      props: { autoEnabled: false, defaultModel: 'gpt-5.5', models: 'gpt-5*' },
    })
    const input = wrapper.get('[data-testid="openai-codex-turn-state-default-model"]')
    expect((input.element as HTMLInputElement).value).toBe('gpt-5.5')
    await input.setValue('custom/probe-model')
    expect(wrapper.emitted('update:defaultModel')).toEqual([['custom/probe-model']])
    expect(wrapper.emitted('update:models')).toBeUndefined()
    wrapper.unmount()
  })
})
