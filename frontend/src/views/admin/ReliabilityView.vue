<template>
  <AppLayout>
    <div class="admin-workspace space-y-5" data-testid="reliability-page">
      <AdminPageHeader
        :eyebrow="t('admin.reliability.eyebrow')"
        :title="t('admin.reliability.title')"
        :description="t('admin.reliability.description')"
      >
        <template #actions>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            :title="t('admin.reliability.refresh')"
            @click="loadData"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ loading ? t('admin.reliability.refreshing') : t('admin.reliability.refresh') }}
          </button>
        </template>
      </AdminPageHeader>

      <div
        v-if="loadError"
        class="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        role="alert"
      >
        <span class="flex items-start gap-2">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
          {{ loadError }}
        </span>
        <button type="button" class="btn btn-secondary text-xs" @click="loadData">
          {{ t('admin.reliability.retry') }}
        </button>
      </div>

      <AdminOverviewStrip :items="overviewItems" :loading="loading" />

      <section class="admin-surface px-4 py-4 sm:px-5" :aria-busy="loading">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="flex min-w-0 items-start gap-3">
            <span
              class="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-xl"
              :class="connectionToneClass"
              aria-hidden="true"
            >
              <Icon name="link" size="md" />
            </span>
            <div class="min-w-0">
              <h2 class="admin-section-heading">{{ t('admin.reliability.overview.title') }}</h2>
              <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">
                {{ t('admin.reliability.overview.description') }}
              </p>
            </div>
          </div>
          <span
            class="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium"
            :class="statusBadgeClass"
          >
            <span class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true" />
            {{ connectionStatusLabel }}
          </span>
        </div>

        <div class="mt-4 grid gap-3 border-t border-gray-100 pt-4 sm:grid-cols-2 dark:border-dark-700 lg:grid-cols-4">
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.connection') }}</p>
            <p class="mt-1 truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{{ connectionStatusLabel }}</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.connectionHint') }}</p>
          </div>
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.availableAccounts') }}</p>
            <p class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(accountAvailable) }}</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.availableAccountsHint', { total: formatCount(accountTotal) }) }}</p>
          </div>
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.limitedAccounts') }}</p>
            <p class="mt-1 text-sm font-semibold tabular-nums" :class="(accountLimited ?? 0) > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-gray-100'">{{ formatCount(accountLimited) }}</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.limitedAccountsHint') }}</p>
          </div>
          <div class="min-w-0">
            <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.updatedAt', { time: formattedUpdatedAt }) }}</p>
            <p class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(currentConcurrency) }}</p>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.overview.concurrencyHint') }}</p>
          </div>
        </div>

        <p class="mt-4 flex items-start gap-2 rounded-xl bg-gray-50 px-3 py-2.5 text-xs leading-5 text-gray-600 dark:bg-dark-800/70 dark:text-dark-300">
          <Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0 text-primary-600 dark:text-primary-400" />
          {{ t('admin.reliability.notAQualityGuarantee') }}
        </p>
      </section>

      <section class="admin-surface p-4 sm:p-5" data-testid="turn-state-status">
        <div class="flex items-start gap-3">
          <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-xl bg-teal-50 text-teal-700 dark:bg-teal-950/40 dark:text-teal-300" aria-hidden="true">
            <Icon name="sync" size="md" />
          </span>
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.reliability.turnState.title') }}</h2>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.description') }}</p>
          </div>
        </div>
        <dl class="mt-4 grid gap-3 border-t border-gray-100 pt-4 sm:grid-cols-2 dark:border-dark-700 lg:grid-cols-3 xl:grid-cols-5">
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.supported') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateSupported) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.http') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateHTTP) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.websocket') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ supportLabel(turnStateWebSocket) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.httpCrossAccountProtection') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-http-protection">{{ protectionLabel(turnStateHTTPCrossAccountProtection) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.websocketCrossAccountProtection') }}</dt>
            <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-websocket-protection">{{ protectionLabel(turnStateWebSocketCrossAccountProtection) }}</dd>
          </div>
        </dl>
        <div
          v-if="turnStateCollector"
          class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700"
          data-testid="turn-state-collector"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
              {{ t('admin.reliability.turnState.collectorTitle') }}
            </h3>
            <span
              class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium"
              :class="collectorStatusToneClass"
              data-testid="turn-state-collector-status"
            >
              {{ collectorStatusLabel }}
            </span>
          </div>
          <dl class="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorReady') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ collectorReadyLabel(collectorReady) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorInjection') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-injection">{{ collectorInjectionLabel(collectorInjection) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCollecting') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100">{{ collectorCollectingLabel(collectorCollecting) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorEntries') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.active_entries) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorCandidates') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.ready_candidates) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorObservations') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.observations) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorSuccesses') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums text-gray-900 dark:text-gray-100">{{ formatCount(turnStateCollector.successes) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorFailures') }}</dt>
              <dd class="mt-1 text-sm font-semibold tabular-nums" :class="numeric(turnStateCollector.failures) > 0 ? 'text-amber-700 dark:text-amber-300' : 'text-gray-900 dark:text-gray-100'">{{ formatCount(turnStateCollector.failures) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastSuccess') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-last-success">{{ formatTimestamp(turnStateCollector.last_success_at) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastFailure') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-gray-900 dark:text-gray-100" data-testid="turn-state-collector-last-failure">{{ formatTimestamp(turnStateCollector.last_failure_at) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.reliability.turnState.collectorLastError') }}</dt>
              <dd class="mt-1 text-sm font-semibold text-amber-700 dark:text-amber-300" data-testid="turn-state-collector-last-error">{{ collectorLastErrorLabel }}</dd>
            </div>
          </dl>
        </div>
        <p class="mt-4 flex items-start gap-2 rounded-xl bg-gray-50 px-3 py-2.5 text-xs leading-5 text-gray-600 dark:bg-dark-800/70 dark:text-dark-300">
          <Icon name="shield" size="sm" class="mt-0.5 shrink-0 text-teal-700 dark:text-teal-300" />
          {{ t('admin.reliability.turnState.privacy') }}
        </p>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  normalizeReliabilityStatus,
  reliabilityAPI,
  type ReliabilityStatusResponse,
  type ReliabilityStatusSummary,
} from '@/api/admin/reliability'
import { opsAPI } from '@/api/admin/ops'
import { formatNumber } from '@/utils/format'

