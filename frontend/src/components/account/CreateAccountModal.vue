<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.createAccount')"
    width="wide"
    @close="handleClose"
  >
    <form
      v-if="step === 1"
      id="create-account-form"
      class="space-y-5"
      @submit.prevent="handleSubmit"
    >
      <section>
        <label class="input-label">{{ t('admin.accounts.platform') }}</label>
        <div class="grid grid-cols-2 gap-3">
          <button
            type="button"
            :class="platformButtonClass('anthropic')"
            @click="selectPlatform('anthropic')"
          >
            <PlatformIcon platform="anthropic" size="sm" />
            <span>{{ t('admin.accounts.claudeConsole') }}</span>
          </button>
          <button
            type="button"
            :class="platformButtonClass('openai')"
            @click="selectPlatform('openai')"
          >
            <PlatformIcon platform="openai" size="sm" />
            <span>OpenAI</span>
          </button>
        </div>
      </section>

      <section>
        <label class="input-label">{{ t('admin.accounts.accountType') }}</label>
        <div class="flex flex-wrap gap-2">
          <button
            v-for="option in accountTypeOptions"
            :key="option.value"
            type="button"
            :class="typeButtonClass(option.value)"
            @click="accountCategory = option.value"
          >
            {{ option.label }}
          </button>
        </div>
      </section>

      <div>
        <label class="input-label">{{ t('admin.accounts.name') }}</label>
        <input
          v-model="form.name"
          type="text"
          class="input w-full"
          :placeholder="t('admin.accounts.namePlaceholder')"
          required
        />
      </div>

      <div v-if="accountCategory === 'apikey'" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKeyRequired') }}</label>
          <input
            v-model="apiKeyValue"
            type="password"
            class="input w-full font-mono"
            autocomplete="off"
            :placeholder="t('admin.accounts.apiKey')"
            required
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input v-model="apiKeyBaseUrl" type="url" class="input w-full font-mono" />
        </div>
        <ModelWhitelistSelector
          v-model="allowedModels"
          :platform="form.platform"
          :sync-credentials="syncPreviewCredentials"
        />
        <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <span class="text-sm text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.upstreamBilling.autoProbe') }}
          </span>
          <button
            type="button"
            role="switch"
            data-testid="upstream-billing-auto-probe"
            :aria-checked="upstreamBillingProbeEnabled"
            :class="toggleClass(upstreamBillingProbeEnabled)"
            @click="upstreamBillingProbeEnabled = !upstreamBillingProbeEnabled"
          >
            <span :class="toggleKnobClass(upstreamBillingProbeEnabled)" />
          </button>
        </label>
        <div v-if="headerOverrideCapable" class="space-y-3">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="headerOverrideEnabled" type="checkbox" />
            {{ t('admin.accounts.headerOverride.enabled') }}
          </label>
          <HeaderOverrideEditor
            v-if="headerOverrideEnabled"
            v-model:rows="headerOverrideRows"
          />
        </div>
      </div>

      <div v-else-if="accountCategory === 'bedrock'" class="space-y-4">
        <fieldset class="border-0 p-0">
          <legend class="input-label">{{ t('admin.accounts.bedrockAuthMode') }}</legend>
          <div class="flex gap-4">
            <label class="flex items-center gap-2 text-sm">
              <input v-model="bedrockAuthMode" type="radio" value="sigv4" />
              {{ t('admin.accounts.bedrockAuthModeSigv4') }}
            </label>
            <label class="flex items-center gap-2 text-sm">
              <input v-model="bedrockAuthMode" type="radio" value="apikey" />
              {{ t('admin.accounts.bedrockAuthModeApikey') }}
            </label>
          </div>
        </fieldset>
        <template v-if="bedrockAuthMode === 'sigv4'">
          <input
            v-model="bedrockAccessKeyId"
            type="text"
            class="input w-full font-mono"
            :placeholder="t('admin.accounts.bedrockAccessKeyId')"
          />
          <input
            v-model="bedrockSecretAccessKey"
            type="password"
            class="input w-full font-mono"
            :placeholder="t('admin.accounts.bedrockSecretAccessKey')"
          />
          <input
            v-model="bedrockSessionToken"
            type="password"
            class="input w-full font-mono"
            :placeholder="t('admin.accounts.bedrockSessionToken')"
          />
        </template>
        <input
          v-else
          v-model="bedrockApiKey"
          type="password"
          class="input w-full font-mono"
          :placeholder="t('admin.accounts.bedrockApiKeyInput')"
        />
        <div>
          <label class="input-label">{{ t('admin.accounts.bedrockRegion') }}</label>
          <input v-model="bedrockRegion" type="text" class="input w-full font-mono" />
        </div>
        <ModelWhitelistSelector v-model="allowedModels" platform="bedrock" />
      </div>

      <div v-else-if="accountCategory === 'service_account'" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.vertexServiceAccountJson') }}</label>
          <textarea
            v-model="vertexServiceAccountJson"
            rows="8"
            class="input w-full resize-y font-mono text-xs"
            :placeholder="t('admin.accounts.vertexServiceAccountJsonPlaceholder')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.vertexLocation') }}</label>
          <select v-model="vertexLocation" class="input w-full">
            <optgroup
              v-for="section in VERTEX_LOCATION_OPTIONS"
              :key="section.label"
              :label="section.label"
            >
              <option v-for="location in section.options" :key="location.value" :value="location.value">
                {{ location.label }}
              </option>
            </optgroup>
          </select>
        </div>
      </div>

      <section v-if="form.platform === 'openai'" class="space-y-3">
        <label
          v-if="!hideOpenAILongContextToggle"
          class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
        >
          <span class="text-sm text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.openai.longContextBilling') }}
          </span>
          <button
            type="button"
            role="switch"
            data-testid="openai-long-context-billing-toggle"
            :aria-checked="openAILongContextBillingEnabled"
            :class="toggleClass(openAILongContextBillingEnabled)"
            @click="toggleOpenAILongContextBilling"
          >
            <span :class="toggleKnobClass(openAILongContextBillingEnabled)" />
          </button>
        </label>
        <label
          v-if="accountCategory === 'oauth'"
          class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
        >
          <span class="text-sm text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.openai.flattenNamespaces') }}
          </span>
          <button
            type="button"
            role="switch"
            data-testid="create-openai-flatten-namespaces-toggle"
            :aria-checked="openAIFlattenNamespaces"
            :class="toggleClass(openAIFlattenNamespaces)"
            @click="openAIFlattenNamespaces = !openAIFlattenNamespaces"
          >
            <span :class="toggleKnobClass(openAIFlattenNamespaces)" />
          </button>
        </label>
        <div data-testid="create-openai-ws-mode">
          <label class="input-label">{{ t('admin.accounts.openai.wsMode') }}</label>
          <select v-model="openAIWSMode" class="input w-full">
            <option value="off">{{ t('common.disabled') }}</option>
            <option value="ctx_pool">{{ t('admin.accounts.openai.wsModeCtxPool') }}</option>
            <option value="passthrough">{{ t('admin.accounts.openai.wsModePassthrough') }}</option>
            <option value="http_bridge">{{ t('admin.accounts.openai.wsModeHttpBridge') }}</option>
          </select>
        </div>
      </section>

      <div>
        <label class="input-label">{{ t('admin.accounts.notes') }}</label>
        <textarea v-model="form.notes" rows="2" class="input w-full resize-y" />
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.accounts.concurrency') }}</label>
          <input v-model.number="form.concurrency" type="number" min="1" class="input w-full" />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.priority') }}</label>
          <input v-model.number="form.priority" type="number" min="0" class="input w-full" />
        </div>
      </div>

      <ProxySelector v-model="form.proxy_id" :proxies="proxies" />
      <GroupSelector
        v-model="form.group_ids"
        :groups="groups"
        :platform="form.platform"
      />
    </form>

    <div v-else class="space-y-4">
      <button type="button" class="text-sm text-primary-600 hover:text-primary-700" @click="step = 1">
        ← {{ t('common.back') }}
      </button>
      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentOAuthLoading"
        :error="currentOAuthError"
        :platform="form.platform"
        :show-help="form.platform === 'anthropic'"
        :show-proxy-warning="form.platform === 'anthropic'"
        :show-cookie-option="form.platform === 'anthropic'"
        :show-refresh-token-option="form.platform === 'openai'"
        :show-mobile-refresh-token-option="form.platform === 'openai'"
        :show-codex-session-import-option="form.platform === 'openai'"
        :show-agent-identity-option="form.platform === 'openai'"
        :show-codex-pat-option="form.platform === 'openai'"
        :show-manual-option="true"
        initial-input-method="manual"
        :allow-multiple="true"
        @generate-url="handleGenerateAuthUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleOpenAIRefreshTokens"
        @validate-mobile-refresh-token="handleOpenAIRefreshTokens"
        @import-codex-session="handleOpenAIImportCodexSession"
        @import-codex-pat="handleOpenAIImportCodexPAT"
      />
    </div>

    <template #footer>
      <div class="flex w-full justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          v-if="step === 1"
          type="submit"
          form="create-account-form"
          class="btn btn-primary"
          :disabled="submitting"
        >
          {{ submitting ? t('common.loading') : t('admin.accounts.createAccount') }}
        </button>
        <button
          v-else-if="oauthFlowRef?.inputMethod === 'manual'"
          type="button"
          class="btn btn-primary"
          :disabled="!canExchangeOAuthCode"
          @click="handleExchangeOAuthCode"
        >
          {{ currentOAuthLoading ? t('admin.accounts.oauth.verifying') : t('admin.accounts.oauth.completeAuth') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import { useAccountOAuth, type AddMethod, type AuthInputMethod } from '@/composables/useAccountOAuth'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import { buildModelMappingObject } from '@/composables/useModelWhitelist'
import { allSelectedGroupsEnableLongContextPricing } from './longContextBilling'
import {
  applyHeaderOverride,
  isHeaderOverrideCapable,
  type HeaderOverrideRow
} from './credentialsBuilder'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import type {
  AccountType,
  AdminGroup,
  CreateAccountRequest,
  Proxy
} from '@/types'
import type { OpenAIWSMode } from '@/utils/openaiWsMode'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import HeaderOverrideEditor from './HeaderOverrideEditor.vue'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'
import OAuthAuthorizationFlow from './OAuthAuthorizationFlow.vue'

type SupportedPlatform = 'anthropic' | 'openai'
type AccountCategory = 'oauth' | 'setup-token' | 'apikey' | 'bedrock' | 'service_account'

interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  inputMethod: AuthInputMethod
  reset: () => void
}

const props = defineProps<{
  show: boolean
  proxies: Proxy[]
  groups: AdminGroup[]
}>()
const emit = defineEmits<{ close: []; created: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const anthropicOAuth = useAccountOAuth()
const openaiOAuth = useOpenAIOAuth()
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)
const step = ref<1 | 2>(1)
const submitting = ref(false)
const accountCategory = ref<AccountCategory>('apikey')

const form = reactive({
  name: '',
  notes: '',
  platform: 'anthropic' as SupportedPlatform,
  proxy_id: null as number | null,
  concurrency: 1,
  priority: 0,
  rate_multiplier: 1,
  group_ids: [] as number[]
})

const apiKeyValue = ref('')
const apiKeyBaseUrl = ref('https://api.anthropic.com')
const allowedModels = ref<string[]>([])
const upstreamBillingProbeEnabled = ref(true)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const bedrockAuthMode = ref<'sigv4' | 'apikey'>('sigv4')
const bedrockAccessKeyId = ref('')
const bedrockSecretAccessKey = ref('')
const bedrockSessionToken = ref('')
const bedrockApiKey = ref('')
const bedrockRegion = ref('us-east-1')
const vertexServiceAccountJson = ref('')
const vertexLocation = ref('global')
const openAILongContextBillingEnabled = ref(false)
const openAILongContextBillingTouched = ref(false)
const openAIFlattenNamespaces = ref(false)
const openAIWSMode = ref<OpenAIWSMode>('off')

const accountTypeOptions = computed<Array<{ value: AccountCategory; label: string }>>(() => {
  if (form.platform === 'openai') {
    return [
      { value: 'oauth', label: 'OAuth' },
      { value: 'apikey', label: 'API Key' }
    ]
  }
  return [
    { value: 'apikey', label: 'API Key' },
    { value: 'oauth', label: 'OAuth' },
    { value: 'setup-token', label: t('admin.accounts.setupTokenLongLived') },
    { value: 'bedrock', label: 'AWS Bedrock' },
    { value: 'service_account', label: 'Vertex Claude' }
  ]
})
const addMethod = computed<AddMethod>(() =>
  accountCategory.value === 'setup-token' ? 'setup-token' : 'oauth'
)
const currentAuthUrl = computed(() =>
  form.platform === 'openai' ? openaiOAuth.authUrl.value : anthropicOAuth.authUrl.value
)
const currentSessionId = computed(() =>
  form.platform === 'openai' ? openaiOAuth.sessionId.value : anthropicOAuth.sessionId.value
)
const currentOAuthLoading = computed(() =>
  form.platform === 'openai' ? openaiOAuth.loading.value : anthropicOAuth.loading.value
)
const currentOAuthError = computed(() =>
  form.platform === 'openai' ? openaiOAuth.error.value : anthropicOAuth.error.value
)
const canExchangeOAuthCode = computed(() =>
  Boolean(oauthFlowRef.value?.authCode?.trim() && currentSessionId.value && !currentOAuthLoading.value)
)
const headerOverrideCapable = computed(() =>
  isHeaderOverrideCapable(form.platform, 'apikey')
)
const syncPreviewCredentials = computed(() => ({
  platform: form.platform,
  type: 'apikey',
  base_url: apiKeyBaseUrl.value.trim(),
  api_key: apiKeyValue.value.trim()
}))
const hideOpenAILongContextToggle = computed(() =>
  !authStore.isSimpleMode &&
  allSelectedGroupsEnableLongContextPricing(form.group_ids, props.groups)
)

watch(() => props.show, visible => {
  if (!visible) resetForm()
})
watch(accountCategory, category => {
  if (form.platform === 'openai' && !['oauth', 'apikey'].includes(category)) {
    accountCategory.value = 'oauth'
  }
})

function platformButtonClass(platform: SupportedPlatform) {
  return [
    'flex items-center justify-center gap-2 rounded-lg border px-4 py-3 text-sm font-medium',
    form.platform === platform
      ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
      : 'border-gray-200 text-gray-700 hover:border-gray-300 dark:border-dark-600 dark:text-gray-300'
  ]
}

function typeButtonClass(category: AccountCategory) {
  return [
    'rounded-lg border px-3 py-2 text-sm',
    accountCategory.value === category
      ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'
      : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'
  ]
}

function toggleClass(enabled: boolean) {
  return [
    'relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors',
    enabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-500'
  ]
}

function toggleKnobClass(enabled: boolean) {
  return [
    'inline-block h-5 w-5 translate-y-0.5 rounded-full bg-white shadow transition-transform',
    enabled ? 'translate-x-5' : 'translate-x-0.5'
  ]
}

function selectPlatform(platform: SupportedPlatform) {
  form.platform = platform
  accountCategory.value = platform === 'openai' ? 'oauth' : 'apikey'
  apiKeyBaseUrl.value = platform === 'openai'
    ? 'https://api.openai.com'
    : 'https://api.anthropic.com'
  allowedModels.value = []
  headerOverrideEnabled.value = false
  headerOverrideRows.value = []
}

function toggleOpenAILongContextBilling() {
  openAILongContextBillingEnabled.value = !openAILongContextBillingEnabled.value
  openAILongContextBillingTouched.value = true
}

function buildOpenAIExtra(forImport = false): Record<string, unknown> | undefined {
  if (form.platform !== 'openai') return undefined
  const extra: Record<string, unknown> = {}
  const modePrefix = accountCategory.value === 'apikey' ? 'openai_apikey' : 'openai_oauth'
  extra[modePrefix + '_responses_websockets_v2_mode'] = openAIWSMode.value
  extra[modePrefix + '_responses_websockets_v2_enabled'] = openAIWSMode.value !== 'off'
  if (accountCategory.value === 'oauth' && openAIFlattenNamespaces.value) {
    extra.openai_responses_flatten_namespaces = true
  }
  if (!forImport || openAILongContextBillingTouched.value) {
    extra.openai_long_context_billing_enabled = openAILongContextBillingEnabled.value
  }
  return Object.keys(extra).length > 0 ? extra : undefined
}

function buildBasePayload(
  platform: SupportedPlatform,
  type: AccountType,
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>
): CreateAccountRequest {
  return {
    name: form.name.trim(),
    notes: form.notes.trim() || null,
    platform,
    type,
    credentials,
    extra,
    proxy_id: form.proxy_id,
    concurrency: form.concurrency,
    priority: form.priority,
    rate_multiplier: form.rate_multiplier,
    group_ids: form.group_ids,
    upstream_billing_probe_enabled: type === 'apikey'
      ? upstreamBillingProbeEnabled.value
      : undefined
  }
}

async function createAndFinish(payload: CreateAccountRequest) {
  submitting.value = true
  try {
    const account = await adminAPI.accounts.create(payload)
    if (payload.type === 'apikey' && payload.upstream_billing_probe_enabled === true) {
      try {
        await adminAPI.accounts.probeUpstreamBilling(account.id)
      } catch {
        appStore.showWarning(t('admin.accounts.upstreamBilling.probeFailed'))
      }
    }
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message ||
      error.response?.data?.detail ||
      t('admin.accounts.failedToCreate')
    )
  } finally {
    submitting.value = false
  }
}

