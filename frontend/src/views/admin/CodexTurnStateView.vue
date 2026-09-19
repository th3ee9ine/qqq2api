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
            <div v-if="proxyError" class="proxy-alert" role="alert">
              <Icon name="exclamationCircle" size="sm" class="shrink-0" />
              <span>{{ proxyError }}</span>
              <button type="button" class="ml-auto shrink-0 text-xs font-medium underline" @click="loadProxies">{{ t('common.retry') }}</button>
            </div>
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
            <CodexTurnStateProxyPoolSelector
              :model-value="settingsForm.proxyIds"
              :proxies="proxies"
              :loading="loadingProxies"
              :disabled="loadingSettings || savingSettings"
              @update:model-value="setProxyIds"
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
          <span class="account-count">{{ filteredAccounts.length }} / {{ accounts.length }}</span>
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
          <article v-for="account in filteredAccounts" :key="account.id" class="account-row" :data-testid="`turn-state-account-${account.id}`">
            <div class="account-main">
              <div class="min-w-0 flex-1">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <h3 class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100" :title="account.name">{{ account.name }}</h3>
                  <span class="account-type">{{ account.type }}</span>
                  <span class="state-badge" :class="`state-${accountState(account).kind}`">{{ accountState(account).label }}</span>
                </div>
                <p class="mt-1 font-mono text-[11px] text-gray-400">#{{ account.id }}</p>
              </div>
              <div class="state-summary">
                <p>{{ accountState(account).detail }}</p>
                <p v-if="accountState(account).model" class="mt-1 font-mono">{{ accountState(account).model }}</p>
              </div>
            </div>
            <div class="collect-controls">
              <label class="min-w-0 flex-1">
                <span class="sr-only">{{ t('admin.codexTurnState.accounts.manualModel') }}</span>
                <input v-model="manualModels[account.id]" type="text" class="input w-full font-mono text-sm" maxlength="128" :placeholder="settingsForm.defaultModel || 'gpt-5.5'" :disabled="collectingIds.has(account.id)" :data-testid="`turn-state-model-${account.id}`" />
              </label>
              <button type="button" class="btn btn-secondary collect-button" :disabled="collectingIds.has(account.id)" :data-testid="`turn-state-collect-${account.id}`" @click="collectAccount(account)">
                <Icon :name="collectingIds.has(account.id) ? 'refresh' : 'play'" size="sm" :class="{ 'animate-spin': collectingIds.has(account.id) }" />
                {{ collectingIds.has(account.id) ? t('admin.codexTurnState.accounts.collecting') : t('admin.codexTurnState.accounts.collect') }}
              </button>
            </div>
          </article>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import CodexTurnStateProxyPoolSelector from '@/components/settings/CodexTurnStateProxyPoolSelector.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { normalizeCodexTurnStateDefaultModel, normalizeCodexTurnStateModels } from '@/utils/codexTurnState'
import type { AccountListItem, CodexTurnStateAutoInfo, Proxy } from '@/types'

type StateKind = 'valid' | 'renewal' | 'missing' | 'expired' | 'pending' | 'cooldown' | 'error'
type StatusFilter = 'all' | StateKind

interface AccountStateView {
  kind: StateKind
  label: string
  detail: string
  model?: string
}

const { t, locale } = useI18n()
const appStore = useAppStore()
const settingsForm = reactive({ autoEnabled: false, models: '', defaultModel: 'gpt-5.5', proxyIds: [] as number[] })
const proxyPoolConfigurationValid = ref(true)
const proxies = ref<Proxy[]>([])
const accounts = ref<AccountListItem[]>([])
const loadingSettings = ref(false)
const loadingProxies = ref(false)
const loadingAccounts = ref(false)
const savingSettings = ref(false)
const settingsError = ref('')
const proxyError = ref('')
const accountsError = ref('')
const accountSearch = ref('')
const statusFilter = ref<StatusFilter>('all')
const manualModels = reactive<Record<number, string>>({})
const collectingIds = reactive(new Set<number>())
const clock = ref(Date.now())

