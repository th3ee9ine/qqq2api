<template>
  <AppLayout>
    <div class="admin-workspace turn-state-workspace">
      <AdminPageHeader
        :eyebrow="t('admin.codexTurnState.eyebrow')"
        :title="t('admin.codexTurnState.title')"
        :description="t('admin.codexTurnState.description')"
      >
        <template #actions>
          <button type="button" class="btn btn-secondary" :disabled="loading" data-testid="turn-state-refresh" @click="loadAll">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ t('admin.codexTurnState.refresh') }}
          </button>
        </template>
      </AdminPageHeader>

      <AdminOverviewStrip :items="overviewItems" :loading="loadingAccounts" />

      <section class="admin-surface settings-panel" :aria-busy="loadingSettings || savingSettings">
        <div class="section-heading-row">
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.codexTurnState.settings.title') }}</h2>
            <p class="section-description">{{ t('admin.codexTurnState.settings.description') }}</p>
          </div>
          <span class="settings-state" :class="settingsForm.autoEnabled ? 'is-enabled' : 'is-disabled'">
            {{ settingsForm.autoEnabled ? t('admin.codexTurnState.settings.enabled') : t('admin.codexTurnState.settings.disabled') }}
          </span>
        </div>

        <div v-if="settingsError" class="panel-alert" role="alert">
          <Icon name="exclamationCircle" size="sm" class="shrink-0" />
          <span>{{ settingsError }}</span>
          <button type="button" class="ml-auto text-xs font-medium underline" @click="loadTurnStateSettings">{{ t('common.retry') }}</button>
        </div>

        <form v-else class="settings-form" @submit.prevent="saveSettings">
          <label class="auto-toggle">
            <input v-model="settingsForm.autoEnabled" type="checkbox" class="peer sr-only" :disabled="loadingSettings || savingSettings" data-testid="turn-state-auto-toggle" />
            <span class="toggle-track" aria-hidden="true"><span /></span>
            <span class="min-w-0">
              <span class="block text-sm font-medium text-gray-800 dark:text-gray-200">{{ t('admin.codexTurnState.settings.autoLabel') }}</span>
              <span class="mt-1 block text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.codexTurnState.settings.autoHint') }}</span>
            </span>
          </label>

          <div class="settings-grid">
            <label class="field-group">
              <span class="field-label">{{ t('admin.codexTurnState.settings.autoInterval') }}</span>
              <input v-model.number="settingsForm.autoIntervalMinutes" type="number" class="input text-sm" min="1" max="60" step="1" inputmode="numeric" :disabled="loadingSettings || savingSettings" data-testid="turn-state-auto-interval" />
              <span class="field-hint">{{ t('admin.codexTurnState.settings.autoIntervalHint') }}</span>
            </label>
            <label class="field-group">
              <span class="field-label">{{ t('admin.codexTurnState.settings.defaultModel') }}</span>
              <input v-model="settingsForm.defaultModel" type="text" class="input font-mono text-sm" maxlength="128" autocomplete="off" placeholder="gpt-5.5" :disabled="loadingSettings || savingSettings" data-testid="turn-state-default-model" />
              <span class="field-hint">{{ t('admin.codexTurnState.settings.defaultModelHint') }}</span>
            </label>
            <label class="field-group">
              <span class="field-label">{{ t('admin.codexTurnState.settings.models') }}</span>
              <input v-model="settingsForm.models" type="text" class="input font-mono text-sm" autocomplete="off" :placeholder="t('admin.codexTurnState.settings.modelsPlaceholder')" :disabled="loadingSettings || savingSettings" data-testid="turn-state-models" />
              <span class="field-hint">{{ t('admin.codexTurnState.settings.modelsHint') }}</span>
            </label>
          </div>

          <div class="field-group">
            <span class="field-label">{{ t('admin.codexTurnState.settings.proxyPool') }}</span>
            <p class="field-hint">{{ t('admin.codexTurnState.settings.proxyPoolHint') }}</p>
            <div v-if="!proxyPoolConfigurationValid" class="proxy-alert" role="alert" data-testid="turn-state-proxy-pool-invalid">
              <Icon name="exclamationCircle" size="sm" class="shrink-0" />
              <span>{{ t('admin.codexTurnState.proxyPool.invalidStored') }}</span>
              <button
                type="button"
                class="ml-auto shrink-0 text-xs font-medium underline"
                :disabled="loadingSettings || savingSettings"
                data-testid="turn-state-proxy-pool-clear-invalid"
                @click="clearInvalidProxyPool"
              >
                {{ t('admin.codexTurnState.proxyPool.clearInvalid') }}
              </button>
            </div>
            <CodexTurnStateProxyUrlEditor
              :model-value="settingsForm.proxyUrls"
              :disabled="loadingSettings || savingSettings"
              @update:model-value="setProxyUrls"
              @validity-change="proxyEditorValid = $event"
            />
          </div>

          <div class="settings-footer">
            <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.codexTurnState.settings.saveHint') }}</p>
            <button type="submit" class="btn btn-primary" :disabled="loadingSettings || savingSettings || !proxyPoolConfigurationValid" data-testid="turn-state-save">
              <Icon v-if="savingSettings" name="refresh" size="sm" class="animate-spin" />
              {{ savingSettings ? t('admin.codexTurnState.settings.saving') : t('admin.codexTurnState.settings.save') }}
            </button>
          </div>
        </form>
      </section>

      <section class="admin-surface accounts-panel" :aria-busy="loadingAccounts">
        <div class="accounts-heading">
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.codexTurnState.accounts.title') }}</h2>
            <p class="section-description">{{ t('admin.codexTurnState.accounts.description') }}</p>
          </div>
          <div class="accounts-heading-actions">
            <span class="account-count">{{ filteredAccounts.length }} / {{ accounts.length }}</span>
            <button
              type="button"
              class="btn btn-primary"
              :disabled="collectAllDisabled"
              data-testid="turn-state-collect-all"
              @click="collectAllAccounts"
            >
              <Icon :name="bulkCollectionActive ? 'refresh' : 'play'" size="sm" :class="{ 'animate-spin': bulkCollectionActive }" />
              {{ bulkCollectionActive
                ? t('admin.codexTurnState.accounts.collectAllRunning')
                : t('admin.codexTurnState.accounts.collectAll', { count: accounts.length }) }}
            </button>
          </div>
        </div>

        <div
          v-if="bulkAccountIds.length > 0"
          class="bulk-progress"
          role="status"
          aria-live="polite"
          data-testid="turn-state-bulk-progress"
        >
          <div class="bulk-progress-heading">
            <span class="font-medium text-gray-800 dark:text-gray-200">
              {{ bulkCollectionActive
                ? t('admin.codexTurnState.accounts.bulkRunning')
                : t('admin.codexTurnState.accounts.bulkComplete') }}
            </span>
            <span class="font-semibold text-gray-900 dark:text-white">{{ bulkProgress.percent }}%</span>
          </div>
          <div
            class="bulk-progress-track"
            role="progressbar"
            :aria-valuenow="bulkProgress.percent"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="t('admin.codexTurnState.accounts.bulkProgressLabel', { percent: bulkProgress.percent })"
            :aria-valuetext="`${t('admin.codexTurnState.accounts.bulkProgressLabel', { percent: bulkProgress.percent })}; ${t('admin.codexTurnState.accounts.bulkOutcomes', { succeeded: bulkProgress.succeededAccounts, failed: bulkProgress.failedAccounts, skipped: bulkProgress.skippedAccounts })}`"
          >
            <span :style="{ width: `${bulkProgress.percent}%` }" />
          </div>
          <div class="bulk-progress-metrics">
            <span>{{ t('admin.codexTurnState.accounts.bulkSubmitted', { current: bulkProgress.submittedAccounts, total: bulkProgress.totalAccounts }) }}</span>
            <span>{{ t('admin.codexTurnState.accounts.bulkAccounts', { current: bulkProgress.completedAccounts, total: bulkProgress.totalAccounts }) }}</span>
            <span>{{ t('admin.codexTurnState.accounts.bulkModels', { current: bulkProgress.completedModels, total: bulkProgress.totalModels }) }}</span>
            <span>{{ t('admin.codexTurnState.accounts.bulkOutcomes', { succeeded: bulkProgress.succeededAccounts, failed: bulkProgress.failedAccounts, skipped: bulkProgress.skippedAccounts }) }}</span>
          </div>
        </div>

        <div class="accounts-toolbar">
          <div class="relative min-w-0 flex-1">
            <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="accountSearch" type="search" class="input w-full pl-9 text-sm" :placeholder="t('admin.codexTurnState.accounts.search')" :aria-label="t('admin.codexTurnState.accounts.search')" data-testid="turn-state-account-search" />
          </div>
          <select v-model="statusFilter" class="input status-filter text-sm" :aria-label="t('admin.codexTurnState.accounts.statusFilter')" data-testid="turn-state-status-filter">
            <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
          </select>
        </div>

        <div v-if="accountsError" class="account-empty" role="alert">
          <Icon name="exclamationCircle" size="lg" class="mb-3 text-red-400" />
          <p>{{ accountsError }}</p>
          <button type="button" class="btn btn-secondary mt-4" @click="loadAccounts">{{ t('common.retry') }}</button>
        </div>
        <div v-else-if="loadingAccounts" class="account-empty" role="status">
          <Icon name="refresh" size="lg" class="mb-3 animate-spin text-primary-600" />
          <p>{{ t('common.loading') }}</p>
        </div>
        <div v-else-if="accounts.length === 0" class="account-empty">
          <Icon name="database" size="lg" class="mb-3 text-gray-300 dark:text-gray-600" />
          <p>{{ t('admin.codexTurnState.accounts.empty') }}</p>
        </div>
        <div v-else-if="filteredAccounts.length === 0" class="account-empty">
          <Icon name="search" size="lg" class="mb-3 text-gray-300 dark:text-gray-600" />
          <p>{{ t('admin.codexTurnState.accounts.noMatches') }}</p>
          <button type="button" class="mt-3 text-xs font-medium text-primary-700 hover:underline dark:text-primary-400" @click="clearAccountFilters">{{ t('admin.codexTurnState.accounts.clearFilters') }}</button>
        </div>
        <div v-else class="account-list">
          <article
            v-for="account in filteredAccounts"
            :key="account.id"
            class="account-row"
            :aria-busy="isCollecting(account.id)"
            :data-testid="`turn-state-account-${account.id}`"
          >
            <div class="account-main">
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100" :title="account.name">{{ account.name }}</h3>
                  <span class="account-type">{{ account.type }}</span>
                  <span class="state-badge" :class="`state-${accountState(account).kind}`">{{ accountState(account).label }}</span>
                </div>
                <p class="mt-1 font-mono text-[11px] text-gray-400">#{{ account.id }}</p>
                <div
                  v-if="modelResults(account).length > 0"
                  class="model-results"
                  :data-testid="`turn-state-model-results-${account.id}`"
                >
                  <div v-for="(result, resultIndex) in modelResults(account)" :key="result.model" class="model-result-row">
                    <div class="min-w-0">
                      <span class="block break-all font-mono text-xs text-gray-700 dark:text-gray-200">{{ result.model }}</span>
                      <span
                        v-if="result.detail"
                        class="model-result-detail"
                        :data-testid="`turn-state-model-detail-${account.id}-${resultIndex}`"
                      >
                        {{ result.detail }}
                      </span>
                    </div>
                    <span class="state-badge" :class="`state-${result.kind}`">{{ result.label }}</span>
                  </div>
                </div>
              </div>
              <div class="state-summary">
                <p>{{ accountState(account).detail }}</p>
                <p v-if="accountState(account).model" class="mt-1 font-mono">{{ accountState(account).model }}</p>
              </div>
            </div>
            <div class="collect-controls">
              <button type="button" class="btn btn-secondary collect-button" :disabled="bulkCollectionActive || isCollecting(account.id)" :data-testid="`turn-state-collect-${account.id}`" @click="collectAccount(account)">
                <Icon :name="isCollecting(account.id) ? 'refresh' : 'play'" size="sm" :class="{ 'animate-spin': isCollecting(account.id) }" />
                {{ collectingIds.has(account.id)
                  ? t('admin.codexTurnState.accounts.collecting')
                  : pollingIds.has(account.id)
                    ? t('admin.codexTurnState.accounts.checking')
                    : t('admin.codexTurnState.accounts.collect') }}
              </button>
              <div
                v-if="accountCollectionRuns[account.id]"
                class="account-progress"
                :data-testid="`turn-state-account-progress-${account.id}`"
              >
                <div class="account-progress-meta">
                  <span>{{ accountCollectionLabel(accountCollectionRuns[account.id]) }}</span>
                  <span v-if="accountCollectionRuns[account.id].targets.length > 0">
                    {{ t('admin.codexTurnState.accounts.progressModels', {
                      current: completedTargetCount(accountCollectionRuns[account.id]),
                      total: accountCollectionRuns[account.id].targets.length,
                    }) }}
                  </span>
                </div>
                <div
                  class="account-progress-track"
                  role="progressbar"
                  :aria-valuenow="accountCollectionPercent(accountCollectionRuns[account.id])"
                  aria-valuemin="0"
                  aria-valuemax="100"
                  :aria-label="accountCollectionLabel(accountCollectionRuns[account.id])"
                >
                  <span :style="{ width: `${accountCollectionPercent(accountCollectionRuns[account.id])}%` }" />
                </div>
              </div>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import CodexTurnStateProxyUrlEditor from '@/components/settings/CodexTurnStateProxyUrlEditor.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  isCodexTurnStateEligibleAccount,
  normalizeCodexTurnStateDefaultModel,
  normalizeCodexTurnStateModels,
  normalizeCodexTurnStateProxyUrls,
} from '@/utils/codexTurnState'
import type { CodexTurnStateCollectResult } from '@/api/admin/accounts'
import type { AccountListItem, CodexTurnStateAutoInfo } from '@/types'

