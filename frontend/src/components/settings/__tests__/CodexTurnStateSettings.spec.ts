import { afterEach, describe, expect, it, vi } from 'vitest'
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

const now = 1_800_000_000_000
const baseProps = { enabled: true, autoEnabled: false, token: '', tokenDirty: false, models: '', configured: true, setAtMs: now,
  status: { enabled: true, configured: true, active: true, reason: 'ready', verdict: 'normal', blocks: 10, issued_at: now / 1000 - 3599, expires_at: now / 1000 + 1 } }
function mountCard(props = baseProps) {
  return mount(CodexTurnStateSettings, { props })
}
afterEach(() => vi.useRealTimers())
describe('CodexTurnStateSettings', () => {
  it('allows automatic lifecycle independently of the global override and explains probe costs', async () => {
    const wrapper = mountCard({ ...baseProps, enabled: false })
    expect(wrapper.text()).toContain('upstream quota')
    expect(wrapper.text()).toContain('312 state (11 cipher blocks)')
    expect(wrapper.text()).toContain('new 292 state (10 blocks)')
    await wrapper.get('[data-testid="openai-codex-turn-state-auto-toggle"]').trigger('click')
    expect(wrapper.emitted('update:autoEnabled')).toEqual([[true]])
    expect(wrapper.emitted('update:enabled')).toBeUndefined()
    wrapper.unmount()
  })
  it('shows countdown and continues eligibility beyond the heuristic TTL', async () => {
    vi.useFakeTimers(); vi.setSystemTime(now)
    const wrapper = mountCard()
    expect(wrapper.text()).toContain('00:01')
    await vi.advanceTimersByTimeAsync(2000)
    expect(wrapper.text()).toContain('warning only, still injected')
    expect(wrapper.text()).toContain('matching requests will use')
    expect(wrapper.get('input[type="password"]').element.value).toBe('')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
  it('previews opaque replacement and clearing without exposing any saved secret', async () => {
    const wrapper = mountCard()
    await wrapper.setProps({ token: 'opaque-state', tokenDirty: true })
    expect(wrapper.text()).toContain('Local preview of unsaved token')
    expect(wrapper.text()).toContain('Unknown envelope (warning only)')
    await wrapper.get('[data-testid="openai-codex-turn-state-clear"]').trigger('click')
    expect(wrapper.emitted('clear-token')).toHaveLength(1)
    await wrapper.setProps({ token: '', tokenDirty: true })
    expect(wrapper.text()).toContain('Token not configured')
    expect(wrapper.text()).toContain('no configured value is injected')
    wrapper.unmount()
  })
})
