<template>
  <BaseDialog
    :show="show"
    :title="t('admin.proxies.bindings.title')"
    width="wide"
    :close-on-escape="false"
    @close="handleClose"
  >
    <div class="space-y-5" :aria-busy="busy || loading">
      <div class="flex flex-col gap-4 rounded-2xl border border-teal-100 bg-teal-50/60 p-5 dark:border-teal-900/70 dark:bg-teal-950/20 sm:flex-row sm:items-center sm:justify-between">
        <div class="flex min-w-0 items-center gap-3">
          <div class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-white text-teal-600 shadow-sm dark:bg-teal-900/40 dark:text-teal-300">
            <Icon name="globe" size="lg" />
          </div>
          <div class="min-w-0">
            <p class="text-xs font-medium text-teal-700 dark:text-teal-400">{{ t('admin.proxies.bindings.source') }}</p>
            <h4 class="mt-0.5 break-all font-semibold text-gray-900 dark:text-white">{{ proxy?.name }}</h4>
            <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-dark-400">{{ proxy?.protocol }}://{{ formatProxyHostPort(proxy) }}</p>
          </div>
        </div>
        <div class="shrink-0 text-left sm:text-right">
          <p class="text-2xl font-semibold tabular-nums text-gray-900 dark:text-white">{{ loading || accountsError ? '—' : accounts.length }}</p>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.boundAccounts') }}</p>
        </div>
      </div>

      <div v-if="outcome" role="status" data-testid="binding-outcome" class="rounded-xl border p-4 text-sm" :class="outcome.tone === 'success' ? 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-300' : 'border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-900 dark:bg-amber-950/30 dark:text-amber-300'">
        <p class="font-medium">{{ outcome.message }}</p>
        <ul v-if="outcome.details.length" class="mt-2 max-h-32 space-y-1 overflow-auto text-xs">
          <li v-for="(detail, index) in outcome.details" :key="index" class="break-words">{{ detail }}</li>
        </ul>
      </div>

      <div v-if="actionError" role="alert" data-testid="binding-error" class="rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-300">
        {{ actionError }}
      </div>

      <div v-if="loading" role="status" class="flex items-center justify-center gap-2 py-12 text-sm text-gray-500 dark:text-dark-400">
        <Icon name="refresh" size="md" class="animate-spin" />
        {{ t('common.loading') }}
      </div>

      <div v-else-if="accountsError" class="rounded-2xl border border-dashed border-gray-200 px-4 py-10 text-center dark:border-dark-700">
        <Icon name="exclamationCircle" size="xl" class="mx-auto text-gray-400" />
        <p role="alert" class="mt-3 text-sm text-gray-600 dark:text-dark-300">{{ accountsError }}</p>
        <button type="button" class="btn btn-secondary mt-4" :disabled="busy" data-testid="retry-accounts" @click="loadData">{{ t('common.retry') }}</button>
      </div>

      <template v-else-if="action">
        <div class="flex items-start gap-3">
          <div class="mt-0.5 rounded-xl p-2.5" :class="action === 'unlink' ? 'bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-400' : 'bg-teal-50 text-teal-600 dark:bg-teal-900/20 dark:text-teal-400'">
            <Icon :name="action === 'unlink' ? 'link' : 'arrowsUpDown'" size="lg" />
          </div>
          <div>
            <h4 class="font-semibold text-gray-900 dark:text-white">{{ t(action === 'unlink' ? 'admin.proxies.bindings.unlinkTitle' : 'admin.proxies.bindings.rebindTitle', { count: operationIds.length }) }}</h4>
            <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t(action === 'unlink' ? 'admin.proxies.bindings.unlinkDescription' : 'admin.proxies.bindings.rebindDescription') }}</p>
          </div>
        </div>

        <div class="rounded-xl border border-gray-200 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-800/40">
          <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.selectedAccounts', { count: operationIds.length }) }}</p>
          <div class="mt-2 flex max-h-28 flex-wrap gap-2 overflow-auto">
            <span v-for="account in operationAccounts" :key="account.id" class="max-w-full break-all rounded-lg border border-gray-200 bg-white px-2.5 py-1 text-xs text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200">{{ account.name }} <span class="text-gray-400">#{{ account.id }}</span></span>
          </div>
        </div>

        <div v-if="action === 'rebind'" class="space-y-3">
          <div>
            <label for="proxy-binding-target" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.proxies.bindings.target') }}</label>
            <Select
              id="proxy-binding-target"
              v-model="targetId"
              data-testid="binding-target"
              :options="targetOptions"
              :searchable="true"
              :disabled="busy || !!targetsError"
              :placeholder="t('admin.proxies.bindings.selectTarget')"
              :search-placeholder="t('admin.proxies.bindings.searchTarget')"
              :empty-text="t('admin.proxies.bindings.noTargets')"
              :aria-label="t('admin.proxies.bindings.target')"
              @change="actionError = ''"
            />
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.targetsHint') }}</p>
          </div>
          <div v-if="targetsError" role="alert" class="flex flex-wrap items-center justify-between gap-2 rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-300">
            <span>{{ targetsError }}</span>
            <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="loadData">{{ t('common.retry') }}</button>
          </div>
          <p v-else-if="!availableTargets.length" class="rounded-xl bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-300">{{ t('admin.proxies.bindings.noTargets') }}</p>
          <p v-if="overCapacity" data-testid="capacity-warning" class="rounded-xl bg-amber-50 p-3 text-sm leading-6 text-amber-800 dark:bg-amber-950/30 dark:text-amber-300">{{ t('admin.proxies.bindings.capacityWarning') }}</p>
        </div>

        <div class="flex flex-col items-stretch gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700 sm:flex-row sm:items-center">
          <div class="min-w-0 flex-1">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.source') }}</p>
            <p class="mt-1 break-all text-sm font-medium text-gray-800 dark:text-dark-100">{{ proxy?.name }}</p>
            <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-dark-400">{{ formatProxyHostPort(proxy) }}</p>
          </div>
          <Icon name="arrowRight" size="md" class="shrink-0 rotate-90 self-center text-gray-400 sm:rotate-0" />
          <div class="min-w-0 flex-1">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.afterChange') }}</p>
            <p class="mt-1 break-all text-sm font-medium" :class="action === 'unlink' ? 'text-amber-700 dark:text-amber-400' : 'text-teal-700 dark:text-teal-400'">{{ action === 'unlink' ? t('admin.proxies.bindings.noProxy') : selectedTarget?.name || t('admin.proxies.bindings.selectTarget') }}</p>
            <p v-if="action === 'rebind' && selectedTarget" class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-dark-400">{{ formatProxyHostPort(selectedTarget) }}</p>
          </div>
        </div>
        <p class="flex items-start gap-2 text-xs leading-5 text-gray-500 dark:text-dark-400"><Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />{{ t('admin.proxies.bindings.shadowSync') }}</p>
      </template>

      <template v-else>
        <p class="text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.description') }}</p>
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div class="relative min-w-0 flex-1 sm:max-w-sm">
            <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="search" type="search" class="input w-full pl-10" :placeholder="t('admin.proxies.bindings.search')" :aria-label="t('admin.proxies.bindings.search')" :disabled="busy" data-testid="account-search" />
          </div>
          <button type="button" class="btn btn-secondary btn-sm self-start sm:self-auto" :disabled="busy" data-testid="refresh-accounts" @click="loadData"><Icon name="refresh" size="sm" class="mr-1.5" />{{ t('common.refresh') }}</button>
        </div>

        <div v-if="accounts.length" class="overflow-hidden rounded-2xl border border-gray-200 dark:border-dark-700">
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 bg-gray-50/80 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/60">
            <label class="flex cursor-pointer items-center gap-2.5 text-xs font-medium text-gray-600 dark:text-dark-300">
              <input type="checkbox" class="checkbox" :checked="allVisibleSelected" :indeterminate="someVisibleSelected && !allVisibleSelected" :disabled="busy || !visibleSelectable.length" :aria-label="t('admin.proxies.bindings.selectAll')" data-testid="select-all-accounts" @change="toggleAll" />
              {{ t('admin.proxies.bindings.selectAll') }}
            </label>
            <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.selectedCount', { count: selectedIds.length }) }}</span>
          </div>
          <div v-if="visibleAccounts.length" class="max-h-[360px] divide-y divide-gray-100 overflow-y-auto dark:divide-dark-700/70">
            <div v-for="account in visibleAccounts" :key="account.id" :data-testid="`proxy-account-${account.id}`" class="flex items-start gap-3 px-4 py-4 transition-colors" :class="selectedIds.includes(account.id) ? 'bg-teal-50/40 dark:bg-teal-950/20' : 'bg-white dark:bg-dark-900'">
              <input type="checkbox" class="checkbox mt-1 shrink-0" :checked="selectedIds.includes(account.id)" :disabled="busy || isShadow(account)" :aria-label="t('admin.proxies.bindings.selectAccount', { name: account.name })" :data-testid="`select-account-${account.id}`" @change="toggleAccount(account.id)" />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <p class="break-all text-sm font-semibold text-gray-900 dark:text-white">{{ account.name }}</p>
                  <span class="font-mono text-[11px] text-gray-400 dark:text-dark-500">#{{ account.id }}</span>
                </div>
                <div class="mt-1.5"><PlatformTypeBadge :platform="account.platform" :type="account.type" /></div>
                <p v-if="account.notes" class="mt-2 break-words text-xs leading-5 text-gray-500 dark:text-dark-400">{{ account.notes }}</p>
                <p v-if="isShadow(account)" class="mt-2 flex items-start gap-1.5 text-xs leading-5 text-gray-500 dark:text-dark-400"><Icon name="link" size="xs" class="mt-1 shrink-0" />{{ t('admin.proxies.bindings.shadowAccount', { id: account.parent_account_id }) }}</p>
                <div v-else class="mt-3 flex flex-wrap gap-2 sm:hidden">
                  <button type="button" class="binding-action" :disabled="busy" :data-testid="`rebind-account-${account.id}-mobile`" @click="beginAction('rebind', [account.id])">{{ t('admin.proxies.bindings.rebind') }}</button>
                  <button type="button" class="binding-action binding-action-unlink" :disabled="busy" :data-testid="`unlink-account-${account.id}-mobile`" @click="beginAction('unlink', [account.id])">{{ t('admin.proxies.bindings.unlink') }}</button>
                </div>
              </div>
              <div v-if="!isShadow(account)" class="hidden shrink-0 items-center gap-1.5 sm:flex">
                <button type="button" class="binding-action" :disabled="busy" :data-testid="`rebind-account-${account.id}`" @click="beginAction('rebind', [account.id])">{{ t('admin.proxies.bindings.rebind') }}</button>
                <button type="button" class="binding-action binding-action-unlink" :disabled="busy" :data-testid="`unlink-account-${account.id}`" @click="beginAction('unlink', [account.id])">{{ t('admin.proxies.bindings.unlink') }}</button>
              </div>
            </div>
          </div>
          <div v-else class="px-4 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.noMatches') }}</div>
        </div>
        <div v-else class="rounded-2xl border border-dashed border-gray-200 px-4 py-10 text-center dark:border-dark-700">
          <Icon name="users" size="xl" class="mx-auto text-gray-300 dark:text-dark-500" />
          <p class="mt-3 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.proxies.accountsEmpty') }}</p>
          <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.proxies.bindings.emptyHint') }}</p>
        </div>
        <p v-if="accounts.some(isShadow)" class="flex items-start gap-2 text-xs leading-5 text-gray-500 dark:text-dark-400"><Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />{{ t('admin.proxies.bindings.shadowSync') }}</p>
      </template>
    </div>

    <template #footer>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <button type="button" class="btn btn-secondary" :disabled="busy" data-testid="binding-back" @click="action ? cancelAction() : handleClose()">{{ action ? t('common.back') : t('common.close') }}</button>
        <button v-if="action" type="button" class="btn" :class="action === 'unlink' ? 'bg-amber-600 text-white hover:bg-amber-700 focus:ring-amber-500' : 'btn-primary'" :disabled="busy || loading || !!accountsError || (action === 'rebind' && (!selectedTarget || !!targetsError))" data-testid="confirm-binding" @click="submit">
          <Icon v-if="busy" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ busy ? t('admin.proxies.bindings.submitting') : t(action === 'unlink' ? 'admin.proxies.bindings.confirmUnlink' : 'admin.proxies.bindings.confirmRebind', { count: operationIds.length }) }}
        </button>
        <div v-else-if="!accountsError && accounts.length" class="flex flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" :disabled="busy || loading || !selectedIds.length" data-testid="bulk-unlink" @click="beginAction('unlink', selectedIds)">{{ t('admin.proxies.bindings.bulkUnlink') }}</button>
          <button type="button" class="btn btn-primary" :disabled="busy || loading || !selectedIds.length" data-testid="bulk-rebind" @click="beginAction('rebind', selectedIds)"><Icon name="arrowsUpDown" size="sm" class="mr-1.5" />{{ t('admin.proxies.bindings.bulkRebind') }}</button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { Proxy, ProxyAccountSummary } from '@/types'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ show: boolean; proxy: Proxy | null }>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'updated'): void }>()
