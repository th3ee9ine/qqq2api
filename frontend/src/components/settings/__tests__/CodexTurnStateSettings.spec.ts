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
    const wrapper = mount(CodexTurnStateSettings, { props: { autoEnabled: false, models: '' } })
    expect(wrapper.text()).toContain('No token entry is needed')
    expect(wrapper.text()).toContain('upstream quota')
    expect(wrapper.text()).toContain('312 signal')
    expect(wrapper.text()).toContain('new 292 state')
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="openai-codex-turn-state-clear"]').exists()).toBe(false)
    await wrapper.get('[data-testid="openai-codex-turn-state-auto-toggle"]').trigger('click')
    expect(wrapper.emitted('update:autoEnabled')).toEqual([[true]])
    wrapper.unmount()
  })
  it('lets model scope be prepared while automatic mode is off', async () => {
    const wrapper = mount(CodexTurnStateSettings, { props: { autoEnabled: false, models: 'gpt-5.5' } })
    const input = wrapper.get('[data-testid="openai-codex-turn-state-models"]')
    expect((input.element as HTMLInputElement).value).toBe('gpt-5.5')
    await input.setValue('gpt-5*')
    expect(wrapper.emitted('update:models')).toEqual([['gpt-5*']])
    wrapper.unmount()
  })
})