type StateKind = 'valid' | 'renewal' | 'missing' | 'expired' | 'pending' | 'cooldown' | 'error'
type StatusFilter = 'all' | StateKind

interface AccountStateView {
  kind: StateKind
  label: string
  detail: string
  model?: string
}

interface ModelStateView extends AccountStateView {
  model: string
}

type PollOutcome = 'pending' | 'success' | 'failure'
type AccountCollectionOutcome = 'success' | 'failure' | 'skipped'
type AccountCollectionPhase = 'queued' | 'submitting' | 'polling' | AccountCollectionOutcome
type CollectionTargetStatus = 'pending' | AccountCollectionOutcome

interface CollectionTargetProgress {
  model: string
  owner: string
  status: CollectionTargetStatus
}

interface AccountCollectionRun {
  phase: AccountCollectionPhase
  targets: CollectionTargetProgress[]
  transientFailures: number
  submitted: boolean
}

interface PollBaseline {
  targets: Map<string, {
    targetSuccesses: Set<string>
    targetFailures: Set<string>
  }>
  outcomes: Map<string, Exclude<PollOutcome, 'pending'>>
  displayTargets: Array<{ model: string; owner: string }>
  notify: boolean
  complete: (outcome: AccountCollectionOutcome) => void
  settled: boolean
}

interface CollectionSubmissionGate {
  abort: () => void
  complete: (outcome: AccountCollectionOutcome) => void
  settled: boolean
  outcome?: AccountCollectionOutcome
}

interface BulkCollectionProgress {
  totalAccounts: number
  submittedAccounts: number
  completedAccounts: number
  succeededAccounts: number
  failedAccounts: number
  skippedAccounts: number
  totalModels: number
  completedModels: number
  percent: number
}

const POLL_INTERVAL_MS = 2_000
const MAX_POLL_RETRY_DELAY_MS = 30_000
const CLOCK_TICK_MS = 30_000
const BULK_COLLECTION_CONCURRENCY = 3

