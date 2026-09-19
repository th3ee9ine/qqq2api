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
      <div class="proxy-url-actions">
        <button
          type="button"
          class="btn btn-secondary shrink-0"
          :disabled="disabled || currentPoolAtCapacity"
          data-testid="codex-turn-state-proxy-url-add"
          @click="addUrl"
        >
          <Icon name="plus" size="sm" />
          {{ t('admin.codexTurnState.proxyPool.add') }}
        </button>
        <button
          type="button"
          class="btn btn-secondary shrink-0"
          :disabled="disabled || normalizedCurrentUrls === null || currentPoolAtCapacity"
          data-testid="codex-turn-state-proxy-url-batch-open"
          @click="openBatchEditor"
        >
          <Icon name="clipboard" size="sm" />
          {{ t('admin.codexTurnState.proxyPool.batchAdd') }}
        </button>
      </div>
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

    <p class="proxy-url-limit" :class="{ 'is-reached': currentPoolAtCapacity }">
      {{ t('admin.codexTurnState.proxyPool.limit', { count: maxEntries }) }}
    </p>

    <BaseDialog
      :show="batchEditorOpen"
      :title="t('admin.codexTurnState.proxyPool.batchTitle')"
      width="normal"
      close-on-click-outside
      @close="closeBatchEditor"
    >
      <div class="batch-editor">
        <label for="codex-turn-state-proxy-url-batch-input" class="input-label">
          {{ t('admin.codexTurnState.proxyPool.batchInputLabel') }}
        </label>
        <div class="batch-input-wrap">
          <textarea
            id="codex-turn-state-proxy-url-batch-input"
            v-model="batchInput"
            rows="10"
            class="input batch-input pr-11 font-mono text-sm"
            :class="{
              'is-masked': !batchInputVisible && batchInput.length > 0,
              'border-red-400 focus:border-red-500 focus:ring-red-500': batchHasBlockingErrors,
            }"
            :disabled="disabled"
            :aria-invalid="batchHasBlockingErrors"
            :aria-describedby="batchDescriptionIds"
            autocomplete="off"
            autocapitalize="off"
            spellcheck="false"
            :placeholder="t('admin.codexTurnState.proxyPool.batchPlaceholder')"
            data-testid="codex-turn-state-proxy-url-batch-input"
          />
          <button
            type="button"
            class="batch-visibility-button"
            :disabled="disabled"
            :title="batchInputVisible ? t('admin.codexTurnState.proxyPool.hideBatchInput') : t('admin.codexTurnState.proxyPool.showBatchInput')"
            :aria-label="batchInputVisible ? t('admin.codexTurnState.proxyPool.hideBatchInput') : t('admin.codexTurnState.proxyPool.showBatchInput')"
            data-testid="codex-turn-state-proxy-url-batch-visibility"
            @click="batchInputVisible = !batchInputVisible"
          >
            <Icon :name="batchInputVisible ? 'eyeOff' : 'eye'" size="sm" />
          </button>
        </div>
        <p id="codex-turn-state-proxy-url-batch-hint" class="input-hint">
          {{ t('admin.codexTurnState.proxyPool.batchHint') }}
          {{ t('admin.codexTurnState.proxyPool.batchInputLimits', {
            lines: batchMaxLines,
            size: batchMaxSizeLabel,
          }) }}
        </p>

        <div
          v-if="batchResult.total > 0"
          class="batch-summary"
          aria-live="polite"
          data-testid="codex-turn-state-proxy-url-batch-summary"
        >
          <span>{{ t('admin.codexTurnState.proxyPool.batchTotal', { count: batchResult.total }) }}</span>
          <span class="text-primary-700 dark:text-primary-400">
            {{ t('admin.codexTurnState.proxyPool.batchReady', { count: batchResult.urls.length }) }}
          </span>
          <span v-if="batchResult.duplicates > 0">
            {{ t('admin.codexTurnState.proxyPool.batchDuplicate', { count: batchResult.duplicates }) }}
          </span>
          <span v-if="batchResult.errors.length > 0" class="text-red-600 dark:text-red-400">
            {{ t('admin.codexTurnState.proxyPool.batchInvalid', { count: batchResult.errors.length }) }}
          </span>
        </div>

        <p
          v-if="batchInputErrorMessage"
          id="codex-turn-state-proxy-url-batch-input-error"
          class="batch-input-error"
          role="alert"
          data-testid="codex-turn-state-proxy-url-batch-input-error"
        >
          {{ batchInputErrorMessage }}
        </p>

        <div
          v-if="batchResult.errors.length > 0"
          id="codex-turn-state-proxy-url-batch-errors"
          class="batch-errors"
          role="alert"
          data-testid="codex-turn-state-proxy-url-batch-errors"
        >
          <p
            v-for="item in batchErrorPreview"
            :key="item.line"
            :data-testid="`codex-turn-state-proxy-url-batch-error-${item.line}`"
          >
            {{ t('admin.codexTurnState.proxyPool.batchLineError', {
              line: item.line,
              error: validationMessage(item.error),
            }) }}
          </p>
          <p v-if="batchRemainingErrorCount > 0">
            {{ t('admin.codexTurnState.proxyPool.batchMoreErrors', { count: batchRemainingErrorCount }) }}
          </p>
        </div>

        <p
          v-if="batchResult.overflow > 0"
          id="codex-turn-state-proxy-url-batch-overflow"
          class="batch-overflow"
          role="alert"
          data-testid="codex-turn-state-proxy-url-batch-overflow"
        >
          {{ t('admin.codexTurnState.proxyPool.batchOverflow', { count: batchResult.overflow, limit: maxEntries }) }}
        </p>
      </div>

      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            type="button"
            class="btn btn-secondary"
            data-testid="codex-turn-state-proxy-url-batch-cancel"
            @click="closeBatchEditor"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="button"
            class="btn btn-primary"
            :disabled="disabled || !batchCanApply"
            data-testid="codex-turn-state-proxy-url-batch-apply"
            @click="applyBatch"
          >
            <Icon name="plus" size="sm" />
            {{ t('admin.codexTurnState.proxyPool.batchApply', { count: batchResult.urls.length }) }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES,
  CODEX_TURN_STATE_PROXY_URL_LIMIT,
  normalizeCodexTurnStateProxyUrls,
  parseCodexTurnStateProxyUrlBatch,
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
const batchMaxLines = CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES
const batchMaxSizeLabel = '1 MiB'
const visibleRows = reactive(new Set<number>())
const batchEditorOpen = ref(false)
const batchInput = ref('')
const batchInputVisible = ref(false)
const validations = computed(() => props.modelValue.map(validateCodexTurnStateProxyUrl))
const normalizedCurrentUrls = computed(() => {
  try {
    return normalizeCodexTurnStateProxyUrls(props.modelValue)
  } catch {
    return null
  }
})
const currentPoolAtCapacity = computed(() => normalizedCurrentUrls.value !== null
  ? normalizedCurrentUrls.value.length >= maxEntries
  : props.modelValue.length >= maxEntries)
const valid = computed(() => normalizedCurrentUrls.value !== null)
const batchResult = computed(() => parseCodexTurnStateProxyUrlBatch(batchInput.value, props.modelValue))
const batchHasBlockingErrors = computed(() => Boolean(batchResult.value.inputError) || batchResult.value.errors.length > 0 || batchResult.value.overflow > 0)
const batchCanApply = computed(() => normalizedCurrentUrls.value !== null && batchResult.value.urls.length > 0 && !batchHasBlockingErrors.value)
const batchErrorPreview = computed(() => batchResult.value.errors.slice(0, 5))
const batchRemainingErrorCount = computed(() => Math.max(0, batchResult.value.errors.length - batchErrorPreview.value.length))
const batchInputErrorMessage = computed(() => {
  if (batchResult.value.inputError === 'tooManyLines') {
    return t('admin.codexTurnState.proxyPool.batchTooManyLines', { count: batchMaxLines })
  }
  if (batchResult.value.inputError === 'tooManyBytes') {
    return t('admin.codexTurnState.proxyPool.batchTooLarge', { size: batchMaxSizeLabel })
  }
  return ''
})
const batchDescriptionIds = computed(() => [
  'codex-turn-state-proxy-url-batch-hint',
  batchResult.value.inputError ? 'codex-turn-state-proxy-url-batch-input-error' : '',
  batchResult.value.errors.length > 0 ? 'codex-turn-state-proxy-url-batch-errors' : '',
  batchResult.value.overflow > 0 ? 'codex-turn-state-proxy-url-batch-overflow' : '',
].filter(Boolean).join(' '))

watch(valid, value => emit('validity-change', value), { immediate: true })

function addUrl() {
  if (props.disabled || currentPoolAtCapacity.value) return
  const currentUrls = normalizedCurrentUrls.value
  if (props.modelValue.length >= maxEntries && currentUrls !== null) {
    visibleRows.clear()
    emit('update:modelValue', [...currentUrls, ''])
    return
  }
  emit('update:modelValue', [...props.modelValue, ''])
}

function openBatchEditor() {
  if (props.disabled || normalizedCurrentUrls.value === null || normalizedCurrentUrls.value.length >= maxEntries) return
  batchInput.value = ''
  batchInputVisible.value = false
  batchEditorOpen.value = true
}

function closeBatchEditor() {
  batchEditorOpen.value = false
  batchInput.value = ''
  batchInputVisible.value = false
}

function applyBatch() {
  const currentUrls = normalizedCurrentUrls.value
  if (props.disabled || !batchCanApply.value || currentUrls === null) return
  visibleRows.clear()
  emit('update:modelValue', [...currentUrls, ...batchResult.value.urls])
  closeBatchEditor()
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
.proxy-url-actions {
  @apply flex flex-wrap items-center justify-end gap-2;
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
.batch-editor {
  @apply space-y-3;
}
.batch-input {
  @apply min-h-56 resize-y;
}
.batch-input-wrap {
  @apply relative;
}
.batch-input.is-masked {
  -webkit-text-security: disc;
}
.batch-visibility-button {
  @apply absolute right-2 top-2 inline-flex h-8 w-8 items-center justify-center rounded-md bg-white text-gray-400 shadow-sm transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-dark-800 dark:hover:bg-dark-700 dark:hover:text-gray-200;
}
.batch-summary {
  @apply flex flex-wrap gap-x-4 gap-y-1 border-y border-gray-100 py-2 text-xs leading-5 text-gray-500 dark:border-dark-700 dark:text-gray-400;
}
.batch-errors {
  @apply space-y-1 border-l-2 border-red-400 pl-3 text-xs leading-5 text-red-600 dark:border-red-500 dark:text-red-400;
}
.batch-input-error {
  @apply border-l-2 border-red-400 pl-3 text-xs leading-5 text-red-600 dark:border-red-500 dark:text-red-400;
}
.batch-overflow {
  @apply border-l-2 border-amber-400 pl-3 text-xs leading-5 text-amber-700 dark:border-amber-500 dark:text-amber-400;
}
</style>
