<template>
  <BaseDialog :show="show" :title="t('admin.usage.cleanup.title')" width="wide" @close="handleClose">
    <div class="space-y-4">
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <label class="input-label">{{ t('admin.usage.cleanup.recordType') }}</label>
        <div class="mt-2 max-w-sm">
          <Select
            v-model="selectedRecordType"
            :options="recordTypeOptions"
            :aria-label="t('admin.usage.cleanup.recordType')"
          />
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.usage.cleanup.recordTypeHint') }}
        </p>
      </div>

      <UsageFilters
        v-model="localFilters"
        :start-date="localStartDate"
        :end-date="localEndDate"
        :mode="filterMode"
        cleanup
        :exporting="false"
        :show-actions="false"
        @change="noop"
      />

      <div v-if="selectedRecordType !== 'errors'" class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <label class="input-label">{{ t('admin.usage.billingType') }}</label>
        <div class="mt-2 max-w-sm">
          <Select
            v-model="localFilters.billing_type"
            :options="billingTypeOptions"
            :aria-label="t('admin.usage.billingType')"
          />
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.usage.cleanup.billingTypeHint') }}
        </p>
      </div>

      <div
        v-if="selectedRecordType === 'all' && localFilters.billing_type != null"
        class="rounded-xl border border-sky-200 bg-sky-50 px-4 py-3 text-sm text-sky-700 dark:border-sky-500/30 dark:bg-sky-500/10 dark:text-sky-200"
      >
        {{ t('admin.usage.cleanup.billingFilterNotice') }}
      </div>

      <div class="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
        {{ t('admin.usage.cleanup.warning') }}
      </div>

      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-center justify-between">
          <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-200">
            {{ t('admin.usage.cleanup.recentTasks') }}
          </h4>
          <button type="button" class="btn btn-ghost btn-sm" @click="loadTasks">
            {{ t('common.refresh') }}
          </button>
        </div>

        <div class="mt-3 space-y-2">
          <div v-if="tasksLoading" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.usage.cleanup.loadingTasks') }}
          </div>
          <div v-else-if="tasks.length === 0" class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.usage.cleanup.noTasks') }}
          </div>
          <div v-else class="space-y-2">
            <div
              v-for="task in tasks"
              :key="task.id"
              class="flex flex-col gap-2 rounded-lg border border-gray-100 px-3 py-2 text-sm text-gray-600 dark:border-dark-700 dark:text-gray-300"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <div class="flex items-center gap-2">
                  <span :class="statusClass(task.status)" class="rounded-full px-2 py-0.5 text-xs font-semibold">
                    {{ statusLabel(task.status) }}
                  </span>
                  <span class="text-xs text-gray-400">#{{ task.id }}</span>
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[11px] text-gray-500 dark:bg-dark-700 dark:text-gray-300">
                    {{ recordTypeLabel(task.filters.record_type) }}
                  </span>
                  <button
                    v-if="canCancel(task)"
                    type="button"
                    class="btn btn-ghost btn-xs text-rose-600 hover:text-rose-700 dark:text-rose-300"
                    @click="openCancelConfirm(task)"
                  >
                    {{ t('admin.usage.cleanup.cancel') }}
                  </button>
                </div>
                <div class="text-xs text-gray-400">
                  {{ formatDateTime(task.created_at) }}
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-4 text-xs text-gray-500 dark:text-gray-400">
                <span>{{ t('admin.usage.cleanup.range') }}: {{ formatRange(task) }}</span>
                <span>{{ t('admin.usage.cleanup.deletedRows') }}: {{ task.deleted_rows.toLocaleString() }}</span>
              </div>
              <div v-if="task.error_message" class="text-xs text-rose-500">
                {{ task.error_message }}
              </div>
            </div>
          </div>
        </div>

        <Pagination
          v-if="tasksTotal > tasksPageSize"
          class="mt-4"
          :total="tasksTotal"
          :page="tasksPage"
          :page-size="tasksPageSize"
          :page-size-options="[5]"
          :show-page-size-selector="false"
          :show-jump="true"
          @update:page="handleTaskPageChange"
          @update:pageSize="handleTaskPageSizeChange"
        />
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-danger" :disabled="submitting" @click="openConfirm">
          {{ submitting ? t('admin.usage.cleanup.submitting') : t('admin.usage.cleanup.submit') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <ConfirmDialog
    :show="confirmVisible"
    :title="t('admin.usage.cleanup.confirmTitle')"
    :message="t('admin.usage.cleanup.confirmMessage')"
    :confirm-text="t('admin.usage.cleanup.confirmSubmit')"
    danger
    @confirm="submitCleanup"
    @cancel="confirmVisible = false"
  />

  <ConfirmDialog
    :show="cancelConfirmVisible"
    :title="t('admin.usage.cleanup.cancelConfirmTitle')"
    :message="t('admin.usage.cleanup.cancelConfirmMessage')"
    :confirm-text="t('admin.usage.cleanup.cancelConfirm')"
    danger
    @confirm="cancelTask"
    @cancel="cancelConfirmVisible = false"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import { adminUsageAPI } from '@/api/admin/usage'
import type { AdminUsageQueryParams, UsageCleanupTask, CreateUsageCleanupTaskRequest } from '@/api/admin/usage'
import { requestTypeToLegacyStream } from '@/utils/usageRequestType'

interface Props {
  show: boolean
  filters: AdminUsageQueryParams
  startDate: string
  endDate: string
  recordType?: 'all' | 'usage' | 'errors'
}

const props = defineProps<Props>()
const emit = defineEmits(['close'])

const { t } = useI18n()
const appStore = useAppStore()

const localFilters = ref<AdminUsageQueryParams>({})
const localStartDate = ref('')
const localEndDate = ref('')
const selectedRecordType = ref<'all' | 'usage' | 'errors'>('usage')

const recordTypeOptions: SelectOption[] = [
  { value: 'all', label: t('admin.usage.cleanup.recordTypeAll') },
  { value: 'usage', label: t('admin.usage.cleanup.recordTypeUsage') },
  { value: 'errors', label: t('admin.usage.cleanup.recordTypeErrors') }
]
const billingTypeOptions: SelectOption[] = [
  { value: null, label: t('admin.usage.allBillingTypes') },
  { value: 0, label: t('admin.usage.billingTypeBalance') },
  { value: 1, label: t('admin.usage.billingTypeSubscription') }
]
const filterMode = computed<'usage' | 'errors' | 'all'>(() => selectedRecordType.value)

const tasks = ref<UsageCleanupTask[]>([])
const tasksLoading = ref(false)
const tasksPage = ref(1)
const tasksPageSize = ref(5)
const tasksTotal = ref(0)
const submitting = ref(false)
const confirmVisible = ref(false)
const cancelConfirmVisible = ref(false)
const canceling = ref(false)
const cancelTarget = ref<UsageCleanupTask | null>(null)
let pollTimer: number | null = null

const noop = () => {}

const resetFilters = () => {
  localFilters.value = { ...props.filters }
  localStartDate.value = props.startDate
  localEndDate.value = props.endDate
  localFilters.value.start_date = localStartDate.value
  localFilters.value.end_date = localEndDate.value
  selectedRecordType.value = props.recordType || (props.filters.error_phase || props.filters.error_category || props.filters.status_code != null ? 'errors' : 'usage')
  tasksPage.value = 1
  tasksTotal.value = 0
}

const startPolling = () => {
  stopPolling()
  pollTimer = window.setInterval(() => {
    loadTasks()
  }, 10000)
}

const stopPolling = () => {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

const handleClose = () => {
  stopPolling()
  confirmVisible.value = false
  cancelConfirmVisible.value = false
  canceling.value = false
  cancelTarget.value = null
  submitting.value = false
  emit('close')
}

const statusLabel = (status: string) => {
  const map: Record<string, string> = {
    pending: t('admin.usage.cleanup.status.pending'),
    running: t('admin.usage.cleanup.status.running'),
    succeeded: t('admin.usage.cleanup.status.succeeded'),
    failed: t('admin.usage.cleanup.status.failed'),
    canceled: t('admin.usage.cleanup.status.canceled')
  }
  return map[status] || status
}

const statusClass = (status: string) => {
  const map: Record<string, string> = {
    pending: 'bg-amber-100 text-amber-700 dark:bg-amber-500/20 dark:text-amber-200',
    running: 'bg-blue-100 text-blue-700 dark:bg-blue-500/20 dark:text-blue-200',
    succeeded: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-200',
    failed: 'bg-rose-100 text-rose-700 dark:bg-rose-500/20 dark:text-rose-200',
    canceled: 'bg-gray-200 text-gray-600 dark:bg-dark-600 dark:text-gray-300'
  }
  return map[status] || 'bg-gray-100 text-gray-600'
}

const formatDateTime = (value?: string | null) => {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

const formatRange = (task: UsageCleanupTask) => {
  const start = formatDateTime(task.filters.start_time)
  const end = formatDateTime(task.filters.end_time)
  return `${start} ~ ${end}`
}

const recordTypeLabel = (value?: 'all' | 'usage' | 'errors') => {
  switch (value) {
    case 'all': return t('admin.usage.cleanup.recordTypeAll')
    case 'errors': return t('admin.usage.cleanup.recordTypeErrors')
    case 'usage': return t('admin.usage.cleanup.recordTypeUsage')
    default: return t('admin.usage.cleanup.recordTypeUsage')
  }
}

const getUserTimezone = () => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return 'UTC'
  }
}

const loadTasks = async () => {
  if (!props.show) return
  tasksLoading.value = true
  try {
    const res = await adminUsageAPI.listCleanupTasks({
      page: tasksPage.value,
      page_size: tasksPageSize.value
    })
    tasks.value = res.items || []
    tasksTotal.value = res.total || 0
    if (res.page) {
      tasksPage.value = res.page
    }
    if (res.page_size) {
      tasksPageSize.value = res.page_size
    }
  } catch (error) {
    console.error('Failed to load cleanup tasks:', error)
    appStore.showError(t('admin.usage.cleanup.loadFailed'))
  } finally {
    tasksLoading.value = false
  }
}

const handleTaskPageChange = (page: number) => {
  tasksPage.value = page
  loadTasks()
}

const handleTaskPageSizeChange = (size: number) => {
  if (!Number.isFinite(size) || size <= 0) return
  tasksPageSize.value = size
  tasksPage.value = 1
  loadTasks()
}

const openConfirm = () => {
  confirmVisible.value = true
}

const canCancel = (task: UsageCleanupTask) => {
  return task.status === 'pending' || task.status === 'running'
}

const openCancelConfirm = (task: UsageCleanupTask) => {
  cancelTarget.value = task
  cancelConfirmVisible.value = true
}

const buildPayload = (): CreateUsageCleanupTaskRequest | null => {
  if (!localStartDate.value || !localEndDate.value) {
    appStore.showError(t('admin.usage.cleanup.missingRange'))
    return null
  }

  const payload: CreateUsageCleanupTaskRequest = {
    start_date: localStartDate.value,
    end_date: localEndDate.value,
    record_type: selectedRecordType.value,
    timezone: getUserTimezone()
  }

  if (localFilters.value.api_key_id && localFilters.value.api_key_id > 0) {
    payload.api_key_id = localFilters.value.api_key_id
  }
  if (localFilters.value.account_id && localFilters.value.account_id > 0) {
    payload.account_id = localFilters.value.account_id
  }
  if (localFilters.value.group_id && localFilters.value.group_id > 0) {
    payload.group_id = localFilters.value.group_id
  }
  if (localFilters.value.model) {
    payload.model = localFilters.value.model
  }
  if (selectedRecordType.value !== 'errors') {
    if (localFilters.value.request_type) {
      payload.request_type = localFilters.value.request_type
      const legacyStream = requestTypeToLegacyStream(localFilters.value.request_type)
      if (legacyStream !== null && legacyStream !== undefined) {
        payload.stream = legacyStream
      }
    } else if (localFilters.value.stream !== null && localFilters.value.stream !== undefined) {
      payload.stream = localFilters.value.stream
    }
    if (localFilters.value.billing_type !== null && localFilters.value.billing_type !== undefined) {
      payload.billing_type = localFilters.value.billing_type
    }
  }
  if (selectedRecordType.value !== 'usage') {
    if (localFilters.value.error_phase) payload.error_phase = localFilters.value.error_phase
    if (localFilters.value.error_category) payload.error_category = localFilters.value.error_category
    if (localFilters.value.status_code !== null && localFilters.value.status_code !== undefined) {
      payload.status_code = Number(localFilters.value.status_code)
      payload.status_codes = [Number(localFilters.value.status_code)]
    }
  }

  return payload
}

const submitCleanup = async () => {
  const payload = buildPayload()
  if (!payload) {
    confirmVisible.value = false
    return
  }
  submitting.value = true
  confirmVisible.value = false
  try {
    await adminUsageAPI.createCleanupTask(payload)
    appStore.showSuccess(t('admin.usage.cleanup.submitSuccess'))
    loadTasks()
  } catch (error) {
    console.error('Failed to create cleanup task:', error)
    appStore.showError(t('admin.usage.cleanup.submitFailed'))
  } finally {
    submitting.value = false
  }
}

const cancelTask = async () => {
  const task = cancelTarget.value
  if (!task) {
    cancelConfirmVisible.value = false
    return
  }
  canceling.value = true
  cancelConfirmVisible.value = false
  try {
    await adminUsageAPI.cancelCleanupTask(task.id)
    appStore.showSuccess(t('admin.usage.cleanup.cancelSuccess'))
    loadTasks()
  } catch (error) {
    console.error('Failed to cancel cleanup task:', error)
    appStore.showError(t('admin.usage.cleanup.cancelFailed'))
  } finally {
    canceling.value = false
    cancelTarget.value = null
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      resetFilters()
      loadTasks()
      startPolling()
    } else {
      stopPolling()
    }
  }
)

watch(
  () => props.recordType,
  (value) => {
    if (value && !props.show) selectedRecordType.value = value
    if (value && props.show) resetFilters()
  }
)

onUnmounted(() => {
  stopPolling()
})
</script>
