import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import AccountTTFTHistory from '../AccountTTFTHistory.vue'
import accountMessages from '@/i18n/locales/en/admin/accounts'
import type { OpenAIOAuthTTFTHistory, OpenAIOAuthTTFTRequest } from '@/types'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
  t: (key: string, params?: Record<string, unknown>) => {
    const messages: Record<string, string> = accountMessages.accounts.ttftHistory
    const message = messages[key.split('.').at(-1)!] ?? key
    return message.replace(/\{(\w+)\}/g, (_, name: string) => String(params?.[name] ?? ''))
  }
  })
}))

enableAutoUnmount(afterEach)

function observation(firstTokenMs: number | null, index = 0): OpenAIOAuthTTFTRequest {
  return { first_token_ms: firstTokenMs, observed_at: `2026-10-11T12:00:0${5 - index}Z`, model: `gpt-model-${index}` }
}

function render(history?: OpenAIOAuthTTFTHistory | null) {
  return mount(AccountTTFTHistory, {
    props: { history },
    global: {
      stubs: { Teleport: true }
    }
  })
}

describe('AccountTTFTHistory', () => {
  it.each([undefined, null])('hides when the API omits the enabled account history: %s', (history) => {
    expect(render(history).find('[data-testid="account-ttft-history"]').exists()).toBe(false)
  })

  it('shows an empty state for enabled protection without requests', () => {
    const wrapper = render({ threshold_ms: 15_000, recent_requests: [] })
    expect(wrapper.text()).toContain('Last 5 TTFTs')
    expect(wrapper.get('[data-testid="account-ttft-empty"]').text()).toBe('No requests yet')
    expect(wrapper.get('[data-testid="account-ttft-empty"]').attributes('title')).toContain('while protection is enabled')
    expect(wrapper.find('button').exists()).toBe(false)
  })

  it('keeps the API newest-first order and renders at most five requests', () => {
    const recentRequests = [6000, 5000, 4000, 3000, 2000, 1000].map(observation)
    const wrapper = render({ threshold_ms: 15_000, recent_requests: recentRequests })
    expect(wrapper.findAll('[data-testid="account-ttft-sample"]').map(item => item.text())).toEqual(['6s', '5s', '4s', '3s', '2s'])
    expect(recentRequests).toHaveLength(6)
    expect(recentRequests[0]!.first_token_ms).toBe(6000)
  })

  it('preserves millisecond precision, zero, missing measurements and the strict threshold boundary', async () => {
    const wrapper = render({
      threshold_ms: 15_000,
      recent_requests: [null, 0, 14_999, 15_000, 15_001].map(observation)
    })
    const samples = wrapper.findAll('[data-testid="account-ttft-sample"]')
    expect(samples.map(item => item.text())).toEqual(['Not measured', '0s', '14.999s', '15s', '15.001s'])
    expect(samples.map(item => item.attributes('data-slow'))).toEqual(['false', 'false', 'false', 'false', 'true'])
    expect(samples[4]!.classes()).toContain('text-red-600')
    expect(wrapper.get('[role="tooltip"]').isVisible()).toBe(false)
    await wrapper.get('[data-testid="account-ttft-details"]').trigger('click')
    expect(wrapper.get('[role="tooltip"]').isVisible()).toBe(true)
    expect(wrapper.get('[role="tooltip"]').text()).toContain('newest first')
    expect(wrapper.get('[role="tooltip"]').text()).toContain('Requests over 15s')
    const observations = wrapper.findAll('[data-testid="account-ttft-observation"]')
    expect(observations[0]!.get('time').attributes('datetime')).toBe('2026-10-11T12:00:05Z')
    expect(observations[0]!.text()).toContain('gpt-model-0')
  })

  it('updates values and the threshold on refresh, then hides when protection is disabled', async () => {
    const wrapper = render({ threshold_ms: 15_000, recent_requests: [observation(16_000)] })
    expect(wrapper.get('[data-testid="account-ttft-sample"]').attributes('data-slow')).toBe('true')
    await wrapper.setProps({ history: { threshold_ms: 20_000, recent_requests: [observation(16_000)] } })
    expect(wrapper.get('[data-testid="account-ttft-sample"]').attributes('data-slow')).toBe('false')
    await wrapper.setProps({ history: { threshold_ms: 20_000, recent_requests: [observation(250)] } })
    expect(wrapper.get('[data-testid="account-ttft-sample"]').text()).toBe('0.25s')
    await wrapper.setProps({ history: undefined })
    expect(wrapper.find('[data-testid="account-ttft-history"]').exists()).toBe(false)
  })
})