const { t, locale } = useI18n()
const appStore = useAppStore()
const settingsForm = reactive({ autoEnabled: false, autoIntervalMinutes: 50, models: '', defaultModel: 'gpt-5.5', proxyUrls: [] as string[] })
const appliedAutoIntervalMinutes = ref(50)
const storedProxyPoolValid = ref(true)
const proxyEditorValid = ref(true)
const accounts = ref<AccountListItem[]>([])
const loadingSettings = ref(false)
const loadingAccounts = ref(false)
const savingSettings = ref(false)
const settingsError = ref('')
const accountsError = ref('')
const accountSearch = ref('')
const statusFilter = ref<StatusFilter>('all')
const collectingIds = reactive(new Set<number>())
const pollingIds = reactive(new Set<number>())
const pollingTargets = reactive<Record<number, Array<{ model: string; owner: string }>>>({})
const successfulModels = reactive<Record<number, string[]>>({})
const accountCollectionRuns = reactive<Record<number, AccountCollectionRun>>({})
const bulkAccountIds = ref<number[]>([])
const bulkCollectionActive = ref(false)
const bulkProgressSnapshot = ref<BulkCollectionProgress | null>(null)
const clock = ref(Date.now())
const pollTimers = new Map<number, ReturnType<typeof setTimeout>>()
const pollBaselines = new Map<number, PollBaseline>()
const collectionSubmissionGates = new Map<number, CollectionSubmissionGate>()
const accountDetailVersions = new Map<number, number>()
let clockTimer: ReturnType<typeof setInterval> | undefined
let componentActive = true
let accountListRequestGeneration = 0
let accountDetailGeneration = 0

const loading = computed(() => loadingSettings.value || loadingAccounts.value)
const proxyPoolConfigurationValid = computed(() => storedProxyPoolValid.value && proxyEditorValid.value)
const collectAllDisabled = computed(() => loading.value
  || accounts.value.length === 0
  || bulkCollectionActive.value
  || collectingIds.size > 0
  || pollingIds.size > 0)

const bulkProgress = computed(() => {
  if (!bulkCollectionActive.value && bulkProgressSnapshot.value) return bulkProgressSnapshot.value
  const runs = bulkAccountIds.value.map(accountId => accountCollectionRuns[accountId])
  const terminal = runs.filter((run): run is AccountCollectionRun => Boolean(run && isTerminalCollectionPhase(run.phase)))
  const targets = runs.flatMap(run => run?.targets || [])
  const completedTargets = targets.filter(target => target.status !== 'pending')
  const totalAccounts = bulkAccountIds.value.length
  const completedAccounts = terminal.length
  return {
    totalAccounts,
    submittedAccounts: runs.filter(run => run?.submitted).length,
    completedAccounts,
    succeededAccounts: terminal.filter(run => run.phase === 'success').length,
    failedAccounts: terminal.filter(run => run.phase === 'failure').length,
    skippedAccounts: terminal.filter(run => run.phase === 'skipped').length,
    totalModels: targets.length,
    completedModels: completedTargets.length,
    percent: totalAccounts > 0 ? Math.round((completedAccounts / totalAccounts) * 100) : 0,
  }
})

const statusOptions = computed(() => (['all', 'valid', 'renewal', 'missing', 'expired', 'pending', 'cooldown', 'error'] as StatusFilter[]).map((value) => ({
  value,
  label: t(`admin.codexTurnState.accounts.filters.${value}`),
})))

const filteredAccounts = computed(() => {
  const query = accountSearch.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    if (statusFilter.value !== 'all' && classifyAccount(account).kind !== statusFilter.value) return false
    if (!query) return true
    return `${account.name} ${account.id} ${account.type}`.toLowerCase().includes(query)
  })
})

const stateCounts = computed(() => {
  const counts: Record<StateKind, number> = { valid: 0, renewal: 0, missing: 0, expired: 0, pending: 0, cooldown: 0, error: 0 }
  for (const account of accounts.value) counts[classifyAccount(account).kind] += 1
  return counts
})

const overviewItems = computed(() => [
  { label: t('admin.codexTurnState.overview.eligible'), value: accounts.value.length },
  { label: t('admin.codexTurnState.overview.valid'), value: stateCounts.value.valid, tone: 'positive' as const },
  { label: t('admin.codexTurnState.overview.attention'), value: accounts.value.length - stateCounts.value.valid, tone: 'warning' as const },
  {
    label: t('admin.codexTurnState.overview.proxies'),
    value: proxyPoolConfigurationValid.value
      ? (settingsForm.proxyUrls.length || t('admin.codexTurnState.overview.globalPool'))
      : t('admin.codexTurnState.overview.invalidPool'),
    tone: proxyPoolConfigurationValid.value ? undefined : 'warning' as const,
  },
])

async function loadTurnStateSettings() {
  loadingSettings.value = true
  settingsError.value = ''
  try {
    const settings = await adminAPI.settings.getSettings()
    const autoIntervalMinutes = Number(settings.openai_codex_turn_state_auto_interval_minutes) || 50
    settingsForm.autoEnabled = Boolean(settings.openai_codex_turn_state_auto_enabled)
    settingsForm.autoIntervalMinutes = autoIntervalMinutes
    appliedAutoIntervalMinutes.value = autoIntervalMinutes
    settingsForm.models = settings.openai_codex_turn_state_models || ''
    settingsForm.defaultModel = settings.openai_codex_turn_state_default_model || 'gpt-5.5'
    storedProxyPoolValid.value = settings.openai_codex_turn_state_proxy_urls_valid !== false
    settingsForm.proxyUrls = Array.isArray(settings.openai_codex_turn_state_proxy_urls)
      ? settings.openai_codex_turn_state_proxy_urls.filter((value): value is string => typeof value === 'string')
      : []
  } catch (error) {
    settingsError.value = extractApiErrorMessage(error, t('admin.codexTurnState.settings.loadFailed'))
  } finally {
    loadingSettings.value = false
  }
}

async function loadAccounts() {
  const requestGeneration = ++accountListRequestGeneration
  const detailGenerationAtStart = accountDetailGeneration
  loadingAccounts.value = true
  accountsError.value = ''
  try {
    const filters = { platform: 'openai', status: 'active' }
    const first = await adminAPI.accounts.list(1, 200, filters)
    const result = [...(first.items || [])]
    const pages = Math.max(1, Number(first.pages) || 1)
    for (let page = 2; page <= pages; page += 1) {
      const response = await adminAPI.accounts.list(page, 200, filters)
      result.push(...(response.items || []))
    }
    const nextAccounts = result.filter(isCodexTurnStateEligibleAccount)
    if (!componentActive || requestGeneration !== accountListRequestGeneration) return

    const currentAccounts = new Map(accounts.value.map(account => [account.id, account]))
    const mergedAccounts: AccountListItem[] = []
    const mergedAccountIds = new Set<number>()
    for (const account of nextAccounts) {
      if ((accountDetailVersions.get(account.id) || 0) > detailGenerationAtStart) {
        const current = currentAccounts.get(account.id)
        if (current) {
          mergedAccounts.push(current)
          mergedAccountIds.add(current.id)
        }
        continue
      }
      mergedAccounts.push(account)
      mergedAccountIds.add(account.id)
    }
    for (const current of accounts.value) {
      if (
        !mergedAccountIds.has(current.id)
        && (accountDetailVersions.get(current.id) || 0) > detailGenerationAtStart
        && isCodexTurnStateEligibleAccount(current)
      ) {
        mergedAccounts.push(current)
        mergedAccountIds.add(current.id)
      }
    }

    clock.value = Date.now()
    for (const accountId of Object.keys(successfulModels).map(Number)) {
      if (!mergedAccountIds.has(accountId)) delete successfulModels[accountId]
    }
    for (const account of mergedAccounts) {
      setSuccessfulModels(account.id, successfulModelsFromInfo(account.codex_turn_state_auto))
    }
    accounts.value = mergedAccounts
    const activeCollectionIds = new Set([...collectionSubmissionGates.keys(), ...pollBaselines.keys()])
    for (const accountId of activeCollectionIds) {
      if (!mergedAccountIds.has(accountId)) stopPolling(accountId, 'skipped')
    }
  } catch (error) {
    if (componentActive && requestGeneration === accountListRequestGeneration) {
      accountsError.value = extractApiErrorMessage(error, t('admin.codexTurnState.accounts.loadFailed'))
    }
  } finally {
    if (componentActive && requestGeneration === accountListRequestGeneration) {
      loadingAccounts.value = false
    }
  }
}

