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
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'admin.reliability.unavailable') return 'UNAVAILABLE'
        if (key === 'admin.reliability.turnState.successfulIPTotal') return `${params?.count} egress observations`
        return key
      },
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

  it('preserves an unsaved proxy pool draft when settings refresh', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('[data-testid="turn-state-proxy-pool-input"]')
    const draft = 'https://draft-user:draft-password@proxy.example.com:443'
    await input.setValue(draft)
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: false,
      injection_enabled: true,
      proxy_pool_configured: true,
      proxy_pool_urls: ['https://legacy-user:legacy-password@old.example.com:443'],
    })

    await wrapper.get('[title="admin.reliability.refresh"]').trigger('click')
    await flushPromises()

    expect(input.element.value).toBe(draft)
    expect(wrapper.get('[data-testid="turn-state-probe-toggle"]').attributes('aria-checked')).toBe('false')
    expect(mocks.updateTurnStateSettings).not.toHaveBeenCalled()
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

  it('edits a batch proxy pool and renders collection IP diagnostics', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      proxy_pool_configured: true,
      proxy_pool_count: 2,
    })
    setTurnStateStatus({
      collector: {
        enabled: true,
        status: 'ready',
        proxy_pool: [
          { protocol: 'http', host: 'proxy.example.com', port: 8080 },
          { protocol: 'socks5', host: '10.0.0.8', port: 1080 },
        ],
        successful_ip_regions: [
          { region: '北美', count: 4 },
          { region: '欧洲', count: 2 },
        ],
        successful_ips: [
          { ip: '198.51.100.8', region: '北美', successes: 3, last_success_at: '2026-09-21T02:00:00Z' },
        ],
        candidate_breakdown: [
          { reason: 'account_identity_changed', count: 1 },
          { reason: 'account_unschedulable', count: 5 },
          { reason: 'response_model_mismatch', count: 2 },
        ],
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-proxy-pool-input"]').element.value).toBe('')
    expect(wrapper.get('[data-testid="turn-state-proxy-pool-settings"]').text()).toContain('proxyPoolConfiguredHint')
    expect(wrapper.get('[data-testid="turn-state-proxy-pool"]').text()).toContain('proxy.example.com')
    expect(wrapper.get('[data-testid="turn-state-ip-regions"]').text()).toContain('北美')
    expect(wrapper.get('[data-testid="turn-state-successful-ips"]').text()).toContain('198.51.100.8')
    expect(wrapper.get('[data-testid="turn-state-successful-ips"] table').classes()).toEqual(
      expect.arrayContaining(['w-full', 'min-w-[42rem]']),
    )
    expect(wrapper.get('[data-testid="turn-state-candidate-breakdown"]').text()).toContain('admin.reliability.turnState.candidateReasons.account_identity_changed')
    expect(wrapper.get('[data-testid="turn-state-candidate-breakdown"]').text()).toContain('admin.reliability.turnState.candidateReasons.account_unschedulable')

    const input = wrapper.get('[data-testid="turn-state-proxy-pool-input"]')
    await input.setValue('https://proxy.example.com:443\nhttps://proxy.example.com:443\nsocks5://proxy.example.net:1080')
    await wrapper.get('[data-testid="turn-state-proxy-pool-save"]').trigger('click')
    await flushPromises()

    expect(mocks.updateTurnStateSettings).toHaveBeenCalledWith({
      probe_enabled: true,
      injection_enabled: true,
      proxy_pool_urls: ['https://proxy.example.com:443', 'socks5://proxy.example.net:1080'],
    })
    expect(input.element.value).toBe('')
  })

  it('clears saved proxy credentials and still allows clearing the configured pool', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('[data-testid="turn-state-proxy-pool-input"]')
    await input.setValue('socks5://saved-user:saved-password@proxy.example.com:1080')
    await wrapper.get('[data-testid="turn-state-proxy-pool-save"]').trigger('click')
    await flushPromises()

    expect(input.element.value).toBe('')
    expect(wrapper.get('[data-testid="turn-state-proxy-pool-settings"]').text()).toContain('proxyPoolConfiguredHint')

    await wrapper.get('[data-testid="turn-state-proxy-pool-save"]').trigger('click')
    await flushPromises()

    expect(mocks.updateTurnStateSettings).toHaveBeenLastCalledWith({
      probe_enabled: true,
      injection_enabled: true,
      proxy_pool_urls: [],
    })
    expect(wrapper.get('[data-testid="turn-state-proxy-pool-settings"]').text()).not.toContain('proxyPoolConfiguredHint')
  })

  it('retains the proxy pool draft when saving fails', async () => {
    mocks.updateTurnStateSettings.mockRejectedValueOnce(new Error('save rejected'))
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('[data-testid="turn-state-proxy-pool-input"]')
    const draft = 'https://draft-user:draft-password@proxy.example.com:443'
    await input.setValue(draft)

    await wrapper.get('[data-testid="turn-state-proxy-pool-save"]').trigger('click')
    await flushPromises()

    expect(input.element.value).toBe(draft)
    expect(wrapper.get('[data-testid="turn-state-proxy-pool-error"]').text()).toContain('save rejected')
  })

  it.each([
    {
      name: 'includes IP observations without region metadata without double counting the regional observations',
      ips: [{ ip: '198.51.100.8', region: 'North America', successes: 3 }, { ip: '198.51.100.9', successes: 5 }],
      total: 8,
    },
    { name: 'falls back to regional observations when individual IPs are absent', ips: [], total: 3 },
  ])('$name', async ({ ips, total }) => {
    setTurnStateStatus({
      collector: {
        enabled: true,
        successful_ip_regions: [{ region: 'North America', successes: 3 }],
        successful_ips: ips,
      },
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-ip-observations-total"]').text()).toBe(`${total} egress observations`)
    expect(wrapper.get('[data-testid="turn-state-successful-ips"]').text()).toContain('ipObservationsHint')
  })

  it('blocks malformed proxy pool entries before persistence', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      proxy_pool_urls: [],
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="turn-state-proxy-pool-input"]').setValue('ftp://proxy.example.com:21')

    const save = wrapper.get('[data-testid="turn-state-proxy-pool-save"]')
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')
    await flushPromises()

    expect(mocks.updateTurnStateSettings).not.toHaveBeenCalled()
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
