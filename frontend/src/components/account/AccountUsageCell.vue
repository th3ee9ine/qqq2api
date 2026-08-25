<template>
  <div ref="rootRef" v-if="showUsageWindows">
    <!-- Anthropic OAuth and Setup Token accounts: fetch real usage data -->
    <template v-if="isAnthropicWindowAccount">
      <!-- Loading state -->
      <div v-if="effectiveLoading" class="space-y-1.5">
        <!-- OAuth: 3 rows, Setup Token: 1 row -->
        <div data-testid="usage-loading-row" class="flex items-center gap-1">
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        </div>
        <template v-if="account.type === 'oauth'">
          <div data-testid="usage-loading-row" class="flex items-center gap-1">
            <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
            <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
            <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
          </div>
          <div data-testid="usage-loading-row" class="flex items-center gap-1">
            <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
            <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
            <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
          </div>
        </template>
      </div>

      <!-- Error state -->
      <div v-else-if="effectiveError" class="text-xs text-red-500">
        {{ effectiveError }}
      </div>

      <!-- Usage data -->
      <div v-else-if="effectiveUsage" class="space-y-1">
        <!-- API error (degraded response) -->
        <div v-if="effectiveUsage.error" class="text-xs text-amber-600 dark:text-amber-400 truncate max-w-[200px]" :title="effectiveUsage.error">
          {{ effectiveUsage.error }}
        </div>
        <!-- 5h Window -->
        <UsageProgressBar
          v-if="effectiveUsage.five_hour"
          label="5h"
          :utilization="effectiveUsage.five_hour.utilization"
          :resets-at="effectiveUsage.five_hour.resets_at"
          :window-stats="effectiveUsage.five_hour.window_stats"
          color="indigo"
        />

        <!-- 7d Window (OAuth only) -->
        <UsageProgressBar
          v-if="effectiveUsage.seven_day"
          label="7d"
          :utilization="effectiveUsage.seven_day.utilization"
          :resets-at="effectiveUsage.seven_day.resets_at"
          color="emerald"
        />

        <!-- 7d Sonnet Window (OAuth only) -->
        <UsageProgressBar
          v-if="effectiveUsage.seven_day_sonnet"
          label="7d S"
          :utilization="effectiveUsage.seven_day_sonnet.utilization"
          :resets-at="effectiveUsage.seven_day_sonnet.resets_at"
          color="purple"
        />

        <!-- 7d Fable Window (7d_oi) -->
        <UsageProgressBar
          v-if="effectiveUsage.seven_day_fable"
          label="7d F"
          :utilization="effectiveUsage.seven_day_fable.utilization"
          :resets-at="effectiveUsage.seven_day_fable.resets_at"
          color="amber"
        />

        <!-- Passive sampling label + active query button -->
        <div class="flex items-center gap-1.5 mt-0.5">
          <span v-if="effectiveUsage.source === 'passive'" class="text-[9px] text-gray-400 dark:text-gray-500 italic">
            {{ t('admin.accounts.usageWindow.passiveSampled') }}
          </span>
          <button
            type="button"
            class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[9px] font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/30 transition-colors"
            :disabled="activeQueryLoading"
            @click="loadActiveUsage"
          >
            <svg class="h-2.5 w-2.5" :class="{ 'animate-spin': activeQueryLoading }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            {{ t('admin.accounts.usageWindow.activeQuery') }}
          </button>
        </div>
      </div>

      <!-- No data yet -->
      <div v-else class="space-y-1">
        <div class="text-xs text-gray-400">-</div>
      </div>
    </template>

    <!-- OpenAI OAuth accounts: single source from /usage API -->
    <template v-else-if="isOpenAIOAuth">
      <div v-if="hasOpenAIUsageFallback" class="space-y-1">
        <UsageProgressBar
          v-if="effectiveUsage?.five_hour"
          label="5h"
          :utilization="effectiveUsage.five_hour.utilization"
          :resets-at="effectiveUsage.five_hour.resets_at"
          :window-stats="effectiveUsage.five_hour.window_stats"
          :show-now-when-idle="true"
          color="indigo"
        />
        <UsageProgressBar
          v-if="effectiveUsage?.seven_day"
          label="7d"
          :utilization="effectiveUsage.seven_day.utilization"
          :resets-at="effectiveUsage.seven_day.resets_at"
          :window-stats="effectiveUsage.seven_day.window_stats"
          :show-now-when-idle="true"
          color="emerald"
        />
        <OpenAIQuotaResetCell :account="account" @account-updated="handleQuotaResetAccountUpdated">
          <template #pre-actions>
            <button
              type="button"
              class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/30 transition-colors disabled:cursor-not-allowed disabled:opacity-50"
              :disabled="activeQueryLoading"
              @click="loadActiveUsage"
            >
              <svg class="h-2.5 w-2.5" :class="{ 'animate-spin': activeQueryLoading }" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              {{ t('admin.accounts.usageWindow.activeQuery') }}
            </button>
          </template>
        </OpenAIQuotaResetCell>
      </div>
      <div v-else-if="effectiveLoading" class="space-y-1.5">
        <div data-testid="usage-loading-row" class="flex items-center gap-1">
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        </div>
        <div data-testid="usage-loading-row" class="flex items-center gap-1">
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-1.5 w-8 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"></div>
          <div class="h-3 w-[32px] animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        </div>
      </div>
      <div v-else>
        <div class="text-xs text-gray-400">-</div>
        <OpenAIQuotaResetCell :account="account" class="mt-1" @account-updated="handleQuotaResetAccountUpdated" />
      </div>
    </template>
  </div>

  <!-- Non-OAuth/Setup-Token accounts -->
  <div ref="rootRef" v-else>
    <!-- Key/Bedrock accounts: show today stats + optional quota bars -->
    <div v-if="isSupportedPlatform" class="space-y-1">
      <OllamaCloudUsageCell
        v-if="account.ollama_cloud_usage?.eligible"
        :account="account"
        @updated="handleOllamaCloudUsageUpdated"
      />
      <!-- Today stats row (requests, tokens, cost) -->
      <div v-if="todayStats" class="mb-0.5 flex items-center">
        <div class="flex items-center gap-1.5 text-[9px] text-gray-500 dark:text-gray-400">
          <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">{{ formatKeyRequests }} req</span>
          <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800">{{ formatKeyTokens }}</span>
          <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-gray-800" :title="t('usage.accountBilled')">A ${{ formatKeyCost }}</span>
        </div>
      </div>
      <!-- Loading skeleton for today stats -->
      <div v-else-if="todayStatsLoading" class="mb-0.5 flex items-center gap-1">
        <div class="h-3 w-10 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-3 w-8 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
        <div class="h-3 w-12 animate-pulse rounded bg-gray-200 dark:bg-gray-700"></div>
      </div>

      <!-- API Key accounts with quota limits: show progress bars -->
      <UsageProgressBar v-if="quotaDailyBar" label="1d" :utilization="quotaDailyBar.utilization" :resets-at="quotaDailyBar.resetsAt" color="indigo" />
      <UsageProgressBar v-if="quotaWeeklyBar" label="7d" :utilization="quotaWeeklyBar.utilization" :resets-at="quotaWeeklyBar.resetsAt" color="emerald" />
      <UsageProgressBar v-if="quotaTotalBar" label="total" :utilization="quotaTotalBar.utilization" color="purple" />

      <!-- No data at all -->
      <div v-if="!todayStats && !todayStatsLoading && !hasApiKeyQuota && !account.ollama_cloud_usage?.eligible" class="text-xs text-gray-400">-</div>
    </div>
    <div v-else class="text-xs text-gray-400">-</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Account, AccountUsageInfo, WindowStats } from '@/types'
