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
            :disabled="pageBusy"
            :title="t('admin.reliability.refresh')"
            @click="loadPageData"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': pageLoading }" />
            {{ pageLoading ? t('admin.reliability.refreshing') : t('admin.reliability.refresh') }}
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
        <div
          class="mt-4 border-t border-gray-100 pt-4 dark:border-dark-700"
          data-testid="turn-state-settings"
        >
          <h3 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-400">
            {{ t('admin.reliability.turnState.settingsTitle') }}
          </h3>
          <div class="mt-3 grid gap-x-6 gap-y-3 sm:grid-cols-2">
            <div class="flex min-h-14 items-center justify-between gap-4">
              <div class="min-w-0">
                <p id="turn-state-probe-label" class="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {{ t('admin.reliability.turnState.probeToggle') }}
                </p>
                <p class="mt-1 min-h-5 text-xs text-gray-500 dark:text-dark-400">
                  {{ settingStatusLabel('probe', turnStateProbeEnabled) }}
                </p>
              </div>
              <Toggle
                data-testid="turn-state-probe-toggle"
                :model-value="turnStateProbeEnabled"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                aria-labelledby="turn-state-probe-label"
                @update:model-value="saveTurnStateSetting('probe', $event)"
              />
            </div>
            <div class="flex min-h-14 items-center justify-between gap-4">
              <div class="min-w-0">
                <p id="turn-state-injection-label" class="text-sm font-medium text-gray-900 dark:text-gray-100">
                  {{ t('admin.reliability.turnState.injectionToggle') }}
                </p>
                <p class="mt-1 min-h-5 text-xs text-gray-500 dark:text-dark-400">
                  {{ settingStatusLabel('injection', turnStateCacheInjectionEnabled) }}
                </p>
              </div>
              <Toggle
                data-testid="turn-state-injection-toggle"
                :model-value="turnStateCacheInjectionEnabled"
                :disabled="turnStateSettingsBusy || !turnStateSettingsLoaded"
                aria-labelledby="turn-state-injection-label"
                @update:model-value="saveTurnStateSetting('injection', $event)"
              />
            </div>
          </div>
          <div
            v-if="turnStateSettingsError"
            class="mt-3 flex flex-wrap items-center justify-between gap-2 text-xs text-red-700 dark:text-red-300"
            data-testid="turn-state-settings-error"
            role="alert"
          >
            <span>{{ turnStateSettingsError }}</span>
            <button
              v-if="!turnStateSettingsLoaded"
              type="button"
              class="font-medium text-red-700 underline decoration-red-300 underline-offset-2 hover:text-red-800 disabled:cursor-not-allowed disabled:opacity-60 dark:text-red-300 dark:hover:text-red-200"
              :disabled="turnStateSettingsBusy"
              @click="loadTurnStateSettings"
            >
              {{ t('admin.reliability.turnState.settingsRetry') }}
            </button>
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
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import {
  normalizeReliabilityStatus,
  reliabilityAPI,
  type ReliabilityStatusResponse,
  type ReliabilityTurnStateSettings,
} from '@/api/admin/reliability'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatNumber } from '@/utils/format'

const { t, locale } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const loadError = ref('')
const status = ref<ReliabilityStatusResponse | null>(null)
const turnStateSettingsLoading = ref(false)
const turnStateSettingsLoaded = ref(false)
const turnStateSettingsError = ref('')
const turnStateSettingSaving = ref<'probe' | 'injection' | null>(null)
const turnStateProbeEnabled = ref(true)
const turnStateCacheInjectionEnabled = ref(true)

const turnStateSettingsBusy = computed(() => turnStateSettingsLoading.value || turnStateSettingSaving.value !== null)
const pageLoading = computed(() => loading.value || turnStateSettingsLoading.value)
const pageBusy = computed(() => pageLoading.value || turnStateSettingSaving.value !== null)

const turnState = computed(() => normalizeReliabilityStatus(status.value ?? {}).turn_state ?? {})
const turnStateCollector = computed(() => {
  const collector = turnState.value.collector
  return collector && typeof collector === 'object' ? collector : null
})

