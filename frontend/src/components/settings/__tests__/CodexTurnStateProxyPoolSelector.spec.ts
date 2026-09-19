import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import CodexTurnStateProxyPoolSelector from '../CodexTurnStateProxyPoolSelector.vue'
import type { Proxy } from '@/types'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({
    t: (key: string, values?: Record<string, unknown>) => {
      if (key.endsWith('selectedCount')) return `${values?.count} selected`
      if (key.endsWith('compatiblePool')) return 'Automatic compatible pool'
      if (key.endsWith('limit')) return `Limit ${values?.count}`
      return key
    },
  }),
}))

function proxy(id: number, overrides: Partial<Proxy> = {}): Proxy {
  return {
    id,
    name: `Proxy ${id}`,
    protocol: 'socks5',
    host: `proxy-${id}.example.com`,
    port: 1080,
    username: 'collector',
    password: 'super-secret',
    status: 'active',
    max_accounts: 0,
    expires_at: null,
    fallback_mode: 'direct',
    expiry_warn_days: 7,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    ...overrides,
  }
}

describe('CodexTurnStateProxyPoolSelector', () => {
  it('renders redacted proxy metadata and emits individual selections', async () => {
    const wrapper = mount(CodexTurnStateProxyPoolSelector, {
      props: {
        modelValue: [],
        proxies: [proxy(7, { name: 'Tokyo route', country: 'JP', city: 'Tokyo' })],
      },
    })

    expect(wrapper.text()).toContain('Tokyo route')
    expect(wrapper.text()).toContain('socks5://proxy-7.example.com:1080')
    expect(wrapper.text()).toContain('JP / Tokyo')
    expect(wrapper.text()).toContain('Automatic compatible pool')
    expect(wrapper.text()).not.toContain('collector')
    expect(wrapper.text()).not.toContain('super-secret')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)

    await wrapper.get('[data-testid="codex-turn-state-proxy-7"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([[[7]]])
  })

  it('selects only visible search results and can clear the pool', async () => {
    const wrapper = mount(CodexTurnStateProxyPoolSelector, {
      props: {
        modelValue: [1],
        proxies: [
          proxy(1, { name: 'Tokyo route', country: 'JP' }),
          proxy(2, { name: 'Frankfurt route', country: 'DE' }),
        ],
      },
    })

    await wrapper.get('[data-testid="codex-turn-state-proxy-search"]').setValue('DE')
    expect(wrapper.text()).toContain('Frankfurt route')
    expect(wrapper.text()).not.toContain('Tokyo route')
    await wrapper.get('[data-testid="codex-turn-state-proxy-select-visible"]').setValue(true)
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[1, 2]])

    await wrapper.setProps({ modelValue: [1, 2] })
    await wrapper.get('[data-testid="codex-turn-state-proxy-clear"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('prevents a 257th explicit proxy selection', () => {
    const proxies = Array.from({ length: 257 }, (_, index) => proxy(index + 1))
    const wrapper = mount(CodexTurnStateProxyPoolSelector, {
      props: {
        modelValue: proxies.slice(0, 256).map((item) => item.id),
        proxies,
      },
    })

    expect(wrapper.get<HTMLInputElement>('[data-testid="codex-turn-state-proxy-256"]').element.disabled).toBe(false)
    expect(wrapper.get<HTMLInputElement>('[data-testid="codex-turn-state-proxy-257"]').element.disabled).toBe(true)
    expect(wrapper.text()).toContain('Limit 256')
  })
})
