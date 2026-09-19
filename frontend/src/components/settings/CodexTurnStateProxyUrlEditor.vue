<template>
  <div class="proxy-url-editor" data-testid="codex-turn-state-proxy-url-editor">
    <div class="proxy-url-toolbar">
      <div class="min-w-0">
        <p class="text-xs font-medium text-gray-700 dark:text-gray-200">
          {{ t('admin.codexTurnState.proxyPool.selectedCount', { count: modelValue.length }) }}
        </p>
        <p class="mt-1 text-[11px] leading-5 text-gray-500 dark:text-gray-400">
          {{ modelValue.length === 0
            ? t('admin.codexTurnState.proxyPool.globalFallback')
            : t('admin.codexTurnState.proxyPool.dedicatedOnly') }}
        </p>
      </div>
      <button
        type="button"
        class="btn btn-secondary shrink-0"
        :disabled="disabled || modelValue.length >= maxEntries"
        data-testid="codex-turn-state-proxy-url-add"
        @click="addUrl"
      >
        <Icon name="plus" size="sm" />
        {{ t('admin.codexTurnState.proxyPool.add') }}
      </button>
    </div>

    <div v-if="modelValue.length > 0" class="proxy-url-list">
      <div v-for="(url, index) in modelValue" :key="index" class="proxy-url-row">
        <div class="proxy-url-input-row">
          <div class="relative min-w-0 flex-1">
            <input
              :value="url"
              :type="visibleRows.has(index) ? 'text' : 'password'"
              class="input w-full pr-10 font-mono text-sm"
              :class="{ 'border-red-400 focus:border-red-500 focus:ring-red-500': validations[index]?.valid === false }"
              autocomplete="new-password"
              spellcheck="false"
              :aria-invalid="validations[index]?.valid === false"
              :aria-describedby="validations[index]?.valid === false ? `codex-turn-state-proxy-url-error-${index}` : undefined"
              :disabled="disabled"
              :placeholder="t('admin.codexTurnState.proxyPool.placeholder')"
              :data-testid="`codex-turn-state-proxy-url-${index}`"
              @input="updateUrl(index, $event)"
            />
            <button
              type="button"
              class="visibility-button"
              :disabled="disabled"
              :title="visibleRows.has(index) ? t('admin.codexTurnState.proxyPool.hideUrl') : t('admin.codexTurnState.proxyPool.showUrl')"
              :aria-label="visibleRows.has(index) ? t('admin.codexTurnState.proxyPool.hideUrl') : t('admin.codexTurnState.proxyPool.showUrl')"
              :data-testid="`codex-turn-state-proxy-url-visibility-${index}`"
              @click="toggleVisibility(index)"
            >
              <Icon :name="visibleRows.has(index) ? 'eyeOff' : 'eye'" size="sm" />
            </button>
          </div>
          <button
            type="button"
            class="remove-button"
            :disabled="disabled"
            :title="t('admin.codexTurnState.proxyPool.remove')"
            :aria-label="t('admin.codexTurnState.proxyPool.remove')"
            :data-testid="`codex-turn-state-proxy-url-remove-${index}`"
            @click="removeUrl(index)"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
        <p
          v-if="validations[index]?.valid === false"
          :id="`codex-turn-state-proxy-url-error-${index}`"
          class="mt-1.5 text-xs leading-5 text-red-600 dark:text-red-400"
          role="alert"
          :data-testid="`codex-turn-state-proxy-url-error-${index}`"
        >
          {{ validationMessage(validations[index]?.error) }}
        </p>
      </div>
    </div>

    <p class="proxy-url-limit" :class="{ 'is-reached': modelValue.length >= maxEntries }">
      {{ t('admin.codexTurnState.proxyPool.limit', { count: maxEntries }) }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import {
  CODEX_TURN_STATE_PROXY_URL_LIMIT,
  validateCodexTurnStateProxyUrl,
} from '@/utils/codexTurnState'
import type { CodexTurnStateProxyUrlError } from '@/utils/codexTurnState'

const props = withDefaults(defineProps<{
  modelValue: string[]
  disabled?: boolean
}>(), {
  disabled: false,
})

const emit = defineEmits<{
  'update:modelValue': [value: string[]]
  'validity-change': [valid: boolean]
}>()

const { t } = useI18n()
const maxEntries = CODEX_TURN_STATE_PROXY_URL_LIMIT
const visibleRows = reactive(new Set<number>())
const validations = computed(() => props.modelValue.map(validateCodexTurnStateProxyUrl))
const valid = computed(() => props.modelValue.length <= maxEntries && validations.value.every(item => item.valid))

watch(valid, value => emit('validity-change', value), { immediate: true })

function addUrl() {
  if (props.disabled || props.modelValue.length >= maxEntries) return
  emit('update:modelValue', [...props.modelValue, ''])
}

function updateUrl(index: number, event: Event) {
  const target = event.target as HTMLInputElement
  const next = [...props.modelValue]
  next[index] = target.value
  emit('update:modelValue', next)
}

function removeUrl(index: number) {
  const next = props.modelValue.filter((_, itemIndex) => itemIndex !== index)
  visibleRows.clear()
  emit('update:modelValue', next)
}

function toggleVisibility(index: number) {
  if (visibleRows.has(index)) visibleRows.delete(index)
  else visibleRows.add(index)
}

function validationMessage(error?: CodexTurnStateProxyUrlError): string {
  return t(`admin.codexTurnState.proxyPool.errors.${error || 'invalid'}`)
}
</script>

<style scoped>
.proxy-url-editor {
  @apply overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600;
}
.proxy-url-toolbar {
  @apply flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 bg-gray-50/60 p-3 dark:border-dark-700 dark:bg-dark-800/40;
}
.proxy-url-list {
  @apply divide-y divide-gray-100 dark:divide-dark-700;
}
.proxy-url-row {
  @apply px-3 py-3;
}
.proxy-url-input-row {
  @apply flex min-w-0 items-start gap-2;
}
.visibility-button {
  @apply absolute right-1 top-1/2 inline-flex h-8 w-8 -translate-y-1/2 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
.remove-button {
  @apply inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-gray-200 text-gray-500 transition-colors hover:border-red-200 hover:bg-red-50 hover:text-red-600 focus:outline-none focus:ring-2 focus:ring-red-500 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:hover:border-red-500/30 dark:hover:bg-red-500/10 dark:hover:text-red-400;
}
.proxy-url-limit {
  @apply border-t border-gray-100 px-3 py-2 text-[11px] leading-5 text-gray-400 dark:border-dark-700 dark:text-gray-500;
}
.proxy-url-limit.is-reached {
  @apply text-amber-700 dark:text-amber-400;
}
</style>