function numeric(value: unknown, fallback = 0): number {
  if (value === undefined || value === null || (typeof value === 'string' && value.trim() === '')) return fallback
  const parsed = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

function optionalBoolean(...values: unknown[]): boolean | null {
  const value = values.find((candidate) => typeof candidate === 'boolean')
  return typeof value === 'boolean' ? value : null
}

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

function settingStatusLabel(kind: 'probe' | 'injection', enabled: boolean): string {
  if (turnStateSettingsLoading.value || (!turnStateSettingsLoaded.value && !turnStateSettingsError.value)) {
    return t('admin.reliability.turnState.settingsLoading')
  }
  if (turnStateSettingSaving.value === kind) return t('admin.reliability.turnState.settingsSaving')
  return enabled
    ? t('admin.reliability.turnState.settingEnabled')
    : t('admin.reliability.turnState.settingDisabled')
}

function protectionLabel(value: boolean | null): string {
  if (value === null) return t('admin.reliability.turnState.unknownValue')
  return value ? t('admin.reliability.turnState.protectedValue') : t('admin.reliability.turnState.unprotectedValue')
}

async function loadData() {
  loading.value = true
  loadError.value = ''
  try {
    status.value = await reliabilityAPI.getStatus()
  } catch {
    status.value = null
    loadError.value = t('admin.reliability.loadFailed')
  } finally {
    loading.value = false
  }
}

function enabledSetting(value: unknown, fallback: boolean): boolean {
  return typeof value === 'boolean' ? value : fallback
}

async function loadTurnStateSettings() {
  if (turnStateSettingsLoading.value || turnStateSettingSaving.value !== null) return
  turnStateSettingsLoading.value = true
  turnStateSettingsError.value = ''
  try {
    const settings = await reliabilityAPI.getTurnStateSettings()
    turnStateProbeEnabled.value = enabledSetting(settings?.probe_enabled, true)
    turnStateCacheInjectionEnabled.value = enabledSetting(settings?.injection_enabled, true)
    turnStateSettingsLoaded.value = true
  } catch (error) {
    turnStateSettingsLoaded.value = false
    turnStateSettingsError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsLoadFailed'),
    )
  } finally {
    turnStateSettingsLoading.value = false
  }
}

async function saveTurnStateSetting(kind: 'probe' | 'injection', enabled: boolean) {
  if (!turnStateSettingsLoaded.value || turnStateSettingsBusy.value) return

  const previous: ReliabilityTurnStateSettings = {
    probe_enabled: turnStateProbeEnabled.value,
    injection_enabled: turnStateCacheInjectionEnabled.value,
  }
  if (kind === 'probe') turnStateProbeEnabled.value = enabled
  else turnStateCacheInjectionEnabled.value = enabled

  turnStateSettingSaving.value = kind
  turnStateSettingsError.value = ''
  try {
    const updated = await reliabilityAPI.updateTurnStateSettings({
      probe_enabled: turnStateProbeEnabled.value,
      injection_enabled: turnStateCacheInjectionEnabled.value,
    })
    turnStateProbeEnabled.value = enabledSetting(updated?.probe_enabled, turnStateProbeEnabled.value)
    turnStateCacheInjectionEnabled.value = enabledSetting(
      updated?.injection_enabled,
      turnStateCacheInjectionEnabled.value,
    )
    appStore.showSuccess(t('admin.reliability.turnState.settingsSaved'))
  } catch (error) {
    turnStateProbeEnabled.value = previous.probe_enabled
    turnStateCacheInjectionEnabled.value = previous.injection_enabled
    turnStateSettingsError.value = extractApiErrorMessage(
      error,
      t('admin.reliability.turnState.settingsSaveFailed'),
    )
    appStore.showError(turnStateSettingsError.value)
  } finally {
    turnStateSettingSaving.value = null
  }
}

async function loadPageData() {
  await Promise.all([loadData(), loadTurnStateSettings()])
}

onMounted(loadPageData)
</script>
