<template>
  <AppLayout>
    <div class="space-y-6">
      <header class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.accountSessions.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.description') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="loadingAccounts || querying || cleaning || revoking" @click="loadAll">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loadingAccounts || querying }" />
            {{ t('admin.accountSessions.refresh') }}
          </button>
          <button type="button" class="btn btn-primary" :disabled="querying || cleaning || revoking || selectedAccountIds.length === 0" @click="querySessions">
            <Icon name="search" size="sm" />
            {{ querying ? t('admin.accountSessions.querying') : t('admin.accountSessions.query') }}
          </button>
          <button
            type="button"
            class="btn bg-violet-600 text-white hover:bg-violet-700"
            :disabled="cleaning || querying || revoking || selectedAccountIds.length === 0"
            @click="cleanupConfirm = true"
          >
            <Icon name="shield" size="sm" :class="{ 'animate-pulse': cleaning }" />
            {{ cleaning ? t('admin.accountSessions.cleaning') : t('admin.accountSessions.cleanup') }}
          </button>
        </div>
      </header>

      <section class="card space-y-4 p-5" data-testid="global-session-cleanup-settings">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.accountSessions.globalCleanup.title') }}</h2>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.globalCleanup.description') }}</p>
        </div>
        <div v-if="cleanupLoading" class="text-sm text-gray-500">{{ t('common.loading') }}</div>
        <div v-else-if="cleanupLoadError" class="flex flex-wrap items-center gap-2 text-sm text-red-600">
          <span>{{ cleanupLoadError }}</span>
          <button type="button" class="btn btn-secondary text-xs" @click="loadCleanupSettings">{{ t('common.retry') }}</button>
        </div>
        <div v-else class="flex flex-wrap items-end gap-4">
          <label class="inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="cleanupEnabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            {{ t('admin.accountSessions.globalCleanup.enabled') }}
          </label>
          <label class="w-56">
            <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">{{ t('admin.accountSessions.globalCleanup.interval') }}</span>
            <input v-model.number="cleanupInterval" type="number" min="5" max="10080" step="1" class="input" :disabled="!cleanupEnabled" />
          </label>
          <button type="button" class="btn btn-secondary" :disabled="cleanupSaving" @click="saveCleanupSettings">
            <Icon v-if="cleanupSaving" name="refresh" size="sm" class="animate-spin" />
            {{ cleanupSaving ? t('admin.accountSessions.globalCleanup.saving') : t('admin.accountSessions.globalCleanup.save') }}
          </button>
          <p class="basis-full text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.globalCleanup.hint') }}</p>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div class="flex items-center gap-3">
            <input
              type="checkbox"
              class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allAccountsSelected"
              :disabled="accounts.length === 0"
              @change="toggleAllAccounts"
            />
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.accountSessions.accounts') }}</h2>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.selectedAccounts', { count: selectedAccountIds.length }) }}</span>
          </div>
          <span v-if="loadingAccounts" class="text-sm text-gray-500">{{ t('common.loading') }}</span>
        </div>
        <div v-if="accountLoadError" class="p-5 text-sm text-red-600">{{ accountLoadError }}</div>
        <div v-else-if="accounts.length === 0 && !loadingAccounts" class="p-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.noAccounts') }}</div>
        <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
          <label v-for="account in accounts" :key="account.id" class="flex cursor-pointer items-center gap-3 px-5 py-3 hover:bg-gray-50 dark:hover:bg-dark-800/60">
            <input v-model="selectedAccountIds" type="checkbox" :value="account.id" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span class="min-w-0 flex-1 truncate text-sm font-medium text-gray-800 dark:text-gray-200">{{ account.name }}</span>
            <span class="font-mono text-xs text-gray-400">#{{ account.id }}</span>
          </label>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-dark-700">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.accountSessions.sessions') }}</h2>
            <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accountSessions.selectedSessions', { count: selectedSessionKeys.length }) }}</span>
          </div>
          <button type="button" class="btn btn-danger" :disabled="revoking || querying || cleaning || selectedSessionKeys.length === 0" @click="revokeConfirm = true">
            <Icon name="login" size="sm" /> {{ t('admin.accountSessions.revokeSelected') }}
          </button>
        </div>
        <div v-if="sessionError" class="p-5 text-sm text-amber-700 dark:text-amber-300">{{ sessionError }}</div>
        <div v-if="sessionRows.length === 0" class="p-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ queried ? t('admin.accountSessions.noSessions') : t('admin.accountSessions.queryBeforeRevoke') }}</div>
        <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
          <label v-for="row in sessionRows" :key="row.key" class="flex items-start gap-3 px-5 py-4 hover:bg-gray-50 dark:hover:bg-dark-800/60" :class="{ 'opacity-60': row.session.current || !row.session.can_revoke }">
            <input v-if="row.session.can_revoke && !row.session.current" v-model="selectedSessionKeys" type="checkbox" :value="row.key" class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span v-else class="h-4 w-4" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="font-medium text-gray-900 dark:text-white">{{ row.session.device_name || row.session.browser || t('admin.accountSessions.unknown') }}</span>
                <span v-if="row.session.current" class="rounded-full bg-blue-100 px-2 py-0.5 text-xs text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">{{ t('admin.accountSessions.current') }}</span>
                <span v-if="row.session.trusted" class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">{{ t('admin.accountSessions.trusted') }}</span>
              </div>
              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ row.account.name }} · {{ [row.session.app_name, row.session.browser, row.session.os, row.session.location].filter(Boolean).join(' · ') || '-' }}</div>
              <div class="mt-1 text-xs text-gray-400">{{ formatTime(row.session.last_active_at || row.session.signed_in_at) }}</div>
            </div>
          </label>
        </div>
      </section>

      <div v-if="cleanupConfirm || revokeConfirm" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="cleanupConfirm = revokeConfirm = false">
        <div class="card w-full max-w-md space-y-4 p-5">
          <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ cleanupConfirm ? t('admin.accountSessions.cleanup') : t('admin.accountSessions.revokeSelected') }}</h3>
          <p class="text-sm text-gray-600 dark:text-gray-300">{{ cleanupConfirm ? t('admin.accountSessions.cleanupConfirm', { count: selectedAccountIds.length }) : t('admin.accountSessions.revokeConfirm', { count: selectedSessionKeys.length }) }}</p>
          <div class="flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="cleanupConfirm = revokeConfirm = false">{{ t('common.cancel') }}</button>
            <button v-if="cleanupConfirm" type="button" class="btn bg-violet-600 text-white" :disabled="cleaning" @click="runCleanup">{{ t('admin.accountSessions.cleanup') }}</button>
            <button v-else type="button" class="btn btn-danger" :disabled="revoking" @click="revokeSelected">{{ t('admin.accountSessions.revoke') }}</button>
          </div>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Account, OpenAIAccountSession } from '@/types'

type SessionRow = { key: string; account: Account; session: OpenAIAccountSession }
const { t } = useI18n()
const appStore = useAppStore()
const accounts = ref<Account[]>([])
const selectedAccountIds = ref<number[]>([])
const sessionRows = ref<SessionRow[]>([])
const selectedSessionKeys = ref<string[]>([])
const loadingAccounts = ref(false)
const querying = ref(false)
const cleaning = ref(false)
const revoking = ref(false)
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

const formatTime = (value?: string) => value ? formatDateTime(value) : '-'

onMounted(() => { void loadAll() })
</script>
