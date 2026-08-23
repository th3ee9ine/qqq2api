<template>
  <div ref="rootRef">
    <template v-if="usesUpstreamUsageWindows">
      <div v-if="effectiveLoading" class="space-y-1.5">
        <div v-for="index in loadingRows" :key="index" class="flex items-center gap-1">
          <div class="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
          <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700" />
          <div class="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
        </div>
      </div>
      <div v-else-if="effectiveError" class="max-w-[200px] truncate text-xs text-red-500">
        {{ effectiveError }}
      </div>
      <div v-else-if="effectiveUsage" class="space-y-1">
        <div
          v-if="effectiveUsage.error"
          class="max-w-[200px] truncate text-xs text-amber-600 dark:text-amber-400"
          :title="effectiveUsage.error"
        >
          {{ effectiveUsage.error }}
        </div>
        <UsageProgressBar
          v-if="effectiveUsage.five_hour"
          label="5h"
          :utilization="effectiveUsage.five_hour.utilization"
          :resets-at="effectiveUsage.five_hour.resets_at"
          :window-stats="effectiveUsage.five_hour.window_stats"
          :show-now-when-idle="isOpenAIOAuth"
          color="indigo"
        />
        <UsageProgressBar
          v-if="effectiveUsage.seven_day"
          label="7d"
          :utilization="effectiveUsage.seven_day.utilization"
          :resets-at="effectiveUsage.seven_day.resets_at"
          :window-stats="effectiveUsage.seven_day.window_stats"
          :show-now-when-idle="isOpenAIOAuth"
          color="emerald"
        />
        <UsageProgressBar
          v-if="effectiveUsage.seven_day_sonnet"
          label="7d S"
          :utilization="effectiveUsage.seven_day_sonnet.utilization"
          :resets-at="effectiveUsage.seven_day_sonnet.resets_at"
          color="purple"
        />
        <UsageProgressBar
          v-if="effectiveUsage.seven_day_fable"
          label="7d F"
          :utilization="effectiveUsage.seven_day_fable.utilization"
          :resets-at="effectiveUsage.seven_day_fable.resets_at"
          color="amber"
        />
        <div v-if="isAnthropicWindowAccount" class="mt-0.5 flex items-center gap-1.5">
          <span v-if="effectiveUsage.source === 'passive'" class="text-[9px] italic text-gray-400">
            {{ t('admin.accounts.usageWindow.passiveSampled') }}
          </span>
          <button
            type="button"
            class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[9px] font-medium text-blue-600 hover:bg-blue-50 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
            :disabled="activeQueryLoading"
            @click="loadActiveUsage"
          >
            <Icon name="refresh" size="xs" :class="activeQueryLoading && 'animate-spin'" />
            {{ t('admin.accounts.usageWindow.activeQuery') }}
          </button>
        </div>
        <OpenAIQuotaResetCell
          v-if="isOpenAIOAuth"
          :account="account"
          @account-updated="handleQuotaResetAccountUpdated"
        >
          <template #pre-actions>
            <button
              type="button"
              class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 hover:bg-blue-50 disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
              :disabled="activeQueryLoading"
              @click="loadActiveUsage"
            >
              <Icon name="refresh" size="xs" :class="activeQueryLoading && 'animate-spin'" />
              {{ t('admin.accounts.usageWindow.activeQuery') }}
            </button>
          </template>
        </OpenAIQuotaResetCell>
      </div>
      <div v-else class="space-y-1">
        <span class="text-xs text-gray-400">-</span>
        <OpenAIQuotaResetCell
          v-if="isOpenAIOAuth"
          :account="account"
          @account-updated="handleQuotaResetAccountUpdated"
        />
      </div>
    </template>

    <template v-else-if="isSupportedPlatform">
      <div class="space-y-1">
        <OllamaCloudUsageCell
          v-if="account.ollama_cloud_usage?.eligible"
          :account="account"
          @updated="handleOllamaCloudUsageUpdated"
        />
        <div v-if="todayStats" class="mb-0.5 flex items-center">
          <div class="flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-gray-400">
            <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">{{ formatKeyRequests }} req</span>
            <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">{{ formatKeyTokens }}</span>
            <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800" :title="t('usage.accountBilled')">A ${{ formatKeyCost }}</span>
            <span
              v-if="todayStats.user_cost != null"
              class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800"
              :title="t('usage.userBilled')"
            >U ${{ formatKeyUserCost }}</span>
          </div>
        </div>
        <div v-else-if="todayStatsLoading" class="mb-0.5 flex items-center gap-1">
          <div class="h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
          <div class="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
          <div class="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700" />
        </div>
        <UsageProgressBar v-if="quotaDailyBar" label="1d" :utilization="quotaDailyBar.utilization" :resets-at="quotaDailyBar.resetsAt" color="indigo" />
        <UsageProgressBar v-if="quotaWeeklyBar" label="7d" :utilization="quotaWeeklyBar.utilization" :resets-at="quotaWeeklyBar.resetsAt" color="emerald" />
        <UsageProgressBar v-if="quotaTotalBar" label="total" :utilization="quotaTotalBar.utilization" color="purple" />
        <span v-if="!todayStats && !todayStatsLoading && !hasApiKeyQuota && !account.ollama_cloud_usage?.eligible" class="text-xs text-gray-400">-</span>
      </div>
    </template>

    <span v-else class="text-xs text-gray-400">-</span>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account, AccountUsageInfo, WindowStats } from '@/types'