async function handleSubmit() {
  if (!form.name.trim()) {
    appStore.showError(t('admin.accounts.pleaseEnterAccountName'))
    return
  }
  if (accountCategory.value === 'oauth' || accountCategory.value === 'setup-token') {
    step.value = 2
    return
  }

  if (accountCategory.value === 'apikey') {
    if (!apiKeyValue.value.trim()) {
      appStore.showError(t('admin.accounts.pleaseEnterApiKey'))
      return
    }
    const credentials: Record<string, unknown> = {
      api_key: apiKeyValue.value.trim(),
      base_url: apiKeyBaseUrl.value.trim() || (
        form.platform === 'openai' ? 'https://api.openai.com' : 'https://api.anthropic.com'
      )
    }
    const mapping = buildModelMappingObject('whitelist', allowedModels.value, [])
    if (mapping) credentials.model_mapping = mapping
    applyHeaderOverride(
      credentials,
      headerOverrideEnabled.value,
      headerOverrideRows.value,
      'create'
    )
    await createAndFinish(buildBasePayload(
      form.platform,
      'apikey',
      credentials,
      form.platform === 'openai' ? buildOpenAIExtra() : undefined
    ))
    return
  }

  if (accountCategory.value === 'bedrock') {
    const credentials: Record<string, unknown> = {
      auth_mode: bedrockAuthMode.value,
      aws_region: bedrockRegion.value.trim() || 'us-east-1'
    }
    if (bedrockAuthMode.value === 'sigv4') {
      if (!bedrockAccessKeyId.value.trim() || !bedrockSecretAccessKey.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockAccessKeyIdRequired'))
        return
      }
      credentials.aws_access_key_id = bedrockAccessKeyId.value.trim()
      credentials.aws_secret_access_key = bedrockSecretAccessKey.value.trim()
      if (bedrockSessionToken.value.trim()) {
        credentials.aws_session_token = bedrockSessionToken.value.trim()
      }
    } else {
      if (!bedrockApiKey.value.trim()) {
        appStore.showError(t('admin.accounts.bedrockApiKeyRequired'))
        return
      }
      credentials.api_key = bedrockApiKey.value.trim()
    }
    const mapping = buildModelMappingObject('whitelist', allowedModels.value, [])
    if (mapping) credentials.model_mapping = mapping
    await createAndFinish(buildBasePayload('anthropic', 'bedrock', credentials))
    return
  }

  const rawServiceAccount = vertexServiceAccountJson.value.trim()
  try {
    const parsed = JSON.parse(rawServiceAccount) as Record<string, unknown>
    const projectId = typeof parsed.project_id === 'string' ? parsed.project_id.trim() : ''
    const clientEmail = typeof parsed.client_email === 'string' ? parsed.client_email.trim() : ''
    const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
    if (!projectId || !clientEmail || !privateKey) throw new Error('missing fields')
    await createAndFinish(buildBasePayload('anthropic', 'service_account', {
      service_account_json: JSON.stringify(parsed),
      project_id: projectId,
      client_email: clientEmail,
      location: vertexLocation.value,
      tier_id: 'vertex'
    }))
  } catch {
    appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
  }
}