async function loadAll() {
  await Promise.all([loadTurnStateSettings(), loadAccounts()])
}

async function saveSettings() {
  if (!proxyPoolConfigurationValid.value) {
    appStore.showError(t('admin.codexTurnState.proxyPool.invalidStored'))
    return
  }
  let defaultModel: string
  let models: string
  let proxyUrls: string[]
  const autoIntervalMinutes = Number(settingsForm.autoIntervalMinutes)
  if (!Number.isInteger(autoIntervalMinutes) || autoIntervalMinutes < 1 || autoIntervalMinutes > 60) {
    appStore.showError(t('admin.codexTurnState.settings.invalidAutoInterval'))
    return
  }
  try {
    defaultModel = normalizeCodexTurnStateDefaultModel(settingsForm.defaultModel)
  } catch {
    appStore.showError(t('admin.codexTurnState.settings.invalidDefaultModel'))
    return
  }
  try {
    models = normalizeCodexTurnStateModels(settingsForm.models)
  } catch {
    appStore.showError(t('admin.codexTurnState.settings.invalidModels'))
    return
  }
  try {
    proxyUrls = normalizeCodexTurnStateProxyUrls(settingsForm.proxyUrls)
  } catch {
    appStore.showError(t('admin.codexTurnState.proxyPool.invalidEntries'))
    return
  }

  savingSettings.value = true
  try {
    await adminAPI.settings.updateSettings({
      openai_codex_turn_state_auto_enabled: settingsForm.autoEnabled,
      openai_codex_turn_state_auto_interval_minutes: autoIntervalMinutes,
      openai_codex_turn_state_models: models,
      openai_codex_turn_state_default_model: defaultModel,
      openai_codex_turn_state_proxy_urls: proxyUrls,
    })
    settingsForm.models = models
    settingsForm.defaultModel = defaultModel
    settingsForm.autoIntervalMinutes = autoIntervalMinutes
    appliedAutoIntervalMinutes.value = autoIntervalMinutes
    settingsForm.proxyUrls = proxyUrls
    storedProxyPoolValid.value = true
    appStore.showSuccess(t('admin.codexTurnState.settings.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.codexTurnState.settings.saveFailed')))
  } finally {
    savingSettings.value = false
  }
}

function setProxyUrls(value: string[]) {
  settingsForm.proxyUrls = [...value]
  storedProxyPoolValid.value = true
}

function clearInvalidProxyPool() {
  settingsForm.proxyUrls = []
  storedProxyPoolValid.value = true
}

function flattenState(info?: CodexTurnStateAutoInfo | null): Array<{ model?: string; info: CodexTurnStateAutoInfo }> {
  if (!info) return []
  const rows: Array<{ model?: string; info: CodexTurnStateAutoInfo }> = []
  const visit = (current: CodexTurnStateAutoInfo, model?: string) => {
    rows.push({ model, info: current })
    for (const [childModel, child] of Object.entries(current.models || {})) visit(child, childModel)
  }
  visit(info)
  return rows
}

function classifyAccount(account: AccountListItem): AccountStateView {
  const rows = flattenState(account.codex_turn_state_auto)
  if (rows.some((row) => row.info.recovery_pending)) {
    return { kind: 'pending', label: t('admin.codexTurnState.accounts.states.pending'), detail: t('admin.codexTurnState.accounts.details.pending') }
  }
  const configured = rows.filter((row) => row.info.configured)
  const expired = configured.find((row) => Number(row.info.expires_at_ms) > 0 && Number(row.info.expires_at_ms) <= clock.value)
  if (expired) {
    return { kind: 'expired', label: t('admin.codexTurnState.accounts.states.expired'), detail: t('admin.codexTurnState.accounts.details.expiredAt', { time: formatTimestamp(expired.info.expires_at_ms) }), model: expired.model || expired.info.verified_model }
  }
  const renewal = configured.find((row) => isRenewalDue(row.info) && (!row.info.expires_at_ms || Number(row.info.expires_at_ms) > clock.value))
  if (renewal) {
    return {
      kind: 'renewal',
      label: t('admin.codexTurnState.accounts.states.renewal'),
      detail: renewal.info.expires_at_ms
        ? t('admin.codexTurnState.accounts.details.renewalBefore', { time: formatTimestamp(renewal.info.expires_at_ms) })
        : t('admin.codexTurnState.accounts.details.renewal'),
      model: renewal.model || renewal.info.verified_model,
    }
  }
  const successful = configured.find((row) => isCollectedSuccess(row.info))
  if (successful) {
    return { kind: 'valid', label: t('admin.codexTurnState.accounts.states.valid'), detail: successful.info.expires_at_ms ? t('admin.codexTurnState.accounts.details.validUntil', { time: formatTimestamp(successful.info.expires_at_ms) }) : t('admin.codexTurnState.accounts.details.configured'), model: successful.model || successful.info.verified_model }
  }
  const cooldown = rows.find((row) => Number(row.info.probe_not_before_ms) > clock.value)
  if (cooldown) {
    return { kind: 'cooldown', label: t('admin.codexTurnState.accounts.states.cooldown'), detail: t('admin.codexTurnState.accounts.details.retryAt', { time: formatTimestamp(cooldown.info.probe_not_before_ms) }), model: cooldown.model }
  }
  const valid = configured.find((row) => !row.info.expires_at_ms || Number(row.info.expires_at_ms) > clock.value)
  if (valid) {
    return { kind: 'valid', label: t('admin.codexTurnState.accounts.states.valid'), detail: valid.info.expires_at_ms ? t('admin.codexTurnState.accounts.details.validUntil', { time: formatTimestamp(valid.info.expires_at_ms) }) : t('admin.codexTurnState.accounts.details.configured'), model: valid.model || valid.info.verified_model }
  }
  if (configured.length > 0) {
    const unknownExpiry = configured[0]
    return { kind: 'valid', label: t('admin.codexTurnState.accounts.states.valid'), detail: t('admin.codexTurnState.accounts.details.configured'), model: unknownExpiry.model || unknownExpiry.info.verified_model }
  }
  const failed = rows.find((row) => Boolean(row.info.last_error))
  if (failed) {
    return { kind: 'error', label: t('admin.codexTurnState.accounts.states.error'), detail: t('admin.codexTurnState.accounts.details.error'), model: failed.model }
  }
  return { kind: 'missing', label: t('admin.codexTurnState.accounts.states.missing'), detail: t('admin.codexTurnState.accounts.details.missing') }
}

function accountState(account: AccountListItem): AccountStateView {
  return classifyAccount(account)
}

function modelStateRows(info?: CodexTurnStateAutoInfo | null): Array<{ model: string; slotModel: string; info: CodexTurnStateAutoInfo }> {
  if (!info) return []
  const entries = Object.entries(info.models || {})
  if (entries.length === 0) {
    const model = info.verified_model?.trim()
    return model ? [{ model, slotModel: model, info }] : []
  }
  const rows: Array<{ model: string; slotModel: string; info: CodexTurnStateAutoInfo }> = []
  for (const [fallbackModel, child] of entries) {
    const nested = modelStateRows(child)
    if (nested.length > 0 && Object.keys(child.models || {}).length > 0) rows.push(...nested)
    else rows.push({ model: child.verified_model?.trim() || fallbackModel, slotModel: fallbackModel, info: child })
  }
  return rows
}

