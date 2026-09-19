<template>
  <AppLayout>
    <div class="admin-workspace task-detail-workspace">
      <AdminPageHeader
        :eyebrow="t('admin.codexTurnState.eyebrow')"
        :title="t('admin.codexTurnState.tasks.detailTitle')"
        :description="t('admin.codexTurnState.tasks.detailDescription', { id: taskId || '-' })"
      >
        <template #actions>
          <RouterLink class="btn btn-secondary" :to="{ name: 'AdminCodexTurnState' }">
            <Icon name="arrowLeft" size="sm" />
            {{ t('admin.codexTurnState.tasks.back') }}
          </RouterLink>
          <button type="button" class="btn btn-secondary" :disabled="loading || actionPending !== null" @click="loadTask()">
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
            {{ t('admin.codexTurnState.tasks.refresh') }}
          </button>
          <button
            v-if="task?.can_cancel"
            type="button"
            class="btn btn-danger"
            :disabled="loading || actionPending !== null"
            data-testid="turn-state-task-cancel"
            @click="cancelConfirmOpen = true"
          >
            <Icon :name="actionPending === 'cancel' ? 'refresh' : 'x'" size="sm" :class="{ 'animate-spin': actionPending === 'cancel' }" />
            {{ actionPending === 'cancel' ? t('admin.codexTurnState.tasks.canceling') : t('admin.codexTurnState.tasks.cancel') }}
          </button>
          <button
            v-if="task?.can_retry"
            type="button"
            class="btn btn-primary"
            :disabled="loading || actionPending !== null"
            data-testid="turn-state-task-retry"
            @click="retryConfirmOpen = true"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': actionPending === 'retry' }" />
            {{ actionPending === 'retry' ? t('admin.codexTurnState.tasks.retrying') : t('admin.codexTurnState.tasks.retry') }}
          </button>
        </template>
      </AdminPageHeader>

      <section v-if="error" class="admin-surface task-message" role="alert">
        <Icon name="exclamationCircle" size="lg" class="text-red-400" />
        <p>{{ error }}</p>
        <button type="button" class="btn btn-secondary" @click="loadTask()">{{ t('common.retry') }}</button>
      </section>

      <section v-else-if="loading && !task" class="admin-surface task-message" role="status">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
        <p>{{ t('common.loading') }}</p>
      </section>

      <template v-else-if="task">
        <AdminOverviewStrip :items="overviewItems" />

        <section class="admin-surface task-summary" data-testid="turn-state-task-summary">
          <div class="task-section-heading">
            <div>
              <h2 class="admin-section-heading">{{ t('admin.codexTurnState.tasks.summaryTitle') }}</h2>
              <p class="task-section-description">{{ t('admin.codexTurnState.tasks.summaryDescription') }}</p>
            </div>
            <span class="task-status" :class="`task-status-${task.status}`">{{ taskStatusLabel(task.status) }}</span>
          </div>
          <dl class="task-details-grid">
            <div class="task-detail-item task-detail-id">
              <dt>{{ t('admin.codexTurnState.tasks.fields.taskId') }}</dt>
              <dd class="font-mono">{{ task.id }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.account') }}</dt>
              <dd>{{ task.account_name || t('admin.codexTurnState.tasks.unknownAccount') }} <span class="font-mono text-xs text-gray-400">#{{ task.account_id }}</span></dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.requestModel') }}</dt>
              <dd class="break-all font-mono">{{ task.request_model || '-' }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.ownerModel') }}</dt>
              <dd class="break-all font-mono">{{ task.owner_model || '-' }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.source') }}</dt>
              <dd>{{ taskSourceLabel(task.source) }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.stage') }}</dt>
              <dd>{{ taskStageLabel(task.stage) }}</dd>
            </div>
            <div v-if="task.retry_of" class="task-detail-item task-detail-id">
              <dt>{{ t('admin.codexTurnState.tasks.fields.retryOf') }}</dt>
              <dd>
                <RouterLink class="task-inline-link font-mono" :to="{ name: 'AdminCodexTurnStateTask', params: { taskId: task.retry_of } }">
                  {{ task.retry_of }}
                </RouterLink>
              </dd>
            </div>
          </dl>
        </section>

        <section class="admin-surface task-progress-section">
          <div class="task-section-heading">
            <div>
              <h2 class="admin-section-heading">{{ t('admin.codexTurnState.tasks.progressTitle') }}</h2>
              <p class="task-section-description">{{ taskStageLabel(task.stage) }}</p>
            </div>
            <strong class="task-progress-value">{{ progress }}%</strong>
          </div>
          <div
            class="task-progress-track"
            role="progressbar"
            :aria-valuenow="progress"
            aria-valuemin="0"
            aria-valuemax="100"
            :aria-label="t('admin.codexTurnState.tasks.progressLabel', { percent: progress })"
          >
            <span :style="{ width: `${progress}%` }" />
          </div>
          <p v-if="task.progress_total > 0" class="task-progress-count">
            {{ t('admin.codexTurnState.tasks.progressCount', { current: task.progress_current, total: task.progress_total }) }}
          </p>
        </section>

        <section class="admin-surface task-times">
          <div class="task-section-heading">
            <div>
              <h2 class="admin-section-heading">{{ t('admin.codexTurnState.tasks.timeTitle') }}</h2>
              <p class="task-section-description">{{ t('admin.codexTurnState.tasks.timeDescription') }}</p>
            </div>
          </div>
          <dl class="task-time-grid">
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.createdAt') }}</dt>
              <dd>{{ formatTimestamp(task.created_at_ms) }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.startedAt') }}</dt>
              <dd>{{ formatTimestamp(task.started_at_ms) }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.updatedAt') }}</dt>
              <dd>{{ formatTimestamp(task.updated_at_ms) }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.finishedAt') }}</dt>
              <dd>{{ formatTimestamp(taskFinishedAt(task)) }}</dd>
            </div>
            <div class="task-detail-item">
              <dt>{{ t('admin.codexTurnState.tasks.fields.duration') }}</dt>
              <dd>{{ durationLabel }}</dd>
            </div>
          </dl>
        </section>

        <section v-if="task.error" class="admin-surface task-error" role="alert" data-testid="turn-state-task-error">
          <Icon name="exclamationTriangle" size="md" class="shrink-0" />
          <div class="min-w-0">
            <h2 class="text-sm font-semibold">{{ t('admin.codexTurnState.tasks.errorTitle') }}</h2>
            <p class="mt-1 break-all font-mono text-xs">{{ task.error }}</p>
          </div>
        </section>

        <section class="admin-surface task-timeline">
          <div class="task-section-heading">
            <div>
              <h2 class="admin-section-heading">{{ t('admin.codexTurnState.tasks.timelineTitle') }}</h2>
              <p class="task-section-description">{{ t('admin.codexTurnState.tasks.timelineDescription') }}</p>
            </div>
            <span class="task-event-count">{{ taskEvents.length }}</span>
          </div>
          <div v-if="taskEvents.length === 0" class="task-message task-message-compact">
            <Icon name="clock" size="lg" class="text-gray-300 dark:text-gray-600" />
            <p>{{ t('admin.codexTurnState.tasks.timelineEmpty') }}</p>
          </div>
          <ol v-else class="task-event-list" data-testid="turn-state-task-events">
            <li v-for="(event, index) in taskEvents" :key="`${eventTime(event)}-${index}`" class="task-event">
              <span class="task-event-marker" :class="`task-event-marker-${event.status}`" aria-hidden="true" />
              <div class="min-w-0 flex-1">
                <div class="task-event-heading">
                  <div class="flex min-w-0 flex-wrap items-center gap-2">
                    <span class="task-status" :class="`task-status-${event.status}`">{{ taskStatusLabel(event.status) }}</span>
                    <span class="text-xs font-medium text-gray-700 dark:text-gray-200">{{ taskStageLabel(event.stage) }}</span>
                  </div>
                  <time :datetime="timestampISO(eventTime(event))">{{ formatTimestamp(eventTime(event)) }}</time>
                </div>
                <div class="task-event-progress">
                  <span>{{ t('admin.codexTurnState.tasks.progressLabel', { percent: eventProgress(event) }) }}</span>
                  <span v-if="event.message">{{ event.message }}</span>
                </div>
                <p v-if="event.error" class="task-event-error">{{ event.error }}</p>
              </div>
            </li>
          </ol>
        </section>
      </template>

      <ConfirmDialog
        :show="cancelConfirmOpen"
        :title="t('admin.codexTurnState.tasks.cancelConfirmTitle')"
        :message="t('admin.codexTurnState.tasks.cancelConfirmMessage')"
        :confirm-text="t('admin.codexTurnState.tasks.cancel')"
        danger
        @cancel="cancelConfirmOpen = false"
        @confirm="cancelTask"
      />
      <ConfirmDialog
        :show="retryConfirmOpen"
        :title="t('admin.codexTurnState.tasks.retryConfirmTitle')"
        :message="t('admin.codexTurnState.tasks.retryConfirmMessage')"
        :confirm-text="t('admin.codexTurnState.tasks.retry')"
        @cancel="retryConfirmOpen = false"
        @confirm="retryTask"
      />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminPageHeader from '@/components/admin/AdminPageHeader.vue'
import AdminOverviewStrip from '@/components/admin/AdminOverviewStrip.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { CodexTurnStateTask, CodexTurnStateTaskEvent } from '@/api/admin/accounts'

type TaskAction = 'cancel' | 'retry'

const TASK_POLL_INTERVAL_MS = 2_000
const TASK_SOURCES = new Set(['manual', 'bulk', 'automatic', 'renewal', 'retry'])
const TASK_STATUSES = new Set(['queued', 'running', 'succeeded', 'failed', 'canceled'])
const TASK_STAGES = new Set([
  'queued',
  'preparing',
  'loading_account',
  'resolving_routes',
  'collecting',
  'verifying',
  'persisting',
  'retry_wait',
  'completed',
  'failed',
  'canceled',
])

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const appStore = useAppStore()
const task = ref<CodexTurnStateTask | null>(null)
const loading = ref(false)
const error = ref('')
const actionPending = ref<TaskAction | null>(null)
const cancelConfirmOpen = ref(false)
const retryConfirmOpen = ref(false)
const clock = ref(Date.now())
let pollTimer: ReturnType<typeof setTimeout> | undefined
let clockTimer: ReturnType<typeof setInterval> | undefined
let requestGeneration = 0
let actionGeneration = 0
let componentActive = true

const taskId = computed(() => String(route.params.taskId || '').trim())
const progress = computed(() => normalizeProgress(task.value?.progress))
const taskEvents = computed(() => [...(task.value?.events || [])]
  .sort((left, right) => eventTime(left) - eventTime(right)))
const durationLabel = computed(() => formatDuration(taskDurationMs(task.value)))
const overviewItems = computed(() => task.value ? [
  { label: t('admin.codexTurnState.tasks.fields.status'), value: taskStatusLabel(task.value.status) },
  { label: t('admin.codexTurnState.tasks.fields.progress'), value: `${progress.value}%`, tone: progress.value === 100 ? 'positive' as const : undefined },
  { label: t('admin.codexTurnState.tasks.fields.stage'), value: taskStageLabel(task.value.stage) },
  { label: t('admin.codexTurnState.tasks.fields.duration'), value: durationLabel.value },
] : [])

function isActiveTask(value: CodexTurnStateTask | null): boolean {
  return value?.status === 'queued' || value?.status === 'running'
}

function normalizeProgress(value?: number): number {
  const progressValue = Number(value)
  if (!Number.isFinite(progressValue)) return 0
  return Math.min(100, Math.max(0, Math.round(progressValue)))
}

function taskStatusLabel(status: string): string {
  if (TASK_STATUSES.has(status)) return t(`admin.codexTurnState.tasks.statuses.${status}`)
  return t('admin.codexTurnState.tasks.unknownValue', { value: status || '-' })
}

function taskSourceLabel(source: string): string {
  if (TASK_SOURCES.has(source)) return t(`admin.codexTurnState.tasks.sources.${source}`)
  return t('admin.codexTurnState.tasks.unknownValue', { value: source || '-' })
}

function taskStageLabel(stage: string): string {
  if (TASK_STAGES.has(stage)) return t(`admin.codexTurnState.tasks.stages.${stage}`)
  return t('admin.codexTurnState.tasks.unknownValue', { value: stage || '-' })
}

function taskFinishedAt(value: CodexTurnStateTask): number | undefined {
  return value.finished_at_ms || value.completed_at_ms
}

function eventTime(event: CodexTurnStateTaskEvent): number {
  return Number(event.at_ms || event.timestamp_ms || 0)
}

function eventProgress(event: CodexTurnStateTaskEvent): number {
  return normalizeProgress(event.progress)
}

function timestampISO(value?: number): string {
  if (!value || !Number.isFinite(value)) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function formatTimestamp(value?: number): string {
  if (!value || !Number.isFinite(value)) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(date)
}

function taskDurationMs(value: CodexTurnStateTask | null): number | undefined {
  if (!value?.started_at_ms) return undefined
  const end = taskFinishedAt(value) || (isActiveTask(value) ? clock.value : value.updated_at_ms)
  if (!end || end < value.started_at_ms) return undefined
  return end - value.started_at_ms
}

function formatDuration(value?: number): string {
  if (value == null || !Number.isFinite(value) || value < 0) return '-'
  if (value < 1_000) return t('admin.codexTurnState.tasks.durationMilliseconds', { value: Math.round(value) })
  const seconds = value / 1_000
  if (seconds < 60) return t('admin.codexTurnState.tasks.durationSeconds', { value: seconds.toFixed(seconds < 10 ? 1 : 0) })
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  if (minutes < 60) return t('admin.codexTurnState.tasks.durationMinutes', { minutes, seconds: remainingSeconds })
  const hours = Math.floor(minutes / 60)
  return t('admin.codexTurnState.tasks.durationHours', { hours, minutes: minutes % 60 })
}

function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
  if (!componentActive || actionPending.value !== null || !isActiveTask(task.value)) return
  pollTimer = setTimeout(() => {
    pollTimer = undefined
    void loadTask({ background: true })
  }, TASK_POLL_INTERVAL_MS)
}

async function loadTask({ background = false }: { background?: boolean } = {}) {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
  const id = taskId.value
  if (!id) {
    task.value = null
    error.value = t('admin.codexTurnState.tasks.invalidTaskId')
    return
  }
  const generation = ++requestGeneration
  if (!background) loading.value = true
  if (!background || !task.value) error.value = ''
  try {
    const result = await adminAPI.accounts.getCodexTurnStateTask(id)
    if (!componentActive || generation !== requestGeneration || id !== taskId.value) return
    task.value = result
    error.value = ''
  } catch (loadError) {
    if (componentActive && generation === requestGeneration && (!background || !task.value)) {
      error.value = extractApiErrorMessage(loadError, t('admin.codexTurnState.tasks.detailLoadFailed'))
    }
  } finally {
    if (componentActive && generation === requestGeneration) {
      if (!background) loading.value = false
      schedulePoll()
    }
  }
}

async function cancelTask() {
  cancelConfirmOpen.value = false
  if (!task.value?.can_cancel || actionPending.value) return
  const id = task.value.id
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
  requestGeneration += 1
  const generation = ++actionGeneration
  actionPending.value = 'cancel'
  try {
    const result = await adminAPI.accounts.cancelCodexTurnStateTask(id)
    if (!componentActive || generation !== actionGeneration || taskId.value !== id) return
    task.value = result
    appStore.showSuccess(t('admin.codexTurnState.tasks.cancelSucceeded'))
  } catch (cancelError) {
    if (componentActive && generation === actionGeneration && taskId.value === id) {
      appStore.showError(extractApiErrorMessage(cancelError, t('admin.codexTurnState.tasks.cancelFailed')))
    }
  } finally {
    if (componentActive && generation === actionGeneration) {
      actionPending.value = null
      schedulePoll()
    }
  }
}

async function retryTask() {
  retryConfirmOpen.value = false
  if (!task.value?.can_retry || actionPending.value) return
  const id = task.value.id
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
  requestGeneration += 1
  const generation = ++actionGeneration
  actionPending.value = 'retry'
  try {
    const retriedTask = await adminAPI.accounts.retryCodexTurnStateTask(id)
    if (!componentActive || generation !== actionGeneration || taskId.value !== id) return
    appStore.showSuccess(t('admin.codexTurnState.tasks.retrySucceeded'))
    if (retriedTask?.id) {
      await router.push({ name: 'AdminCodexTurnStateTask', params: { taskId: retriedTask.id } })
    } else {
      await loadTask()
    }
  } catch (retryError) {
    if (componentActive && generation === actionGeneration && taskId.value === id) {
      appStore.showError(extractApiErrorMessage(retryError, t('admin.codexTurnState.tasks.retryFailed')))
    }
  } finally {
    if (componentActive && generation === actionGeneration) {
      actionPending.value = null
      schedulePoll()
    }
  }
}

watch(taskId, () => {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = undefined
  task.value = null
  error.value = ''
  cancelConfirmOpen.value = false
  retryConfirmOpen.value = false
  actionGeneration += 1
  actionPending.value = null
  void loadTask()
}, { immediate: true })

clockTimer = setInterval(() => {
  if (componentActive && isActiveTask(task.value)) clock.value = Date.now()
}, 1_000)

onBeforeUnmount(() => {
  componentActive = false
  requestGeneration += 1
  actionGeneration += 1
  if (pollTimer) clearTimeout(pollTimer)
  if (clockTimer) clearInterval(clockTimer)
  pollTimer = undefined
  clockTimer = undefined
})
</script>

<style scoped>
.task-detail-workspace {
  @apply space-y-5;
}
.task-detail-workspace :deep(.admin-overview) {
  border-radius: 0.5rem;
}
.task-summary,
.task-progress-section,
.task-times,
.task-timeline,
.task-message,
.task-error {
  @apply min-w-0 overflow-hidden;
  border-radius: 0.5rem;
}
.task-message {
  @apply flex min-h-64 flex-col items-center justify-center gap-3 px-5 py-10 text-center text-sm text-gray-500 dark:text-gray-400;
}
.task-message-compact {
  @apply min-h-40;
}
.task-section-heading {
  @apply flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 px-5 py-4 dark:border-dark-700;
}
.task-section-description {
  @apply mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400;
}
.task-status {
  @apply inline-flex shrink-0 items-center rounded-md px-2 py-1 text-[11px] font-medium;
}
.task-status-queued,
.task-status-running {
  @apply bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-400;
}
.task-status-succeeded {
  @apply bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-400;
}
.task-status-failed {
  @apply bg-red-50 text-red-700 dark:bg-red-500/10 dark:text-red-400;
}
.task-status-canceled {
  @apply bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.task-details-grid,
.task-time-grid {
  @apply grid sm:grid-cols-2 xl:grid-cols-3;
}
.task-detail-item {
  @apply min-w-0 border-b border-gray-100 px-5 py-4 sm:border-r dark:border-dark-700;
}
.task-detail-item dt {
  @apply text-xs font-medium text-gray-500 dark:text-gray-400;
}
.task-detail-item dd {
  @apply mt-1.5 break-words text-sm text-gray-900 dark:text-gray-100;
}
.task-detail-id {
  @apply xl:col-span-2;
}
.task-inline-link {
  @apply break-all text-primary-700 hover:text-primary-800 hover:underline dark:text-primary-400 dark:hover:text-primary-300;
}
.task-progress-section {
  @apply px-0 pb-5;
}
.task-progress-value {
  @apply text-2xl font-semibold text-gray-900 dark:text-gray-100;
  font-variant-numeric: tabular-nums;
}
.task-progress-track {
  @apply mx-5 mt-5 h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600;
  width: calc(100% - 2.5rem);
}
.task-progress-track > span {
  @apply block h-full rounded-full bg-primary-600 transition-all duration-300 dark:bg-primary-500;
}
.task-progress-count {
  @apply mx-5 mt-2 text-right text-xs text-gray-500 dark:text-gray-400;
}
.task-error {
  @apply flex items-start gap-3 border-red-200 bg-red-50 px-5 py-4 text-red-700 dark:border-red-500/20 dark:bg-red-500/5 dark:text-red-300;
}
.task-event-count {
  @apply rounded-md bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300;
}
.task-event-list {
  @apply divide-y divide-gray-100 px-5 dark:divide-dark-700;
}
.task-event {
  @apply relative flex gap-4 py-4;
}
.task-event-marker {
  @apply mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full bg-gray-300 ring-4 ring-gray-100 dark:bg-dark-500 dark:ring-dark-700;
}
.task-event-marker-running,
.task-event-marker-queued {
  @apply bg-blue-500 ring-blue-50 dark:bg-blue-400 dark:ring-blue-500/10;
}
.task-event-marker-succeeded {
  @apply bg-emerald-500 ring-emerald-50 dark:bg-emerald-400 dark:ring-emerald-500/10;
}
.task-event-marker-failed {
  @apply bg-red-500 ring-red-50 dark:bg-red-400 dark:ring-red-500/10;
}
.task-event-heading {
  @apply flex min-w-0 flex-wrap items-start justify-between gap-2;
}
.task-event-heading time {
  @apply text-[11px] text-gray-400 dark:text-gray-500;
}
.task-event-progress {
  @apply mt-2 flex min-w-0 flex-wrap gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400;
}
.task-event-error {
  @apply mt-2 break-all font-mono text-xs text-red-600 dark:text-red-400;
}
@media (max-width: 639px) {
  .task-detail-item {
    @apply border-r-0;
  }
}
</style>