async function handleGenerateAuthUrl() {
  if (form.platform === 'openai') {
    await openaiOAuth.generateAuthUrl(form.proxy_id)
  } else {
    await anthropicOAuth.generateAuthUrl(addMethod.value, form.proxy_id)
  }
}

async function createOAuthAccount(
  credentials: Record<string, unknown>,
  extra?: Record<string, unknown>,
  name?: string
) {
  const payload = buildBasePayload(
    form.platform,
    form.platform === 'anthropic' ? addMethod.value : 'oauth',
    credentials,
    extra
  )
  if (name) payload.name = name
  await createAndFinish(payload)
}

async function handleExchangeOAuthCode() {
  const code = oauthFlowRef.value?.authCode.trim()
  if (!code) return
  if (form.platform === 'openai') {
    const state = (oauthFlowRef.value?.oauthState || openaiOAuth.oauthState.value).trim()
    const tokenInfo = await openaiOAuth.exchangeAuthCode(
      code,
      openaiOAuth.sessionId.value,
      state,
      form.proxy_id
    )
    if (tokenInfo) {
      await createOAuthAccount(
        openaiOAuth.buildCredentials(tokenInfo),
        buildOpenAIExtra(openAILongContextBillingTouched.value)
      )
    }
    return
  }
  anthropicOAuth.authCode.value = code
  const tokenInfo = await anthropicOAuth.exchangeAuthCode(addMethod.value, form.proxy_id)
  if (tokenInfo) {
    await createOAuthAccount(
      { ...tokenInfo },
      anthropicOAuth.buildExtraInfo(tokenInfo)
    )
  }
}