function classifyModelInfo(model: string, info: CodexTurnStateAutoInfo): ModelStateView {
  let kind: StateKind = 'missing'
  if (info.recovery_pending) kind = 'pending'
  else if (info.configured && Number(info.expires_at_ms) > 0 && Number(info.expires_at_ms) <= clock.value) kind = 'expired'
  else if (info.configured && isRenewalDue(info)) kind = 'renewal'
  else if (isCollectedSuccess(info)) kind = 'valid'
  else if (Number(info.probe_not_before_ms) > clock.value) kind = 'cooldown'
  else if (info.collection_succeeded === true || (info.configured && (!info.expires_at_ms || Number(info.expires_at_ms) > clock.value))) kind = 'valid'
  else if (info.last_error) kind = 'error'
  let detail = t('admin.codexTurnState.accounts.details.missing')
  if (kind === 'pending') detail = t('admin.codexTurnState.accounts.details.pending')
  else if (kind === 'expired') detail = t('admin.codexTurnState.accounts.details.expiredAt', { time: formatTimestamp(info.expires_at_ms) })
  else if (kind === 'renewal') {
    detail = info.expires_at_ms
      ? t('admin.codexTurnState.accounts.details.renewalBefore', { time: formatTimestamp(info.expires_at_ms) })
      : t('admin.codexTurnState.accounts.details.renewal')
  } else if (kind === 'valid') {
    detail = info.expires_at_ms
      ? t('admin.codexTurnState.accounts.details.validUntil', { time: formatTimestamp(info.expires_at_ms) })
      : t('admin.codexTurnState.accounts.details.configured')
  } else if (kind === 'cooldown') detail = t('admin.codexTurnState.accounts.details.retryAt', { time: formatTimestamp(info.probe_not_before_ms) })
  else if (kind === 'error') detail = t('admin.codexTurnState.accounts.details.error')
  return {
    kind,
    model,
    label: t(`admin.codexTurnState.accounts.modelStates.${kind === 'valid' ? 'success' : kind}`),
    detail,
  }
}

function isCollectedSuccess(info: CodexTurnStateAutoInfo, now: number = clock.value): boolean {
  return isCollectionTerminalSuccess(info, now) && !isRenewalDue(info, now)
}

function isCollectionTerminalSuccess(info: CodexTurnStateAutoInfo, now: number = clock.value): boolean {
  return info.configured &&
    info.collection_succeeded !== false &&
    !info.recovery_pending &&
    !info.last_error &&
    (!info.expires_at_ms || Number(info.expires_at_ms) > now)
}

function isRenewalDue(info: CodexTurnStateAutoInfo, now: number = clock.value): boolean {
  const setAt = Number(info.set_at_ms)
  const verifiedAt = Number(info.verified_at_ms)
  const intervalMinutes = appliedAutoIntervalMinutes.value
  if (info.configured &&
      Number.isFinite(setAt) && setAt > 0 &&
      Number.isInteger(intervalMinutes) && intervalMinutes >= 1 && intervalMinutes <= 60) {
    const renewalBaseAt = Number.isFinite(verifiedAt) && verifiedAt > setAt ? verifiedAt : setAt
    const expiresAt = Number(info.expires_at_ms)
    const configuredDueAt = renewalBaseAt + intervalMinutes * 60_000
    const dueAt = Number.isFinite(expiresAt) && expiresAt > 0
      ? Math.min(configuredDueAt, expiresAt)
      : configuredDueAt
    return dueAt <= now
  }
  return info.due
}

function successfulModelsFromInfo(info?: CodexTurnStateAutoInfo | null): string[] {
  if (!info) return []
  const rows = modelStateRows(info)
  const result = new Set<string>()
  for (const reportedModel of info.successful_models || []) {
    const model = reportedModel.trim()
    if (!model) continue
    const matchingRow = rows.find(row => row.slotModel === model || row.model === model)
    if (matchingRow && isCollectedSuccess(matchingRow.info)) result.add(matchingRow.model)
    else if (!matchingRow && rows.length === 0 && isCollectedSuccess(info)) result.add(info.verified_model?.trim() || model)
  }
  for (const row of rows) {
    if (row.info.collection_succeeded === true && isCollectedSuccess(row.info)) result.add(row.info.verified_model?.trim() || row.model)
  }
  if (info.collection_succeeded === true && info.verified_model?.trim() && isCollectedSuccess(info)) result.add(info.verified_model.trim())
  return [...result]
}

function setSuccessfulModels(accountId: number, models: string[]) {
  const next = new Set<string>()
  for (const model of models) {
    const normalized = model.trim()
    if (normalized) next.add(normalized)
  }
  if (next.size > 0) successfulModels[accountId] = [...next]
  else delete successfulModels[accountId]
}

function addSuccessfulModels(accountId: number, models: string[]) {
  setSuccessfulModels(accountId, [...(successfulModels[accountId] || []), ...models])
}

function modelResults(account: AccountListItem): ModelStateView[] {
  const stateRows = modelStateRows(account.codex_turn_state_auto)
  const rows = new Map<string, ModelStateView>()
  for (const row of stateRows) {
    rows.set(row.model, classifyModelInfo(row.model, row.info))
  }
  for (const model of [
    ...successfulModelsFromInfo(account.codex_turn_state_auto),
    ...(successfulModels[account.id] || []),
  ]) {
    const existing = rows.get(model)
    if (!existing) {
      rows.set(model, {
        kind: 'valid',
        model,
        label: t('admin.codexTurnState.accounts.modelStates.success'),
        detail: '',
      })
    }
  }
  for (const target of pollingTargets[account.id] || []) {
    const ownerRow = stateRows.find(row => row.slotModel === target.owner || row.model === target.owner)
    const existing = ownerRow ? classifyModelInfo(ownerRow.model, ownerRow.info) : rows.get(target.model)
    if (existing?.kind === 'valid') continue
    if (ownerRow && ownerRow.model !== target.model) rows.delete(ownerRow.model)
    if (existing?.kind === 'error') {
      rows.set(target.model, { ...existing, model: target.model })
      continue
    }
    rows.set(target.model, {
      kind: 'pending',
      model: target.model,
      label: t('admin.codexTurnState.accounts.modelStates.pending'),
      detail: t('admin.codexTurnState.accounts.details.pending'),
    })
  }
  return [...rows.values()]
}

function formatTimestamp(value?: number): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}

function isTerminalCollectionPhase(phase: AccountCollectionPhase): boolean {
  return phase === 'success' || phase === 'failure' || phase === 'skipped'
}

function completedTargetCount(run: AccountCollectionRun): number {
  return run.targets.filter(target => target.status !== 'pending').length
}

function accountCollectionPercent(run: AccountCollectionRun): number {
  if (run.targets.length === 0) return isTerminalCollectionPhase(run.phase) ? 100 : 0
  return Math.round((completedTargetCount(run) / run.targets.length) * 100)
}

function accountCollectionLabel(run: AccountCollectionRun): string {
  if (run.phase === 'queued') return t('admin.codexTurnState.accounts.progressQueued')
  if (run.phase === 'submitting') return t('admin.codexTurnState.accounts.progressSubmitting')
  if (run.phase === 'polling' && run.transientFailures > 0) {
    return t('admin.codexTurnState.accounts.progressRetrying', { count: run.transientFailures })
  }
  if (run.phase === 'polling') return t('admin.codexTurnState.accounts.progressPolling')
  if (run.phase === 'success') return t('admin.codexTurnState.accounts.progressSucceeded')
  if (run.phase === 'skipped') return t('admin.codexTurnState.accounts.progressSkipped')
  return run.targets.some(target => target.status === 'success')
    ? t('admin.codexTurnState.accounts.progressPartial')
    : t('admin.codexTurnState.accounts.progressFailed')
}

function beginAccountCollection(accountId: number, phase: 'queued' | 'submitting' = 'submitting') {
  accountCollectionRuns[accountId] = {
    phase,
    targets: [],
    transientFailures: 0,
    submitted: false,
  }
}

function markAccountCollectionSubmitted(accountId: number) {
  const current = accountCollectionRuns[accountId]
  if (!current) return
  accountCollectionRuns[accountId] = { ...current, submitted: true }
}

function collectionTargetsFromResult(
  result: CodexTurnStateCollectResult,
  targetModels: string[],
  queuedModels: string[],
): CollectionTargetProgress[] {
  const mapped = (result.model_targets || [])
    .map(({ model, owner }) => ({ model: model.trim(), owner: owner.trim() }))
    .filter(({ model, owner }) => Boolean(model && owner))
  const fallbackModels = targetModels.length > 0 ? targetModels : queuedModels
  const source = mapped.length > 0
    ? mapped
    : fallbackModels.map(model => ({ model, owner: model }))
  const queuedOwners = new Set(queuedModels)
  const failed = result.status === 'rejected' || result.status === 'error'
  const seen = new Set<string>()
  const targets: CollectionTargetProgress[] = []
  for (const target of source) {
    const key = `${target.model}\u0000${target.owner}`
    if (seen.has(key)) continue
    seen.add(key)
    targets.push({
      ...target,
      status: failed ? 'failure' : queuedOwners.has(target.owner) ? 'pending' : 'success',
    })
  }
  return targets
}

