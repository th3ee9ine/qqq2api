<template>
  <section class="rounded-lg border border-amber-200 bg-amber-50/60 p-4 dark:border-amber-900/60 dark:bg-amber-900/10" data-testid="codex-turn-state-settings">
    <div class="flex flex-wrap items-start justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.gatewayForwarding.codexTurnStateTitle') }}</h3>
        <p class="mt-1 max-w-3xl text-xs text-gray-600 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateHint') }}</p>
      </div>
      <Toggle :model-value="autoEnabled" :aria-label="t('admin.settings.gatewayForwarding.codexTurnStateAutoTitle')" data-testid="openai-codex-turn-state-auto-toggle" @update:model-value="emit('update:autoEnabled', $event)" />
    </div>

    <p class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateAutoHint') }}</p>

    <div class="mt-4">
      <label class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300" for="openai-codex-turn-state-proxy">
        {{ t('admin.settings.gatewayForwarding.codexTurnStateProxy') }}
      </label>
      <ProxySelector
        id="openai-codex-turn-state-proxy"
        :model-value="proxyId > 0 ? proxyId : null"
        :proxies="proxies"
        data-testid="openai-codex-turn-state-proxy"
        @update:model-value="emit('update:proxyId', $event ?? 0)"
      />
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.gatewayForwarding.codexTurnStateProxyHint') }}
      </p>
    </div>

    <div class="mt-4">
      <label for="openai-codex-turn-state-default-model" class="mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.gatewayForwarding.codexTurnStateDefaultModel') }}</label>
      <input
        id="openai-codex-turn-state-default-model"
        :value="defaultModel"
        type="text"
        class="input w-full font-mono text-sm"
        data-testid="openai-codex-turn-state-default-model"
        placeholder="gpt-5.5"
        maxlength="128"
        autocomplete="off"
        @input="emit('update:defaultModel', ($event.target as HTMLInputElement).value)"
      />
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.gatewayForwarding.codexTurnStateDefaultModelHint') }}</p>
    </div>

    <div class="mt-4">
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
  </section>
</template>

<script setup lang="ts">
import Toggle from '@/components/common/Toggle.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { useI18n } from 'vue-i18n'
import type { Proxy } from '@/types'
import { toRefs } from 'vue'

const props = withDefaults(defineProps<{
  autoEnabled: boolean
  models: string
  defaultModel: string
  proxyId?: number
  proxies?: Proxy[]
}>(), {
  proxyId: 0,
  proxies: () => [],
})
const emit = defineEmits<{
  'update:autoEnabled': [value: boolean]
  'update:models': [value: string]
  'update:defaultModel': [value: string]
  'update:proxyId': [value: number]
}>()
const { t } = useI18n()
const { proxyId, proxies } = toRefs(props)
</script>