async function handleCookieAuth(input: string) {
  if (form.platform !== 'anthropic') return
  const keys = anthropicOAuth.parseSessionKeys(input)
  if (keys.length === 0) return
  submitting.value = true
  try {
    for (let index = 0; index < keys.length; index += 1) {
      const tokenInfo = await anthropicOAuth.cookieAuth(addMethod.value, keys[index], form.proxy_id)
      if (!tokenInfo) continue
      const payload = buildBasePayload(
        'anthropic',
        addMethod.value,
        { ...tokenInfo },
        anthropicOAuth.buildExtraInfo(tokenInfo)
      )
      if (keys.length > 1) payload.name = form.name.trim() + ' #' + (index + 1)
      await adminAPI.accounts.create(payload)
    }
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } finally {
    submitting.value = false
  }
}

async function handleOpenAIRefreshTokens(input: string) {
  if (form.platform !== 'openai') return
  const tokens = input.split('\n').map(value => value.trim()).filter(Boolean)
  if (tokens.length === 0) return
  openaiOAuth.loading.value = true
  try {
    for (let index = 0; index < tokens.length; index += 1) {
      const tokenInfo = await openaiOAuth.validateRefreshToken(tokens[index], form.proxy_id)
      if (!tokenInfo) continue
      const payload = buildBasePayload(
        'openai',
        'oauth',
        openaiOAuth.buildCredentials(tokenInfo),
        buildOpenAIExtra(openAILongContextBillingTouched.value)
      )
      if (tokens.length > 1) payload.name = form.name.trim() + ' #' + (index + 1)
      await adminAPI.accounts.create(payload)
    }
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } finally {
    openaiOAuth.loading.value = false
  }
}