const loading = computed(() => loadingSettings.value || loadingProxies.value || loadingAccounts.value)

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
      ? (settingsForm.proxyIds.length || t('admin.codexTurnState.overview.compatiblePool'))
      : t('admin.codexTurnState.overview.invalidPool'),
    tone: proxyPoolConfigurationValid.value ? undefined : 'warning' as const,
  },
])

function normalizeProxyIds(value: unknown): number[] {
  if (!Array.isArray(value)) return []
  const seen = new Set<number>()
  const result: number[] = []
  for (const raw of value) {
    const id = Number(raw)
    if (Number.isInteger(id) && id > 0 && !seen.has(id)) {
      seen.add(id)
      result.push(id)
    }
  }
  return result
}

async function loadSettingsAndProxies() {
  await Promise.all([loadTurnStateSettings(), loadProxies()])
}

async function loadTurnStateSettings() {
  loadingSettings.value = true
  settingsError.value = ''
  try {
    const settings = await adminAPI.settings.getSettings()
    settingsForm.autoEnabled = Boolean(settings.openai_codex_turn_state_auto_enabled)
    settingsForm.models = settings.openai_codex_turn_state_models || ''
    settingsForm.defaultModel = settings.openai_codex_turn_state_default_model || 'gpt-5.5'
    proxyPoolConfigurationValid.value = settings.openai_codex_turn_state_proxy_ids_valid !== false
    const legacyId = Number(settings.openai_codex_turn_state_proxy_id)
    settingsForm.proxyIds = !proxyPoolConfigurationValid.value
      ? []
      : (Array.isArray(settings.openai_codex_turn_state_proxy_ids)
          ? normalizeProxyIds(settings.openai_codex_turn_state_proxy_ids)
          : (Number.isInteger(legacyId) && legacyId > 0 ? [legacyId] : []))
  } catch (error) {
    settingsError.value = extractApiErrorMessage(error, t('admin.codexTurnState.settings.loadFailed'))
  } finally {
    loadingSettings.value = false
  }
}

async function loadAllProxies(): Promise<Proxy[]> {
  const first = await adminAPI.proxies.list(1, 200)
  const result = [...(first.items || [])]
  const pages = Math.max(1, Number(first.pages) || 1)
  for (let page = 2; page <= pages; page += 1) {
    const response = await adminAPI.proxies.list(page, 200)
    result.push(...(response.items || []))
  }
  return result
}

async function loadProxies() {
  loadingProxies.value = true
  proxyError.value = ''
  try {
    proxies.value = await loadAllProxies()
  } catch (error) {
    proxies.value = []
    proxyError.value = extractApiErrorMessage(error, t('admin.codexTurnState.proxyPool.loadFailed'))
  } finally {
    loadingProxies.value = false
  }
}

async function loadAccounts() {
  loadingAccounts.value = true
  accountsError.value = ''
  try {
    const first = await adminAPI.accounts.list(1, 200, { platform: 'openai' })
    const result = [...(first.items || [])]
    const pages = Math.max(1, Number(first.pages) || 1)
    for (let page = 2; page <= pages; page += 1) {
      const response = await adminAPI.accounts.list(page, 200, { platform: 'openai' })
      result.push(...(response.items || []))
    }
    accounts.value = result.filter((account) => account.platform === 'openai' && (account.type === 'oauth' || account.type === 'setup-token'))
    clock.value = Date.now()
  } catch (error) {
    accountsError.value = extractApiErrorMessage(error, t('admin.codexTurnState.accounts.loadFailed'))
  } finally {
    loadingAccounts.value = false
  }
}

async function loadAll() {
  await Promise.all([loadSettingsAndProxies(), loadAccounts()])
}

