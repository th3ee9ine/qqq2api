<template>
  <AppLayout>
    <div class="admin-workspace account-sessions-workspace">
      <AdminPageHeader
        :eyebrow="t('admin.accountSessions.eyebrow')"
        :title="t('admin.accountSessions.title')"
        :description="t('admin.accountSessions.description')"
      >
        <template #actions>
          <button type="button" class="btn btn-secondary" :disabled="busy || loadingAccounts" @click="loadAll">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingAccounts }" />
            {{ t('admin.accountSessions.refresh') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="busy || loadingAccounts || selectedAccountIds.length === 0" @click="querySessions">
            <Icon :name="querying ? 'refresh' : 'search'" size="sm" :class="{ 'animate-spin': querying }" />
            {{ querying ? t('admin.accountSessions.querying') : t('admin.accountSessions.query') }}
          </button>
        </template>
      </AdminPageHeader>

      <AdminOverviewStrip :items="overviewItems" />

      <section class="admin-surface cleanup-settings" data-testid="global-session-cleanup-settings" :aria-busy="cleanupLoading">
        <div class="cleanup-heading">
          <span class="settings-icon" aria-hidden="true"><Icon name="shield" size="md" /></span>
          <div class="min-w-0">
            <h2 class="admin-section-heading">{{ t('admin.accountSessions.globalCleanup.title') }}</h2>
            <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.globalCleanup.description') }}</p>
          </div>
        </div>
        <div v-if="cleanupLoading" class="flex items-center gap-2 text-sm text-gray-500" role="status">
          <Icon name="refresh" size="sm" class="animate-spin" />{{ t('common.loading') }}
        </div>
        <div v-else-if="cleanupLoadError" class="flex flex-wrap items-center gap-2 text-sm text-red-600 dark:text-red-400" role="alert">
          <span>{{ cleanupLoadError }}</span>
          <button type="button" class="btn btn-secondary text-xs" @click="loadCleanupSettings">{{ t('common.retry') }}</button>
        </div>
        <form v-else class="cleanup-controls" @submit.prevent="saveCleanupSettings">
          <label class="cleanup-toggle">
            <input v-model="cleanupEnabled" type="checkbox" class="peer sr-only" :disabled="cleanupSaving" />
            <span class="toggle-track" aria-hidden="true"><span /></span>
            <span>{{ t('admin.accountSessions.globalCleanup.enabled') }}</span>
          </label>
          <label class="cleanup-interval">
            <span>{{ t('admin.accountSessions.globalCleanup.interval') }}</span>
            <input v-model.number="cleanupInterval" type="number" min="5" max="10080" step="1" class="input" :disabled="!cleanupEnabled || cleanupSaving" aria-describedby="cleanup-interval-hint" />
          </label>
          <button type="submit" class="btn btn-secondary" :disabled="cleanupSaving">
            <Icon v-if="cleanupSaving" name="refresh" size="sm" class="animate-spin" />
            {{ cleanupSaving ? t('admin.accountSessions.globalCleanup.saving') : t('admin.accountSessions.globalCleanup.save') }}
          </button>
          <p id="cleanup-interval-hint" class="cleanup-hint">{{ t('admin.accountSessions.globalCleanup.hint') }}</p>
        </form>
      </section>

      <div class="session-layout">
        <section class="admin-surface account-panel" :aria-busy="loadingAccounts" aria-labelledby="session-accounts-heading">
          <div class="panel-heading">
            <div class="flex items-center justify-between gap-2">
              <h2 id="session-accounts-heading" class="admin-section-heading">{{ t('admin.accountSessions.accounts') }}</h2>
              <span class="count-badge">{{ loadingAccounts || accountLoadError ? '—' : accounts.length }}</span>
            </div>
            <p class="mt-1.5 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.accountScope') }}</p>
            <div class="relative mt-4">
              <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input v-model="accountSearch" type="search" class="input pl-9 text-sm" :placeholder="t('admin.accountSessions.searchAccounts')" :aria-label="t('admin.accountSessions.searchAccounts')" />
            </div>
          </div>
          <div class="account-selection-bar">
            <label class="flex cursor-pointer items-center gap-2 text-xs font-medium text-gray-600 dark:text-gray-300">
              <input
                type="checkbox"
                class="session-checkbox"
                :checked="allAccountsSelected"
                :indeterminate="selectedAccountIds.length > 0 && !allAccountsSelected"
                :disabled="accounts.length === 0 || loadingAccounts || busy"
                @change="toggleAllAccounts"
              />
              {{ t('admin.accountSessions.selectAllAccounts') }}
            </label>
            <span class="text-xs tabular-nums text-primary-600 dark:text-primary-400">{{ t('admin.accountSessions.selectedCount', { count: selectedAccountIds.length }) }}</span>
          </div>
          <div v-if="loadingAccounts" class="space-y-4 p-5" role="status">
            <span class="sr-only">{{ t('common.loading') }}</span>
            <div v-for="index in 5" :key="index" class="flex animate-pulse items-center gap-3" aria-hidden="true">
              <div class="h-4 w-4 rounded bg-gray-100 dark:bg-dark-700" />
              <div class="h-9 flex-1 rounded-lg bg-gray-100 dark:bg-dark-700" />
            </div>
          </div>
          <div v-else-if="accountLoadError" class="space-y-3 p-5 text-sm text-red-600 dark:text-red-400" role="alert">
            <p>{{ accountLoadError }}</p>
            <button type="button" class="btn btn-secondary text-xs" @click="loadAccounts">{{ t('common.retry') }}</button>
          </div>
          <div v-else-if="accounts.length === 0" class="account-empty">
            <Icon name="users" size="lg" class="mx-auto mb-3 text-gray-300 dark:text-gray-600" />
            {{ t('admin.accountSessions.noAccounts') }}
          </div>
          <div v-else-if="filteredAccounts.length === 0" class="account-empty">
            {{ t('admin.accountSessions.noMatchingAccounts') }}
            <button type="button" class="mt-3 text-primary-600 hover:underline dark:text-primary-400" @click="accountSearch = ''">{{ t('admin.accountSessions.clearSearch') }}</button>
          </div>
          <div v-else class="account-list">
            <label v-for="account in filteredAccounts" :key="account.id" class="account-option" :class="{ 'is-selected': selectedAccountIds.includes(account.id), 'is-disabled': busy }">
              <input v-model="selectedAccountIds" type="checkbox" :value="account.id" class="session-checkbox" :disabled="busy" />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium text-gray-800 dark:text-gray-200" :title="account.name">{{ account.name }}</span>
                <span class="mt-0.5 block font-mono text-[11px] text-gray-400">#{{ account.id }}</span>
              </span>
              <Icon v-if="selectedAccountIds.includes(account.id)" name="check" size="xs" class="shrink-0 text-primary-500" aria-hidden="true" />
            </label>
          </div>
          <p class="account-footer">{{ t('admin.accountSessions.selectionHint') }}</p>
        </section>

        <section class="admin-surface sessions-panel" :aria-busy="querying" aria-labelledby="device-sessions-heading">
          <div class="sessions-heading">
            <div class="min-w-0">
              <div class="flex items-center gap-2">
                <h2 id="device-sessions-heading" class="admin-section-heading">{{ t('admin.accountSessions.sessions') }}</h2>
                <span v-if="queried" class="count-badge">{{ sessionRows.length }}</span>
              </div>
              <p class="mt-1.5 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ queried ? t('admin.accountSessions.resultScope', { count: queriedAccountCount }) : t('admin.accountSessions.sessionsHint') }}</p>
            </div>
            <button type="button" class="btn btn-secondary cleanup-button" :disabled="busy || loadingAccounts || selectedAccountIds.length === 0" @click="cleanupConfirm = true">
              <Icon :name="cleaning ? 'refresh' : 'shield'" size="sm" :class="{ 'animate-spin': cleaning }" />
              {{ cleaning ? t('admin.accountSessions.cleaning') : t('admin.accountSessions.cleanup') }}
            </button>
          </div>
          <div v-if="sessionError" class="session-alert" role="alert">
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 shrink-0" />
            <span>{{ sessionError }}</span>
          </div>
          <div v-if="queried && !querying && sessionRows.length > 0" class="session-selection-bar">
            <span class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.selectedSessions', { count: selectedSessionKeys.length }) }}</span>
            <button type="button" class="revoke-button" :disabled="busy || selectedSessionKeys.length === 0" @click="revokeConfirm = true">
              <Icon name="login" size="sm" />{{ t('admin.accountSessions.revokeSelected') }}
            </button>
          </div>
          <div v-if="querying" class="session-empty" role="status">
            <span class="empty-device-icon"><Icon name="refresh" size="lg" class="animate-spin" /></span>
            <h3>{{ t('admin.accountSessions.querying') }}</h3>
            <p>{{ t('admin.accountSessions.queryingHint') }}</p>
          </div>
          <div v-else-if="sessionRows.length === 0" class="session-empty" role="status">
            <span class="empty-device-icon" aria-hidden="true">
              <svg class="h-8 w-8" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.4"><rect x="3" y="4" width="18" height="13" rx="2" /><path stroke-linecap="round" d="M8 21h8M12 17v4M8.5 10.5l2 2 5-5" /></svg>
            </span>
            <h3>{{ queried ? t('admin.accountSessions.noSessions') : t('admin.accountSessions.queryBeforeRevoke') }}</h3>
            <p>{{ queried ? t('admin.accountSessions.noSessionsHint') : t('admin.accountSessions.emptyHint') }}</p>
            <button v-if="!queried" type="button" class="btn btn-primary mt-5" :disabled="busy || loadingAccounts || selectedAccountIds.length === 0" @click="querySessions">
              <Icon name="search" size="sm" />{{ t('admin.accountSessions.query') }}
            </button>
          </div>
          <div v-else class="session-list">
            <article v-for="row in sessionRows" :key="row.key" class="device-row" :class="{ 'is-selected': selectedSessionKeys.includes(row.key) }">
              <div class="device-selection">
                <input v-if="row.session.can_revoke && !row.session.current" :id="`session-${row.key}`" v-model="selectedSessionKeys" type="checkbox" :value="row.key" class="session-checkbox" :disabled="busy" :aria-label="t('admin.accountSessions.selectSession', { name: row.session.device_name || row.session.browser || t('admin.accountSessions.unknown') })" />
                <Icon v-else name="lock" size="sm" class="text-gray-300 dark:text-gray-600" :aria-label="t('admin.accountSessions.protectedSession')" />
              </div>
              <span class="device-icon" :class="{ 'is-current': row.session.current }" aria-hidden="true">
                <svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                  <template v-if="deviceShape(row.session) === 'phone'">
                    <rect x="6.5" y="2" width="11" height="20" rx="2.5" /><path stroke-linecap="round" d="M10 5h4M11 19h2" />
                  </template>
                  <template v-else-if="deviceShape(row.session) === 'tablet'">
                    <rect x="4" y="2.5" width="16" height="19" rx="2.5" /><path stroke-linecap="round" d="M11 18.5h2" />
                  </template>
                  <template v-else>
                    <rect x="3" y="4" width="18" height="13" rx="2" /><path stroke-linecap="round" d="M8 21h8M12 17v4" />
                  </template>
                </svg>
              </span>
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <label :for="row.session.can_revoke && !row.session.current ? `session-${row.key}` : undefined" class="break-words text-sm font-semibold text-gray-900 dark:text-white">{{ row.session.device_name || row.session.browser || t('admin.accountSessions.unknown') }}</label>
                  <span v-if="row.session.current" class="session-badge session-badge-current">{{ t('admin.accountSessions.current') }}</span>
                  <span v-if="row.session.trusted" class="session-badge session-badge-trusted"><Icon name="shield" size="xs" />{{ t('admin.accountSessions.trusted') }}</span>
                </div>
                <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
                  <span class="inline-flex min-w-0 items-center gap-1.5"><Icon name="user" size="xs" class="shrink-0" /><span class="break-all">{{ row.account.name }}</span></span>
                  <span class="break-words">{{ [row.session.app_name, row.session.browser, row.session.os, row.session.location].filter(Boolean).join(' · ') || '—' }}</span>
                </div>
                <p class="mt-2 flex items-start gap-1.5 text-[11px] leading-5 text-gray-400 dark:text-gray-500"><Icon name="clock" size="xs" class="mt-0.5 shrink-0" />{{ t('admin.accountSessions.lastActivity') }} {{ formatTime(row.session.last_active_at || row.session.signed_in_at) }}</p>
                <button
                  v-if="row.session.current && !row.session.trusted"
                  type="button"
                  data-testid="session-trust"
                  class="trust-button"
                  :disabled="busy"
                  @click="trustCurrentSession(row)"
                >
                  <Icon :name="trustingKey === row.key ? 'refresh' : 'shield'" size="xs" :class="{ 'animate-spin': trustingKey === row.key }" />
                  {{ trustingKey === row.key ? t('admin.accountSessions.trusting') : t('admin.accountSessions.trustCurrent') }}
                </button>
              </div>
            </article>
          </div>
          <div class="session-footer"><Icon name="shield" size="xs" class="shrink-0" />{{ t('admin.accountSessions.protectionHint') }}</div>
        </section>
      </div>

      <ConfirmDialog
        :show="cleanupConfirm"
        :title="t('admin.accountSessions.cleanup')"
        :message="t('admin.accountSessions.cleanupConfirm', { count: selectedAccountIds.length })"
        :confirm-text="t('admin.accountSessions.cleanup')"
        danger
        @cancel="cleanupConfirm = false"
        @confirm="runCleanup"
      />
      <ConfirmDialog
        :show="revokeConfirm"
        :title="t('admin.accountSessions.revokeSelected')"
        :message="t('admin.accountSessions.revokeConfirm', { count: selectedSessionKeys.length })"
        :confirm-text="t('admin.accountSessions.revoke')"
        danger
        @cancel="revokeConfirm = false"
        @confirm="revokeSelected"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Account, OpenAIAccountSession } from '@/types'

type SessionRow = { key: string; account: Account; session: OpenAIAccountSession }
const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<Account[]>([])
const accountSearch = ref('')
const queriedAccountCount = ref(0)
const selectedAccountIds = ref<number[]>([])
const sessionRows = ref<SessionRow[]>([])
const selectedSessionKeys = ref<string[]>([])
const loadingAccounts = ref(false)
const querying = ref(false)
const cleaning = ref(false)
const revoking = ref(false)
const trustingKey = ref<string | null>(null)
const queried = ref(false)
const accountLoadError = ref('')
const sessionError = ref('')
const cleanupLoading = ref(false)
const cleanupSaving = ref(false)
const cleanupLoadError = ref('')
const cleanupEnabled = ref(false)
const cleanupInterval = ref(60)
const cleanupConfirm = ref(false)
const revokeConfirm = ref(false)

const deviceShape = (session: OpenAIAccountSession): 'phone' | 'tablet' | 'desktop' => {
  const description = [session.device_type, session.device_name, session.os, session.app_name].filter(Boolean).join(' ').toLowerCase()
  if (/ipad|tablet|galaxy[\s-]*tab|平板/.test(description)) return 'tablet'
  if (/iphone|ipod|phone|mobile|android|\bios\b|手机/.test(description)) return 'phone'
  return 'desktop'
}

const busy = computed(() => querying.value || cleaning.value || revoking.value || trustingKey.value !== null)
const filteredAccounts = computed(() => {
  const search = accountSearch.value.trim().toLocaleLowerCase()
  return search ? accounts.value.filter(account => account.name.toLocaleLowerCase().includes(search) || String(account.id).includes(search.replace(/^#/, ''))) : accounts.value
})
const overviewItems = computed(() => [
  { label: t('admin.accountSessions.overview.accounts'), value: loadingAccounts.value || accountLoadError.value ? '—' : accounts.value.length, hint: t('admin.accountSessions.overview.accountsHint') },
  { label: t('admin.accountSessions.overview.selected'), value: selectedAccountIds.value.length, hint: t('admin.accountSessions.overview.selectedHint') },
  { label: t('admin.accountSessions.overview.sessions'), value: queried.value ? sessionRows.value.length : '—', hint: t('admin.accountSessions.overview.sessionsHint') },
  { label: t('admin.accountSessions.overview.trusted'), value: queried.value ? sessionRows.value.filter(row => row.session.trusted).length : '—', hint: t('admin.accountSessions.overview.sessionsHint'), tone: 'positive' as const },
])

const allAccountsSelected = computed(() => accounts.value.length > 0 && accounts.value.every(account => selectedAccountIds.value.includes(account.id)))

const loadCleanupSettings = async () => {
  cleanupLoading.value = true
  cleanupLoadError.value = ''
  try {
    const settings = await adminAPI.settings.getOpenAISessionCleanupSettings()
    cleanupEnabled.value = Boolean(settings.enabled)
    cleanupInterval.value = Number(settings.interval_minutes) || 60
  } catch {
    cleanupLoadError.value = t('admin.accountSessions.globalCleanup.loadFailed')
  } finally {
    cleanupLoading.value = false
  }
}

const saveCleanupSettings = async () => {
  const interval = Math.trunc(Number(cleanupInterval.value))
  if (!Number.isInteger(interval) || interval < 5 || interval > 10080) {
    appStore.showError(t('admin.accountSessions.globalCleanup.invalidInterval'))
    return
  }
  cleanupSaving.value = true
  try {
    const settings = await adminAPI.settings.updateOpenAISessionCleanupSettings({ enabled: cleanupEnabled.value, interval_minutes: interval })
    cleanupEnabled.value = Boolean(settings.enabled)
    cleanupInterval.value = settings.interval_minutes
    appStore.showSuccess(t('admin.accountSessions.globalCleanup.saved'))
  } catch {
    appStore.showError(t('admin.accountSessions.globalCleanup.saveFailed'))
  } finally {
    cleanupSaving.value = false
  }
}

const loadAccounts = async () => {
  loadingAccounts.value = true
  accountLoadError.value = ''
  try {
    const loaded: Account[] = []
    let page = 1
    let pages = 1
    while (page <= pages) {
      const result = await adminAPI.accounts.list(page, 200, { platform: 'openai', type: 'oauth', status: 'active' })
      const pageItems = result.items || []
      loaded.push(...pageItems)
      pages = result.pages && result.pages > 0
        ? result.pages
        : pageItems.length === 200 ? page + 1 : page
      if (pageItems.length === 0) break
      page++
    }
    accounts.value = loaded.filter(account => account.parent_account_id == null)
    selectedAccountIds.value = accounts.value.map(account => account.id)
  } catch {
    accountLoadError.value = t('admin.accountSessions.queryFailed')
  } finally {
    loadingAccounts.value = false
  }
}

const loadAll = async () => {
  await Promise.all([loadAccounts(), loadCleanupSettings()])
  queried.value = false
  queriedAccountCount.value = 0
  sessionError.value = ''
  sessionRows.value = []
  selectedSessionKeys.value = []
}

const toggleAllAccounts = () => {
  selectedAccountIds.value = allAccountsSelected.value ? [] : accounts.value.map(account => account.id)
}

const querySessions = async () => {
  if (selectedAccountIds.value.length === 0) return
  querying.value = true
  sessionError.value = ''
  const selected = new Set(selectedAccountIds.value)
  const rows: SessionRow[] = []
  let failures = 0
  try {
    const targets = accounts.value.filter(account => selected.has(account.id))
    let nextIndex = 0
    const worker = async () => {
      while (nextIndex < targets.length) {
        const account = targets[nextIndex++]
        try {
          const result = await adminAPI.accounts.listOpenAISessions(account.id)
          for (const session of result.sessions || []) rows.push({ key: `${account.id}:${session.id}`, account, session })
        } catch {
          failures++
        }
      }
    }
    await Promise.all(Array.from({ length: Math.min(8, targets.length) }, () => worker()))
    sessionRows.value = rows.sort((a, b) => a.account.id - b.account.id || a.key.localeCompare(b.key))
    selectedSessionKeys.value = []
    queriedAccountCount.value = targets.length
    queried.value = true
    if (failures > 0) sessionError.value = t('admin.accountSessions.queryFailed')
    else appStore.showSuccess(t('admin.accountSessions.querySuccess', { count: selected.size }))
  } finally {
    querying.value = false
  }
}

const runCleanup = async () => {
  cleanupConfirm.value = false
  cleaning.value = true
  let success = 0
  let failed = 0
  try {
    // Keep each request within the backend's bounded batch size while still
    // allowing the menu to operate on every selected account in a large
    // installation.
    for (let start = 0; start < selectedAccountIds.value.length; start += 100) {
      const result = await adminAPI.accounts.runOpenAISessionCleanupBatch(selectedAccountIds.value.slice(start, start + 100))
      success += result.success_count || 0
      failed += result.failed_count || 0
    }
    appStore.showSuccess(t('admin.accountSessions.cleanupSuccess', { success, failed }))
    await querySessions()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountSessions.cleanupFailed')))
  } finally {
    cleaning.value = false
  }
}

const revokeSelected = async () => {
  revokeConfirm.value = false
  revoking.value = true
  const grouped = new Map<number, string[]>()
  for (const key of selectedSessionKeys.value) {
    const [accountID, ...parts] = key.split(':')
    const id = Number(accountID)
    if (!id || parts.length === 0) continue
    grouped.set(id, [...(grouped.get(id) || []), parts.join(':')])
  }
  let count = 0
  try {
    for (const [accountID, ids] of grouped) {
      const result = await adminAPI.accounts.revokeOpenAISessions(accountID, ids)
      count += result.success_count || 0
    }
    appStore.showSuccess(t('admin.accountSessions.revokeSuccess', { count }))
    await querySessions()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountSessions.revokeFailed')))
  } finally {
    revoking.value = false
  }
}

const trustCurrentSession = async (row: SessionRow) => {
  if (!row.session.current || row.session.trusted || trustingKey.value !== null) return
  if (typeof adminAPI.accounts.trustOpenAISession !== 'function') {
    appStore.showError(t('admin.accountSessions.trustFailed'))
    return
  }
  trustingKey.value = row.key
  try {
    await adminAPI.accounts.trustOpenAISession(row.account.id, row.session.id || undefined)
    row.session.trusted = true
    appStore.showSuccess(t('admin.accountSessions.trustSuccess'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.accountSessions.trustFailed')))
  } finally {
    trustingKey.value = null
  }
}

const formatTime = (value?: string) => value ? formatDateTime(value) : '-'

onMounted(() => { void loadAll() })
</script>

<style scoped>
.account-sessions-workspace {
  @apply space-y-5;
}
.cleanup-settings {
  @apply flex flex-col gap-5 p-5;
}
.cleanup-heading {
  @apply flex items-start gap-3;
}
.settings-icon {
  @apply flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-400;
}
.cleanup-controls {
  @apply flex flex-wrap items-end gap-x-5 gap-y-4;
}
.cleanup-toggle {
  @apply inline-flex min-h-10 cursor-pointer items-center gap-2.5 text-xs font-medium text-gray-700 dark:text-gray-300;
}
.toggle-track {
  @apply relative inline-flex h-5 w-9 shrink-0 rounded-full bg-gray-200 transition-colors peer-checked:bg-primary-600 peer-focus-visible:ring-2 peer-focus-visible:ring-primary-500 peer-focus-visible:ring-offset-2 peer-disabled:opacity-50 dark:bg-dark-600 dark:peer-focus-visible:ring-offset-dark-900;
}
.toggle-track > span {
  @apply absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-transform;
}
.peer:checked + .toggle-track > span {
  transform: translateX(1rem);
}
.cleanup-interval {
  @apply flex items-center gap-3 text-xs font-medium text-gray-500 dark:text-gray-400;
}
.cleanup-interval .input {
  @apply w-24 text-sm tabular-nums;
}
.cleanup-hint {
  @apply basis-full text-[11px] leading-5 text-gray-400 dark:text-gray-500;
}
.session-layout {
  @apply grid items-start gap-5;
  grid-template-columns: minmax(0, 1fr);
}
.account-panel,
.sessions-panel {
  @apply min-w-0 overflow-hidden;
}
.panel-heading {
  @apply p-5;
}
.count-badge {
  @apply rounded-md bg-gray-100 px-2 py-0.5 text-[11px] font-semibold tabular-nums text-gray-500 dark:bg-dark-700 dark:text-gray-400;
}
.account-selection-bar {
  @apply flex flex-wrap items-center justify-between gap-2 border-y border-gray-100 bg-gray-50/60 px-5 py-3 dark:border-dark-700 dark:bg-dark-800/40;
}
.session-checkbox {
  @apply h-4 w-4 shrink-0 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-500 dark:bg-dark-800 dark:focus:ring-offset-dark-800;
}
.account-list {
  @apply max-h-72 space-y-1 overflow-y-auto p-2;
  scrollbar-width: thin;
}
.account-option {
  @apply flex cursor-pointer items-center gap-3 rounded-lg border border-transparent px-3 py-3 transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/40;
}
.account-option.is-selected {
  @apply border-primary-100 bg-primary-50/60 dark:border-primary-500/15 dark:bg-primary-500/5;
}
.account-option.is-disabled {
  @apply cursor-default;
}
.account-empty {
  @apply flex min-h-40 flex-col justify-center px-5 py-8 text-center text-xs leading-6 text-gray-500 dark:text-gray-400;
}
.account-footer {
  @apply border-t border-gray-100 px-5 py-3 text-[11px] leading-5 text-gray-400 dark:border-dark-700 dark:text-gray-500;
}
.sessions-heading {
  @apply flex flex-wrap items-center justify-between gap-4 border-b border-gray-100 p-5 dark:border-dark-700;
}
.cleanup-button {
  @apply min-h-9 text-xs;
}
.session-selection-bar {
  @apply flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 bg-gray-50/60 px-5 py-2.5 dark:border-dark-700 dark:bg-dark-800/40;
}
.revoke-button {
  @apply inline-flex items-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium text-red-600 transition-colors hover:bg-red-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-red-500 disabled:cursor-not-allowed disabled:text-gray-400 disabled:hover:bg-transparent dark:text-red-400 dark:hover:bg-red-500/10 dark:disabled:text-gray-600;
}
.session-alert {
  @apply m-4 flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs leading-5 text-amber-700 dark:border-amber-500/20 dark:bg-amber-500/5 dark:text-amber-300;
}
.session-empty {
  @apply flex min-h-80 flex-col items-center justify-center px-6 py-14 text-center;
}
.empty-device-icon {
  @apply mb-5 flex h-16 w-16 items-center justify-center rounded-2xl border border-primary-100 bg-primary-50/60 text-primary-600 dark:border-primary-500/15 dark:bg-primary-500/5 dark:text-primary-400;
}
.session-empty h3 {
  @apply text-base font-semibold text-gray-800 dark:text-gray-100;
}
.session-empty p {
  @apply mt-2 max-w-sm text-xs leading-6 text-gray-500 dark:text-gray-400;
}
.session-list {
  @apply divide-y divide-gray-100 dark:divide-dark-700;
}
.device-row {
  @apply flex items-start gap-3 px-5 py-5 transition-colors;
}
.device-row.is-selected {
  @apply bg-primary-50/50 dark:bg-primary-500/5;
}
.device-selection {
  @apply flex h-10 w-4 shrink-0 items-center justify-center;
}
.device-icon {
  @apply flex h-10 w-10 shrink-0 items-center justify-center rounded-xl border border-gray-100 bg-gray-50 text-gray-400 dark:border-dark-700 dark:bg-dark-800 dark:text-gray-500;
}
.device-icon.is-current {
  @apply border-primary-100 bg-primary-50 text-primary-600 dark:border-primary-500/20 dark:bg-primary-500/5 dark:text-primary-400;
}
.session-badge {
  @apply inline-flex items-center gap-1 rounded-md px-1.5 py-0.5 text-[10px] font-medium leading-4;
}
.session-badge-current {
  @apply bg-primary-50 text-primary-700 dark:bg-primary-500/10 dark:text-primary-400;
}
.session-badge-trusted {
  @apply bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.trust-button {
  @apply mt-3 inline-flex items-center gap-1.5 rounded-md border border-primary-200 px-2.5 py-1.5 text-[11px] font-medium text-primary-700 transition-colors hover:bg-primary-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-50 dark:border-primary-500/20 dark:text-primary-400 dark:hover:bg-primary-500/10;
}
.session-footer {
  @apply flex items-start gap-2 border-t border-gray-100 px-5 py-3 text-[11px] leading-5 text-gray-400 dark:border-dark-700 dark:text-gray-500;
}
@media (min-width: 1024px) {
  .session-layout {
    grid-template-columns: 288px minmax(0, 1fr);
  }
  .account-list {
    max-height: 540px;
  }
}
@media (min-width: 1536px) {
  .cleanup-settings {
    @apply flex-row items-center justify-between gap-8;
  }
  .cleanup-heading {
    @apply max-w-md;
  }
  .cleanup-controls {
    @apply max-w-2xl flex-1;
  }
}
@media (max-width: 639px) {
  .device-row {
    @apply gap-2.5 px-4;
  }
  .device-icon {
    @apply hidden;
  }
  .cleanup-controls {
    @apply grid items-end gap-x-3 gap-y-4;
    grid-template-columns: minmax(0, 1fr) auto;
  }
  .cleanup-toggle,
  .cleanup-hint {
    grid-column: 1 / -1;
  }
  .cleanup-interval {
    @apply min-w-0 flex-col items-start gap-2;
  }
  .cleanup-interval > span {
    @apply whitespace-nowrap text-[11px];
  }
  .cleanup-interval .input {
    @apply w-full;
  }
  .cleanup-controls > button {
    @apply whitespace-nowrap;
  }
}
@media (prefers-reduced-motion: reduce) {
  .account-sessions-workspace * {
    animation: none !important;
    transition: none !important;
  }
}
</style>
