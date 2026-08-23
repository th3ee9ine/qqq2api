<template>
  <BaseDialog
    :show="show && supported"
    :title="t('admin.accounts.editAccount')"
    width="wide"
    @close="handleClose"
  >
    <form
      v-if="account"
      id="edit-account-form"
      class="space-y-5"
      @submit.prevent="handleSubmit"
    >
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.accounts.name') }}</label>
          <input v-model="name" type="text" class="input w-full" required />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.type') }}</label>
          <input
            :value="account.type"
            type="text"
            class="input w-full bg-gray-50 dark:bg-dark-700"
            readonly
          />
        </div>
      </div>

      <div v-if="isApiKey" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.apiKey') }}</label>
          <input
            v-model="newApiKey"
            type="password"
            class="input w-full font-mono"
            autocomplete="off"
            :placeholder="t('admin.accounts.leaveBlankToKeep')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.baseUrl') }}</label>
          <input v-model="baseUrl" type="url" class="input w-full font-mono" />
        </div>
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

      <div v-if="isServiceAccount" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.vertexServiceAccountJson') }}</label>
          <textarea
            v-model="serviceAccountJson"
            rows="8"
            class="input w-full resize-y font-mono text-xs"
            :placeholder="t('admin.accounts.leaveBlankToKeep')"
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

      <ModelWhitelistSelector
        v-if="!isServiceAccount"
        v-model="allowedModels"
        :platform="modelPlatform"
      />

      <section v-if="isOpenAI && !isSparkShadow" class="space-y-4">
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
            @click="openAILongContextBillingEnabled = !openAILongContextBillingEnabled"
          >
            <span :class="toggleKnobClass(openAILongContextBillingEnabled)" />
          </button>
        </label>

        <label
          v-if="account.type === 'oauth'"
          class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
        >
          <span class="text-sm text-gray-700 dark:text-gray-300">
            {{ t('admin.accounts.openai.flattenNamespaces') }}
          </span>
          <button
            type="button"
            role="switch"
            data-testid="edit-openai-flatten-namespaces-toggle"
            :aria-checked="openAIFlattenNamespaces"
            :class="toggleClass(openAIFlattenNamespaces)"
            @click="openAIFlattenNamespaces = !openAIFlattenNamespaces"
          >
            <span :class="toggleKnobClass(openAIFlattenNamespaces)" />
          </button>
        </label>

        <div>
          <label class="input-label">{{ t('admin.accounts.openai.wsMode') }}</label>
          <select
            v-model="openAIWSMode"
            class="input w-full"
            data-testid="edit-openai-ws-mode-select"
          >
            <option value="off">{{ t('common.disabled') }}</option>
            <option value="ctx_pool">{{ t('admin.accounts.openai.wsModeCtxPool') }}</option>
            <option value="passthrough">{{ t('admin.accounts.openai.wsModePassthrough') }}</option>
            <option value="http_bridge">{{ t('admin.accounts.openai.wsModeHttpBridge') }}</option>
          </select>
        </div>

        <div v-if="isApiKey" class="space-y-3">
          <label class="input-label">{{ t('admin.accounts.openai.endpointCapabilities') }}</label>
          <div class="flex gap-4">
            <label class="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                data-testid="openai-endpoint-capability-chat_completions"
                :checked="openAIEndpointCapabilities.includes('chat_completions')"
                @change="toggleEndpointCapability('chat_completions', $event)"
              />
              Chat Completions
            </label>
            <label class="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                data-testid="openai-endpoint-capability-embeddings"
                :checked="openAIEndpointCapabilities.includes('embeddings')"
                @change="toggleEndpointCapability('embeddings', $event)"
              />
              Embeddings
            </label>
          </div>
          <select
            v-model="openAIResponsesMode"
            data-testid="openai-responses-mode-select"
            class="input w-full"
            :disabled="!openAIEndpointCapabilities.includes('chat_completions')"
          >
            <option value="auto">Auto</option>
            <option value="force_responses">Responses</option>
            <option value="force_chat_completions">Chat Completions</option>
          </select>
          <p
            v-if="!openAIEndpointCapabilities.includes('chat_completions')"
            data-testid="openai-responses-mode-not-applicable"
            class="text-xs text-gray-500"
          >
            {{ t('admin.accounts.openai.responsesModeNotApplicable') }}
          </p>
        </div>

        <div class="space-y-2">
          <p class="input-label">{{ t('admin.accounts.openai.codexImageTool') }}</p>
          <p class="text-xs text-gray-500">{{ t('admin.accounts.openai.codexImageToolDesc') }}</p>
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              data-testid="codex-image-tool-inherit"
              :class="modeButtonClass('inherit')"
              @click="codexImageToolMode = 'inherit'"
            >
              {{ t('admin.accounts.openai.codexImageToolInherit') }}
            </button>
            <button
              type="button"
              data-testid="codex-image-tool-enabled"
              :class="modeButtonClass('enabled')"
              @click="codexImageToolMode = 'enabled'"
            >
              {{ t('admin.accounts.openai.codexImageToolEnabled') }}
            </button>
            <button
              type="button"
              data-testid="codex-image-tool-disabled"
              :class="modeButtonClass('disabled')"
              @click="codexImageToolMode = 'disabled'"
            >
              {{ t('admin.accounts.openai.codexImageToolDisabled') }}
            </button>
            <button
              type="button"
              data-testid="codex-image-tool-block"
              :class="modeButtonClass('block')"
              @click="codexImageToolMode = 'block'"
            >
              {{ t('admin.accounts.openai.codexImageToolBlock') }}
            </button>
          </div>
          <p class="text-xs text-gray-500">
            {{ t('admin.accounts.openai.codexImageToolEnabledDesc') }}
          </p>
          <p class="text-xs text-gray-500">
            {{ t('admin.accounts.openai.codexImageToolBlockDesc') }}
          </p>
        </div>

        <div class="grid gap-3 sm:grid-cols-2">
          <label class="text-sm">
            <span class="input-label">{{ t('admin.accounts.autoPause5hThreshold') }}</span>
            <input
              v-model.number="autoPause5hThresholdPercent"
              type="number"
              min="0"
              max="100"
              data-testid="auto-pause-5h-threshold"
              class="input w-full"
            />
          </label>
          <label class="text-sm">
            <span class="input-label">{{ t('admin.accounts.autoPause7dThreshold') }}</span>
            <input
              v-model.number="autoPause7dThresholdPercent"
              type="number"
              min="0"
              max="100"
              data-testid="auto-pause-7d-threshold"
              class="input w-full"
            />
          </label>
        </div>
        <div class="flex flex-wrap gap-3">
          <button
            type="button"
            role="switch"
            data-testid="auto-pause-5h-disabled"
            :aria-checked="autoPause5hDisabled"
            class="btn btn-secondary"
            @click="autoPause5hDisabled = !autoPause5hDisabled"
          >
            5h
          </button>
          <button
            type="button"
            role="switch"
            data-testid="auto-pause-7d-disabled"
            :aria-checked="autoPause7dDisabled"
            class="btn btn-secondary"
            @click="autoPause7dDisabled = !autoPause7dDisabled"
          >
            7d
          </button>
        </div>
      </section>

      <section v-if="isApiKey" class="space-y-3">
        <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <span class="text-sm">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</span>
          <button
            type="button"
            role="switch"
            data-testid="upstream-billing-auto-probe"
            :aria-checked="upstreamBillingProbeEnabled"
            :class="toggleClass(upstreamBillingProbeEnabled)"
            @click="toggleUpstreamProbe"
          >
            <span :class="toggleKnobClass(upstreamBillingProbeEnabled)" />
          </button>
        </label>
        <label class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <span class="text-sm">{{ t('admin.accounts.upstreamBilling.rateSync') }}</span>
          <button
            type="button"
            role="switch"
            data-testid="upstream-billing-rate-sync"
            :aria-checked="upstreamBillingRateSyncEnabled"
            :class="toggleClass(upstreamBillingRateSyncEnabled)"
            @click="toggleUpstreamRateSync"
          >
            <span :class="toggleKnobClass(upstreamBillingRateSyncEnabled)" />
          </button>
        </label>
      </section>

      <div>
        <label class="input-label">{{ t('admin.accounts.billingRateMultiplier') }}</label>
        <input
          v-model.number="rateMultiplier"
          type="number"
          min="0"
          step="0.01"
          data-testid="account-rate-multiplier"
          class="input w-full"
          :disabled="upstreamBillingRateSyncEnabled"
        />
        <p class="input-hint">
          {{
            upstreamBillingRateSyncEnabled
              ? t('admin.accounts.upstreamBilling.syncRateManagedHint')
              : t('admin.accounts.billingRateMultiplierHint')
          }}
        </p>
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.notes') }}</label>
        <textarea v-model="notes" rows="2" class="input w-full resize-y" />
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <label>
          <span class="input-label">{{ t('admin.accounts.concurrency') }}</span>
          <input v-model.number="concurrency" type="number" min="1" class="input w-full" />
        </label>
        <label>
          <span class="input-label">{{ t('admin.accounts.priority') }}</span>
          <input v-model.number="priority" type="number" min="0" class="input w-full" />
        </label>
      </div>

      <ProxySelector v-model="proxyId" :proxies="proxies" />
      <GroupSelector
        v-model="groupIds"
        :groups="groups"
        :platform="account.platform"
      />
    </form>

    <template #footer>
      <div class="flex w-full justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="edit-account-form"
          class="btn btn-primary"
          :disabled="submitting || !supported"
        >
          {{ submitting ? t('common.loading') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { adminAPI } from '@/api/admin'
import {
  buildModelMappingObject,
  splitModelMappingObject,
  type ModelMappingEntry
} from '@/composables/useModelWhitelist'
import { allSelectedGroupsEnableLongContextPricing } from './longContextBilling'
import {
  applyHeaderOverride,
  isHeaderOverrideCapable,
  splitHeaderOverridesObject,
  type HeaderOverrideRow
} from './credentialsBuilder'
import { VERTEX_LOCATION_OPTIONS } from '@/constants/account'
import type {
  Account,
  AdminGroup,
  OpenAIEndpointCapability,
  OpenAIResponsesMode,
  Proxy,
  UpdateAccountRequest
} from '@/types'
import type { OpenAIWSMode } from '@/utils/openaiWsMode'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import HeaderOverrideEditor from './HeaderOverrideEditor.vue'
import ModelWhitelistSelector from './ModelWhitelistSelector.vue'

type CodexImageToolMode = 'inherit' | 'enabled' | 'disabled' | 'block'

const props = defineProps<{
  show: boolean
  account: Account | null
  proxies: Proxy[]
  groups: AdminGroup[]
}>()
const emit = defineEmits<{ close: []; updated: [account: Account] }>()
const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const supported = computed(() =>
  props.account?.platform === 'anthropic' || props.account?.platform === 'openai'
)
const isOpenAI = computed(() => props.account?.platform === 'openai')
const isApiKey = computed(() => props.account?.type === 'apikey')
const isServiceAccount = computed(() => props.account?.type === 'service_account')
const isSparkShadow = computed(() => Boolean(props.account?.parent_account_id))
const modelPlatform = computed(() => props.account?.type === 'bedrock' ? 'bedrock' : props.account?.platform)

const submitting = ref(false)
const name = ref('')
const notes = ref('')
const newApiKey = ref('')
const baseUrl = ref('')
const serviceAccountJson = ref('')
const vertexLocation = ref('global')
const concurrency = ref(1)
const priority = ref(0)
const rateMultiplier = ref(1)
const proxyId = ref<number | null>(null)
const groupIds = ref<number[]>([])
const allowedModels = ref<string[]>([])
const preservedModelMappings = ref<ModelMappingEntry[]>([])
const compactModelMapping = ref<Record<string, unknown> | null>(null)
const headerOverrideEnabled = ref(false)
const headerOverrideRows = ref<HeaderOverrideRow[]>([])
const openAILongContextBillingEnabled = ref(false)
const openAIFlattenNamespaces = ref(false)
const openAICompactMode = ref('auto')
const openAIResponsesMode = ref<OpenAIResponsesMode>('auto')
const openAIEndpointCapabilities = ref<OpenAIEndpointCapability[]>([
  'chat_completions',
  'embeddings'
])
const openAIWSMode = ref<OpenAIWSMode>('off')
const codexImageToolMode = ref<CodexImageToolMode>('inherit')
const autoPause5hThresholdPercent = ref<number | null>(null)
const autoPause7dThresholdPercent = ref<number | null>(null)
const autoPause5hDisabled = ref(false)
const autoPause7dDisabled = ref(false)
const upstreamBillingProbeEnabled = ref(false)
const upstreamBillingRateSyncEnabled = ref(false)

const headerOverrideCapable = computed(() =>
  Boolean(props.account && isHeaderOverrideCapable(props.account.platform, props.account.type))
)
const hideOpenAILongContextToggle = computed(() =>
  !authStore.isSimpleMode &&
  allSelectedGroupsEnableLongContextPricing(groupIds.value, props.groups)
)

watch(
  () => [props.show, props.account] as const,
  ([visible]) => {
    if (visible && props.account && supported.value) hydrate()
    if (visible && props.account && !supported.value) emit('close')
  },
  { immediate: true }
)

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {}
}

function readBoolean(value: unknown): boolean {
  return value === true
}

function readPercent(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value * 100 : null
}

function hydrate() {
  const account = props.account
  if (!account) return
  const credentials = asRecord(account.credentials)
  const extra = asRecord(account.extra)
  name.value = account.name
  notes.value = account.notes || ''
  newApiKey.value = ''
  baseUrl.value = typeof credentials.base_url === 'string'
    ? credentials.base_url
    : account.platform === 'openai'
      ? 'https://api.openai.com'
      : 'https://api.anthropic.com'
  serviceAccountJson.value = typeof credentials.service_account_json === 'string'
    ? credentials.service_account_json
    : ''
  vertexLocation.value = typeof credentials.location === 'string' ? credentials.location : 'global'
  concurrency.value = account.concurrency || 1
  priority.value = account.priority || 0
  rateMultiplier.value = account.rate_multiplier ?? 1
  proxyId.value = account.proxy_id ?? null
  groupIds.value = [...(account.group_ids || [])]
  const split = splitModelMappingObject(asRecord(credentials.model_mapping))
  allowedModels.value = split.allowedModels
  preservedModelMappings.value = split.modelMappings
  compactModelMapping.value = Object.keys(asRecord(credentials.compact_model_mapping)).length > 0
    ? { ...asRecord(credentials.compact_model_mapping) }
    : null
  headerOverrideEnabled.value = credentials.header_override_enabled === true
  headerOverrideRows.value = splitHeaderOverridesObject(credentials.header_overrides)
  openAILongContextBillingEnabled.value = readBoolean(extra.openai_long_context_billing_enabled)
  openAIFlattenNamespaces.value = readBoolean(extra.openai_responses_flatten_namespaces)
  openAICompactMode.value = typeof extra.openai_compact_mode === 'string'
    ? extra.openai_compact_mode
    : 'auto'
  const storedResponsesMode = extra.openai_responses_mode
  openAIResponsesMode.value = storedResponsesMode === 'force_responses' ||
    storedResponsesMode === 'force_chat_completions'
    ? storedResponsesMode
    : 'auto'
  const capabilities = Array.isArray(credentials.openai_capabilities)
    ? credentials.openai_capabilities.filter(
        (value): value is OpenAIEndpointCapability =>
          value === 'chat_completions' || value === 'embeddings'
      )
    : []
  openAIEndpointCapabilities.value = capabilities.length > 0
    ? [...capabilities]
    : ['chat_completions', 'embeddings']
  const wsKey = account.type === 'apikey'
    ? 'openai_apikey_responses_websockets_v2_mode'
    : 'openai_oauth_responses_websockets_v2_mode'
  const storedWSMode = extra[wsKey]
  openAIWSMode.value = storedWSMode === 'ctx_pool' ||
    storedWSMode === 'passthrough' ||
    storedWSMode === 'http_bridge'
    ? storedWSMode
    : 'off'
  codexImageToolMode.value = extra.codex_image_generation_explicit_tool_policy === 'strip'
    ? 'block'
    : extra.codex_image_generation_bridge === true
      ? 'enabled'
      : extra.codex_image_generation_bridge === false
        ? 'disabled'
        : 'inherit'
  autoPause5hThresholdPercent.value = readPercent(extra.auto_pause_5h_threshold)
  autoPause7dThresholdPercent.value = readPercent(extra.auto_pause_7d_threshold)
  autoPause5hDisabled.value = readBoolean(extra.auto_pause_5h_disabled)
  autoPause7dDisabled.value = readBoolean(extra.auto_pause_7d_disabled)
  upstreamBillingProbeEnabled.value = readBoolean(
    asRecord(account).upstream_billing_probe_enabled ?? extra.upstream_billing_probe_enabled
  )
  upstreamBillingRateSyncEnabled.value = readBoolean(
    asRecord(account).upstream_billing_rate_sync_enabled ?? extra.upstream_billing_rate_sync_enabled
  )
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

function modeButtonClass(mode: CodexImageToolMode) {
  return [
    'rounded-lg border px-3 py-2 text-sm',
    codexImageToolMode.value === mode
      ? 'border-primary-500 bg-primary-50 text-primary-700'
      : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'
  ]
}

function toggleEndpointCapability(
  capability: OpenAIEndpointCapability,
  event: Event
) {
  const input = event.target as HTMLInputElement
  const checked = input.checked
  if (checked) {
    if (!openAIEndpointCapabilities.value.includes(capability)) {
      openAIEndpointCapabilities.value = [...openAIEndpointCapabilities.value, capability]
    }
    return
  }
  if (openAIEndpointCapabilities.value.length === 1) {
    input.checked = true
    return
  }
  openAIEndpointCapabilities.value = openAIEndpointCapabilities.value.filter(
    value => value !== capability
  )
}

function toggleUpstreamProbe() {
  upstreamBillingProbeEnabled.value = !upstreamBillingProbeEnabled.value
  if (!upstreamBillingProbeEnabled.value) upstreamBillingRateSyncEnabled.value = false
}

function toggleUpstreamRateSync() {
  upstreamBillingRateSyncEnabled.value = !upstreamBillingRateSyncEnabled.value
  if (upstreamBillingRateSyncEnabled.value) upstreamBillingProbeEnabled.value = true
}

function applyOptionalPercent(
  target: Record<string, unknown>,
  key: string,
  value: number | null
) {
  if (typeof value === 'number' && Number.isFinite(value)) {
    target[key] = Math.max(0, Math.min(100, value)) / 100
  } else {
    delete target[key]
  }
}

async function handleSubmit() {
  const account = props.account
  if (!account || !supported.value || !name.value.trim()) return
  let credentials: Record<string, unknown>
  const currentCredentials = asRecord(account.credentials)
  const mapping = buildModelMappingObject(
    'combined',
    allowedModels.value,
    preservedModelMappings.value
  )

  if (isSparkShadow.value) {
    credentials = {}
    if (mapping) credentials.model_mapping = mapping
    if (compactModelMapping.value) {
      credentials.compact_model_mapping = { ...compactModelMapping.value }
    }
  } else {
    credentials = { ...currentCredentials }
    if (mapping) credentials.model_mapping = mapping
    else delete credentials.model_mapping

    if (isApiKey.value) {
      const existingKey = typeof currentCredentials.api_key === 'string' &&
        currentCredentials.api_key.trim().length > 0
      const statusHasKey = account.credentials_status?.has_api_key === true
      if (!newApiKey.value.trim() && !existingKey && !statusHasKey) {
        appStore.showError(t('admin.accounts.pleaseEnterApiKey'))
        return
      }
      if (newApiKey.value.trim()) credentials.api_key = newApiKey.value.trim()
      credentials.base_url = baseUrl.value.trim() || (
        account.platform === 'openai' ? 'https://api.openai.com' : 'https://api.anthropic.com'
      )
      applyHeaderOverride(
        credentials,
        headerOverrideEnabled.value,
        headerOverrideRows.value,
        'edit'
      )
    }

    if (isServiceAccount.value) {
      const existingJson = typeof currentCredentials.service_account_json === 'string' &&
        currentCredentials.service_account_json.trim().length > 0
      const statusHasJson = account.credentials_status?.has_service_account_json === true
      const inputJson = serviceAccountJson.value.trim()
      if (!inputJson && !existingJson && !statusHasJson) {
        appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
        return
      }
      if (inputJson) {
        try {
          const parsed = JSON.parse(inputJson) as Record<string, unknown>
          const projectId = typeof parsed.project_id === 'string'
            ? parsed.project_id.trim()
            : typeof currentCredentials.project_id === 'string'
              ? currentCredentials.project_id.trim()
              : ''
          const clientEmail = typeof parsed.client_email === 'string'
            ? parsed.client_email.trim()
            : typeof currentCredentials.client_email === 'string'
              ? currentCredentials.client_email.trim()
              : ''
          const privateKey = typeof parsed.private_key === 'string' ? parsed.private_key.trim() : ''
          if (!projectId || !clientEmail || !privateKey) throw new Error('missing fields')
          credentials.service_account_json = JSON.stringify(parsed)
          credentials.project_id = projectId
          credentials.client_email = clientEmail
        } catch {
          appStore.showError(t('admin.accounts.vertexSaJsonInvalid'))
          return
        }
      }
      credentials.location = vertexLocation.value
      credentials.tier_id = 'vertex'
    }

    if (isOpenAI.value && isApiKey.value) {
      credentials.openai_capabilities = [...openAIEndpointCapabilities.value]
    }
  }

  const extra = { ...asRecord(account.extra) }
  if (isOpenAI.value) {
    if (isSparkShadow.value) {
      delete extra.openai_long_context_billing_enabled
    } else {
      extra.openai_long_context_billing_enabled = openAILongContextBillingEnabled.value
      if (account.type === 'oauth' && openAIFlattenNamespaces.value) {
        extra.openai_responses_flatten_namespaces = true
      } else {
        delete extra.openai_responses_flatten_namespaces
      }
      if (openAICompactMode.value === 'auto') delete extra.openai_compact_mode
      else extra.openai_compact_mode = openAICompactMode.value
      const wsPrefix = account.type === 'apikey' ? 'openai_apikey' : 'openai_oauth'
      extra[wsPrefix + '_responses_websockets_v2_mode'] = openAIWSMode.value
      extra[wsPrefix + '_responses_websockets_v2_enabled'] = openAIWSMode.value !== 'off'
      if (
        isApiKey.value &&
        openAIEndpointCapabilities.value.includes('chat_completions') &&
        openAIResponsesMode.value !== 'auto'
      ) {
        extra.openai_responses_mode = openAIResponsesMode.value
      } else {
        delete extra.openai_responses_mode
      }
      delete extra.codex_image_generation_bridge_enabled
      if (codexImageToolMode.value === 'block') {
        extra.codex_image_generation_explicit_tool_policy = 'strip'
        delete extra.codex_image_generation_bridge
      } else {
        delete extra.codex_image_generation_explicit_tool_policy
        if (codexImageToolMode.value === 'enabled') {
          extra.codex_image_generation_bridge = true
        } else if (codexImageToolMode.value === 'disabled') {
          extra.codex_image_generation_bridge = false
        } else {
          delete extra.codex_image_generation_bridge
        }
      }
      applyOptionalPercent(extra, 'auto_pause_5h_threshold', autoPause5hThresholdPercent.value)
      applyOptionalPercent(extra, 'auto_pause_7d_threshold', autoPause7dThresholdPercent.value)
      if (autoPause5hDisabled.value) extra.auto_pause_5h_disabled = true
      else delete extra.auto_pause_5h_disabled
      if (autoPause7dDisabled.value) extra.auto_pause_7d_disabled = true
      else delete extra.auto_pause_7d_disabled
    }
  }
  delete extra.upstream_billing_probe_enabled
  delete extra.upstream_billing_rate_sync_enabled

  const updates: UpdateAccountRequest = {
    name: name.value.trim(),
    notes: notes.value.trim() || null,
    credentials,
    extra,
    proxy_id: proxyId.value,
    concurrency: concurrency.value,
    priority: priority.value,
    group_ids: groupIds.value
  }
  if (isApiKey.value) {
    updates.upstream_billing_probe_enabled = upstreamBillingProbeEnabled.value
    updates.upstream_billing_rate_sync_enabled = upstreamBillingRateSyncEnabled.value
  }
  if (!upstreamBillingRateSyncEnabled.value) {
    updates.rate_multiplier = rateMultiplier.value
  }

  submitting.value = true
  try {
    const updated = await adminAPI.accounts.update(account.id, updates)
    appStore.showSuccess(t('admin.accounts.accountUpdated'))
    emit('updated', updated)
    handleClose()
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.message ||
      error.response?.data?.detail ||
      t('admin.accounts.failedToUpdate')
    )
  } finally {
    submitting.value = false
  }
}

function handleClose() {
  emit('close')
}
</script>
