import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ReliabilityView from '../ReliabilityView.vue'

const mocks = vi.hoisted(() => ({
  getStatus: vi.fn(),
  getTurnStateSettings: vi.fn(),
  updateTurnStateSettings: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin/reliability', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/api/admin/reliability')>()
  return {
    ...actual,
    reliabilityAPI: {
      getStatus: mocks.getStatus,
      getTurnStateSettings: mocks.getTurnStateSettings,
      updateTurnStateSettings: mocks.updateTurnStateSettings,
    },
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess: mocks.showSuccess,
    showError: mocks.showError,
  }),
}))

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
const IconStub = { props: ['name'], template: '<i :data-icon="name" />' }
const RouterLinkStub = { props: ['to'], template: '<a :data-to="to"><slot /></a>' }

function mountView() {
  return mount(ReliabilityView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        AdminPageHeader: AdminPageHeaderStub,
        Icon: IconStub,
        RouterLink: RouterLinkStub,
      },
    },
  })
}

function setTurnStateStatus(overrides: Record<string, unknown> = {}) {
  mocks.getStatus.mockResolvedValue({
    turn_state: {
      supported: true,
      http_enabled: true,
      ...overrides,
    },
  })
}

describe('ReliabilityView', () => {
  beforeEach(() => {
    mocks.getStatus.mockReset()
    mocks.getTurnStateSettings.mockReset()
    mocks.updateTurnStateSettings.mockReset()
    mocks.showSuccess.mockReset()
    mocks.showError.mockReset()
    setTurnStateStatus()
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
    })
    mocks.updateTurnStateSettings.mockImplementation(async (settings) => settings)
  })

  it('does not render the removed overview card or metric strip', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('[data-testid="overview-strip"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('admin.reliability.overview.title')
    expect(wrapper.text()).not.toContain('admin.reliability.overview.connection')
    expect(wrapper.text()).not.toContain('admin.reliability.overview.availableAccounts')
    expect(wrapper.text()).not.toContain('admin.reliability.overview.limitedAccounts')
    expect(wrapper.text()).not.toContain('admin.reliability.overview.concurrency')
    expect(wrapper.text()).not.toContain('admin.reliability.overview.errorRate')
    expect(wrapper.get('[data-testid="turn-state-status"]').exists()).toBe(true)
  })

  it('loads only turn-state status and omits removed sections and navigation actions', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(mocks.getStatus).toHaveBeenCalledOnce()
    expect(mocks.getTurnStateSettings).toHaveBeenCalledOnce()

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
    setTurnStateStatus({
      supported: true,
      http_enabled: true,
      websocket_enabled: true,
      cross_account_protection: false,
      http_cross_account_protection: true,
      websocket_cross_account_protection: false,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-http-protection"]').text()).toContain('admin.reliability.turnState.protectedValue')
    expect(wrapper.get('[data-testid="turn-state-websocket-protection"]').text()).toContain('admin.reliability.turnState.unprotectedValue')
  })

  it('shows aggregate collector health without rendering an opaque state value', async () => {
    setTurnStateStatus({
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
    setTurnStateStatus({
      supported: true,
      collector: {
        enabled: true,
        injection_enabled: false,
        status: collectorStatus,
        ready: false,
        collecting: false,
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-collector-status"]').text()).toContain(`admin.reliability.turnState.collectorStatus.${collectorStatus}`)
    expect(wrapper.get('[data-testid="turn-state-collector-injection"]').text()).toContain('admin.reliability.turnState.collectorInjectionDisabled')
  })

  it('does not render unknown collector text or identity-bearing response fields', async () => {
    const sensitiveValues = ['opaque-state-value', 'account-42', 'scope-secret', 'route-secret', 'proxy-secret', 'upstream free-form error']
    setTurnStateStatus({
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
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-collector-status"]').text()).toContain('admin.reliability.turnState.collectorUnknown')
    expect(wrapper.get('[data-testid="turn-state-collector-last-error"]').text()).toContain('admin.reliability.turnState.collectorUnknown')
    for (const value of sensitiveValues) expect(wrapper.text()).not.toContain(value)
  })

  it('reloads turn-state status from the refresh action', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[title="admin.reliability.refresh"]').trigger('click')
    await flushPromises()

    expect(mocks.getStatus).toHaveBeenCalledTimes(2)
    expect(mocks.getTurnStateSettings).toHaveBeenCalledTimes(2)
  })

  it('reports a status failure without falling back to removed metric APIs', async () => {
    mocks.getStatus.mockRejectedValueOnce(new Error('status rejected'))

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[role="alert"]').text()).toContain('admin.reliability.loadFailed')
    expect(wrapper.get('[data-testid="turn-state-http-protection"]').text()).toContain('admin.reliability.turnState.unknownValue')
    expect(mocks.getTurnStateSettings).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-testid="turn-state-probe-toggle"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="turn-state-injection-toggle"]').attributes('disabled')).toBeUndefined()

    setTurnStateStatus({ http_cross_account_protection: true })
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()

    expect(mocks.getStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="turn-state-http-protection"]').text()).toContain('admin.reliability.turnState.protectedValue')
  })

  it('defaults missing turn-state settings to enabled', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({})

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-probe-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="turn-state-injection-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="turn-state-probe-toggle"]').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('[data-testid="turn-state-injection-toggle"]').attributes('disabled')).toBeUndefined()
  })

  it('loads and persists the probe and injection switches independently', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: false,
      injection_enabled: true,
    })

    const wrapper = mountView()
    await flushPromises()

    const probeToggle = wrapper.get('[data-testid="turn-state-probe-toggle"]')
    const injectionToggle = wrapper.get('[data-testid="turn-state-injection-toggle"]')
    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(injectionToggle.attributes('aria-checked')).toBe('true')

    await probeToggle.trigger('click')
    await flushPromises()
    expect(mocks.updateTurnStateSettings).toHaveBeenNthCalledWith(1, {
      probe_enabled: true,
      injection_enabled: true,
    })

    await injectionToggle.trigger('click')
    await flushPromises()
    expect(mocks.updateTurnStateSettings).toHaveBeenNthCalledWith(2, {
      probe_enabled: true,
      injection_enabled: false,
    })
    expect(mocks.showSuccess).toHaveBeenCalledTimes(2)
  })

  it('rolls a switch back when persistence fails', async () => {
    mocks.updateTurnStateSettings.mockRejectedValue(new Error('save rejected'))

    const wrapper = mountView()
    await flushPromises()

    const probeToggle = wrapper.get('[data-testid="turn-state-probe-toggle"]')
    expect(probeToggle.attributes('aria-checked')).toBe('true')

    await probeToggle.trigger('click')
    await flushPromises()

    expect(probeToggle.attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-testid="turn-state-settings-error"]').text()).toContain('save rejected')
    expect(mocks.showError).toHaveBeenCalledWith('save rejected')
  })

  it('keeps default-on controls disabled until a failed settings load is retried', async () => {
    mocks.getTurnStateSettings.mockRejectedValueOnce(new Error('load rejected'))

    const wrapper = mountView()
    await flushPromises()

    const probeToggle = wrapper.get('[data-testid="turn-state-probe-toggle"]')
    const injectionToggle = wrapper.get('[data-testid="turn-state-injection-toggle"]')
    expect(probeToggle.attributes('aria-checked')).toBe('true')
    expect(injectionToggle.attributes('aria-checked')).toBe('true')
    expect(probeToggle.attributes('disabled')).toBeDefined()
    expect(injectionToggle.attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="turn-state-settings-error"]').text()).toContain('load rejected')

    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: false,
      injection_enabled: false,
    })
    await wrapper.get('[data-testid="turn-state-settings-error"] button').trigger('click')
    await flushPromises()

    expect(probeToggle.attributes('aria-checked')).toBe('false')
    expect(injectionToggle.attributes('aria-checked')).toBe('false')
    expect(probeToggle.attributes('disabled')).toBeUndefined()
    expect(injectionToggle.attributes('disabled')).toBeUndefined()
  })
})
