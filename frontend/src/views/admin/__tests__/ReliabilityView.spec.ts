import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ReliabilityView from '../ReliabilityView.vue'

const mocks = vi.hoisted(() => ({
  getStatus: vi.fn(),
}))

vi.mock('@/api/admin/reliability', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/reliability')>()
  return { ...actual, reliabilityAPI: { getStatus: mocks.getStatus } }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'en' },
      t: (key: string) => key === 'admin.reliability.unavailable' ? 'UNAVAILABLE' : key,
    }),
  }
})

const AppLayoutStub = { template: '<main><slot /></main>' }
const AdminPageHeaderStub = { template: '<header><slot name="actions" /></header>' }
const AdminOverviewStripStub = {
  props: ['items'],
  template: '<div data-testid="overview-strip"><span v-for="(item, index) in items" :key="index" :data-testid="`overview-value-${index}`">{{ item.value }}</span></div>',
}
const IconStub = { props: ['name'], template: '<i :data-icon="name" />' }
const RouterLinkStub = { props: ['to'], template: '<a :data-to="to"><slot /></a>' }

function mountView() {
  return mount(ReliabilityView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        AdminPageHeader: AdminPageHeaderStub,
        AdminOverviewStrip: AdminOverviewStripStub,
        Icon: IconStub,
        RouterLink: RouterLinkStub,
      },
    },
  })
}

function setAggregateStatus(overrides: Record<string, unknown> = {}) {
  mocks.getStatus.mockResolvedValue({
    enabled: true,
    timestamp: '2026-09-21T00:00:00Z',
    summary: {
      account_availability: { total: 4, available: 3, cooling_down: 1 },
      traffic: { current_concurrency: 2, error_rate: 1.25 },
      connection: { status: 'unknown' },
      runtime: { ops_enabled: true, error: { available: true, error_rate: 1.25 } },
      diagnostics: { source_errors: [], warnings: [] },
      ...overrides,
    },
  })
}