import { formatCompactNumber } from '@/utils/format'
import { enqueueUsageRequest } from '@/utils/usageLoadQueue'
import Icon from '@/components/icons/Icon.vue'
import UsageProgressBar from './UsageProgressBar.vue'
import OpenAIQuotaResetCell from './OpenAIQuotaResetCell.vue'
import OllamaCloudUsageCell from './OllamaCloudUsageCell.vue'

const CACHE_TTL_MS = 5 * 60 * 1000
const QUOTA_RESET_SUPPRESS_MS = 5 * 1000
const usageCache = new Map<number, { data: AccountUsageInfo; at: number }>()

const props = withDefaults(defineProps<{
  account: Account
  todayStats?: WindowStats | null
  todayStatsLoading?: boolean
  manualRefreshToken?: number
  batchedUsage?: AccountUsageInfo | null
  batchedUsageError?: string | null
  batchedUsageLoading?: boolean
  requestBatchedUsage?: ((account: Account, options?: { force?: boolean }) => void) | null
}>(), {
  todayStats: null,
  todayStatsLoading: false,
  manualRefreshToken: 0,
  batchedUsage: null,
  batchedUsageError: null,
  batchedUsageLoading: false,
  requestBatchedUsage: null
})

const emit = defineEmits<{
  'account-updated': [account: Account]
  'usage-loaded': [usage: AccountUsageInfo]
}>()

const { t } = useI18n()
const rootRef = ref<HTMLElement | null>(null)
const localUsage = ref<AccountUsageInfo | null>(null)
const localError = ref<string | null>(null)
const localLoading = ref(false)
const activeQueryLoading = ref(false)
const unmounted = ref(false)
const suppressRefreshUntil = ref(0)

const isSupportedPlatform = computed(() =>
  props.account.platform === 'anthropic' || props.account.platform === 'openai'
)
const isAnthropicWindowAccount = computed(() =>
  props.account.platform === 'anthropic' &&
  (props.account.type === 'oauth' || props.account.type === 'setup-token')
)
const isOpenAIOAuth = computed(() =>
  props.account.platform === 'openai' && props.account.type === 'oauth'
)
const usesUpstreamUsageWindows = computed(() =>
  isAnthropicWindowAccount.value || isOpenAIOAuth.value
)
const isBatchManaged = computed(() => typeof props.requestBatchedUsage === 'function')
const effectiveUsage = computed(() => isBatchManaged.value ? props.batchedUsage : localUsage.value)
const effectiveError = computed(() => isBatchManaged.value ? props.batchedUsageError : localError.value)
const effectiveLoading = computed(() => isBatchManaged.value ? props.batchedUsageLoading : localLoading.value)
const loadingRows = computed(() => isAnthropicWindowAccount.value && props.account.type === 'oauth' ? 3 : 2)

