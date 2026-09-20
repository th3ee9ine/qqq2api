import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { authIsAccountAdmin } = vi.hoisted(() => ({
  authIsAccountAdmin: { value: false },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isAccountAdmin() {
      return authIsAccountAdmin.value
    },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import AccountTodayStatsCell from '../AccountTodayStatsCell.vue'

const stats = {
  requests: 12,
  tokens: 3456,
  cost: 4.25,
  standard_cost: 7.5,
  user_cost: 9.75,
}

describe('AccountTodayStatsCell account administrator earnings', () => {
  beforeEach(() => {
    authIsAccountAdmin.value = false
  })

  it('labels account cost as earnings and hides user billing for account administrators', () => {
    authIsAccountAdmin.value = true
    const wrapper = mount(AccountTodayStatsCell, { props: { stats } })

    expect(wrapper.text()).toContain('admin.accounts.stats.earnings')
    expect(wrapper.text()).not.toContain('usage.accountBilled')
    expect(wrapper.text()).not.toContain('usage.userBilled')
  })

  it('keeps both billing values visible for super administrators', () => {
    const wrapper = mount(AccountTodayStatsCell, { props: { stats } })

    expect(wrapper.text()).toContain('usage.accountBilled')
    expect(wrapper.text()).toContain('usage.userBilled')
    expect(wrapper.text()).not.toContain('admin.accounts.stats.earnings')
  })
})
