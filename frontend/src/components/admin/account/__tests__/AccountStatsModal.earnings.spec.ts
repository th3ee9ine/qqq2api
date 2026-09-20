import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const { authIsAccountAdmin, getStatsMock } = vi.hoisted(() => ({
  authIsAccountAdmin: { value: true },
  getStatsMock: vi.fn(),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isAccountAdmin() {
      return authIsAccountAdmin.value
    },
  }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getStats: getStatsMock,
    },
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('vue-chartjs', () => ({
  Line: {
    props: ['data'],
    template: '<div data-testid="trend-labels">{{ data.datasets.map(dataset => dataset.label).join("|") }}</div>',
  },
}))

import AccountStatsModal from '../AccountStatsModal.vue'

const BaseDialogStub = defineComponent({
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const statsResponse = {
  history: [{
    date: '2026-09-20',
    label: '09-20',
    requests: 3,
    tokens: 400,
    cost: 1,
    actual_cost: 2,
    user_cost: 8,
  }],
  summary: {
    days: 30,
    actual_days_used: 1,
    total_cost: 2,
    total_user_cost: 8,
    total_standard_cost: 10,
    total_requests: 3,
    total_tokens: 400,
    avg_daily_cost: 2,
    avg_daily_user_cost: 8,
    avg_daily_requests: 3,
    avg_daily_tokens: 400,
    avg_duration_ms: 120,
    today: { date: '2026-09-20', cost: 2, user_cost: 8, requests: 3, tokens: 400 },
    highest_cost_day: { date: '2026-09-20', label: '09-20', cost: 2, user_cost: 8, requests: 3 },
    highest_request_day: { date: '2026-09-20', label: '09-20', cost: 2, user_cost: 8, requests: 3 },
  },
  models: [],
  endpoints: [],
  upstream_endpoints: [],
}

const account = {
  id: 7,
  name: 'Owned account',
  status: 'active',
} as any

function mountModal() {
  return mount(AccountStatsModal, {
    props: { show: false, account },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        LoadingSpinner: true,
        ModelDistributionChart: true,
        EndpointDistributionChart: true,
        Icon: true,
      },
    },
  })
}

describe('AccountStatsModal account administrator earnings', () => {
  beforeEach(() => {
    authIsAccountAdmin.value = true
    getStatsMock.mockReset().mockResolvedValue(statsResponse)
  })

  it('uses cost fields as earnings and hides user and standard billing details', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getStatsMock).toHaveBeenCalledWith(7, 30)
    expect(wrapper.text()).toContain('admin.accounts.stats.totalEarnings')
    expect(wrapper.text()).toContain('admin.accounts.stats.accumulatedEarnings')
    expect(wrapper.text()).toContain('admin.accounts.stats.earningsTrend')
    expect(wrapper.text()).not.toContain('usage.userBilled')
    expect(wrapper.text()).not.toContain('admin.accounts.stats.standardCost')
    expect(wrapper.get('[data-testid="trend-labels"]').text()).toContain('admin.accounts.stats.earnings (USD)')
    expect(wrapper.get('[data-testid="trend-labels"]').text()).not.toContain('usage.userBilled')
  })
})
