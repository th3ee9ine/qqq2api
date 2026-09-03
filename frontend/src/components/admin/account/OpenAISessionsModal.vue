<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.sessions.title')"
    width="wide"
    @close="emit('close')"
  >
    <div class="space-y-4">
      <div
        v-if="account"
        class="flex flex-col gap-3 rounded-xl border border-cyan-200 bg-cyan-50/70 p-4 dark:border-cyan-800/50 dark:bg-cyan-950/20 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="min-w-0">
          <div class="truncate font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</div>
          <div class="mt-1 text-xs text-gray-600 dark:text-gray-400">
            {{ t('admin.accounts.sessions.description') }}
          </div>
        </div>
        <button
          type="button"
          class="btn btn-secondary shrink-0"
          :disabled="loading || revokingId !== null || batchRevoking || cleanupRunning"
          @click="loadSessions"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('admin.accounts.sessions.refresh') }}
        </button>
      </div>

      <!-- Account-level periodic cleanup policy and its redacted runtime state. -->
      <section
        v-if="cleanupEligible"
        class="rounded-xl border border-violet-200 bg-violet-50/70 p-4 dark:border-violet-800/50 dark:bg-violet-950/20"
        data-testid="session-cleanup-settings"
      >
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">
              {{ t('admin.accounts.sessions.cleanup.title') }}
            </h3>
            <p class="mt-1 text-xs text-gray-600 dark:text-gray-400">
              {{ t('admin.accounts.sessions.cleanup.description') }}
            </p>
          </div>
          <span
            v-if="cleanupState"
            data-testid="session-cleanup-status"
            class="inline-flex shrink-0 items-center rounded-full px-2.5 py-1 text-xs font-medium"
            :class="cleanupStatusClass(cleanupState.status)"
          >
            {{ t('admin.accounts.sessions.cleanup.statusLabel') }}:
            {{ cleanupStatusLabel(cleanupState.status) }}
          </span>
        </div>

        <div v-if="cleanupLoading" class="mt-3 flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
          <Icon name="refresh" size="sm" class="animate-spin" />
          {{ t('admin.accounts.sessions.cleanup.loading') }}
        </div>
        <div v-else-if="cleanupLoadError" class="mt-3 flex flex-wrap items-center gap-2 text-xs text-red-700 dark:text-red-300">
          <span>{{ cleanupLoadError }}</span>
          <button type="button" class="btn btn-secondary text-xs" :disabled="cleanupSaving || cleanupRunning" @click="loadCleanup">
            {{ t('admin.accounts.sessions.cleanup.retry') }}
          </button>
        </div>
        <template v-else>
          <div class="mt-4 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
            <div class="flex flex-1 flex-col gap-3 sm:flex-row sm:items-end">
              <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <Toggle
                  v-model="cleanupEnabled"
                  data-testid="session-cleanup-enabled"
                  :disabled="cleanupSaving || cleanupRunning"
                />
                <span>{{ t('admin.accounts.sessions.cleanup.enabled') }}</span>
              </label>
              <label class="w-full sm:max-w-56">
                <span class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-400">
                  {{ t('admin.accounts.sessions.cleanup.interval') }}
                </span>
                <input
                  v-model.number="cleanupIntervalMinutes"
                  data-testid="session-cleanup-interval"
                  type="number"
                  min="5"
                  max="10080"
                  step="1"
                  class="input"
                  :disabled="cleanupSaving || cleanupRunning || !cleanupEnabled"
                />
                <span class="mt-1 block text-[11px] text-gray-500 dark:text-gray-400">
                  {{ t('admin.accounts.sessions.cleanup.intervalHint') }}
                </span>
              </label>
            </div>
            <div class="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                data-testid="session-cleanup-save"
                class="btn btn-secondary"
                :disabled="cleanupSaving || cleanupRunning"
                @click="saveCleanup"
              >
                <Icon v-if="cleanupSaving" name="refresh" size="sm" class="animate-spin" />
                {{ t('admin.accounts.sessions.cleanup.save') }}
              </button>
              <button
                type="button"
                data-testid="session-cleanup-run-now"
                class="btn bg-violet-600 text-white hover:bg-violet-700"
                :disabled="cleanupSaving || cleanupRunning || !cleanupEnabled || !cleanupIntervalValid"
                @click="runCleanup"
              >
                <Icon v-if="cleanupRunning" name="refresh" size="sm" class="animate-spin" />
                {{ t('admin.accounts.sessions.cleanup.runNow') }}
              </button>
            </div>
          </div>

          <div v-if="cleanupState" class="mt-4 grid grid-cols-1 gap-2 text-xs text-gray-600 dark:text-gray-400 sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <span class="block text-gray-500 dark:text-gray-500">{{ t('admin.accounts.sessions.cleanup.lastRun') }}</span>
              <span class="font-medium text-gray-800 dark:text-gray-200">{{ formatSessionTime(cleanupState.last_run_at) }}</span>
            </div>
            <div>
              <span class="block text-gray-500 dark:text-gray-500">{{ t('admin.accounts.sessions.cleanup.lastSuccess') }}</span>
              <span class="font-medium text-gray-800 dark:text-gray-200">{{ formatSessionTime(cleanupState.last_success_at) }}</span>
            </div>
            <div>
              <span class="block text-gray-500 dark:text-gray-500">{{ t('admin.accounts.sessions.cleanup.resultCounts') }}</span>
              <span class="font-medium text-gray-800 dark:text-gray-200">
                {{ t('admin.accounts.sessions.cleanup.counts', { revoked: cleanupState.revoked_count || 0, failed: cleanupState.failed_count || 0 }) }}
              </span>
            </div>
            <div>
              <span class="block text-gray-500 dark:text-gray-500">{{ t('admin.accounts.sessions.cleanup.currentDevice') }}</span>
              <span class="font-medium" :class="cleanupState.current_session_known ? 'text-emerald-700 dark:text-emerald-300' : 'text-amber-700 dark:text-amber-300'">
                {{ cleanupState.current_session_known ? t('admin.accounts.sessions.cleanup.known') : t('admin.accounts.sessions.cleanup.unknown') }}
              </span>
            </div>
          </div>
          <p
            v-if="cleanupState?.message || cleanupState?.error_code"
            data-testid="session-cleanup-message"
            class="mt-3 text-xs text-amber-700 dark:text-amber-300"
          >
            {{ cleanupStateMessage(cleanupState) }}
          </p>
        </template>
      </section>

      <div
        v-if="!loading && !loadError && sessions.length > 0"
        class="flex flex-col gap-3 rounded-xl border border-gray-200 bg-gray-50/80 p-3 dark:border-dark-700 dark:bg-dark-900/40 sm:flex-row sm:items-center sm:justify-between"
      >
        <label class="inline-flex cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input
            type="checkbox"
            data-testid="select-all-sessions"
            class="h-4 w-4 rounded border-gray-300 text-cyan-600 focus:ring-cyan-500 dark:border-dark-600 dark:bg-dark-800"
            :checked="allRevokableSelected"
            :disabled="revokableSessions.length === 0 || batchRevoking || revokingId !== null || cleanupRunning"
            @change="toggleAllSessions"
          />
          <span>{{ t('admin.accounts.sessions.selectAll') }}</span>
          <span class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.sessions.selectedCount', { count: selectedSessionIds.length }) }}
          </span>
        </label>
        <button
          v-if="selectedSessionIds.length > 0"
          type="button"
          data-testid="bulk-session-logout"
          class="btn bg-red-600 text-white hover:bg-red-700"
          :disabled="batchRevoking || revokingId !== null || cleanupRunning"
          @click="batchRevokeConfirm = true"
        >
          <Icon v-if="batchRevoking" name="refresh" size="sm" class="animate-spin" />
          <Icon v-else name="login" size="sm" />
          {{ t('admin.accounts.sessions.logoutSelected') }}
        </button>
      </div>

      <div
        v-if="revokeTarget"
        class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-900/60 dark:bg-red-950/20"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationTriangle" size="md" class="mt-0.5 shrink-0 text-red-600 dark:text-red-400" />
          <div class="min-w-0 flex-1">
            <div class="text-sm font-semibold text-red-800 dark:text-red-200">
              {{ t('admin.accounts.sessions.logoutTitle') }}
            </div>
            <p class="mt-1 text-sm text-red-700 dark:text-red-300">
              {{ t('admin.accounts.sessions.logoutConfirm', { device: sessionTitle(revokeTarget) }) }}
            </p>
            <div class="mt-3 flex flex-wrap justify-end gap-2">
              <button type="button" class="btn btn-secondary" @click="revokeTarget = null">
                {{ t('common.cancel') }}
              </button>
              <button
                type="button"
                data-testid="confirm-session-logout"
                class="btn bg-red-600 text-white hover:bg-red-700"
                :disabled="revokingId !== null || batchRevoking || cleanupRunning"
                @click="confirmRevoke"
              >
                {{ t('admin.accounts.sessions.logout') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div
        v-if="batchRevokeConfirm"
        class="rounded-xl border border-red-200 bg-red-50 p-4 dark:border-red-900/60 dark:bg-red-950/20"
      >
        <div class="flex items-start gap-3">
          <Icon name="exclamationTriangle" size="md" class="mt-0.5 shrink-0 text-red-600 dark:text-red-400" />
          <div class="min-w-0 flex-1">
            <div class="text-sm font-semibold text-red-800 dark:text-red-200">
              {{ t('admin.accounts.sessions.logoutSelectedTitle') }}
            </div>
            <p class="mt-1 text-sm text-red-700 dark:text-red-300">
              {{ t('admin.accounts.sessions.logoutSelectedConfirm', { count: selectedSessionIds.length }) }}
            </p>
            <div class="mt-3 flex flex-wrap justify-end gap-2">
              <button type="button" class="btn btn-secondary" :disabled="batchRevoking" @click="batchRevokeConfirm = false">
                {{ t('common.cancel') }}
              </button>
              <button
                type="button"
                data-testid="confirm-bulk-session-logout"
                class="btn bg-red-600 text-white hover:bg-red-700"
                :disabled="batchRevoking || selectedSessionIds.length === 0 || cleanupRunning"
                @click="confirmBatchRevoke"
              >
                <Icon v-if="batchRevoking" name="refresh" size="sm" class="animate-spin" />
                {{ t('admin.accounts.sessions.logoutSelected') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div v-if="loading" class="flex min-h-48 flex-col items-center justify-center gap-3 text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="lg" class="animate-spin text-cyan-600" />
        <span class="text-sm">{{ t('admin.accounts.sessions.loading') }}</span>
      </div>

      <div
        v-else-if="loadError"
        class="flex min-h-40 flex-col items-center justify-center gap-3 rounded-xl border border-red-200 bg-red-50 p-5 text-center dark:border-red-900/60 dark:bg-red-950/20"
      >
        <Icon name="exclamationCircle" size="lg" class="text-red-500" />
        <p class="text-sm text-red-700 dark:text-red-300">{{ loadError }}</p>
        <button type="button" class="btn btn-secondary" @click="loadSessions">
          {{ t('admin.accounts.sessions.retry') }}
        </button>
      </div>

      <div
        v-else-if="sessions.length === 0"
        class="flex min-h-40 flex-col items-center justify-center gap-3 rounded-xl border border-dashed border-gray-300 text-gray-500 dark:border-dark-600 dark:text-gray-400"
      >
        <Icon name="server" size="xl" />
        <span class="text-sm">{{ t('admin.accounts.sessions.empty') }}</span>
      </div>

      <div v-else class="space-y-3">
        <article
          v-for="(session, index) in sessions"
          :key="session.id || `session-${index}`"
          class="rounded-xl border bg-white p-4 shadow-sm transition-colors dark:bg-dark-800"
          :class="selectedSessionIds.includes(session.id)
            ? 'border-cyan-400 ring-1 ring-cyan-300 dark:border-cyan-600 dark:ring-cyan-800'
            : 'border-gray-200 dark:border-dark-700'"
        >
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
                <input
                  v-if="session.can_revoke && !session.current"
                  type="checkbox"
                  data-testid="session-select"
                  class="h-4 w-4 shrink-0 rounded border-gray-300 text-cyan-600 focus:ring-cyan-500 dark:border-dark-600 dark:bg-dark-800"
                  :checked="selectedSessionIds.includes(session.id)"
                  :disabled="batchRevoking || revokingId !== null || cleanupRunning"
                  :aria-label="sessionTitle(session)"
                  @change="toggleSession(session.id)"
                />
                <div class="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-cyan-100 text-cyan-700 dark:bg-cyan-900/40 dark:text-cyan-300">
                  <Icon name="server" size="sm" />
                </div>
                <div class="min-w-0">
                  <h4 class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">
                    {{ sessionTitle(session) }}
                  </h4>
                  <p v-if="sessionSubtitle(session)" class="truncate text-xs text-gray-500 dark:text-gray-400">
                    {{ sessionSubtitle(session) }}
                  </p>
                </div>
                <span v-if="session.current" class="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
                  {{ t('admin.accounts.sessions.current') }}
                </span>
                <span v-if="session.trusted" class="rounded-full bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  {{ t('admin.accounts.sessions.trusted') }}
                </span>
              </div>

              <dl class="mt-4 grid grid-cols-1 gap-x-6 gap-y-3 text-xs sm:grid-cols-2">
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.sessions.location') }}</dt>
                  <dd class="mt-0.5 font-medium text-gray-800 dark:text-gray-200">{{ session.location || '-' }}</dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.sessions.signedInAt') }}</dt>
                  <dd class="mt-0.5 font-medium text-gray-800 dark:text-gray-200">{{ formatSessionTime(session.signed_in_at) }}</dd>
                </div>
                <div>
                  <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.sessions.lastActiveAt') }}</dt>
                  <dd class="mt-0.5 font-medium text-gray-800 dark:text-gray-200">{{ formatSessionTime(session.last_active_at) }}</dd>
                </div>
                <div v-if="!session.status_available">
                  <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.accounts.sessions.status') }}</dt>
                  <dd class="mt-0.5 font-medium text-amber-600 dark:text-amber-400">
                    {{ t('admin.accounts.sessions.statusUnavailable') }}
                  </dd>
                </div>
              </dl>
            </div>

            <button
              v-if="session.can_revoke && !session.current"
              type="button"
              data-testid="session-logout"
              class="btn shrink-0 border border-red-200 bg-red-50 text-red-700 hover:bg-red-100 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300 dark:hover:bg-red-900/40"
              :disabled="revokingId !== null || batchRevoking || cleanupRunning"
              @click="revokeTarget = session"
            >
              <Icon v-if="revokingId === session.id" name="refresh" size="sm" class="animate-spin" />
              <Icon v-else name="login" size="sm" />
              {{ t('admin.accounts.sessions.logout') }}
            </button>
          </div>
        </article>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import type {
  Account,
  OpenAIAccountSession,
  OpenAISessionCleanupSettings,
  OpenAISessionCleanupState,
  OpenAISessionCleanupUpdateRequest
} from '@/types'

const props = defineProps<{
  show: boolean
  account: Account | null
}>()

const emit = defineEmits<{
  (event: 'close'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const sessions = ref<OpenAIAccountSession[]>([])
const loading = ref(false)
const loadError = ref('')
const revokeTarget = ref<OpenAIAccountSession | null>(null)
const revokingId = ref<string | null>(null)
const selectedSessionIds = ref<string[]>([])
const batchRevokeConfirm = ref(false)
const batchRevoking = ref(false)
const cleanupSettings = ref<OpenAISessionCleanupSettings | null>(null)
const cleanupState = ref<OpenAISessionCleanupState | null>(null)
const cleanupEnabled = ref(false)
const cleanupIntervalMinutes = ref(60)
const cleanupLoading = ref(false)
const cleanupLoadError = ref('')
const cleanupSaving = ref(false)
const cleanupRunning = ref(false)
let requestSequence = 0
let cleanupRequestSequence = 0

// Keep the modal compatible with older embedded frontends/test doubles that
// expose the original session APIs but not the optional cleanup endpoints.
// The production client always provides all three methods; when the GET method
// is absent (for example in an older embedded frontend), hide the policy panel
// instead of turning a normal session listing into a runtime TypeError. PUT/POST
// are checked at the mutation call sites so read-only integrations can still
// display the fetched policy.
const cleanupAPIAvailable = computed(() =>
  typeof adminAPI.accounts.getOpenAISessionCleanup === 'function'
)

const cleanupEligible = computed(() =>
  props.account?.platform === 'openai' &&
  props.account?.type === 'oauth' &&
  props.account?.parent_account_id == null &&
  cleanupAPIAvailable.value
)

const revokableSessions = computed(() => sessions.value.filter(session => session.can_revoke && !session.current))
const allRevokableSelected = computed(() =>
  revokableSessions.value.length > 0 && revokableSessions.value.every(session => selectedSessionIds.value.includes(session.id))
)

const cleanupIntervalValid = computed(() => {
  const value = Number(cleanupIntervalMinutes.value)
  return Number.isInteger(value) && value >= 5 && value <= 10080
})

const cleanupDirty = computed(() => {
  if (!cleanupSettings.value) return false
  return cleanupEnabled.value !== cleanupSettings.value.enabled ||
    Number(cleanupIntervalMinutes.value) !== cleanupSettings.value.interval_minutes
})

const cleanupStatusLabel = (status?: string): string => {
  const normalized = String(status || '').toLowerCase()
  const known = ['running', 'success', 'skipped', 'failed'].includes(normalized)
  return t(`admin.accounts.sessions.cleanup.status.${known ? normalized : 'unknown'}`)
}

const cleanupStatusClass = (status?: string): string => {
  switch (String(status || '').toLowerCase()) {
    case 'success':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'running':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/40 dark:text-blue-300'
    case 'skipped':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    case 'failed':
      return 'bg-red-100 text-red-700 dark:bg-red-900/40 dark:text-red-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
  }
}

const cleanupStateMessage = (state: OpenAISessionCleanupState | null): string => {
  if (!state) return ''
  if (state.error_code) {
    const key = `admin.accounts.sessions.cleanup.errors.${state.error_code}`
    const translated = t(key)
    if (translated !== key) return translated
  }
  return state.message || state.error_code || ''
}

const loadSessions = async () => {
  const accountId = props.account?.id
  if (!props.show || !accountId) return
  const sequence = ++requestSequence
  loading.value = true
  loadError.value = ''
  try {
    const result = await adminAPI.accounts.listOpenAISessions(accountId)
    if (sequence !== requestSequence) return
    sessions.value = Array.isArray(result.sessions) ? result.sessions : []
    selectedSessionIds.value = []
    batchRevokeConfirm.value = false
  } catch (error) {
    if (sequence !== requestSequence) return
    loadError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.accounts.sessions.errors',
      t('admin.accounts.sessions.loadFailed')
    )
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

const hydrateCleanup = (result: OpenAISessionCleanupSettings) => {
  cleanupSettings.value = result
  cleanupEnabled.value = Boolean(result?.enabled)
  const interval = Number(result?.interval_minutes)
  cleanupIntervalMinutes.value = Number.isInteger(interval) && interval >= 5 && interval <= 10080 ? interval : 60
  cleanupState.value = result?.state ?? null
}

const loadCleanup = async () => {
  const accountId = props.account?.id
  if (!props.show || !accountId || !cleanupEligible.value) return
  const sequence = ++cleanupRequestSequence
  cleanupLoading.value = true
  cleanupLoadError.value = ''
  try {
    const result = await adminAPI.accounts.getOpenAISessionCleanup(accountId)
    if (sequence !== cleanupRequestSequence) return
    hydrateCleanup(result)
  } catch (error) {
    if (sequence !== cleanupRequestSequence) return
    cleanupLoadError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.accounts.sessions.cleanup.errors',
      t('admin.accounts.sessions.cleanup.loadFailed')
    )
  } finally {
    if (sequence === cleanupRequestSequence) cleanupLoading.value = false
  }
}

const cleanupPayload = (): OpenAISessionCleanupUpdateRequest => ({
  enabled: cleanupEnabled.value,
  interval_minutes: Number(cleanupIntervalMinutes.value)
})

const persistCleanup = async (showToast = true): Promise<boolean> => {
  const accountId = props.account?.id
  if (!accountId || !cleanupEligible.value || !cleanupIntervalValid.value || cleanupSaving.value) return false
  if (typeof adminAPI.accounts.updateOpenAISessionCleanup !== 'function') {
    appStore.showError(t('admin.accounts.sessions.cleanup.saveFailed'))
    return false
  }
  cleanupSaving.value = true
  try {
    const result = await adminAPI.accounts.updateOpenAISessionCleanup(accountId, cleanupPayload())
    if (!props.show || props.account?.id !== accountId) return false
    hydrateCleanup(result)
    if (showToast) appStore.showSuccess(t('admin.accounts.sessions.cleanup.saveSuccess'))
    return true
  } catch (error) {
    appStore.showError(
      extractI18nErrorMessage(
        error,
        t,
        'admin.accounts.sessions.cleanup.errors',
        t('admin.accounts.sessions.cleanup.saveFailed')
      )
    )
    return false
  } finally {
    cleanupSaving.value = false
  }
}

const saveCleanup = async () => {
  if (!cleanupIntervalValid.value) {
    appStore.showError(t('admin.accounts.sessions.cleanup.invalidInterval'))
    return
  }
  await persistCleanup(true)
}

const runCleanup = async () => {
  const accountId = props.account?.id
  if (!accountId || !cleanupEligible.value || cleanupRunning.value || cleanupSaving.value) return
  if (typeof adminAPI.accounts.runOpenAISessionCleanup !== 'function') {
    appStore.showError(t('admin.accounts.sessions.cleanup.runFailed'))
    return
  }
  if (!cleanupIntervalValid.value) {
    appStore.showError(t('admin.accounts.sessions.cleanup.invalidInterval'))
    return
  }
  if (!cleanupEnabled.value) {
    appStore.showError(t('admin.accounts.sessions.cleanup.enableBeforeRun'))
    return
  }
  if (cleanupDirty.value && !(await persistCleanup(false))) return
  // The account can change while the settings request is in flight.  Never
  // send a manual cleanup command using the stale ID captured above.
  if (!props.show || props.account?.id !== accountId) return
  cleanupRunning.value = true
  try {
    await adminAPI.accounts.runOpenAISessionCleanup(accountId)
    appStore.showSuccess(t('admin.accounts.sessions.cleanup.runSuccess'))
    await Promise.all([loadCleanup(), loadSessions()])
  } catch (error) {
    appStore.showError(
      extractI18nErrorMessage(
        error,
        t,
        'admin.accounts.sessions.cleanup.errors',
        t('admin.accounts.sessions.cleanup.runFailed')
      )
    )
  } finally {
    cleanupRunning.value = false
  }
}

const confirmRevoke = async () => {
  const accountId = props.account?.id
  const target = revokeTarget.value
  if (!accountId || !target?.id || revokingId.value !== null) return
  revokeTarget.value = null
  revokingId.value = target.id
  try {
    await adminAPI.accounts.revokeOpenAISession(accountId, target.id)
    sessions.value = sessions.value.filter(session => session.id !== target.id)
    selectedSessionIds.value = selectedSessionIds.value.filter(id => id !== target.id)
    appStore.showSuccess(t('admin.accounts.sessions.logoutSuccess'))
  } catch (error) {
    appStore.showError(
      extractI18nErrorMessage(
        error,
        t,
        'admin.accounts.sessions.errors',
        t('admin.accounts.sessions.logoutFailed')
      )
    )
  } finally {
    revokingId.value = null
  }
}

const toggleSession = (sessionId: string) => {
  if (!sessionId || batchRevoking.value || revokingId.value !== null) return
  selectedSessionIds.value = selectedSessionIds.value.includes(sessionId)
    ? selectedSessionIds.value.filter(id => id !== sessionId)
    : [...selectedSessionIds.value, sessionId]
}

const toggleAllSessions = () => {
  if (batchRevoking.value || revokingId.value !== null) return
  selectedSessionIds.value = allRevokableSelected.value
    ? []
    : revokableSessions.value.map(session => session.id)
}

const confirmBatchRevoke = async () => {
  const accountId = props.account?.id
  const ids = [...selectedSessionIds.value]
  if (!accountId || ids.length === 0 || batchRevoking.value) return
  batchRevokeConfirm.value = false
  batchRevoking.value = true
  try {
    const result = await adminAPI.accounts.revokeOpenAISessions(accountId, ids)
    const revoked = new Set(result.revoked_session_ids || [])
    sessions.value = sessions.value.filter(session => !revoked.has(session.id))
    selectedSessionIds.value = ids.filter(id => !revoked.has(id))
    if (result.failed_count > 0) {
      appStore.showWarning(t('admin.accounts.sessions.logoutPartialSuccess', {
        success: result.success_count,
        failed: result.failed_count
      }))
    } else {
      appStore.showSuccess(t('admin.accounts.sessions.logoutSelectedSuccess', { count: result.success_count }))
    }
  } catch (error) {
    appStore.showError(
      extractI18nErrorMessage(
        error,
        t,
        'admin.accounts.sessions.errors',
        t('admin.accounts.sessions.logoutFailed')
      )
    )
  } finally {
    batchRevoking.value = false
  }
}

const sessionTitle = (session: OpenAIAccountSession): string => {
  return session.device_name || session.browser || session.app_name || t('admin.accounts.sessions.unknownDevice')
}

const sessionSubtitle = (session: OpenAIAccountSession): string => {
  return [session.app_name, session.browser, session.os, session.device_type]
    .filter((value, index, values): value is string => Boolean(value) && values.indexOf(value) === index)
    .join(' · ')
}

const formatSessionTime = (value?: string): string => {
  if (!value) return '-'
  const trimmed = value.trim()
  if (/^\d+(?:\.\d+)?$/.test(trimmed)) {
    const numeric = Number(trimmed)
    if (Number.isFinite(numeric)) {
      const milliseconds = numeric < 10_000_000_000 ? numeric * 1000 : numeric
      return formatDateTime(new Date(milliseconds))
    }
  }
  const parsed = new Date(trimmed)
  return Number.isNaN(parsed.getTime()) ? trimmed : formatDateTime(parsed)
}

watch(
  [() => props.show, () => props.account?.id],
  ([visible]) => {
    requestSequence++
    cleanupRequestSequence++
    sessions.value = []
    selectedSessionIds.value = []
    loadError.value = ''
    cleanupLoadError.value = ''
    cleanupSettings.value = null
    cleanupState.value = null
    cleanupEnabled.value = false
    cleanupIntervalMinutes.value = 60
    cleanupLoading.value = false
    cleanupSaving.value = false
    cleanupRunning.value = false
    revokeTarget.value = null
    batchRevokeConfirm.value = false
    if (visible) {
      void loadSessions()
      if (cleanupEligible.value) void loadCleanup()
    }
  },
  { immediate: true }
)
</script>
