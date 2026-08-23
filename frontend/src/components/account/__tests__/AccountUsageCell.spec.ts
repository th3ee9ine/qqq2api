import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AccountUsageCell from '../AccountUsageCell.vue'
import type { Account } from '@/types'

const { getUsage } = vi.hoisted(() => ({ getUsage: vi.fn() }))

vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getUsage } } }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

function account(overrides: Partial<Account> = {}): Account {
  return {
    id: 1,
    name: 'account',
    platform: 'anthropic',
    type: 'apikey',
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: true,
    created_at: '2026-03-15T00:00:00Z',
    updated_at: '2026-03-15T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    ...overrides
  }
}

describe('AccountUsageCell', () => {
  beforeEach(() => getUsage.mockReset())

  it('renders API-key today statistics', () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: account(),
        todayStats: { requests: 1_000_000, tokens: 1_000_000_000, cost: 12.345, user_cost: 6.789 }
      },
      global: { stubs: { UsageProgressBar: true, OllamaCloudUsageCell: true } }
    })
    expect(wrapper.text()).toContain('1.0M req')
    expect(wrapper.text()).toContain('1.0B')
    expect(wrapper.text()).toContain('A $12.35')
    expect(wrapper.text()).toContain('U $6.79')
    expect(getUsage).not.toHaveBeenCalled()
  })

  it('loads Anthropic OAuth usage windows', async () => {
    getUsage.mockResolvedValue({
      five_hour: { utilization: 25, resets_at: null, remaining_seconds: 0 },
      seven_day: { utilization: 50, resets_at: null, remaining_seconds: 0 },
      seven_day_sonnet: null
    })
    const wrapper = mount(AccountUsageCell, {
      props: { account: account({ type: 'oauth' }) },
      global: {
        stubs: {
          UsageProgressBar: { props: ['label', 'utilization'], template: '<span>{{ label }}|{{ utilization }}</span>' }
        }
      }
    })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledWith(1, 'passive', false)
    expect(wrapper.text()).toContain('5h|25')
    expect(wrapper.text()).toContain('7d|50')
  })

  it('loads OpenAI OAuth usage and refreshes on demand', async () => {
    getUsage.mockResolvedValue({
      five_hour: { utilization: 18, resets_at: null, remaining_seconds: 0 },
      seven_day: { utilization: 36, resets_at: null, remaining_seconds: 0 },
      seven_day_sonnet: null
    })
    const wrapper = mount(AccountUsageCell, {
      props: { account: account({ id: 2, platform: 'openai', type: 'oauth' }), manualRefreshToken: 0 },
      global: {
        stubs: {
          UsageProgressBar: { props: ['label', 'utilization'], template: '<span>{{ label }}|{{ utilization }}</span>' },
          OpenAIQuotaResetCell: true
        }
      }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('5h|18')
    await wrapper.setProps({ manualRefreshToken: 1 })
    await flushPromises()
    expect(getUsage).toHaveBeenCalledTimes(2)
  })

  it('does not fetch usage for a legacy removed platform', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: { account: account({ platform: 'gemini', type: 'oauth' }) },
      global: { stubs: { UsageProgressBar: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toBe('-')
    expect(getUsage).not.toHaveBeenCalled()
  })
})
