import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  probeUpstreamBillingMock,
  importCodexSessionMock,
  createOpenAICodexPATMock,
  exchangeCodeMock,
  listTLSProfilesMock,
  getWebSearchEmulationConfigMock,
  oauthFlowResetMock,
  authIsSimpleMode,
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  probeUpstreamBillingMock: vi.fn(),
  importCodexSessionMock: vi.fn(),
  createOpenAICodexPATMock: vi.fn(),
  exchangeCodeMock: vi.fn(),
  listTLSProfilesMock: vi.fn(),
  getWebSearchEmulationConfigMock: vi.fn(),
  oauthFlowResetMock: vi.fn(),
  authIsSimpleMode: { value: true },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    get isSimpleMode() {
      return authIsSimpleMode.value
    },
  }),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAccountMock,
      probeUpstreamBilling: probeUpstreamBillingMock,
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false }),
      importCodexSession: importCodexSessionMock,
      createOpenAICodexPAT: createOpenAICodexPATMock,
      exchangeCode: exchangeCodeMock,
    },
    settings: {
      getWebSearchEmulationConfig: getWebSearchEmulationConfigMock,
      getSettings: vi.fn().mockResolvedValue({}),
    },
    tlsFingerprintProfiles: {
      list: listTLSProfilesMock,
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

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  props: {
    showManualOption: Boolean,
    showCodexSessionImportOption: Boolean,
    showAgentIdentityOption: Boolean,
    showCodexPatOption: Boolean,
    initialInputMethod: String,
  },
  data: () => ({ inputMethod: 'manual' }),
  emits: ['cookie-auth', 'import-codex-session', 'import-codex-pat'],
  methods: {
    reset() {
      oauthFlowResetMock()
    },
  },
  template: `
    <div>
      <button data-testid="cookie-auth" @click="$emit('cookie-auth', 'session-key')">cookie</button>
      <button data-testid="import-codex-session" @click="$emit('import-codex-session', 'session-json')">session</button>
      <button data-testid="import-codex-pat" @click="$emit('import-codex-pat', 'pat-token')">pat</button>
    </div>
  `,
})