import { formatCompactNumber } from '@/utils/format'
import { buildOpenAIUsageRefreshKey } from '@/utils/accountUsageRefresh'
import { enqueueUsageRequest } from '@/utils/usageLoadQueue'
import UsageProgressBar from './UsageProgressBar.vue'
import OpenAIQuotaResetCell from './OpenAIQuotaResetCell.vue'
import OllamaCloudUsageCell from './OllamaCloudUsageCell.vue'

const CACHE_TTL_MS = 5 * 60 * 1000
const QUOTA_RESET_SUPPRESS_MS = 5 * 1000
const DESKTOP_VIEWPORT_QUERY = '(min-width: 768px)'
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
const isDesktopViewport = ref(
  typeof window === 'undefined' || typeof window.matchMedia !== 'function'
    ? true
    : window.matchMedia(DESKTOP_VIEWPORT_QUERY).matches
)
const hasEnteredViewport = ref(isDesktopViewport.value)
const pendingAutoLoad = ref(false)
const pendingAutoLoadForce = ref(false)
const pendingAutoLoadSource = ref<'passive' | 'active' | undefined>(undefined)
const localUsage = ref<AccountUsageInfo | null>(null)
const localError = ref<string | null>(null)
const localLoading = ref(false)
const activeQueryLoading = ref(false)
const unmounted = ref(false)
const suppressRefreshUntil = ref(0)
let desktopViewportMediaQuery: MediaQueryList | null = null
let desktopViewportListener: ((event: MediaQueryListEvent) => void) | null = null
let visibilityObserver: IntersectionObserver | null = null

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
const showUsageWindows = computed(() => usesUpstreamUsageWindows.value)
const isBatchManaged = computed(() => typeof props.requestBatchedUsage === 'function')
const effectiveUsage = computed(() => isBatchManaged.value ? props.batchedUsage : localUsage.value)
const effectiveError = computed(() => isBatchManaged.value ? props.batchedUsageError : localError.value)
const effectiveLoading = computed(() => isBatchManaged.value ? props.batchedUsageLoading : localLoading.value)
const hasOpenAIUsageFallback = computed(() =>
  isOpenAIOAuth.value && Boolean(effectiveUsage.value?.five_hour || effectiveUsage.value?.seven_day)
)

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

