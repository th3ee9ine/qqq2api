<template>
  <section
    v-if="state?.eligible"
    class="space-y-4 border-t border-gray-200 pt-4 dark:border-dark-600"
    data-testid="opencode-go-usage-settings"
  >
    <div class="flex items-start justify-between gap-4">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.accounts.opencodeGo.title') }}
        </h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.opencodeGo.panelHint') }}
        </p>
      </div>
      <span
        class="whitespace-nowrap rounded px-2 py-1 text-xs font-medium"
        :class="statusOk
          ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
          : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
      >
        {{ statusLabel }}
      </span>
    </div>

    <div v-if="loading" class="flex h-20 items-center justify-center text-gray-400">
      <Icon name="refresh" size="sm" class="animate-spin" />
    </div>
    <template v-else>
      <div
        v-if="snapshot"
        class="border-y border-gray-100 py-3 dark:border-dark-700"
        data-testid="opencode-go-usage-details"
      >
        <div class="grid grid-cols-[minmax(4rem,auto)_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-xs">
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.rolling') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ windowSummary(snapshot.data?.rolling) }}</span>
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.weekly') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ windowSummary(snapshot.data?.weekly) }}</span>
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.monthly') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ windowSummary(snapshot.data?.monthly) }}</span>
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.status') }}</span>
          <span class="break-words font-medium text-gray-900 dark:text-white">{{ statusLabel }}</span>
          <span class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.opencodeGo.updatedAt') }}</span>
          <span class="break-words text-gray-900 dark:text-white">{{ formatDate(snapshot.fetched_at || snapshot.last_attempt_at) }}</span>
        </div>
        <p
          v-if="snapshot.last_error"
          class="mt-2 break-words border-t border-gray-100 pt-2 text-xs text-amber-700 dark:border-dark-700 dark:text-amber-300"
        >
          {{ t(`admin.accounts.opencodeGo.errors.${snapshot.last_error}`, snapshot.last_error) }}
        </p>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          class="btn btn-secondary btn-sm"
          :disabled="refreshing"
          data-testid="opencode-go-refresh"
          @click="refreshUsage"
        >
          <Icon name="refresh" size="xs" class="mr-1.5" :class="{ 'animate-spin': refreshing }" />
          {{ t('admin.accounts.opencodeGo.refreshNow') }}
        </button>
      </div>

      <div class="flex items-center justify-between gap-4 border-t border-gray-100 pt-4 dark:border-dark-700">
        <div>
          <label class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.accounts.opencodeGo.autoRefresh') }}
          </label>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.opencodeGo.autoRefreshHint') }}
          </p>
        </div>
        <Toggle
          :model-value="state.auto_refresh_enabled"
          :disabled="saving"
          data-testid="opencode-go-auto-refresh"
          @update:model-value="setAutoRefresh"
        />
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import type { Account, OpenCodeGoUsageState, OpenCodeGoUsageWindow } from '@/types'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ account: Account }>()
const emit = defineEmits<{ updated: [state: OpenCodeGoUsageState] }>()
const { t } = useI18n()
const appStore = useAppStore()
const state = ref<OpenCodeGoUsageState | null>(props.account.opencode_go_usage ?? null)
const loading = ref(false)
const saving = ref(false)
const refreshing = ref(false)
const snapshot = computed(() => state.value?.snapshot)
const statusOk = computed(() => snapshot.value?.status === 'ok')
const statusLabel = computed(() => {
  if (!snapshot.value) return t('admin.accounts.opencodeGo.notRefreshed')
  if (snapshot.value.status === 'unauthorized') return t('admin.accounts.opencodeGo.unauthorized')
  if (snapshot.value.status === 'failed') return t('admin.accounts.opencodeGo.failed')
  return t('admin.accounts.opencodeGo.ok')
})
const formatPercent = (value?: number) => typeof value === 'number' && Number.isFinite(value)
  ? `${value.toFixed(value % 1 ? 1 : 0)}%`
  : '-'
const formatDate = (value?: string) => {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
const windowSummary = (window?: OpenCodeGoUsageWindow) => {
  if (!window) return '-'
  const reset = window.resets_at ? formatDate(window.resets_at) : null
  return reset
    ? t('admin.accounts.opencodeGo.windowWithReset', { percent: formatPercent(window.percent), reset })
    : formatPercent(window.percent)
}
const applyState = (next: OpenCodeGoUsageState) => {
  state.value = next
  emit('updated', next)
}
const load = async () => {
  loading.value = true
  try {
    applyState(await adminAPI.accounts.getOpenCodeGoUsage(props.account.id))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.opencodeGo.loadFailed')))
  } finally {
    loading.value = false
  }
}
const setAutoRefresh = async (enabled: boolean) => {
  saving.value = true
  try {
    applyState(await adminAPI.accounts.setOpenCodeGoUsageAutoRefresh(props.account.id, enabled))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accounts.opencodeGo.autoRefreshFailed')))
  } finally {
    saving.value = false
  }
}
const refreshUsage = async () => {
  refreshing.value = true
  try {
    applyState(await adminAPI.accounts.refreshOpenCodeGoUsage(props.account.id))
    appStore.showSuccess(t('admin.accounts.opencodeGo.refreshSuccess'))
  } catch (error) {
    appStore.showError(extractI18nErrorMessage(error, t, 'admin.accounts.opencodeGo.errors', t('admin.accounts.opencodeGo.refreshFailed')))
  } finally {
    refreshing.value = false
  }
}
watch(() => props.account.opencode_go_usage, (next) => {
  state.value = next ?? null
})
watch(() => props.account.id, () => {
  state.value = props.account.opencode_go_usage ?? null
  if (state.value && !state.value.snapshot) void load()
})
onMounted(() => {
  if (state.value && !state.value.snapshot) void load()
})
</script>
