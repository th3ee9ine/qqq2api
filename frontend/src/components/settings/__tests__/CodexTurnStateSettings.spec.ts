import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import CodexTurnStateSettings from '../CodexTurnStateSettings.vue'
import en from '@/i18n/locales/en/admin/settings'

vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, values: Record<string, unknown> = {}) => {
    let value: unknown = { admin: en }
    for (const part of key.split('.')) value = (value as Record<string, unknown>)?.[part]
    return String(value || key).replace(/\{(\w+)\}/g, (_, name: string) => String(values[name] ?? ''))
  },
}) }))

describe('CodexTurnStateSettings', () => {
  it('provides a single automatic switch with no token editor', async () => {
    const wrapper = mount(CodexTurnStateSettings, { props: { autoEnabled: false, defaultModel: 'gpt-5.5', models: '' } })
    expect(wrapper.text()).toContain('clients do not enter it manually')
    expect(wrapper.text()).toContain('upstream validity guarantee')
    expect(wrapper.text()).toContain('response.created and response.completed models')
    expect(wrapper.text()).toContain('429 stops IP rotation')
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="openai-codex-turn-state-clear"]').exists()).toBe(false)
    await wrapper.get('[data-testid="openai-codex-turn-state-auto-toggle"]').trigger('click')
    expect(wrapper.emitted('update:autoEnabled')).toEqual([[true]])
    wrapper.unmount()
  })
  it('lets model scope be prepared while automatic mode is off', async () => {
    const wrapper = mount(CodexTurnStateSettings, { props: { autoEnabled: false, defaultModel: 'gpt-5.5', models: 'gpt-5.5' } })
    const input = wrapper.get('[data-testid="openai-codex-turn-state-models"]')
    expect((input.element as HTMLInputElement).value).toBe('gpt-5.5')
    await input.setValue('gpt-5*')
    expect(wrapper.emitted('update:models')).toEqual([['gpt-5*']])
    wrapper.unmount()
  })
  it('allows editing the probe model without changing the model scope', async () => {
    const wrapper = mount(CodexTurnStateSettings, { props: { autoEnabled: false, defaultModel: 'gpt-5.5', models: 'gpt-5*' } })
    const input = wrapper.get('[data-testid="openai-codex-turn-state-default-model"]')
    expect((input.element as HTMLInputElement).value).toBe('gpt-5.5')
    await input.setValue('custom/probe-model')
    expect(wrapper.emitted('update:defaultModel')).toEqual([['custom/probe-model']])
    expect(wrapper.emitted('update:models')).toBeUndefined()
    wrapper.unmount()
  })

})