function flushPendingAutoLoad() {
  if (!pendingAutoLoad.value) return
  const force = pendingAutoLoadForce.value
  const source = pendingAutoLoadSource.value
  pendingAutoLoad.value = false
  pendingAutoLoadForce.value = false
  pendingAutoLoadSource.value = undefined
  void loadUsage(force, source)
}

function requestAutoLoad(force = false, source?: 'passive' | 'active') {
  if (!usesUpstreamUsageWindows.value) return
  if (!isDesktopViewport.value && !hasEnteredViewport.value) {
    pendingAutoLoad.value = true
    pendingAutoLoadForce.value = pendingAutoLoadForce.value || force
    pendingAutoLoadSource.value = source
    return
  }
  void loadUsage(force, source)
}

function detachVisibilityObserver() {
  visibilityObserver?.disconnect()
  visibilityObserver = null
}

function attachVisibilityObserver() {
  detachVisibilityObserver()
  if (isDesktopViewport.value || hasEnteredViewport.value || !usesUpstreamUsageWindows.value) return
  if (typeof IntersectionObserver === 'undefined' || !rootRef.value) {
    hasEnteredViewport.value = true
    flushPendingAutoLoad()
    return
  }
  visibilityObserver = new IntersectionObserver((entries) => {
    if (!entries.some(entry => entry.isIntersecting)) return
    hasEnteredViewport.value = true
    detachVisibilityObserver()
    flushPendingAutoLoad()
  }, {
    root: null,
    rootMargin: '200px 0px',
    threshold: 0.01
  })
  visibilityObserver.observe(rootRef.value)
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
function quotaResetAt(dimension: 'daily' | 'weekly'): string | null {
  const exposedResetAt = dimension === 'daily'
    ? props.account.quota_daily_reset_at
    : props.account.quota_weekly_reset_at
  if (exposedResetAt) return exposedResetAt

  const extra = props.account.extra as Record<string, unknown> | undefined
  const mode = extra?.[`quota_${dimension}_reset_mode`] as string | undefined
  if (mode === 'fixed') {
    const fixedResetAt = extra?.[`quota_${dimension}_reset_at`]
    return typeof fixedResetAt === 'string' && fixedResetAt ? fixedResetAt : null
  }

  const start = extra?.[`quota_${dimension}_start`]
  if (typeof start !== 'string' || !start) return null
  const startDate = new Date(start)
  if (Number.isNaN(startDate.getTime())) return null
  const periodMs = dimension === 'daily'
    ? 24 * 60 * 60 * 1000
    : 7 * 24 * 60 * 60 * 1000
  return new Date(startDate.getTime() + periodMs).toISOString()
}
function quotaBar(used: number, limit: number, resetAt: string | null = null): QuotaBarInfo {
  return { utilization: limit > 0 ? (used / limit) * 100 : 0, resetsAt: resetAt }
}
const hasApiKeyQuota = computed(() =>
  (props.account.type === 'apikey' || props.account.type === 'bedrock') &&
  ((props.account.quota_daily_limit ?? 0) > 0 ||
   (props.account.quota_weekly_limit ?? 0) > 0 ||
   (props.account.quota_limit ?? 0) > 0)
)
const quotaDailyBar = computed(() => {
  const limit = props.account.quota_daily_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_daily_used ?? 0, limit, quotaResetAt('daily')) : null
})
const quotaWeeklyBar = computed(() => {
  const limit = props.account.quota_weekly_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_weekly_used ?? 0, limit, quotaResetAt('weekly')) : null
})
const quotaTotalBar = computed(() => {
  const limit = props.account.quota_limit ?? 0
  return limit > 0 ? quotaBar(props.account.quota_used ?? 0, limit) : null
})

