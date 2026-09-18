<template>
  <section class="rounded-lg border border-amber-200 bg-amber-50/60 p-4 dark:border-amber-900/60 dark:bg-amber-900/10" data-testid="codex-turn-state-settings">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.gatewayForwarding.codexTurnStateTitle') }}</h3>
        <p class="mt-1 max-w-3xl text-xs text-gray-600 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateHint') }}</p>
      </div>
      <Toggle :model-value="enabled" :aria-label="t('admin.settings.gatewayForwarding.codexTurnStateTitle')" data-testid="openai-codex-turn-state-toggle" @update:model-value="emit('update:enabled', $event)" />
    </div>

    <div class="mt-4 flex items-start justify-between gap-4 rounded border border-gray-200 bg-white/70 p-3 dark:border-dark-600 dark:bg-dark-800/50">
      <div>
        <h4 class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.settings.gatewayForwarding.codexTurnStateAutoTitle') }}</h4>
        <p class="mt-1 max-w-3xl text-xs text-gray-600 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateAutoHint') }}</p>
      </div>
      <Toggle :model-value="autoEnabled" :aria-label="t('admin.settings.gatewayForwarding.codexTurnStateAutoTitle')" data-testid="openai-codex-turn-state-auto-toggle" @update:model-value="emit('update:autoEnabled', $event)" />
    </div>

    <div class="mt-4 space-y-4" :class="!enabled && 'opacity-70'">
      <div>
        <label for="openai-codex-turn-state-token" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateToken') }}</label>
        <div class="flex gap-2">
          <input
            id="openai-codex-turn-state-token"
            :value="token"
            :placeholder="configured ? t('admin.settings.gatewayForwarding.codexTurnStateTokenConfiguredPlaceholder') : t('admin.settings.gatewayForwarding.codexTurnStateTokenPlaceholder')"
            maxlength="4096"
            type="password"
            autocomplete="new-password"
            spellcheck="false"
            class="input w-full font-mono text-sm"
            data-testid="openai-codex-turn-state-token"
            @input="emit('update:token', ($event.target as HTMLInputElement).value)"
          />
          <button v-if="configured || token || tokenDirty" type="button" class="btn btn-secondary whitespace-nowrap text-xs" data-testid="openai-codex-turn-state-clear" @click="emit('clear-token')">{{ t('admin.settings.gatewayForwarding.codexTurnStateClear') }}</button>
        </div>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateTokenHint') }}</p>
      </div>

      <div>
        <label for="openai-codex-turn-state-models" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateModels') }}</label>
        <input
          id="openai-codex-turn-state-models"
          :value="models"
          type="text"
          class="input w-full font-mono text-sm"
          data-testid="openai-codex-turn-state-models"
          :placeholder="t('admin.settings.gatewayForwarding.codexTurnStateModelsPlaceholder')"
          autocomplete="off"
          @input="emit('update:models', ($event.target as HTMLInputElement).value)"
        />
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateModelsHint') }}</p>
      </div>

      <div class="rounded border border-gray-200 bg-white/70 p-3 text-xs dark:border-dark-600 dark:bg-dark-800/50" data-testid="openai-codex-turn-state-diagnostics">
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
          <span class="font-medium text-gray-700 dark:text-gray-200">{{ t(tokenDirty ? 'admin.settings.gatewayForwarding.codexTurnStatePreview' : 'admin.settings.gatewayForwarding.codexTurnStateStatusLabel') }}:</span>
          <span :class="statusClass">{{ statusLabel }}</span>
          <span v-if="displayStatus?.verdict" class="text-gray-500 dark:text-gray-400">· {{ t('admin.settings.gatewayForwarding.codexTurnStateVerdict', { verdict: displayStatus.verdict }) }}</span>
        </div>
        <p class="mt-1 text-gray-500 dark:text-gray-400">{{ injectionLabel }}</p>
        <p class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateNotice') }}</p>
        <p v-if="displayStatus?.blocks" class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateBlocks', { blocks: displayStatus.blocks }) }}</p>
        <p v-if="displayStatus?.issued_at" class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateIssuedAt', { value: formatDate(displayStatus.issued_at * 1000) }) }}</p>
        <p v-if="setAtMs" class="mt-1 text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateSetAt', { value: formatDate(setAtMs) }) }}</p>
        <p v-if="displayStatus?.expires_at" class="mt-1 text-gray-500 dark:text-gray-400">{{ expiryLabel }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import Toggle from '@/components/common/Toggle.vue'
import { useI18n } from 'vue-i18n'
import type { OpenAICodexTurnStateStatus } from '@/api/admin/settings'
import { formatCodexTurnStateDuration, inspectCodexTurnState, refreshCodexTurnStateStatus } from '@/utils/codexTurnState'

const props = defineProps<{
  enabled: boolean
  autoEnabled: boolean
  token: string
  tokenDirty: boolean
  models: string
  configured: boolean
  setAtMs: number
  status?: OpenAICodexTurnStateStatus
}>()
const emit = defineEmits<{
  'update:enabled': [value: boolean]
  'update:autoEnabled': [value: boolean]
  'update:token': [value: string]
  'update:models': [value: string]
  'clear-token': []
}>()
const { t } = useI18n()
const now = ref(Date.now())
const timer = window.setInterval(() => { now.value = Date.now() }, 1000)
onBeforeUnmount(() => window.clearInterval(timer))

const displayStatus = computed(() => props.tokenDirty
  ? inspectCodexTurnState(props.token, now.value, props.enabled)
  : refreshCodexTurnStateStatus(props.status || {
      enabled: props.enabled, configured: props.configured, active: false,
      reason: props.configured ? 'unknown' : 'missing', verdict: 'unknown', blocks: 0,
    }, props.enabled, now.value))
const statusLabel = computed(() => {
  const reason = displayStatus.value.reason
  const known = ['disabled', 'missing', 'invalid', 'unknown', 'ready', 'future', 'expired', 'suspect']
  return t(`admin.settings.gatewayForwarding.codexTurnStateStatus.${known.includes(reason) ? reason : 'unknown'}`)
})
const injectionLabel = computed(() => t(displayStatus.value.active
  ? 'admin.settings.gatewayForwarding.codexTurnStateInjectionActive'
  : 'admin.settings.gatewayForwarding.codexTurnStateInjectionInactive'))
const statusClass = computed(() => ['ready', 'disabled', 'missing'].includes(displayStatus.value.reason)
  ? 'font-medium text-gray-700 dark:text-gray-200'
  : 'font-medium text-amber-700 dark:text-amber-300')
const expiryLabel = computed(() => {
  const expiry = displayStatus.value.expires_at
  if (!expiry) return ''
  const remaining = expiry * 1000 - now.value
  const key = remaining >= 0 ? 'codexTurnStateExpiresIn' : 'codexTurnStateExpiredAgo'
  return t(`admin.settings.gatewayForwarding.${key}`, { duration: formatCodexTurnStateDuration(remaining), value: formatDate(expiry * 1000) })
})
function formatDate(value: number): string {
  return new Date(value).toLocaleString()
}
</script>
