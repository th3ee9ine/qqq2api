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
          :disabled="loading || revokingId !== null"
          @click="loadSessions"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('admin.accounts.sessions.refresh') }}
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
                :disabled="revokingId !== null"
                @click="confirmRevoke"
              >
                {{ t('admin.accounts.sessions.logout') }}
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
          class="rounded-xl border border-gray-200 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-center gap-2">
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
              :disabled="revokingId !== null"
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
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractI18nErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import type { Account, OpenAIAccountSession } from '@/types'

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
let requestSequence = 0

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

const confirmRevoke = async () => {
  const accountId = props.account?.id
  const target = revokeTarget.value
  if (!accountId || !target?.id || revokingId.value !== null) return
  revokeTarget.value = null
  revokingId.value = target.id
  try {
    await adminAPI.accounts.revokeOpenAISession(accountId, target.id)
    sessions.value = sessions.value.filter(session => session.id !== target.id)
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
    sessions.value = []
    loadError.value = ''
    revokeTarget.value = null
    if (visible) void loadSessions()
  },
  { immediate: true }
)
</script>