function isAgentIdentityImport(content: string): boolean {
  const hasIdentity = (value: unknown): boolean => {
    if (!value || typeof value !== 'object') return false
    const record = value as Record<string, unknown>
    return record.authMode === 'agentIdentity' ||
      record.auth_mode === 'agent_identity' ||
      Boolean(record.agentIdentity) ||
      Boolean(record.agent_identity)
  }
  try {
    const parsed = JSON.parse(content)
    return Array.isArray(parsed) ? parsed.every(hasIdentity) : hasIdentity(parsed)
  } catch {
    try {
      return content.split('\n').map(line => line.trim()).filter(Boolean)
        .every(line => hasIdentity(JSON.parse(line)))
    } catch {
      return false
    }
  }
}

async function handleOpenAIImportCodexSession(content: string) {
  if (form.platform !== 'openai') return
  if (oauthFlowRef.value?.inputMethod === 'agent_identity' && !isAgentIdentityImport(content)) {
    openaiOAuth.error.value = t('admin.accounts.oauth.openai.agentIdentityInvalid')
    return
  }
  openaiOAuth.loading.value = true
  try {
    const result = await adminAPI.accounts.importCodexSession({
      content: content.trim(),
      name: form.name.trim(),
      notes: form.notes.trim() || null,
      proxy_id: form.proxy_id,
      concurrency: form.concurrency,
      priority: form.priority,
      rate_multiplier: form.rate_multiplier,
      group_ids: form.group_ids,
      extra: buildOpenAIExtra(true),
      update_existing: true
    })
    if (result.created + result.updated > 0) emit('created')
    if (result.failed === 0) handleClose()
  } catch (error: any) {
    openaiOAuth.error.value = error.response?.data?.detail || error.message
  } finally {
    openaiOAuth.loading.value = false
  }
}