const { t } = useI18n()
type BindingAction = 'unlink' | 'rebind'
type Outcome = { tone: 'success' | 'warning'; message: string; details: string[] }

const accounts = ref<ProxyAccountSummary[]>([])
const targets = ref<Proxy[]>([])
const selectedIds = ref<number[]>([])
const search = ref('')
const loading = ref(false)
const busy = ref(false)
const accountsError = ref('')
const targetsError = ref('')
const actionError = ref('')
const outcome = ref<Outcome | null>(null)
const action = ref<BindingAction | null>(null)
const operationIds = ref<number[]>([])
const targetId = ref<number | null>(null)
let generation = 0
let requestVersion = 0

const isShadow = (account: ProxyAccountSummary) => account.parent_account_id != null && account.parent_account_id > 0
const visibleAccounts = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  return query ? accounts.value.filter(account => [account.name, account.id, account.platform, account.type, account.notes].join(' ').toLocaleLowerCase().includes(query)) : accounts.value
})
const visibleSelectable = computed(() => visibleAccounts.value.filter(account => !isShadow(account)))
const allVisibleSelected = computed(() => visibleSelectable.value.length > 0 && visibleSelectable.value.every(account => selectedIds.value.includes(account.id)))
const someVisibleSelected = computed(() => visibleSelectable.value.some(account => selectedIds.value.includes(account.id)))
const operationAccounts = computed(() => accounts.value.filter(account => operationIds.value.includes(account.id)))
const isAvailableTarget = (proxy: Proxy, sourceId: number, now = Date.now()) => proxy.id !== sourceId && proxy.status === 'active' && (!proxy.expires_at || new Date(proxy.expires_at).getTime() > now)
const availableTargets = computed(() => targets.value.filter(proxy => isAvailableTarget(proxy, props.proxy?.id ?? 0)))
const selectedTarget = computed(() => availableTargets.value.find(proxy => proxy.id === targetId.value))
const targetOptions = computed(() => availableTargets.value.map(proxy => ({
  value: proxy.id,
  label: `${proxy.name} · ${formatProxyHostPort(proxy)} · ${t('admin.proxies.bindings.targetCapacity', { count: proxy.account_count ?? 0, max: proxy.max_accounts > 0 ? proxy.max_accounts : t('admin.proxies.unlimited') })}`
})))
const overCapacity = computed(() => {
  const target = selectedTarget.value
  const shadowCount = accounts.value.filter(account => isShadow(account) && operationIds.value.includes(account.parent_account_id!)).length
  return !!target && target.max_accounts > 0 && (target.account_count ?? 0) + operationIds.value.length + shadowCount > target.max_accounts
})