async function saveSettings() {
  if (!proxyPoolConfigurationValid.value) {
    appStore.showError(t('admin.codexTurnState.proxyPool.invalidStored'))
    return
  }
  let defaultModel: string
  let models: string
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

  savingSettings.value = true
  try {
    await adminAPI.settings.updateSettings({
      openai_codex_turn_state_auto_enabled: settingsForm.autoEnabled,
      openai_codex_turn_state_models: models,
      openai_codex_turn_state_default_model: defaultModel,
      openai_codex_turn_state_proxy_ids: normalizeProxyIds(settingsForm.proxyIds),
    })
    settingsForm.models = models
    settingsForm.defaultModel = defaultModel
    settingsForm.proxyIds = normalizeProxyIds(settingsForm.proxyIds)
    proxyPoolConfigurationValid.value = true
    appStore.showSuccess(t('admin.codexTurnState.settings.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.codexTurnState.settings.saveFailed')))
  } finally {
    savingSettings.value = false
  }
}

function setProxyIds(value: number[]) {
  settingsForm.proxyIds = normalizeProxyIds(value)
  proxyPoolConfigurationValid.value = true
}

function clearInvalidProxyPool() {
  settingsForm.proxyIds = []
  proxyPoolConfigurationValid.value = true
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
  const cooldown = rows.find((row) => Number(row.info.probe_not_before_ms) > clock.value)
  if (cooldown) {
    return { kind: 'cooldown', label: t('admin.codexTurnState.accounts.states.cooldown'), detail: t('admin.codexTurnState.accounts.details.retryAt', { time: formatTimestamp(cooldown.info.probe_not_before_ms) }), model: cooldown.model }
  }
  const configured = rows.filter((row) => row.info.configured)
  const expired = configured.find((row) => Number(row.info.expires_at_ms) > 0 && Number(row.info.expires_at_ms) <= clock.value)
  if (expired) {
    return { kind: 'expired', label: t('admin.codexTurnState.accounts.states.expired'), detail: t('admin.codexTurnState.accounts.details.expiredAt', { time: formatTimestamp(expired.info.expires_at_ms) }), model: expired.model || expired.info.verified_model }
  }
  const renewal = configured.find((row) => row.info.due && (!row.info.expires_at_ms || Number(row.info.expires_at_ms) > clock.value))
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

function formatTimestamp(value?: number): string {
  if (!value) return '-'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}

function clearAccountFilters() {
  accountSearch.value = ''
  statusFilter.value = 'all'
}

async function collectAccount(account: AccountListItem) {
  if (collectingIds.has(account.id)) return
  let model: string | undefined
  const rawModel = manualModels[account.id]?.trim()
  if (rawModel) {
    try {
      model = normalizeCodexTurnStateDefaultModel(rawModel)
    } catch {
      appStore.showError(t('admin.codexTurnState.settings.invalidDefaultModel'))
      return
    }
  }

  collectingIds.add(account.id)
  try {
    const result = await adminAPI.accounts.collectCodexTurnState(account.id, model)
    if (result.codex_turn_state_auto) account.codex_turn_state_auto = result.codex_turn_state_auto
    clock.value = Date.now()
    if (result.status === 'rejected' || result.status === 'error') {
      appStore.showError(result.message || t('admin.codexTurnState.accounts.collectFailed'))
    } else if (result.status === 'already_valid') {
      appStore.showSuccess(t('admin.codexTurnState.accounts.alreadyValid'))
    } else {
      appStore.showSuccess(result.message || t('admin.codexTurnState.accounts.collectQueued'))
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.codexTurnState.accounts.collectFailed')))
  } finally {
    collectingIds.delete(account.id)
  }
}

onMounted(() => { void loadAll() })
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
.collect-controls {
  @apply flex min-w-0 flex-col gap-2 sm:flex-row;
}
.collect-button {
  @apply shrink-0 justify-center whitespace-nowrap;
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