watch(effectiveUsage, usage => {
  if (usage) emit('usage-loaded', usage)
}, { immediate: true })

async function loadUsage(force = false, source?: 'passive' | 'active') {
  if (!usesUpstreamUsageWindows.value) return
  if (isBatchManaged.value && source !== 'active') {
    props.requestBatchedUsage?.(props.account, { force })
    return
  }
  const cached = usageCache.get(props.account.id)
  if (!force && cached && Date.now() - cached.at < CACHE_TTL_MS) {
    localUsage.value = cached.data
    return
  }
  localLoading.value = true
  localError.value = null
  try {
    const result = await enqueueUsageRequest(
      props.account,
      () => adminAPI.accounts.getUsage(props.account.id, source, force)
    )
    if (unmounted.value) return
    localUsage.value = result
    usageCache.set(props.account.id, { data: result, at: Date.now() })
  } catch (error) {
    if (!unmounted.value) localError.value = t('common.error')
  } finally {
    if (!unmounted.value) localLoading.value = false
  }
}

async function loadActiveUsage() {
  activeQueryLoading.value = true
  try {
    const result = await adminAPI.accounts.getUsage(props.account.id, 'active', true)
    if (!unmounted.value) {
      localUsage.value = result
      usageCache.set(props.account.id, { data: result, at: Date.now() })
      emit('usage-loaded', result)
    }
  } finally {
    activeQueryLoading.value = false
  }
}

interface QuotaBarInfo { utilization: number; resetsAt: string | null }
function quotaBar(used: number, limit: number, resetAt?: string | null): QuotaBarInfo {
  return { utilization: limit > 0 ? (used / limit) * 100 : 0, resetsAt: resetAt || null }
}
const hasApiKeyQuota = computed(() =>
  (props.account.type === 'apikey' || props.account.type === 'bedrock') &&
  ((props.account.quota_daily_limit ?? 0) > 0 ||
   (props.account.quota_weekly_limit ?? 0) > 0 ||
   (props.account.quota_limit ?? 0) > 0)
)
const quotaDailyBar = computed(() => {
  const limit = props.account.quota_daily_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_daily_used ?? 0, limit, props.account.quota_daily_reset_at) : null
})
const quotaWeeklyBar = computed(() => {
  const limit = props.account.quota_weekly_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_weekly_used ?? 0, limit, props.account.quota_weekly_reset_at) : null
})
const quotaTotalBar = computed(() => {
  const limit = props.account.quota_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_used ?? 0, limit) : null
})

const formatKeyRequests = computed(() => props.todayStats ? formatCompactNumber(props.todayStats.requests, { allowBillions: false }) : '')
const formatKeyTokens = computed(() => props.todayStats ? formatCompactNumber(props.todayStats.tokens) : '')
const formatKeyCost = computed(() => props.todayStats?.cost.toFixed(2) ?? '0.00')
const formatKeyUserCost = computed(() => props.todayStats?.user_cost?.toFixed(2) ?? '0.00')

function handleQuotaResetAccountUpdated(account: Account) {
  suppressRefreshUntil.value = Date.now() + QUOTA_RESET_SUPPRESS_MS
  emit('account-updated', account)
}
function handleOllamaCloudUsageUpdated(state: NonNullable<Account['ollama_cloud_usage']>) {
  emit('account-updated', { ...props.account, ollama_cloud_usage: state })
}

onMounted(() => {
  void loadUsage(false, isAnthropicWindowAccount.value ? 'passive' : undefined)
})
onBeforeUnmount(() => { unmounted.value = true })

watch(() => props.manualRefreshToken, (value, previous) => {
  if (value !== previous) void loadUsage(true)
})
watch(() => [props.account.id, props.account.updated_at] as const, ([id], [previousID]) => {
  if (id === previousID && Date.now() < suppressRefreshUntil.value) return
  void loadUsage(true)
})
</script>