function keepCurrentSelection(ids: number[]) {
  const validIds = new Set(accounts.value.filter(account => !isShadow(account)).map(account => account.id))
  return ids.filter(id => validIds.has(id))
}

function formatProxyHostPort(proxy: Pick<Proxy, 'host' | 'port'> | null | undefined) {
  if (!proxy) return ''
  const host = proxy.host.trim().replace(/^\[|\]$/g, '')
  return `${host.includes(':') ? `[${host}]` : host}:${proxy.port}`
}

function toggleAccount(id: number) {
  if (busy.value || !accounts.value.some(account => account.id === id && !isShadow(account))) return
  selectedIds.value = selectedIds.value.includes(id) ? selectedIds.value.filter(value => value !== id) : [...selectedIds.value, id]
}

function toggleAll() {
  if (busy.value) return
  const visibleIds = new Set(visibleSelectable.value.map(account => account.id))
  selectedIds.value = allVisibleSelected.value ? selectedIds.value.filter(id => !visibleIds.has(id)) : [...new Set([...selectedIds.value, ...visibleIds])]
}

function handleClose() {
  if (!busy.value) emit('close')
}

function cancelAction() {
  if (busy.value) return
  action.value = null
  operationIds.value = []
  targetId.value = null
  actionError.value = ''
}

