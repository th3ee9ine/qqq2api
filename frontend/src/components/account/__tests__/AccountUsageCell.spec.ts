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
      global: { stubs: { UsageProgressBar: true } }
    })
    expect(wrapper.text()).toContain('1.0M req')
    expect(wrapper.text()).toContain('1.0B')
    expect(wrapper.text()).toContain('A $12.35')
    expect(wrapper.text()).not.toContain('U $')
    expect(getUsage).not.toHaveBeenCalled()
  })

  it('keeps the original rolling quota reset timestamp when the DTO has no fixed reset field', () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: account({
          quota_daily_limit: 100,
          quota_daily_used: 25,
          quota_daily_reset_at: null,
          extra: {
            quota_daily_reset_mode: 'rolling',
            quota_daily_start: '2026-03-15T00:00:00.000Z'
          }
        })
      },
      global: {
        stubs: {
          UsageProgressBar: {
            props: ['label', 'resetsAt'],
            template: '<span :data-label="label" :data-resets-at="resetsAt" />'
          }
        }
      }
    })

    expect(wrapper.get('[data-label="1d"]').attributes('data-resets-at')).toBe(
      '2026-03-16T00:00:00.000Z'
    )
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

  it('renders one loading row for an Anthropic setup-token account', () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: account({ type: 'setup-token' }),
        batchedUsageLoading: true,
        requestBatchedUsage: vi.fn()
      },
      global: { stubs: { UsageProgressBar: true } }
    })

    expect(wrapper.findAll('[data-testid="usage-loading-row"]')).toHaveLength(1)
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

  it('keeps the last OpenAI usage snapshot visible while a refresh is loading', () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: account({ id: 3, platform: 'openai', type: 'oauth' }),
        batchedUsage: {
          five_hour: { utilization: 44, resets_at: null, remaining_seconds: 0 },
          seven_day: { utilization: 66, resets_at: null, remaining_seconds: 0 }
        },
        batchedUsageLoading: true,
        batchedUsageError: 'refresh failed',
        requestBatchedUsage: vi.fn()
      },
      global: {
        stubs: {
          UsageProgressBar: { props: ['label', 'utilization'], template: '<span>{{ label }}|{{ utilization }}</span>' },
          OpenAIQuotaResetCell: { template: '<div><slot name="pre-actions" /></div>' }
        }
      }
    })

    expect(wrapper.text()).toContain('5h|44')
    expect(wrapper.text()).toContain('7d|66')
    expect(wrapper.findAll('[data-testid="usage-loading-row"]')).toHaveLength(0)
    expect(wrapper.text()).not.toContain('refresh failed')
  })

  it('keeps the OpenAI quota action available before any usage snapshot exists', () => {
    const wrapper = mount(AccountUsageCell, {
      props: {
        account: account({ id: 4, platform: 'openai', type: 'oauth' }),
        batchedUsage: null,
        batchedUsageLoading: false,
        requestBatchedUsage: vi.fn()
      },
      global: {
        stubs: {
          UsageProgressBar: true,
          OpenAIQuotaResetCell: { template: '<button data-testid="openai-quota-action">quota</button>' }
        }
      }
    })

    expect(wrapper.text()).toContain('-')
    expect(wrapper.find('[data-testid="openai-quota-action"]').exists()).toBe(true)
  })

  it('defers mobile usage loading until the account row approaches the viewport', async () => {
    const originalMatchMedia = window.matchMedia
    const originalIntersectionObserver = globalThis.IntersectionObserver
    let intersectionCallback: IntersectionObserverCallback | undefined
    const observe = vi.fn()
    const disconnect = vi.fn()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: vi.fn().mockReturnValue({
        matches: false,
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        addListener: vi.fn(),
        removeListener: vi.fn()
      })
    })
    globalThis.IntersectionObserver = class {
      constructor(callback: IntersectionObserverCallback) {
        intersectionCallback = callback
      }
      observe = observe
      disconnect = disconnect
      unobserve = vi.fn()
      takeRecords = vi.fn().mockReturnValue([])
      root = null
      rootMargin = ''
      thresholds = []
    } as unknown as typeof IntersectionObserver
    getUsage.mockResolvedValue({
      five_hour: { utilization: 12, resets_at: null, remaining_seconds: 0 }
    })

    const wrapper = mount(AccountUsageCell, {
      props: { account: account({ id: 99, platform: 'openai', type: 'oauth' }) },
      global: { stubs: { UsageProgressBar: true, OpenAIQuotaResetCell: true } }
    })
    await flushPromises()

    expect(observe).toHaveBeenCalledTimes(1)
    expect(getUsage).not.toHaveBeenCalled()

    intersectionCallback?.(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver
    )
    await flushPromises()

    expect(getUsage).toHaveBeenCalledWith(99, undefined, false)
    wrapper.unmount()
    expect(disconnect).toHaveBeenCalled()

    Object.defineProperty(window, 'matchMedia', {
      configurable: true,
      value: originalMatchMedia
    })
    globalThis.IntersectionObserver = originalIntersectionObserver
  })

  it('does not fetch usage for an unsupported platform', async () => {
    const wrapper = mount(AccountUsageCell, {
      props: { account: account({ platform: 'unsupported' as Account['platform'], type: 'oauth' }) },
      global: { stubs: { UsageProgressBar: true } }
    })
    await flushPromises()
    expect(wrapper.text()).toBe('-')
    expect(getUsage).not.toHaveBeenCalled()
  })
})
