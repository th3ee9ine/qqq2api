import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ReliabilityView from '../ReliabilityView.vue'

const mocks = vi.hoisted(() => ({
  getStatus: vi.fn(),
  getTurnStateSettings: vi.fn(),
  startTurnStateHarvest: vi.fn(),
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
      startTurnStateHarvest: mocks.startTurnStateHarvest,
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
    mocks.startTurnStateHarvest.mockReset()
    mocks.updateTurnStateSettings.mockReset()
    mocks.showSuccess.mockReset()
    mocks.showError.mockReset()
    setTurnStateStatus()
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      speed_preset: 'standard',
      max_requests_per_round: 4,
      failure_cooldown_seconds: 120,
      presets: ['slow', 'standard', 'fast', 'burst'],
      bounds: {
        max_requests_per_round: { min: 1, max: 20, step: 1 },
        failure_cooldown_seconds: { min: 10, max: 3600, step: 10 },
      },
    })
    mocks.startTurnStateHarvest.mockResolvedValue({ accepted: true, message: 'collected' })
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
      speed_preset: 'standard',
      max_requests_per_round: 4,
      failure_cooldown_seconds: 120,
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

  it('does not save or discard an unsaved collection policy when toggling a switch', async () => {
    const wrapper = mountView()
    await flushPromises()
    const maxRequests = wrapper.get('[data-testid="turn-state-max-requests"]')
    await maxRequests.setValue('999')
    expect(wrapper.get('[data-testid="turn-state-harvest-policy-save"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="turn-state-probe-toggle"]').trigger('click')
    await flushPromises()

    expect(mocks.updateTurnStateSettings).toHaveBeenCalledWith({
      probe_enabled: false,
      injection_enabled: true,
    })
    expect((maxRequests.element as HTMLInputElement).value).toBe('999')
    expect(wrapper.get('[data-testid="turn-state-probe-toggle"]').attributes('aria-checked')).toBe('false')
  })

  it('uses advertised presets and bounds when saving collection pace', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      speed_preset: 'slow',
      max_requests_per_round: 2,
      failure_cooldown_seconds: 300,
      presets: {
        slow: { max_requests_per_round: 2, failure_cooldown_seconds: 300 },
        fast: { max_requests_per_round: 10, failure_cooldown_seconds: 60 },
      },
      bounds: {
        max_requests_per_round: { min: 2, max: 12, step: 2 },
        failure_cooldown_seconds: { min: 30, max: 900, step: 30 },
      },
    })

    const wrapper = mountView()
    await flushPromises()

    const presetButtons = wrapper.get('[data-testid="turn-state-speed-preset"]').findAll('button')
    expect(presetButtons).toHaveLength(2)
    expect(presetButtons[0].attributes('aria-pressed')).toBe('true')
    const maxRequests = wrapper.get('[data-testid="turn-state-max-requests"]')
    const cooldown = wrapper.get('[data-testid="turn-state-failure-cooldown"]')
    expect(maxRequests.attributes()).toMatchObject({ min: '2', max: '12', step: '2' })
    expect(cooldown.attributes()).toMatchObject({ min: '30', max: '900', step: '30' })

    await presetButtons[1].trigger('click')
    expect((maxRequests.element as HTMLInputElement).value).toBe('10')
    expect((cooldown.element as HTMLInputElement).value).toBe('60')
    await maxRequests.setValue('8')
    await cooldown.setValue('180')
    await wrapper.get('[data-testid="turn-state-harvest-policy-save"]').trigger('click')
    await flushPromises()

    expect(mocks.updateTurnStateSettings).toHaveBeenCalledWith({
      probe_enabled: true,
      injection_enabled: true,
      speed_preset: 'fast',
      max_requests_per_round: 8,
      failure_cooldown_seconds: 180,
    })

    await maxRequests.setValue('13')
    expect(wrapper.get('[data-testid="turn-state-harvest-policy-save"]').attributes('disabled')).toBeDefined()
  })

  it('uses the matching backend defaults when presets are returned as names', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      speed_preset: 'standard',
      max_requests_per_round: 6,
      failure_cooldown_seconds: 180,
      presets: ['slow', 'standard', 'fast', 'burst'],
      bounds: {
        max_requests_per_round: { min: 1, max: 100, step: 1 },
        failure_cooldown_seconds: { min: 1, max: 3600, step: 1 },
      },
    })
    const wrapper = mountView()
    await flushPromises()
    const buttons = wrapper.get('[data-testid="turn-state-speed-preset"]').findAll('button')
    await buttons[3].trigger('click')
    expect((wrapper.get('[data-testid="turn-state-max-requests"]').element as HTMLInputElement).value).toBe('20')
    expect((wrapper.get('[data-testid="turn-state-failure-cooldown"]').element as HTMLInputElement).value).toBe('1')
  })

  it('starts a targeted collection without rendering the backend message', async () => {
    mocks.startTurnStateHarvest.mockResolvedValue({
      accepted: true,
      message: 'queued via https://proxy-user:proxy-password@proxy.example.com',
    })
    const wrapper = mountView()
    await flushPromises()

    const submit = wrapper.get('[data-testid="turn-state-harvest-submit"]')
    expect(submit.attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="turn-state-harvest-account"]').setValue('42')
    await wrapper.get('[data-testid="turn-state-harvest-model"]').setValue('  gpt-5.6-sol  ')
    await submit.trigger('submit')
    await flushPromises()

    expect(mocks.startTurnStateHarvest).toHaveBeenCalledWith({ account_id: 42, model: 'gpt-5.6-sol' })
    expect(wrapper.get('[data-testid="turn-state-harvest-feedback"]').text()).toContain('harvestAccepted')
    expect(wrapper.text()).not.toContain('proxy-user')
    expect(wrapper.text()).not.toContain('proxy-password')
    expect(mocks.getStatus).toHaveBeenCalledTimes(2)
  })

  it('uses fixed safe feedback when a collection request is rejected', async () => {
    mocks.startTurnStateHarvest.mockResolvedValue({
      accepted: false,
      message: 'cookie_name=session; cookie_value=secret; turn-state=opaque',
    })
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="turn-state-harvest-account"]').setValue('7')
    await wrapper.get('[data-testid="turn-state-harvest-model"]').setValue('gpt-5')
    await wrapper.get('[data-testid="turn-state-harvest-submit"]').trigger('submit')
    await flushPromises()

    const feedback = wrapper.get('[data-testid="turn-state-harvest-feedback"]')
    expect(feedback.attributes('role')).toBe('alert')
    expect(feedback.text()).toContain('harvestRejected')
    expect(wrapper.text()).not.toContain('cookie_name')
    expect(wrapper.text()).not.toContain('cookie_value')
    expect(wrapper.text()).not.toContain('turn-state=opaque')
  })

  it('renders aggregate cookie and node health without sensitive node fields', async () => {
    setTurnStateStatus({
      collector: {
        enabled: true,
        status: 'ready',
        cookie_count: 8,
        cookie_active_count: 6,
        cookie_expired_count: 2,
        cookie_remaining_seconds: 3665,
        budget_used: 3,
        budget_limit: 12,
        budget_reset_at: '2026-09-22T02:00:00Z',
        cookie_name: 'session-secret-name',
        cookie_value: 'session-secret-value',
        turn_state: 'opaque-turn-state',
        nodes: [{
          node_id: 'node-1',
          label: 'https://proxy-user:proxy-password@proxy.example.com:443',
          successes: 12,
          failures: 3,
          consecutive_failures: 1,
          cooldown_remaining_seconds: 75,
          last_result: 'success',
          proxy_url: 'https://raw-user:raw-password@proxy.example.com:443',
          cookie: 'raw-cookie',
        }],
      },
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="turn-state-cookie-count"]').text()).toBe('8')
    expect(wrapper.get('[data-testid="turn-state-budget"]').text()).toBe('3 / 12')
    expect(wrapper.get('[data-testid="turn-state-budget-reset"]').text()).not.toBe('UNAVAILABLE')
    expect(wrapper.get('[data-testid="turn-state-cookie-active-count"]').text()).toBe('6')
    expect(wrapper.get('[data-testid="turn-state-cookie-expired-count"]').text()).toBe('2')
    expect(wrapper.get('[data-testid="turn-state-cookie-remaining"]').text()).toContain('durationHoursMinutes')
    const nodes = wrapper.get('[data-testid="turn-state-collector-nodes"]')
    expect(nodes.text()).toContain('https://***@proxy.example.com:443')
    expect(nodes.text()).toContain('collectorNodeResults.success')
    for (const secret of [
      'proxy-user', 'proxy-password', 'raw-user', 'raw-password', 'raw-cookie',
      'session-secret-name', 'session-secret-value', 'opaque-turn-state',
    ]) expect(wrapper.text()).not.toContain(secret)
  })

  it('renders only local labels for known and unrecognized node results', async () => {
    setTurnStateStatus({
      collector: {
        enabled: true,
        status: 'ready',
        nodes: [
          { node_id: 'node-1', label: 'proxy.example', last_result: 'transport_error' },
          { node_id: 'node-2', label: 'proxy2.example', last_result: 'private-failure-detail' },
        ],
      },
    })
    const wrapper = mountView()
    await flushPromises()

    const nodes = wrapper.get('[data-testid="turn-state-collector-nodes"]').text()
    expect(nodes).toContain('collectorErrors.transport_error')
    expect(nodes).toContain('collectorUnknown')
    expect(nodes).not.toContain('private-failure-detail')
  })

  it('edits a batch proxy pool and renders collection IP diagnostics', async () => {
    mocks.getTurnStateSettings.mockResolvedValue({
      probe_enabled: true,
      injection_enabled: true,
      speed_preset: 'standard',
      max_requests_per_round: 4,
      failure_cooldown_seconds: 120,
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
          { reason: 'request_budget_exhausted', count: 3 },
          { reason: 'routes_cooling_down', count: 4 },
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
    expect(wrapper.get('[data-testid="turn-state-candidate-breakdown"]').text()).toContain('admin.reliability.turnState.candidateReasons.request_budget_exhausted')
    expect(wrapper.get('[data-testid="turn-state-candidate-breakdown"]').text()).toContain('admin.reliability.turnState.candidateReasons.routes_cooling_down')

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