function handleEscape(event: KeyboardEvent) {
  // A searchable Select consumes Escape before it reaches the document.
  // An unhandled Escape goes back one view, without stacking dialogs.
  if (!props.show || busy.value || event.key !== 'Escape' || event.defaultPrevented) return
  event.preventDefault()
  if (action.value) cancelAction()
  else handleClose()
}

function beginAction(nextAction: BindingAction, ids: number[]) {
  if (busy.value || loading.value || accountsError.value) return
  const validIds = keepCurrentSelection(ids)
  if (!validIds.length) return
  selectedIds.value = [...validIds]
  operationIds.value = [...validIds]
  action.value = nextAction
  targetId.value = null
  actionError.value = ''
  outcome.value = null
}

function isCurrent(sourceId: number, currentGeneration: number) {
  return props.show && props.proxy?.id === sourceId && generation === currentGeneration
}

async function loadData() {
  if (!props.show || !props.proxy) return
  const sourceId = props.proxy.id
  const currentGeneration = generation
  const version = ++requestVersion
  loading.value = true
  const [accountResult, targetResult] = await Promise.allSettled([
    adminAPI.proxies.getProxyAccounts(sourceId),
    adminAPI.proxies.getAllWithCount()
  ])
  if (!isCurrent(sourceId, currentGeneration) || version !== requestVersion) return
  if (accountResult.status === 'fulfilled') {
    accounts.value = accountResult.value
    accountsError.value = ''
    selectedIds.value = keepCurrentSelection(selectedIds.value)
    if (action.value && keepCurrentSelection(operationIds.value).length !== operationIds.value.length) {
      action.value = null
      operationIds.value = []
      actionError.value = t('admin.proxies.bindings.sourceChanged')
    }
  } else {
    accountsError.value = t('admin.proxies.accountsFailed')
  }
  if (targetResult.status === 'fulfilled') {
    targets.value = targetResult.value
    targetsError.value = ''
  } else {
    targets.value = []
    targetsError.value = t('admin.proxies.bindings.targetsFailed')
  }
  loading.value = false
}