const { t, locale } = useI18n()

const loading = ref(false)
const loadError = ref('')
const status = ref<ReliabilityStatusResponse | null>(null)
const lastUpdated = ref<string | null>(null)

const summary = computed<ReliabilityStatusSummary>(() => normalizeReliabilityStatus(status.value ?? {}))
const accountSummary = computed(() => summary.value.account_availability ?? summary.value.accounts ?? {})
const traffic = computed(() => summary.value.traffic ?? {})
const connection = computed(() => summary.value.connection ?? {})
const turnState = computed(() => summary.value.turn_state ?? {})
const turnStateCollector = computed(() => {
  const collector = turnState.value.collector
  return collector && typeof collector === 'object' ? collector : null
})

function numeric(value: unknown, fallback = 0): number {
  if (value === undefined || value === null || (typeof value === 'string' && value.trim() === '')) return fallback
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function optionalNumber(value: unknown): number | null {
  if (value === undefined || value === null || (typeof value === 'string' && value.trim() === '')) return null
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : null
}

function optionalBoolean(...values: unknown[]): boolean | null {
  const value = values.find((candidate) => typeof candidate === 'boolean')
  return typeof value === 'boolean' ? value : null
}

function diagnosticCodes(kind: 'source' | 'warning'): string[] {
  const raw = summary.value.diagnostics
  if (!raw || Array.isArray(raw)) return []
  return kind === 'source' ? (raw.source_errors || []) : (raw.warnings || [])
}

const accountAvailabilityUnavailable = computed(() => {
  if (!status.value || status.value.enabled === false) return true
  return diagnosticCodes('source').includes('account_availability_unavailable')
})
const accountTotal = computed(() => accountAvailabilityUnavailable.value ? null : optionalNumber(accountSummary.value.total ?? accountSummary.value.total_accounts))
const accountAvailable = computed(() => accountAvailabilityUnavailable.value ? null : optionalNumber(accountSummary.value.available ?? accountSummary.value.available_count))
const accountLimited = computed(() => accountAvailabilityUnavailable.value ? null : optionalNumber(accountSummary.value.cooling_down ?? accountSummary.value.limited ?? accountSummary.value.rate_limit_count))
const currentConcurrency = computed(() => {
  if (!status.value || status.value.enabled === false || diagnosticCodes('source').includes('concurrency_unavailable')) return null
  return optionalNumber(traffic.value.current_concurrency ?? traffic.value.concurrency)
})
const errorRate = computed(() => {
  if (!status.value || status.value.enabled === false || diagnosticCodes('source').includes('error_summary_unavailable')) return null
  const direct = optionalNumber(traffic.value.error_rate)
  if (direct !== null) return direct
  const runtimeError = summary.value.runtime?.error
  if (runtimeError?.available === false) return null
  return optionalNumber(runtimeError?.error_rate)
})
const turnStateSupported = computed(() => optionalBoolean(turnState.value.supported))
const turnStateHTTP = computed(() => optionalBoolean(turnState.value.http_enabled, turnState.value.http_supported))
const turnStateWebSocket = computed(() => optionalBoolean(turnState.value.websocket_enabled, turnState.value.websocket_supported))
const turnStateHTTPCrossAccountProtection = computed(() => optionalBoolean(turnState.value.http_cross_account_protection))
const turnStateWebSocketCrossAccountProtection = computed(() => optionalBoolean(turnState.value.websocket_cross_account_protection))
const collectorReady = computed(() => optionalBoolean(turnStateCollector.value?.ready))
const collectorInjection = computed(() => optionalBoolean(turnStateCollector.value?.injection_enabled))
const collectorCollecting = computed(() => optionalBoolean(turnStateCollector.value?.collecting))
const collectorStatusLabel = computed(() => {
  const status = String(turnStateCollector.value?.status || '').trim().toLowerCase()
  if (!status) return t('admin.reliability.turnState.collectorUnknown')
  const supported = new Set(['disabled', 'unavailable', 'idle', 'collecting', 'warming', 'ready', 'stale', 'cooldown', 'degraded', 'error'])
  if (!supported.has(status)) return t('admin.reliability.turnState.collectorUnknown')
  return t(`admin.reliability.turnState.collectorStatus.${status}`)
})
const collectorStatusToneClass = computed(() => {
  const status = String(turnStateCollector.value?.status || '').trim().toLowerCase()
  if (status === 'ready') return 'bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-300'
  if (status === 'collecting' || status === 'warming') return 'bg-teal-50 text-teal-700 dark:bg-teal-950/40 dark:text-teal-300'
  if (status === 'cooldown' || status === 'degraded' || status === 'stale') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
  if (status === 'error' || status === 'unavailable') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
})
const collectorLastErrorLabel = computed(() => {
  const code = String(turnStateCollector.value?.last_error_code || '').trim().toLowerCase()
  const supported = new Set([
    'probe_timeout', 'transport_error', 'upstream_401', 'upstream_403', 'upstream_429', 'upstream_5xx',
    'model_capacity', 'upstream_rate_limited', 'response_failed', 'response_model_mismatch', 'invalid_state',
    'invalid_model', 'incomplete_stream', 'state_time_rejected', 'cooldown', 'cancelled', 'disabled', 'unavailable', 'other',
  ])
  if (!supported.has(code)) return t('admin.reliability.turnState.collectorUnknown')
  return t(`admin.reliability.turnState.collectorErrors.${code}`)
})

const connectionStatus = computed(() => {
  if (connection.value.status) return String(connection.value.status).toLowerCase()
  if (connection.value.reachable === true) return 'healthy'
  if (connection.value.reachable === false) return 'unavailable'
  return 'unknown'
})

const connectionStatusLabel = computed(() => {
  const labels: Record<string, string> = {
    healthy: t('admin.reliability.overview.connectionReachable'),
    degraded: t('admin.reliability.overview.connectionDegraded'),
    unavailable: t('admin.reliability.overview.connectionUnavailable'),
    unknown: t('admin.reliability.overview.connectionUnknown'),
  }
  return labels[connectionStatus.value] || connectionStatus.value
})

const connectionToneClass = computed(() => {
  if (connectionStatus.value === 'healthy') return 'bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-300'
  if (connectionStatus.value === 'degraded') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
  if (connectionStatus.value === 'unavailable') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
})

const statusBadgeClass = computed(() => {
  if (connectionStatus.value === 'healthy') return 'bg-green-50 text-green-700 dark:bg-green-950/40 dark:text-green-300'
  if (connectionStatus.value === 'degraded') return 'bg-amber-50 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300'
  if (connectionStatus.value === 'unavailable') return 'bg-red-50 text-red-700 dark:bg-red-950/40 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-dark-300'
})

const formattedUpdatedAt = computed(() => {
  const value = status.value?.timestamp || status.value?.generated_at || lastUpdated.value
  if (!value) return t('admin.reliability.unavailable')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(date)
})

const overviewItems = computed(() => [
  { label: t('admin.reliability.overview.availableAccounts'), value: formatCount(accountAvailable.value), hint: t('admin.reliability.overview.availableAccountsHint', { total: formatCount(accountTotal.value) }), tone: accountAvailable.value === null ? 'default' as const : 'positive' as const },
  { label: t('admin.reliability.overview.limitedAccounts'), value: formatCount(accountLimited.value), hint: t('admin.reliability.overview.limitedAccountsHint'), tone: (accountLimited.value ?? 0) > 0 ? 'warning' as const : 'default' as const },
  { label: t('admin.reliability.overview.concurrency'), value: formatCount(currentConcurrency.value), hint: t('admin.reliability.overview.concurrencyHint') },
  { label: t('admin.reliability.overview.errorRate'), value: errorRate.value === null ? t('admin.reliability.unavailable') : `${errorRate.value.toFixed(2)}%`, hint: t('admin.reliability.overview.errorRateHint'), tone: errorRate.value !== null && errorRate.value > 0 ? 'warning' as const : 'default' as const },
])

function formatCount(value: unknown): string {
  if (value === null || value === undefined || (typeof value === 'string' && value.trim() === '')) return t('admin.reliability.unavailable')
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? formatNumber(parsed) : t('admin.reliability.unavailable')
}

function formatTimestamp(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return t('admin.reliability.unavailable')
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return t('admin.reliability.unavailable')
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(date)
}

function supportLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.supportedValue') : t('admin.reliability.turnState.unsupportedValue')
}

function collectorReadyLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorReadyValue') : t('admin.reliability.turnState.collectorNotReadyValue')
}

function collectorInjectionLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorInjectionEnabled') : t('admin.reliability.turnState.collectorInjectionDisabled')
}

function collectorCollectingLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.collectorCollectingValue') : t('admin.reliability.turnState.collectorIdleValue')
}

function protectionLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.protectedValue') : t('admin.reliability.turnState.unprotectedValue')
}

async function loadLegacyStatus() {
  const results = await Promise.allSettled([
    opsAPI.getAccountAvailabilityStats(),
    opsAPI.getConcurrencyStats(),
    opsAPI.getDashboardOverview({ time_range: '5m' }),
    opsAPI.getRealtimeTrafficSummary('5m'),
  ])
  const availability = results[0].status === 'fulfilled' ? results[0].value : null
  const concurrency = results[1].status === 'fulfilled' ? results[1].value : null
  const dashboard = results[2].status === 'fulfilled' ? results[2].value : null
  const realtime = results[3].status === 'fulfilled' ? results[3].value : null
  const successCount = results.filter((result) => result.status === 'fulfilled').length
  const failureCount = results.length - successCount
  if (successCount === 0) {
    status.value = null
    return { successCount, failureCount, timestamp: null as string | null }
  }
  const rows = availability ? Object.values(availability.platform || {}) : []
  const platforms = concurrency ? Object.values(concurrency.platform || {}) : []
  const total = availability ? rows.reduce((sum, item) => sum + numeric(item.total_accounts), 0) : undefined
  const available = availability ? rows.reduce((sum, item) => sum + numeric(item.available_count), 0) : undefined
  const limited = availability ? rows.reduce((sum, item) => sum + numeric(item.rate_limit_count), 0) : undefined
  const current = concurrency ? platforms.reduce((sum, item) => sum + numeric(item.current_in_use), 0) : undefined
  const queued = concurrency ? platforms.reduce((sum, item) => sum + numeric(item.waiting_in_queue), 0) : undefined
  const hasTraffic = Boolean(concurrency || dashboard || realtime)
  const timestamp = availability?.timestamp || concurrency?.timestamp || realtime?.timestamp || null
  status.value = {
    enabled: availability?.enabled ?? concurrency?.enabled,
    timestamp: timestamp ?? undefined,
    summary: {
      account_availability: availability ? { total, available, limited } : undefined,
      traffic: hasTraffic ? {
        current_concurrency: current,
        queued_requests: queued,
        qps: realtime?.summary?.qps?.current,
        tps: realtime?.summary?.tps?.current,
        error_rate: dashboard ? numeric(dashboard.error_rate) * 100 : undefined,
        upstream_429: dashboard?.upstream_429_count,
        upstream_529: dashboard?.upstream_529_count,
      } : undefined,
      connection: { status: 'unknown' },
      diagnostics: failureCount > 0 ? {
        source_errors: results.flatMap((result, index) => result.status === 'rejected' ? [`legacy_source_${index}_unavailable`] : []),
        warnings: [],
      } : undefined,
    },
  }
  return { successCount, failureCount, timestamp }
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  let statusSourceSucceeded = false
  let legacyFailureCount = 0
  let collectedAt: string | null = null

  try {
    const nextStatus = await reliabilityAPI.getStatus()
    status.value = nextStatus
    statusSourceSucceeded = true
    collectedAt = nextStatus.timestamp || nextStatus.generated_at || null
  } catch {
    try {
      const legacy = await loadLegacyStatus()
      statusSourceSucceeded = legacy.successCount > 0
      legacyFailureCount = legacy.failureCount
      collectedAt = legacy.timestamp
    } catch {
      status.value = null
    }
  }

  if (!status.value || !statusSourceSucceeded) loadError.value = t('admin.reliability.loadFailed')
  else if (legacyFailureCount > 0) loadError.value = t('admin.reliability.partialData')
  if (statusSourceSucceeded) lastUpdated.value = collectedAt || new Date().toISOString()
  loading.value = false
}

onMounted(loadData)
</script>