const GroupSelectorStub = defineComponent({
  name: 'GroupSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  template: `
    <button
      type="button"
      data-testid="select-pricing-groups"
      @click="$emit('update:modelValue', [1, 2])"
    >
      groups
    </button>
  `,
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
    platform: String,
    syncCredentials: Object,
  },
  emits: ['update:modelValue'],
  template: '<button type="button" data-testid="model-whitelist-selector" @click="$emit(\'update:modelValue\', [\'gpt-5.4\'])">models</button>',
})

function mountModal(groups: any[] = []) {
  return mount(CreateAccountModal, {
    props: { show: true, proxies: [], groups },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        PlatformIcon: true,
        ProxySelector: true,
        ProxyAdBanner: true,
        GroupSelector: GroupSelectorStub,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
      },
    },
  })
}

async function selectButtonByText(wrapper: ReturnType<typeof mountModal>, text: string) {
  const button = wrapper.findAll('button').find((candidate) => candidate.text().includes(text))
  expect(button).toBeDefined()
  await button?.trigger('click')
}

async function submitApiKeyAccount(
  platform: 'openai' | 'anthropic',
  enableLongContextBilling = false,
  disableUpstreamBillingProbe = false
) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, platform === 'openai' ? 'OpenAI' : 'admin.accounts.claudeConsole')
  if (platform === 'openai') {
    await selectButtonByText(wrapper, 'API Key')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue(`${platform} account`)
  await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
  if (enableLongContextBilling) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  if (disableUpstreamBillingProbe) {
    await wrapper.get('[data-testid="upstream-billing-auto-probe"]').trigger('click')
  }
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

async function openCodexImportStep(toggleClicks = 0) {
  const wrapper = mountModal()
  await selectButtonByText(wrapper, 'OpenAI')
  for (let click = 0; click < toggleClicks; click += 1) {
    await wrapper.get('[data-testid="openai-long-context-billing-toggle"]').trigger('click')
  }
  await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
  await wrapper.get('form#create-account-form').trigger('submit.prevent')
  return wrapper
}

describe('CreateAccountModal OpenAI long-context billing', () => {
  beforeEach(() => {
    authIsSimpleMode.value = true
    createAccountMock.mockReset().mockResolvedValue({ id: 42, platform: 'openai', type: 'apikey' })
    probeUpstreamBillingMock.mockReset().mockResolvedValue({})
    importCodexSessionMock.mockReset().mockResolvedValue({
      created: 1,
      updated: 0,
      skipped: 0,
      failed: 0,
      errors: [],
      warnings: [],
    })
    createOpenAICodexPATMock.mockReset().mockResolvedValue({})
    exchangeCodeMock.mockReset().mockResolvedValue({ access_token: 'anthropic-token' })
    listTLSProfilesMock.mockReset().mockResolvedValue([{ id: 7, name: 'Chrome profile' }])
    getWebSearchEmulationConfigMock.mockReset().mockResolvedValue({ enabled: false, providers: [] })
    oauthFlowResetMock.mockReset()
  })

  it('renders the retained pre-simplification platform and account-type layout', async () => {
    const wrapper = mountModal()
    const platformSelector = wrapper.get('[data-tour="account-form-platform"]')
    expect(platformSelector.classes()).toContain('flex-wrap')
    expect(platformSelector.findAll('button')).toHaveLength(2)
    expect(platformSelector.text()).toContain('Anthropic')
    expect(platformSelector.text()).toContain('OpenAI')

    const anthropicTypes = wrapper.get('[data-tour="account-form-type"]')
    expect(anthropicTypes.classes()).toContain('sm:grid-cols-4')
    expect(anthropicTypes.findAll('button')).toHaveLength(4)
    expect(wrapper.find('[data-testid="account-advanced-settings"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-account-step-indicator"]').exists()).toBe(true)

    await wrapper.get('[data-testid="create-platform-openai"]').trigger('click')
    const openAITypes = wrapper.get('[data-tour="account-form-type"]')
    expect(openAITypes.findAll('button')).toHaveLength(2)
  })

  it('keeps OpenAI-only quota pause controls out of retained Anthropic account forms', async () => {
    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="create-openai-auto-pause-section"]').exists()).toBe(false)

    await wrapper.get('[data-testid="create-platform-openai"]').trigger('click')

    const section = wrapper.get('[data-testid="create-openai-auto-pause-section"]')
    expect(section.classes()).toEqual(
      expect.arrayContaining(['border-t', 'dark:border-dark-600', 'space-y-4'])
    )

    const toggle = wrapper.get('[data-testid="create-auto-pause-5h-disabled"]')
    const threshold = wrapper.get('[data-testid="create-auto-pause-5h-threshold"]')
    expect(toggle.element.tagName).toBe('BUTTON')
    expect(toggle.classes()).toContain('dark:bg-dark-600')
    expect(toggle.get('span').classes()).toContain('translate-x-0')
    expect(threshold.attributes('disabled')).toBeUndefined()

    await toggle.trigger('click')

    expect(toggle.classes()).toContain('bg-primary-600')
    expect(toggle.get('span').classes()).toContain('translate-x-5')
    expect(threshold.attributes('disabled')).toBeDefined()
  })

  it('preserves the responsive and dark-mode Codex image policy cards', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="create-platform-openai"]').trigger('click')

    const inheritCard = wrapper.get('[data-testid="create-codex-image-tool-inherit"]')
    expect(inheritCard.classes()).toEqual(
      expect.arrayContaining(['min-h-[62px]', 'dark:bg-sky-900/25', 'dark:text-sky-100'])
    )
    expect(inheritCard.element.parentElement?.classList).toContain('sm:grid-cols-2')

    const enabledCard = wrapper.get('[data-testid="create-codex-image-tool-enabled"]')
    await enabledCard.trigger('click')

    expect(enabledCard.classes()).toEqual(
      expect.arrayContaining(['dark:bg-emerald-900/25', 'dark:text-emerald-100'])
    )
    expect(wrapper.text()).toContain('admin.accounts.openai.codexImageToolBadgeEnabled')
  })

  it('resets the authorization flow when returning to basic information', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="create-platform-openai"]').trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI OAuth')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')

    expect(wrapper.find('form#create-account-form').exists()).toBe(false)
    const backButton = wrapper.findAll('button').find((button) => button.text() === 'common.back')
    expect(backButton).toBeDefined()
    await backButton?.trigger('click')

    expect(oauthFlowResetMock).toHaveBeenCalledTimes(1)
    expect(wrapper.find('form#create-account-form').exists()).toBe(true)
  })

  it('hides only the redundant account toggle when every selected group enables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: true },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('keeps the account toggle when any selected group disables tier pricing', async () => {
    authIsSimpleMode.value = false
    const wrapper = mountModal([
      { id: 1, long_context_pricing_enabled: true },
      { id: 2, long_context_pricing_enabled: false },
    ])

    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="select-pricing-groups"]').trigger('click')

    expect(wrapper.find('[data-testid="openai-long-context-billing-toggle"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="create-openai-ws-mode"]').exists()).toBe(true)
  })

  it('sends false explicitly for normal OpenAI account creation by default', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('requests automatic assignment to an available proxy when selected', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Auto proxy account')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('test-api-key')
    await wrapper.get('[data-testid="create-auto-assign-proxy"]').setValue(true)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({ auto_assign_proxy: true })
    expect(createAccountMock.mock.calls[0]?.[0]?.proxy_id).toBeUndefined()
  })

  // namespace 摊平是仅 OAuth 的兼容开关：API Key 走 chat completions 回退桥时由桥自行摊平
  it('shows the Codex namespace flatten toggle only for OpenAI OAuth accounts', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')

    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      true
    )

    await selectButtonByText(wrapper, 'API Key')
    expect(wrapper.find('[data-testid="create-openai-flatten-namespaces-toggle"]').exists()).toBe(
      false
    )
  })

  it('enables upstream billing probes by default for new OpenAI API key accounts', async () => {
    await submitApiKeyAccount('openai')

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('waits for the initial upstream billing probe before refreshing the account list', async () => {
    let resolveProbe: (() => void) | undefined
    probeUpstreamBillingMock.mockImplementationOnce(
      () => new Promise<void>((resolve) => {
        resolveProbe = resolve
      })
    )

    const wrapper = await submitApiKeyAccount('openai')

    expect(probeUpstreamBillingMock).toHaveBeenCalledWith(42)
    expect(wrapper.emitted('created')).toBeUndefined()

    resolveProbe?.()
    await flushPromises()

    expect(wrapper.emitted('created')).toHaveLength(1)
  })

  it('sends an explicit disabled state when the create toggle is turned off', async () => {
    await submitApiKeyAccount('openai', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
    expect(probeUpstreamBillingMock).not.toHaveBeenCalled()
  })

  it('submits the administrator-selected billing rate multiplier', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI key')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-test')
    await wrapper.get('[data-testid="create-rate-multiplier"]').setValue('1.25')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.rate_multiplier).toBe(1.25)
  })

  it('serializes the Bedrock force-global setting on creation', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="create-account-type-bedrock"]').trigger('click')
    await selectButtonByText(wrapper, 'admin.accounts.modelMapping')
    await selectButtonByText(wrapper, 'admin.accounts.addMapping')
    expect(wrapper.get('input[placeholder="admin.accounts.fromModel"]').exists()).toBe(true)
    expect(wrapper.get('input[placeholder="admin.accounts.toModel"]').exists()).toBe(true)
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Bedrock account')
    const passwordInputs = wrapper.findAll('form#create-account-form input[type="password"]')
    await passwordInputs[0].setValue('secret-key')
    await wrapper.get('[data-testid="create-bedrock-access-key-id"]').setValue('AKIA-TEST')
    await wrapper.get('[data-testid="create-bedrock-force-global"]').setValue(true)
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials?.aws_force_global).toBe('true')
  })

  it('imports and previews a Vertex Claude service-account JSON file', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="create-account-type-service-account"]').trigger('click')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Vertex Claude')
    const serviceAccount = JSON.stringify({
      type: 'service_account',
      project_id: 'vertex-project',
      client_email: 'vertex@example.iam.gserviceaccount.com',
      private_key: '-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n'
    })

    await (wrapper.vm as any).handleVertexServiceAccountDrop({
      dataTransfer: { files: [{ text: async () => serviceAccount }] }
    })
    await wrapper.vm.$nextTick()

    expect(wrapper.get('[data-testid="create-vertex-service-account-preview"]').text()).toContain('vertex-project')
    expect(wrapper.get('[data-testid="create-vertex-service-account-preview"]').text()).toContain('vertex@example.iam.gserviceaccount.com')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]).toMatchObject({
      platform: 'anthropic',
      type: 'service_account',
      credentials: {
        project_id: 'vertex-project',
        client_email: 'vertex@example.iam.gserviceaccount.com',
        tier_id: 'vertex'
      }
    })
  })

  it('adds an OpenAI model-mapping preset to the creation payload', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await selectButtonByText(wrapper, 'API Key')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI preset')
    await wrapper.get('form#create-account-form input[type="password"]').setValue('sk-test')
    await wrapper.get('[data-testid="create-model-restriction-mapping"]').trigger('click')
    await wrapper.get('[data-testid="create-model-preset-gpt-5.4"]').trigger('click')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(createAccountMock.mock.calls[0]?.[0]?.credentials?.model_mapping).toMatchObject({
      'gpt-5.4': 'gpt-5.4'
    })
  })

  it('shows Anthropic web-search emulation only when a global provider is enabled', async () => {
    getWebSearchEmulationConfigMock.mockResolvedValueOnce({
      enabled: true,
      providers: [{ name: 'provider' }]
    })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="create-account-type-apikey"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="create-anthropic-web-search-select"]').exists()).toBe(true)
  })

  it('exposes Agent Identity in the OpenAI authorization methods', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('OpenAI account')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')

    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    expect(flow.props('showManualOption')).toBe(true)
    expect(flow.props('showCodexSessionImportOption')).toBe(true)
    expect(flow.props('showAgentIdentityOption')).toBe(true)
    expect(flow.props('showCodexPatOption')).toBe(true)
    expect(flow.props('initialInputMethod')).toBe('manual')
  })

  it.each([
    ['camelCase', { authMode: 'agentIdentity', agentIdentity: { agentRuntimeId: 'runtime' } }],
    ['nested identity without auth_mode', { agent_identity: { agent_runtime_id: 'runtime' } }],
  ])('accepts backend-compatible %s Agent Identity imports', async (_name, content) => {
    const wrapper = await openCodexImportStep()
    const flow = wrapper.getComponent(OAuthAuthorizationFlowStub)
    flow.vm.inputMethod = 'agent_identity'

    flow.vm.$emit('import-codex-session', JSON.stringify(content))
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
  })

  it('sends true explicitly when OpenAI long-context billing is enabled', async () => {
    await submitApiKeyAccount('openai', true)

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('omits the OpenAI setting for non-OpenAI account creation', async () => {
    await submitApiKeyAccount('anthropic')

    expect(createAccountMock).toHaveBeenCalledTimes(1)
    expect(createAccountMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
    // 上游倍率探测已放宽到全部 API-key 平台：非 OpenAI 平台与 OpenAI 一致，默认开启。
    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(true)
  })

  it('sends an explicit disabled state when the non-OpenAI create toggle is turned off', async () => {
    await submitApiKeyAccount('anthropic', false, true)

    expect(createAccountMock.mock.calls[0]?.[0]?.upstream_billing_probe_enabled).toBe(false)
  })

  it('leaves Codex session import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock).toHaveBeenCalledTimes(1)
    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('does not leak hidden API-key quota fields into a Codex OAuth import', async () => {
    const wrapper = mountModal()
    ;(wrapper.vm as any).quotaLimit = 25
    ;(wrapper.vm as any).quotaDailyLimit = 5
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    const extra = importCodexSessionMock.mock.calls[0]?.[0]?.extra || {}
    expect(extra).not.toHaveProperty('quota_limit')
    expect(extra).not.toHaveProperty('quota_daily_limit')
  })

  it('sends OAuth model restrictions and compact mappings with Codex session imports', async () => {
    const wrapper = mountModal()
    await selectButtonByText(wrapper, 'OpenAI')
    await wrapper.get('[data-testid="model-whitelist-selector"]').trigger('click')
    await wrapper.get('[data-testid="create-openai-compact-model-add"]').trigger('click')
    expect(wrapper.get('[data-testid="create-openai-compact-model-from"]').attributes('placeholder')).toBe(
      'admin.accounts.fromModel'
    )
    expect(wrapper.get('[data-testid="create-openai-compact-model-to"]').attributes('placeholder')).toBe(
      'admin.accounts.toModel'
    )
    await wrapper.get('[data-testid="create-openai-compact-model-from"]').setValue('gpt-5.4')
    await wrapper.get('[data-testid="create-openai-compact-model-to"]').setValue('gpt-5.4-mini')
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Codex import')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.credential_extras).toMatchObject({
      model_mapping: { 'gpt-5.4': 'gpt-5.4' },
      compact_model_mapping: { 'gpt-5.4': 'gpt-5.4-mini' },
    })
  })

  it('submits the restored Claude OAuth scheduling controls during cookie import', async () => {
    const wrapper = mountModal()
    await wrapper.get('form#create-account-form input[type="text"]').setValue('Claude OAuth')
    await wrapper.get('[data-testid="create-window-cost-enabled"]').trigger('click')
    await wrapper.get('[data-testid="create-window-cost-limit"]').setValue('50')
    await wrapper.get('[data-testid="create-window-cost-sticky-reserve"]').setValue('12')
    await wrapper.get('[data-testid="create-session-limit-enabled"]').trigger('click')
    await wrapper.get('[data-testid="create-max-sessions"]').setValue('4')
    await wrapper.get('[data-testid="create-session-idle-timeout"]').setValue('9')
    await wrapper.get('[data-testid="create-user-message-queue-mode-serialize"]').trigger('click')
    await wrapper.get('[data-testid="create-tls-fingerprint-enabled"]').trigger('click')
    await vi.waitFor(() => {
      expect(wrapper.findAll('[data-testid="create-tls-fingerprint-profile-select"] option')).toHaveLength(3)
    })
    await wrapper.get('[data-testid="create-tls-fingerprint-profile-select"]').setValue('7')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await wrapper.get('[data-testid="cookie-auth"]').trigger('click')
    await flushPromises()

    expect(exchangeCodeMock).toHaveBeenCalledWith('/admin/accounts/cookie-auth', {
      session_id: '',
      code: 'session-key',
    })
    expect(createAccountMock.mock.calls[0]?.[0]?.extra).toMatchObject({
      window_cost_limit: 50,
      window_cost_sticky_reserve: 12,
      max_sessions: 4,
      session_idle_timeout_minutes: 9,
      user_msg_queue_mode: 'serialize',
      enable_tls_fingerprint: true,
      tls_fingerprint_profile_id: 7,
    })
  })

  it('leaves Codex PAT import billing ownership to the backend', async () => {
    const wrapper = await openCodexImportStep()
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock).toHaveBeenCalledTimes(1)
    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBeUndefined()
  })

  it('sends explicit true for Codex session import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex session import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-session"]').trigger('click')
    await flushPromises()

    expect(importCodexSessionMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })

  it('sends explicit true for Codex PAT import after the toggle is enabled', async () => {
    const wrapper = await openCodexImportStep(1)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(true)
  })

  it('sends explicit false for Codex PAT import after the toggle is changed back', async () => {
    const wrapper = await openCodexImportStep(2)
    await wrapper.get('[data-testid="import-codex-pat"]').trigger('click')
    await flushPromises()

    expect(createOpenAICodexPATMock.mock.calls[0]?.[0]?.extra?.openai_long_context_billing_enabled).toBe(false)
  })
})