function errorMessage(error: unknown): string {
  const value = error as { reason?: string; status?: number; response?: { status?: number; data?: { reason?: string } } }
  const normalizedError = value?.response?.data?.reason && !value.reason ? { ...value, reason: value.response.data.reason } : error
  const messages: Record<string, string> = {
    PROXY_BINDING_CHANGED: t('admin.proxies.bindings.sourceChanged'),
    PROXY_BINDING_TARGET_UNAVAILABLE: t('admin.proxies.bindings.targetUnavailable'),
    PROXY_BINDING_INPUT_INVALID: t('admin.proxies.bindings.invalidInput'),
    ACCOUNT_NOT_FOUND: t('admin.proxies.bindings.accountUnavailable')
  }
  const code = extractApiErrorCode(normalizedError)
  if (code && messages[code]) return messages[code]
  if (value?.status === 409 || value?.response?.status === 409) return t('admin.proxies.bindings.sourceChanged')
  return extractApiErrorMessage(normalizedError, t('admin.proxies.bindings.operationFailed'))
}

async function submit() {
  if (busy.value || loading.value || !action.value || !props.proxy || !operationIds.value.length) return
  const sourceId = props.proxy.id
  const currentGeneration = generation
  const submittedAction = action.value
  const ids = [...operationIds.value]
  const destinationId = submittedAction === 'unlink' ? 0 : targetId.value
  if (destinationId == null) return
  busy.value = true
  actionError.value = ''
  outcome.value = null
  let submitted = false
  try {
    // Recheck the source membership and destination immediately before the write.
    // expected_proxy_id also makes the source condition atomic on the server.
    const [accountResult, targetResult] = await Promise.allSettled([
      adminAPI.proxies.getProxyAccounts(sourceId),
      adminAPI.proxies.getAllWithCount()
    ])
    if (!isCurrent(sourceId, currentGeneration)) return
    if (accountResult.status === 'rejected') {
      actionError.value = t('admin.proxies.bindings.preflightFailed')
      return
    }
    accounts.value = accountResult.value
    accountsError.value = ''
    selectedIds.value = keepCurrentSelection(selectedIds.value)
    if (targetResult.status === 'fulfilled') {
      targets.value = targetResult.value
      targetsError.value = ''
    } else {
      targets.value = []
      targetsError.value = t('admin.proxies.bindings.targetsFailed')
    }
    if (keepCurrentSelection(ids).length !== ids.length) {
      action.value = null
      operationIds.value = []
      actionError.value = t('admin.proxies.bindings.sourceChanged')
      emit('updated')
      return
    }
    if (submittedAction === 'rebind' && targetResult.status === 'rejected') {
      actionError.value = t('admin.proxies.bindings.preflightFailed')
      return
    }
    if (submittedAction === 'rebind' && !targets.value.some(proxy => proxy.id === destinationId && isAvailableTarget(proxy, sourceId))) {
      targetId.value = null
      actionError.value = t('admin.proxies.bindings.targetUnavailable')
      return
    }
    submitted = true
    const result = await adminAPI.accounts.bulkUpdate(ids, { proxy_id: destinationId, expected_proxy_id: sourceId })
    if (!isCurrent(sourceId, currentGeneration)) return
    const details = (result.results ?? []).filter(item => !item.success).map(item => {
      const name = accounts.value.find(account => account.id === item.account_id)?.name ?? `#${item.account_id}`
      return `${name}: ${item.error || t('admin.proxies.bindings.operationFailed')}`
    })
    const fullySucceeded = result.success === ids.length && result.failed === 0 && details.length === 0 && !result.failed_ids?.length
    const countsComplete = Number.isInteger(result.success) && Number.isInteger(result.failed) && result.success >= 0 && result.failed >= 0 && result.success + result.failed === ids.length
    outcome.value = {
      tone: fullySucceeded ? 'success' : 'warning',
      message: fullySucceeded
        ? t(submittedAction === 'unlink' ? 'admin.proxies.bindings.unlinkSuccess' : 'admin.proxies.bindings.rebindSuccess', { count: result.success })
        : countsComplete ? t('admin.proxies.bindings.partialResult', { success: result.success, failed: result.failed }) : t('admin.proxies.bindings.unknownResult'),
      details
    }
    const successIds = new Set(result.success_ids ?? (result.results ?? []).filter(item => item.success).map(item => item.account_id))
    selectedIds.value = fullySucceeded ? [] : ids.filter(id => !successIds.has(id))
    action.value = null
    operationIds.value = []
    targetId.value = null
  } catch (error) {
    if (isCurrent(sourceId, currentGeneration)) {
      outcome.value = { tone: 'warning', message: t('admin.proxies.bindings.unknownResult'), details: [errorMessage(error), t('admin.proxies.bindings.reviewAfterFailure')] }
      action.value = null
      operationIds.value = []
      targetId.value = null
    }
  } finally {
    if (submitted) {
      // Refresh after any attempted write, including network errors with an unknown outcome.
      // Failed accounts remain selected only if they are still on the source IP.
      if (isCurrent(sourceId, currentGeneration)) await loadData()
      emit('updated')
    }
    if (isCurrent(sourceId, currentGeneration)) busy.value = false
  }
}