const formatKeyRequests = computed(() => props.todayStats ? formatCompactNumber(props.todayStats.requests, { allowBillions: false }) : '')
const formatKeyTokens = computed(() => props.todayStats ? formatCompactNumber(props.todayStats.tokens) : '')
const formatKeyCost = computed(() => props.todayStats?.cost.toFixed(2) ?? '0.00')
function handleQuotaResetAccountUpdated(account: Account) {
  suppressRefreshUntil.value = Date.now() + QUOTA_RESET_SUPPRESS_MS
  emit('account-updated', account)
}
function handleOllamaCloudUsageUpdated(state: NonNullable<Account['ollama_cloud_usage']>) {
  emit('account-updated', { ...props.account, ollama_cloud_usage: state })
}
onMounted(() => {
  if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
    desktopViewportMediaQuery = window.matchMedia(DESKTOP_VIEWPORT_QUERY)
    isDesktopViewport.value = desktopViewportMediaQuery.matches
    hasEnteredViewport.value = desktopViewportMediaQuery.matches
    desktopViewportListener = event => {
      isDesktopViewport.value = event.matches
      if (event.matches) {
        hasEnteredViewport.value = true
        detachVisibilityObserver()
        flushPendingAutoLoad()
      } else {
        hasEnteredViewport.value = false
        attachVisibilityObserver()
      }
    }
    if (typeof desktopViewportMediaQuery.addEventListener === 'function') {
      desktopViewportMediaQuery.addEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.addListener(desktopViewportListener)
    }
  }
  attachVisibilityObserver()
  requestAutoLoad(false, isAnthropicWindowAccount.value ? 'passive' : undefined)
})
onBeforeUnmount(() => {
  unmounted.value = true
  detachVisibilityObserver()
  if (desktopViewportMediaQuery && desktopViewportListener) {
    if (typeof desktopViewportMediaQuery.removeEventListener === 'function') {
      desktopViewportMediaQuery.removeEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.removeListener(desktopViewportListener)
    }
  }
  desktopViewportMediaQuery = null
  desktopViewportListener = null
})

watch(() => props.manualRefreshToken, (value, previous) => {
  if (value !== previous) {
    void loadUsage(true, isAnthropicWindowAccount.value ? 'passive' : undefined)
  }
})
const openAIUsageRefreshKey = computed(() => buildOpenAIUsageRefreshKey(props.account))
watch(openAIUsageRefreshKey, (value, previous) => {
  if (!previous || value === previous || !isOpenAIOAuth.value) return
  if (Date.now() < suppressRefreshUntil.value) {
    suppressRefreshUntil.value = 0
    return
  }
  if (isBatchManaged.value) {
    props.requestBatchedUsage?.(props.account, { force: true })
    return
  }
  usageCache.delete(props.account.id)
  requestAutoLoad(true)
})
watch(() => [props.account.id, props.account.updated_at] as const, ([id], [previousID]) => {
  // OpenAI OAuth accounts are refreshed by the more complete usage key above;
  // skipping this generic watcher avoids duplicate quota requests.
  if (isOpenAIOAuth.value) return
  if (id === previousID && Date.now() < suppressRefreshUntil.value) return
  requestAutoLoad(true)
})
</script>