function setAccountCollectionResult(
  accountId: number,
  result: CodexTurnStateCollectResult,
  targetModels: string[],
  queuedModels: string[],
) {
  const failed = result.status === 'rejected' || result.status === 'error'
  accountCollectionRuns[accountId] = {
    phase: failed ? 'failure' : queuedModels.length > 0 ? 'polling' : 'success',
    targets: collectionTargetsFromResult(result, targetModels, queuedModels),
    transientFailures: 0,
    submitted: true,
  }
}

function updateAccountCollectionFromBaseline(accountId: number, baseline: PollBaseline) {
  const current = accountCollectionRuns[accountId]
  if (!current) return
  accountCollectionRuns[accountId] = {
    ...current,
    phase: 'polling',
    transientFailures: 0,
    targets: current.targets.map((target) => {
      const outcome = baseline.outcomes.get(target.owner)
      return outcome ? { ...target, status: outcome } : target
    }),
  }
}

function markAccountCollectionRetry(accountId: number, failures: number) {
  const current = accountCollectionRuns[accountId]
  if (!current || isTerminalCollectionPhase(current.phase)) return
  accountCollectionRuns[accountId] = { ...current, phase: 'polling', transientFailures: failures }
}

function markAccountCollectionTerminal(accountId: number, outcome: AccountCollectionOutcome) {
  const current = accountCollectionRuns[accountId] || { phase: 'submitting', targets: [], transientFailures: 0, submitted: false }
  accountCollectionRuns[accountId] = {
    ...current,
    phase: outcome,
    transientFailures: 0,
    targets: current.targets.map(target => target.status === 'pending' ? { ...target, status: outcome } : target),
  }
}

function clearAccountFilters() {
  accountSearch.value = ''
  statusFilter.value = 'all'
}

function markAccountDetailVersion(accountId: number) {
  accountDetailGeneration += 1
  accountDetailVersions.set(accountId, accountDetailGeneration)
}

function removeAccountFromDetails(accountId: number) {
  markAccountDetailVersion(accountId)
  const index = accounts.value.findIndex(account => account.id === accountId)
  if (index >= 0) accounts.value.splice(index, 1)
  delete successfulModels[accountId]
  stopPolling(accountId, 'skipped')
}

function replaceAccount(account: AccountListItem): boolean {
  markAccountDetailVersion(account.id)
  const index = accounts.value.findIndex(item => item.id === account.id)
  if (index < 0) return false
  const next = { ...accounts.value[index], ...account }
  if (!isCodexTurnStateEligibleAccount(next)) {
    accounts.value.splice(index, 1)
    delete successfulModels[account.id]
    stopPolling(account.id, 'skipped')
    return false
  }
  setSuccessfulModels(account.id, successfulModelsFromInfo(next.codex_turn_state_auto))
  accounts.value.splice(index, 1, next)
  return true
}

function replaceAccountState(accountId: number, info: CodexTurnStateAutoInfo) {
  const current = accounts.value.find(item => item.id === accountId)
  if (!current) {
    markAccountDetailVersion(accountId)
    return
  }
  replaceAccount({ ...current, codex_turn_state_auto: info })
}

function targetSuccessFingerprints(info: CodexTurnStateAutoInfo | null | undefined, model: string, allowAggregateFallback = true): Set<string> {
  const now = Date.now()
  const result = new Set<string>()
  const rows = modelStateRows(info)
  const relevantRows = rows.filter(row => row.model === model || row.slotModel === model || row.info.verified_model === model)
  if (relevantRows.length === 0 && allowAggregateFallback && rows.length === 0 && info) {
    relevantRows.push({ model, slotModel: model, info })
  }
  for (const row of relevantRows) {
    const valid = isCollectionTerminalSuccess(row.info, now)
    if (!valid) continue
    result.add([
      row.slotModel,
      row.model,
      row.info.probe_at_ms || 0,
      row.info.verified_at_ms || 0,
      row.info.set_at_ms || 0,
      row.info.state_length || 0,
    ].join(':'))
  }
  return result
}

function targetFailureFingerprints(info: CodexTurnStateAutoInfo | null | undefined, model: string, allowAggregateFallback = true): Set<string> {
  const result = new Set<string>()
  const rows = modelStateRows(info)
  const relevantRows = rows.filter(row => row.model === model || row.slotModel === model || row.info.verified_model === model)
  if (relevantRows.length === 0 && allowAggregateFallback && rows.length === 0 && info) {
    relevantRows.push({ model, slotModel: model, info })
  }
  for (const row of relevantRows) {
    const error = row.info.last_error?.trim()
    if (!error || row.info.recovery_pending) continue
    // Starting a new round clears the old error while persisting a new probe
    // timestamp. Polling may miss that cleared state when the round finishes in
    // under one interval, so the timestamp distinguishes a repeated error code.
    result.add(JSON.stringify([row.slotModel, row.model, row.info.probe_at_ms || 0, error]))
  }
  return result
}

function createPollBaseline(
  info: CodexTurnStateAutoInfo | null | undefined,
  targets: string[],
  displayTargets: Array<{ model: string; owner: string }>,
  notify: boolean,
  complete: (outcome: AccountCollectionOutcome) => void,
): PollBaseline {
  const allowAggregateFallback = targets.length === 1
  return {
    targets: new Map(targets.map((model) => {
      const targetSuccesses = targetSuccessFingerprints(info, model, allowAggregateFallback)
      return [model, {
        targetSuccesses,
        targetFailures: targetFailureFingerprints(info, model, allowAggregateFallback),
      }]
    })),
    outcomes: new Map(),
    displayTargets,
    notify,
    complete,
    settled: false,
  }
}

function targetPollOutcome(info: CodexTurnStateAutoInfo | null | undefined, model: string, baseline: PollBaseline): PollOutcome {
  const recorded = baseline.outcomes.get(model)
  if (recorded) return recorded
  if (!info) return 'pending'
  const targetBaseline = baseline.targets.get(model)
  if (!targetBaseline) return 'pending'
  const allowAggregateFallback = baseline.targets.size === 1
  if ([...targetSuccessFingerprints(info, model, allowAggregateFallback)].some(fingerprint => !targetBaseline.targetSuccesses.has(fingerprint))) return 'success'
  if ([...targetFailureFingerprints(info, model, allowAggregateFallback)].some(fingerprint => !targetBaseline.targetFailures.has(fingerprint))) return 'failure'
  return 'pending'
}

function pollOutcome(info: CodexTurnStateAutoInfo | null | undefined, baseline: PollBaseline): PollOutcome {
  let pending = false
  let failed = false
  for (const model of baseline.targets.keys()) {
    const outcome = targetPollOutcome(info, model, baseline)
    if (outcome === 'pending') {
      pending = true
      continue
    }
    baseline.outcomes.set(model, outcome)
    if (outcome === 'failure') failed = true
  }
  if (pending) return 'pending'
  return failed ? 'failure' : 'success'
}

function stopPolling(accountId: number, outcome: AccountCollectionOutcome) {
  const timer = pollTimers.get(accountId)
  if (timer) clearTimeout(timer)
  pollTimers.delete(accountId)
  pollingIds.delete(accountId)
  delete pollingTargets[accountId]
  const baseline = pollBaselines.get(accountId)
  pollBaselines.delete(accountId)
  const submissionGate = collectionSubmissionGates.get(accountId)
  collectionSubmissionGates.delete(accountId)
  markAccountCollectionTerminal(accountId, outcome)
  if (submissionGate && !submissionGate.settled) {
    submissionGate.outcome = outcome
    submissionGate.settled = true
    submissionGate.abort()
    submissionGate.complete(outcome)
  }
  if (baseline && !baseline.settled) {
    baseline.settled = true
    baseline.complete(outcome)
  }
}

function pollingErrorStatus(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const candidate = (error as { status?: unknown; response?: { status?: unknown } }).status
    ?? (error as { response?: { status?: unknown } }).response?.status
  if (candidate == null || candidate === '') return undefined
  const status = Number(candidate)
  return Number.isInteger(status) ? status : undefined
}