watch(() => [props.show, props.proxy?.id] as const, () => {
  generation++
  requestVersion++
  accounts.value = []
  targets.value = []
  selectedIds.value = []
  search.value = ''
  loading.value = false
  busy.value = false
  accountsError.value = ''
  targetsError.value = ''
  actionError.value = ''
  outcome.value = null
  action.value = null
  operationIds.value = []
  targetId.value = null
  if (props.show && props.proxy) void loadData()
}, { immediate: true })

onMounted(() => document.addEventListener('keydown', handleEscape))
onBeforeUnmount(() => {
  generation++
  requestVersion++
  document.removeEventListener('keydown', handleEscape)
})
</script>

<style scoped>
.binding-action {
  @apply rounded-lg border border-teal-200 bg-white px-3 py-1.5 text-xs font-medium text-teal-700 transition-colors hover:bg-teal-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-teal-500/40 disabled:cursor-not-allowed disabled:opacity-50 dark:border-teal-900 dark:bg-dark-900 dark:text-teal-400 dark:hover:bg-teal-950/40;
}

.binding-action-unlink {
  @apply border-gray-200 text-gray-500 hover:border-amber-200 hover:bg-amber-50 hover:text-amber-700 dark:border-dark-600 dark:text-dark-400 dark:hover:border-amber-900 dark:hover:bg-amber-950/30 dark:hover:text-amber-400;
}
</style>