async function handleOpenAIImportCodexPAT(accessToken: string) {
  if (form.platform !== 'openai') return
  openaiOAuth.loading.value = true
  try {
    await adminAPI.accounts.createOpenAICodexPAT({
      access_token: accessToken.trim(),
      name: form.name.trim(),
      notes: form.notes.trim() || null,
      proxy_id: form.proxy_id,
      concurrency: form.concurrency,
      priority: form.priority,
      rate_multiplier: form.rate_multiplier,
      group_ids: form.group_ids,
      extra: buildOpenAIExtra(true)
    })
    appStore.showSuccess(t('admin.accounts.accountCreated'))
    emit('created')
    handleClose()
  } catch (error: any) {
    openaiOAuth.error.value = error.response?.data?.detail || error.message
  } finally {
    openaiOAuth.loading.value = false
  }
}

function resetForm() {
  step.value = 1
  form.name = ''
  form.notes = ''
  form.platform = 'anthropic'
  form.proxy_id = null
  form.concurrency = 1
  form.priority = 0
  form.rate_multiplier = 1
  form.group_ids = []
  accountCategory.value = 'apikey'
  apiKeyValue.value = ''
  apiKeyBaseUrl.value = 'https://api.anthropic.com'
  allowedModels.value = []
  upstreamBillingProbeEnabled.value = true
  headerOverrideEnabled.value = false
  headerOverrideRows.value = []
  bedrockAuthMode.value = 'sigv4'
  bedrockAccessKeyId.value = ''
  bedrockSecretAccessKey.value = ''
  bedrockSessionToken.value = ''
  bedrockApiKey.value = ''
  bedrockRegion.value = 'us-east-1'
  vertexServiceAccountJson.value = ''
  vertexLocation.value = 'global'
  openAILongContextBillingEnabled.value = false
  openAILongContextBillingTouched.value = false
  openAIFlattenNamespaces.value = false
  openAIWSMode.value = 'off'
  anthropicOAuth.resetState()
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

function handleClose() {
  emit('close')
}
</script>