function isTransientPollingError(error: unknown): boolean {
  const status = pollingErrorStatus(error)
  return status == null || status === 0 || status === 408 || status === 429 || status >= 500
}

function schedulePoll(accountId: number, baseline: PollBaseline, transientFailureCount = 0) {
  if (baseline.settled) return
  pollBaselines.set(accountId, baseline)
  pollingIds.add(accountId)
  pollingTargets[accountId] = baseline.displayTargets
  const delay = transientFailureCount === 0
    ? POLL_INTERVAL_MS
    : Math.min(POLL_INTERVAL_MS * (2 ** transientFailureCount), MAX_POLL_RETRY_DELAY_MS)
  const timer = setTimeout(async () => {
    pollTimers.delete(accountId)
    if (!componentActive || !pollingIds.has(accountId)) return
    try {
      const refreshed = await adminAPI.accounts.getById(accountId)
      if (!componentActive || !pollingIds.has(accountId)) return
      clock.value = Date.now()
      if (!replaceAccount(refreshed)) {
        stopPolling(accountId, 'skipped')
        return
      }
      const info = refreshed.codex_turn_state_auto
      const outcome = pollOutcome(info, baseline)
      updateAccountCollectionFromBaseline(accountId, baseline)
      if (outcome === 'success') {
        const reportedSuccesses = successfulModelsFromInfo(info)
        addSuccessfulModels(accountId, reportedSuccesses.length > 0 ? reportedSuccesses : [...baseline.targets.keys()])
        stopPolling(accountId, 'success')
        if (baseline.notify) appStore.showSuccess(t('admin.codexTurnState.accounts.collectSucceeded'))
        return
      }
      if (outcome === 'failure') {
        stopPolling(accountId, 'failure')
        if (baseline.notify) appStore.showError(t('admin.codexTurnState.accounts.collectFailed'))
        return
      }
      schedulePoll(accountId, baseline)
    } catch (error) {
      if (!componentActive || !pollingIds.has(accountId)) return
      const status = pollingErrorStatus(error)
      if (status === 404 || status === 410) {
        removeAccountFromDetails(accountId)
        return
      }
      if (isTransientPollingError(error)) {
        markAccountCollectionRetry(accountId, transientFailureCount + 1)
        schedulePoll(accountId, baseline, transientFailureCount + 1)
        return
      }
      stopPolling(accountId, 'failure')
      if (baseline.notify) appStore.showError(t('admin.codexTurnState.accounts.collectFailed'))
    }
  }, delay)
  pollTimers.set(accountId, timer)
}

function isCollecting(accountId: number): boolean {
  return collectingIds.has(accountId) || pollingIds.has(accountId)
}

async function collectAccount(
  account: AccountListItem,
  { notify = true }: { notify?: boolean } = {},
): Promise<AccountCollectionOutcome> {
  if (isCollecting(account.id)) return 'skipped'
  beginAccountCollection(account.id)
  markAccountCollectionSubmitted(account.id)
  collectingIds.add(account.id)
  let submissionGate: CollectionSubmissionGate | undefined
  try {
    const controller = new AbortController()
    let cancelSubmission!: (outcome: AccountCollectionOutcome) => void
    const cancelled = new Promise<AccountCollectionOutcome>((complete) => {
      cancelSubmission = complete
    })
    submissionGate = { abort: () => controller.abort(), complete: cancelSubmission, settled: false }
    collectionSubmissionGates.set(account.id, submissionGate)
    const response = await Promise.race([
      adminAPI.accounts.collectCodexTurnState(account.id, controller.signal).then(result => ({ kind: 'result' as const, result })),
      cancelled.then(outcome => ({ kind: 'cancelled' as const, outcome })),
    ])
    if (submissionGate.outcome) return submissionGate.outcome
    submissionGate.settled = true
    if (collectionSubmissionGates.get(account.id) === submissionGate) collectionSubmissionGates.delete(account.id)
    collectingIds.delete(account.id)
    if (response.kind === 'cancelled') return response.outcome
    if (!componentActive) return 'skipped'
    const result = response.result
    clock.value = Date.now()
    const targetModels = [...new Set(
      (Array.isArray(result.target_models) ? result.target_models : result.model ? [result.model] : [])
        .map(model => model?.trim())
        .filter((model): model is string => Boolean(model)),
    )]
    const modelTargets = (result.model_targets || [])
      .map(({ model, owner }) => ({ model: model.trim(), owner: owner.trim() }))
      .filter(({ model, owner }) => Boolean(model && owner))
    const queuedModels = result.status === 'queued' ? [...new Set(
      (Array.isArray(result.queued_models) ? result.queued_models : targetModels)
        .map(model => model.trim())
        .filter(Boolean),
    )] : []
    const queuedOwners = new Set(queuedModels)
    const queuedDisplayTargets = modelTargets.length > 0
      ? modelTargets.filter(({ owner }) => queuedOwners.has(owner))
      : queuedModels.map(model => ({ model, owner: model }))
    if (result.status === 'rejected' && result.codex_turn_state_auto === null) {
      replaceAccount({ ...account, codex_turn_state_auto: null })
    } else if (result.codex_turn_state_auto) {
      replaceAccountState(account.id, result.codex_turn_state_auto)
    }
    setAccountCollectionResult(account.id, result, targetModels, queuedModels)
    const reportedSuccesses = [
      ...(result.successful_models || []),
      ...successfulModelsFromInfo(result.codex_turn_state_auto),
    ]
    if (result.status === 'rejected' || result.status === 'error') {
      markAccountCollectionTerminal(account.id, 'failure')
      if (notify) appStore.showError(result.message || t('admin.codexTurnState.accounts.collectFailed'))
      return 'failure'
    } else if (result.status === 'already_valid') {
      addSuccessfulModels(account.id, reportedSuccesses.length > 0 ? reportedSuccesses : targetModels)
      markAccountCollectionTerminal(account.id, 'success')
      if (notify) appStore.showSuccess(t('admin.codexTurnState.accounts.alreadyValid'))
      return 'success'
    } else if (result.status === 'queued') {
      if (notify) appStore.showSuccess(result.message || t('admin.codexTurnState.accounts.collectQueued'))
      if (queuedModels.length > 0) {
        return await new Promise<AccountCollectionOutcome>((complete) => {
          schedulePoll(account.id, createPollBaseline(
            result.codex_turn_state_auto,
            queuedModels,
            queuedDisplayTargets,
            notify,
            complete,
          ))
        })
      }
    } else if (result.collection_succeeded === true || result.codex_turn_state_auto?.collection_succeeded === true) {
      addSuccessfulModels(account.id, reportedSuccesses.length > 0 ? reportedSuccesses : targetModels)
      if (notify) appStore.showSuccess(result.message || t('admin.codexTurnState.accounts.collectSucceeded'))
    } else if (notify) {
      appStore.showSuccess(result.message || t('admin.codexTurnState.accounts.collectSucceeded'))
    }
    markAccountCollectionTerminal(account.id, 'success')
    return 'success'
  } catch (error) {
    if (submissionGate?.outcome) return submissionGate.outcome
    if (!componentActive) return 'skipped'
    markAccountCollectionTerminal(account.id, 'failure')
    if (notify) appStore.showError(extractApiErrorMessage(error, t('admin.codexTurnState.accounts.collectFailed')))
    return 'failure'
  } finally {
    if (submissionGate && collectionSubmissionGates.get(account.id) === submissionGate) {
      submissionGate.settled = true
      collectionSubmissionGates.delete(account.id)
    }
    collectingIds.delete(account.id)
  }
}