describe('ReliabilityView', () => {
  beforeEach(() => {
    mocks.getStatus.mockReset()
    setAggregateStatus()
  })

  it('shows unavailable overview metrics when Ops collection is disabled', async () => {
    mocks.getStatus.mockResolvedValue({
      enabled: false,
      timestamp: '2026-09-21T00:00:00Z',
      summary: {
        connection: { status: 'unknown' },
        runtime: { ops_enabled: false, error: { available: false } },
        diagnostics: { source_errors: [], warnings: [] },
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="overview-value-0"]').text()).toBe('UNAVAILABLE')
    expect(wrapper.get('[data-testid="overview-value-1"]').text()).toBe('UNAVAILABLE')
    expect(wrapper.get('[data-testid="overview-value-2"]').text()).toBe('UNAVAILABLE')
    expect(wrapper.get('[data-testid="overview-value-3"]').text()).toBe('UNAVAILABLE')
  })

  it('loads only aggregate status and omits removed sections and navigation actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.getStatus).toHaveBeenCalledOnce()

    const removedTestIds = [
      'fallback-policy',
      'overload-cooldown',
      'rate429-cooldown',
      'stream-unsched-minutes',
      'stream-threshold-count',
      'stream-threshold-window',
      'panel-user-rpm',
      'panel-heavy-rpm',
      'panel-public-rpm',
      'save-overload',
      'save-rate429',
      'save-stream',
      'save-panel',
      'http-dial-timeout',
      'http-tls-timeout',
      'ws-dial-timeout',
      'diagnostics-empty',
    ]
    for (const testId of removedTestIds) {
      expect(wrapper.find(`[data-testid="${testId}"]`).exists()).toBe(false)
    }

    for (const destination of ['/admin/settings', '/admin/groups', '/admin/ops', '/admin/accounts', '/admin/proxies']) {
      expect(wrapper.find(`[data-to="${destination}"]`).exists()).toBe(false)
    }

    expect(wrapper.text()).not.toContain('admin.reliability.fallback.title')
    expect(wrapper.text()).not.toContain('admin.reliability.limits.title')
    expect(wrapper.text()).not.toContain('admin.reliability.diagnostics.title')
  })

  it('shows HTTP and WebSocket cross-account turn-state protection separately', async () => {
    setAggregateStatus({
      turn_state: {
        supported: true,
        http_enabled: true,
        websocket_enabled: true,
        cross_account_protection: false,
        http_cross_account_protection: true,
        websocket_cross_account_protection: false,
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-http-protection"]').text()).toContain('admin.reliability.turnState.protectedValue')
    expect(wrapper.get('[data-testid="turn-state-websocket-protection"]').text()).toContain('admin.reliability.turnState.unprotectedValue')
  })

  it('shows aggregate collector health without rendering an opaque state value', async () => {
    setAggregateStatus({
      turn_state: {
        supported: true,
        http_enabled: true,
        websocket_enabled: true,
        collector: {
          enabled: true,
          injection_enabled: true,
          status: 'ready',
          ready: true,
          collecting: false,
          active_entries: 3,
          ready_candidates: 1,
          observations: 12,
          successes: 8,
          failures: 2,
          last_success_at: '2026-09-21T02:00:00Z',
          last_failure_at: '2026-09-21T02:01:00Z',
          last_error_code: 'probe_timeout',
        },
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-collector"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="turn-state-collector-status"]').text()).toContain('admin.reliability.turnState.collectorStatus.ready')
    expect(wrapper.get('[data-testid="turn-state-collector-injection"]').text()).toContain('admin.reliability.turnState.collectorInjectionEnabled')
    expect(wrapper.get('[data-testid="turn-state-collector-last-success"]').text()).not.toBe('UNAVAILABLE')
    expect(wrapper.get('[data-testid="turn-state-collector-last-failure"]').text()).not.toBe('UNAVAILABLE')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('admin.reliability.turnState.collectorErrors.probe_timeout')
    expect(wrapper.text()).not.toContain('X-Codex-Turn-State')
  })

  it.each(['cooldown', 'degraded'])('shows the %s collector state with injection disabled', async (collectorStatus) => {
    setAggregateStatus({
      turn_state: {
        supported: true,
        collector: {
          enabled: true,
          injection_enabled: false,
          status: collectorStatus,
          ready: false,
          collecting: false,
        },
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-collector-status"]').text()).toContain(`admin.reliability.turnState.collectorStatus.${collectorStatus}`)
    expect(wrapper.get('[data-testid="turn-state-collector-injection"]').text()).toContain('admin.reliability.turnState.collectorInjectionDisabled')
  })

  it('does not render unknown collector text or identity-bearing response fields', async () => {
    const sensitiveValues = ['opaque-state-value', 'account-42', 'scope-secret', 'route-secret', 'proxy-secret', 'upstream free-form error']
    setAggregateStatus({
      turn_state: {
        supported: true,
        collector: {
          enabled: true,
          injection_enabled: true,
          status: sensitiveValues[5],
          last_error_code: sensitiveValues[5],
          state: sensitiveValues[0],
          account: sensitiveValues[1],
          scope: sensitiveValues[2],
          route: sensitiveValues[3],
          proxy: sensitiveValues[4],
        },
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-collector-status"]').text()).toContain('admin.reliability.turnState.collectorUnknown')
    expect(wrapper.get('[data-testid="turn-state-collector-last-error"]').text()).toContain('admin.reliability.turnState.collectorUnknown')
    for (const value of sensitiveValues) expect(wrapper.text()).not.toContain(value)
  })

  it('reloads aggregate status from the refresh action', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[title="admin.reliability.refresh"]').trigger('click')
    await flushPromises()

    expect(mocks.getStatus).toHaveBeenCalledTimes(2)
  })
})