async function collectAllAccounts() {
  if (collectAllDisabled.value) return
  const targetAccounts = accounts.value.filter(isCodexTurnStateEligibleAccount)
  if (targetAccounts.length === 0) return

  bulkAccountIds.value = targetAccounts.map(account => account.id)
  bulkProgressSnapshot.value = null
  bulkCollectionActive.value = true
  for (const account of targetAccounts) beginAccountCollection(account.id, 'queued')

  let cursor = 0
  const worker = async () => {
    while (componentActive) {
      const index = cursor
      cursor += 1
      if (index >= targetAccounts.length) return
      const snapshot = targetAccounts[index]
      const current = accounts.value.find(account => account.id === snapshot.id)
      if (!current || !isCodexTurnStateEligibleAccount(current)) {
        markAccountCollectionTerminal(snapshot.id, 'skipped')
        continue
      }
      try {
        const outcome = await collectAccount(current, { notify: false })
        // A skipped submitted round may still be winding down server-side.
        // Retire this worker so its concurrency slot is not immediately reused.
        if (outcome === 'skipped') return
      } catch {
        markAccountCollectionTerminal(current.id, 'failure')
      }
    }
  }

  const workerCount = Math.min(BULK_COLLECTION_CONCURRENCY, targetAccounts.length)
  await Promise.all(Array.from({ length: workerCount }, () => worker()))
  if (!componentActive) return
  for (const accountId of bulkAccountIds.value) {
    const run = accountCollectionRuns[accountId]
    if (run && !isTerminalCollectionPhase(run.phase)) markAccountCollectionTerminal(accountId, 'skipped')
  }

  const progress = { ...bulkProgress.value }
  bulkProgressSnapshot.value = progress
  bulkCollectionActive.value = false
  if (progress.failedAccounts === 0 && progress.skippedAccounts === 0) {
    appStore.showSuccess(t('admin.codexTurnState.accounts.bulkFinishedSuccess', {
      accounts: progress.succeededAccounts,
      models: progress.completedModels,
    }))
  } else {
    appStore.showError(t('admin.codexTurnState.accounts.bulkFinishedWithFailures', {
      succeeded: progress.succeededAccounts,
      failed: progress.failedAccounts,
      skipped: progress.skippedAccounts,
    }))
  }
}

onMounted(() => {
  clock.value = Date.now()
  clockTimer = setInterval(() => {
    if (componentActive) clock.value = Date.now()
  }, CLOCK_TICK_MS)
  void loadAll()
})
onBeforeUnmount(() => {
  componentActive = false
  accountListRequestGeneration += 1
  if (clockTimer) clearInterval(clockTimer)
  clockTimer = undefined
  const activeCollectionIds = new Set([...collectionSubmissionGates.keys(), ...pollBaselines.keys()])
  for (const accountId of activeCollectionIds) stopPolling(accountId, 'skipped')
  for (const timer of pollTimers.values()) clearTimeout(timer)
  pollTimers.clear()
  pollBaselines.clear()
  collectionSubmissionGates.clear()
  collectingIds.clear()
  pollingIds.clear()
  for (const accountId of Object.keys(pollingTargets).map(Number)) delete pollingTargets[accountId]
})
</script>

<style scoped>
.turn-state-workspace {
  @apply space-y-5;
}
.settings-panel,
.accounts-panel {
  @apply min-w-0 overflow-hidden;
  border-radius: 0.5rem;
}
.turn-state-workspace :deep(.admin-overview) {
  border-radius: 0.5rem;
}
.section-heading-row,
.accounts-heading {
  @apply flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 p-5 dark:border-dark-700;
}
.section-description {
  @apply mt-1.5 max-w-3xl text-xs leading-5 text-gray-500 dark:text-gray-400;
}
.accounts-heading-actions {
  @apply flex w-full flex-wrap items-center justify-between gap-3 sm:w-auto sm:justify-end;
}
.settings-state,
.account-count,
.account-type,
.state-badge {
  @apply shrink-0 rounded-md px-2 py-1 text-[11px] font-medium;
}
.settings-state.is-enabled {
  @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400;
}
.settings-state.is-disabled,
.account-count,
.account-type {
  @apply bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.panel-alert {
  @apply m-5 flex items-start gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2.5 text-xs leading-5 text-red-700 dark:border-red-500/20 dark:bg-red-500/5 dark:text-red-300;
}
.proxy-alert {
  @apply mb-3 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-700 dark:border-amber-500/20 dark:bg-amber-500/5 dark:text-amber-300;
}
.settings-form {
  @apply space-y-5 p-5;
}
.auto-toggle {
  @apply flex cursor-pointer items-start gap-3 rounded-lg border border-gray-200 p-4 dark:border-dark-600;
}
.toggle-track {
  @apply relative mt-0.5 inline-flex h-5 w-9 shrink-0 rounded-full bg-gray-200 transition-colors peer-checked:bg-primary-600 peer-focus-visible:ring-2 peer-focus-visible:ring-primary-500 peer-focus-visible:ring-offset-2 peer-disabled:opacity-50 dark:bg-dark-600 dark:peer-focus-visible:ring-offset-dark-900;
}
.toggle-track > span {
  @apply absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform;
}
.peer:checked + .toggle-track > span {
  transform: translateX(1rem);
}
.settings-grid {
  @apply grid gap-5 md:grid-cols-2;
}
.field-group {
  @apply block min-w-0;
}
.field-label {
  @apply mb-1.5 block text-sm font-medium text-gray-700 dark:text-gray-300;
}
.field-hint {
  @apply mb-2 block text-xs leading-5 text-gray-500 dark:text-gray-400;
}
.settings-footer {
  @apply flex flex-wrap items-center justify-between gap-4 border-t border-gray-100 pt-5 dark:border-dark-700;
}
.bulk-progress {
  @apply border-b border-gray-100 bg-gray-50/60 px-5 py-4 text-xs text-gray-600 dark:border-dark-700 dark:bg-dark-800/30 dark:text-gray-300;
}
.bulk-progress-heading {
  @apply flex items-center justify-between gap-3;
}
.bulk-progress-track,
.account-progress-track {
  @apply mt-2 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600;
}
.bulk-progress-track > span,
.account-progress-track > span {
  @apply block h-full rounded-full bg-primary-600 transition-all duration-300 dark:bg-primary-500;
}
.bulk-progress-metrics {
  @apply mt-3 grid gap-x-5 gap-y-1.5 sm:grid-cols-2 lg:grid-cols-4;
}
.accounts-toolbar {
  @apply flex flex-wrap gap-3 border-b border-gray-100 p-4 dark:border-dark-700;
}
.status-filter {
  @apply w-full sm:w-44;
}
.account-empty {
  @apply flex min-h-52 flex-col items-center justify-center px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400;
}
.account-list {
  @apply divide-y divide-gray-100 dark:divide-dark-700;
}
.account-row {
  @apply grid gap-4 p-5;
}
.account-main {
  @apply flex min-w-0 flex-wrap items-start justify-between gap-3;
}
.state-summary {
  @apply max-w-full text-left text-xs leading-5 text-gray-500 dark:text-gray-400 sm:max-w-sm sm:text-right;
}
.model-results {
  @apply mt-3 grid gap-1.5 sm:max-w-md;
}
.model-result-row {
  @apply flex min-w-0 items-center justify-between gap-3 rounded-md border border-gray-100 bg-gray-50/60 px-2.5 py-1.5 dark:border-dark-700 dark:bg-dark-800/40;
}
.model-result-detail {
  @apply mt-0.5 block text-[11px] leading-4 text-gray-500 dark:text-gray-400;
}
.collect-controls {
  @apply flex min-w-0 flex-col items-stretch gap-2 sm:items-end;
}
.collect-button {
  @apply w-full shrink-0 justify-center whitespace-nowrap sm:w-auto;
}
.account-progress {
  @apply w-full min-w-0 text-[11px] text-gray-500 dark:text-gray-400 lg:max-w-sm;
}
.account-progress-meta {
  @apply flex min-w-0 flex-wrap items-center justify-between gap-x-3 gap-y-1;
}
.state-valid {
  @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400;
}
.state-renewal {
  @apply bg-teal-50 text-teal-700 dark:bg-teal-500/10 dark:text-teal-400;
}
.state-missing {
  @apply bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.state-expired,
.state-error {
  @apply bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400;
}
.state-pending {
  @apply bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-400;
}
.state-cooldown {
  @apply bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-400;
}
@media (min-width: 1024px) {
  .account-row {
    grid-template-columns: minmax(0, 1.4fr) minmax(320px, 0.6fr);
    align-items: center;
  }
}
@media (prefers-reduced-motion: reduce) {
  .turn-state-workspace * {
    animation: none !important;
    transition: none !important;
  }
}
</style>
