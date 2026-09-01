<template>
  <AppLayout>
    <div class="space-y-6">
      <UsageStatsCards :stats="usageStats" />
      <!-- Charts Section -->
      <div class="space-y-4">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:source="modelDistributionSource"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :upstream-model-stats="upstreamModelStats"
            :mapping-model-stats="mappingModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <!-- 明细区：tab 栏 + 筛选 + 内容收进同一张卡片，消除割裂感 -->
      <div class="card">
        <div class="flex flex-wrap items-center border-b border-gray-200 px-2 dark:border-dark-700 sm:px-4">
          <button
            v-for="tab in detailTabs"
            :key="tab.key"
            type="button"
            data-testid="usage-detail-tab"
            class="-mb-px inline-flex items-center gap-1.5 border-b-2 px-3 py-3 text-sm font-medium transition-colors sm:px-4"
            :class="activeTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
            @click="switchTab(tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>

        <UsageFilters v-model="filters" flat :mode="activeTab" class="border-b border-gray-100 dark:border-dark-700/50" :start-date="startDate" :end-date="endDate" :exporting="exporting" :model-options="modelNameOptions" @change="applyFilters" @refresh="refreshData" @reset="resetFilters" @cleanup="openCleanupDialog" @export="exportToExcel">
          <template #after-reset>
            <div class="relative" ref="columnDropdownRef">
              <button
                @click="showColumnDropdown = !showColumnDropdown"
                class="btn btn-secondary px-2 md:px-3"
                :title="t('admin.users.columnSettings')"
              >
                <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
                </svg>
                <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
              </button>
              <div
                v-if="showColumnDropdown"
                class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
              >
                <button
                  v-for="col in currentToggleableColumns"
                  :key="col.key"
                  @click="toggleCurrentColumn(col.key)"
                  class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
                >
                  <span>{{ col.label }}</span>
                  <Icon
                    v-if="isCurrentColumnVisible(col.key)"
                    name="check"
                    size="sm"
                    class="text-primary-500"
                    :stroke-width="2"
                  />
                </button>
              </div>
            </div>
          </template>
        </UsageFilters>

        <div v-show="activeTab === 'usage'" class="overflow-hidden rounded-b-2xl">
          <UsageTable
            flat
            :data="usageLogs"
            :loading="loading"
            :columns="visibleColumns"
            :server-side-sort="true"
            :default-sort-key="'created_at'"
            :default-sort-order="'desc'"
            @sort="handleSort"
            @ipGeoBatchFailed="handleIpGeoBatchFailed"
          />
          <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="handlePageChange" @update:pageSize="handlePageSizeChange" />
        </div>
        <div v-show="activeTab === 'errors'" class="overflow-hidden rounded-b-2xl">
          <OpsErrorLogTable
            flat
            :rows="errRows" :total="errTotal" :loading="errLoading"
            :page="errPage" :page-size="errPageSize"
            :visible-column-keys="errVisibleColumnKeys"
            @openErrorDetail="openError"
            @sort="onErrSort"
            @update:page="onErrPage"
            @update:pageSize="onErrPageSize"
            @ipGeoBatchFailed="handleIpGeoBatchFailed" />
        </div>
      </div>
      <OpsErrorDetailModal v-model:show="showErrorModal" :error-id="selectedErrorId" :error-type="'request'" />
    </div>
  </AppLayout>
  <UsageExportProgress :show="exportProgress.show" :progress="exportProgress.progress" :current="exportProgress.current" :total="exportProgress.total" :estimated-time="exportProgress.estimatedTime" @cancel="cancelExport" />
  <UsageCleanupDialog
    :show="cleanupDialogVisible"
    :filters="filters"
    :start-date="startDate"
    :end-date="endDate"
    :record-type="cleanupRecordType"
    @close="cleanupDialogVisible = false"
  />
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'; import { adminAPI } from '@/api/admin'; import { adminUsageAPI } from '@/api/admin/usage'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatReasoningEffort } from '@/utils/format'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import AppLayout from '@/components/layout/AppLayout.vue'; import Pagination from '@/components/common/Pagination.vue'; import Select from '@/components/common/Select.vue'; import DateRangePicker from '@/components/common/DateRangePicker.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'; import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'; import UsageExportProgress from '@/components/admin/usage/UsageExportProgress.vue'
import UsageCleanupDialog from '@/components/admin/usage/UsageCleanupDialog.vue'
import OpsErrorLogTable from '@/views/admin/ops/components/OpsErrorLogTable.vue'
import OpsErrorDetailModal from '@/views/admin/ops/components/OpsErrorDetailModal.vue'
import { getErrorLogDetail, listErrorLogs } from '@/api/admin/ops'
import type { OpsErrorDetail, OpsErrorLog, OpsErrorListQueryParams } from '@/api/admin/ops'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'; import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'; import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import Icon from '@/components/icons/Icon.vue'
import { mapErrorCategory } from '@/utils/errorCategory'
import type { AdminUsageLog, TrendDataPoint, ModelStat, GroupStat, EndpointStat } from '@/types'; import type { AdminUsageStatsResponse, AdminUsageQueryParams } from '@/api/admin/usage'

const { t } = useI18n()
const appStore = useAppStore()
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
const route = useRoute()
const usageStats = ref<AdminUsageStatsResponse | null>(null); const usageLogs = ref<AdminUsageLog[]>([]); const loading = ref(false); const exporting = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const requestedModelStats = ref<ModelStat[]>([]); const upstreamModelStats = ref<ModelStat[]>([]); const mappingModelStats = ref<ModelStat[]>([]); const groupStats = ref<GroupStat[]>([]); const chartsLoading = ref(false); const modelStatsLoading = ref(false); const granularity = ref<'day' | 'hour'>('hour')
const modelDistributionMetric = ref<DistributionMetric>('tokens')
const modelDistributionSource = ref<ModelDistributionSource>('requested')
const loadedModelSources = reactive<Record<ModelDistributionSource, boolean>>({
  requested: false,
  upstream: false,
  mapping: false,
})
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const endpointStatsLoading = ref(false)
let abortController: AbortController | null = null; let exportAbortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0
const exportProgress = reactive({ show: false, progress: 0, current: 0, total: 0, estimatedTime: '' })
const cleanupDialogVisible = ref(false)
const cleanupRecordType = ref<'usage' | 'errors'>('usage')
const breakdownFilters = computed(() => {
  const result: Record<string, unknown> = {}
  if (filters.value.api_key_id) result.api_key_id = filters.value.api_key_id
  if (filters.value.account_id) result.account_id = filters.value.account_id
  if (filters.value.group_id) result.group_id = filters.value.group_id
  if (filters.value.request_type != null) result.request_type = filters.value.request_type
  if (filters.value.billing_type != null) result.billing_type = filters.value.billing_type
  return result
})
const modelNameOptions = computed(() =>
  Array.from(new Set(requestedModelStats.value.map((m) => m.model).filter(Boolean))).sort()
)

const granularityOptions = computed(() => [{ value: 'day', label: t('admin.dashboard.day') }, { value: 'hour', label: t('admin.dashboard.hour') }])
// Use local timezone to avoid UTC timezone issues
const formatLD = (d: Date) => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const getLast24HoursRangeDates = (): { start: string; end: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLD(start),
    end: formatLD(end)
  }
}
const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  const daysDiff = Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24))
  return daysDiff <= 1 ? 'hour' : 'day'
}
const defaultRange = getLast24HoursRangeDates()
const startDate = ref(defaultRange.start); const endDate = ref(defaultRange.end)
const filters = ref<AdminUsageQueryParams>({ model: undefined, group_id: undefined, request_type: undefined, billing_type: null, start_date: startDate.value, end_date: endDate.value })
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const getSingleQueryValue = (value: string | null | Array<string | null> | undefined): string | undefined => {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

const applyRouteQueryFilters = () => {
  const queryStartDate = getSingleQueryValue(route.query.start_date)
  const queryEndDate = getSingleQueryValue(route.query.end_date)

  if (queryStartDate) startDate.value = queryStartDate
  if (queryEndDate) endDate.value = queryEndDate

  filters.value = {
    ...filters.value,
    start_date: startDate.value,
    end_date: endDate.value
  }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  filters.value = {
    ...filters.value,
    start_date: range.startDate,
    end_date: range.endDate
  }
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  applyFilters()
}

const buildUsageListParams = (
  page: number,
  pageSize: number,
  exactTotal: boolean
): AdminUsageQueryParams => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    page,
    page_size: pageSize,
    exact_total: exactTotal,
    ...filters.value,
    stream: legacyStream === null ? undefined : legacyStream,
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}

const loadLogs = async () => {
  abortController?.abort(); const c = new AbortController(); abortController = c; loading.value = true
  try {
    const res = await adminAPI.usage.list(
      buildUsageListParams(pagination.page, pagination.page_size, false),
      { signal: c.signal }
    )
    if(!c.signal.aborted) { usageLogs.value = res.items; pagination.total = res.total }
  } catch (error: any) { if(error?.name !== 'AbortError') console.error('Failed to load usage logs:', error) } finally { if(abortController === c) loading.value = false }
}
const loadStats = async (force = false) => {
  const seq = ++statsReqSeq
  endpointStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const s = await adminAPI.usage.getStats({
      ...filters.value,
      stream: legacyStream === null ? undefined : legacyStream,
      ...(force ? { nocache: 1 } : {}),
    })
    if (seq !== statsReqSeq) return
    usageStats.value = s
    inboundEndpointStats.value = s.endpoints || []
    upstreamEndpointStats.value = s.upstream_endpoints || []
    endpointPathStats.value = s.endpoint_paths || []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

// 失效模型统计缓存:仅标记需要重取,保留旧数据直到新数据到达(避免刷新时图表闪空)。
const invalidateModelStatsCache = () => {
  loadedModelSources.requested = false
  loadedModelSources.upstream = false
  loadedModelSources.mapping = false
}

const loadModelStats = async (source: ModelDistributionSource, force = false) => {
  if (!force && loadedModelSources[source]) {
    return
  }

  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const baseParams = {
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
    }

    const response = await adminAPI.dashboard.getModelStats({ ...baseParams, model_source: source })

    if (seq !== modelStatsReqSeq) return

    const models = response.models || []
    if (source === 'requested') {
      requestedModelStats.value = models
    } else if (source === 'upstream') {
      upstreamModelStats.value = models
    } else {
      mappingModelStats.value = models
    }
    loadedModelSources[source] = true
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
    if (source === 'requested') {
      requestedModelStats.value = []
    } else if (source === 'upstream') {
      upstreamModelStats.value = []
    } else {
      mappingModelStats.value = []
    }
    loadedModelSources[source] = false
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const snapshot = await adminAPI.dashboard.getSnapshotV2({
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      granularity: granularity.value,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      billing_type: filters.value.billing_type,
	  upstream_model_mismatch: filters.value.upstream_model_mismatch,
      include_stats: false,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) { console.error('Failed to load chart data:', error) } finally { if (seq === chartReqSeq) chartsLoading.value = false }
}
const applyFilters = () => {
  pagination.page = 1
  invalidateModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  errPage.value = 1
  if (activeTab.value === 'errors') {
    loadAdminErrors()
  } else {
    errRows.value = []
  }
}
const refreshData = () => {
  invalidateModelStatsCache()
  loadLogs()
  loadStats(true)
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeTab.value === 'errors') loadAdminErrors()
}
const resetFilters = () => {
  const range = getLast24HoursRangeDates()
  startDate.value = range.start
  endDate.value = range.end
  filters.value = { start_date: startDate.value, end_date: endDate.value, request_type: undefined, billing_type: null, billing_mode: undefined }
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
  applyFilters()
}
const handlePageChange = (p: number) => { pagination.page = p; loadLogs() }
const handlePageSizeChange = (s: number) => { pagination.page_size = s; pagination.page = 1; loadLogs() }
const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadLogs()
}

const handleIpGeoBatchFailed = () => {
  appStore.showError(t('usage.ipGeo.batchFailed'))
}
const cancelExport = () => exportAbortController?.abort()
const openCleanupDialog = () => {
  cleanupRecordType.value = activeTab.value === 'errors' ? 'errors' : 'usage'
  cleanupDialogVisible.value = true
}
const getRequestTypeLabel = (log: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'cyber') return t('usage.cyber')
  if (requestType === 'live') return t('usage.live')
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const exportToExcel = async () => {
  // The same toolbar is used by the usage and error tabs.  Error rows come
  // from the ops log endpoint and require a detail lookup so the exported
  // workbook can include the complete user request snapshot.
  if (activeTab.value === 'errors') {
    await exportErrorsToExcel()
    return
  }

  if (exporting.value) return; exporting.value = true; exportProgress.show = true
  const c = new AbortController(); exportAbortController = c
  try {
    let p = 1; let total = pagination.total; let exportedCount = 0
    const XLSX = await import('xlsx')
    const headers = [
      t('usage.time'), t('usage.apiKeyFilter'),
      t('admin.usage.account'), t('usage.requestedModel'), t('usage.sentUpstreamModel'), t('usage.upstreamResponseModel'), t('usage.upstreamModelMismatch'), t('usage.reasoningEffort'), t('admin.usage.group'),
      t('usage.inboundEndpoint'), t('usage.upstreamEndpoint'),
      t('usage.type'),
      t('admin.usage.inputTokens'), t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'), t('admin.usage.cacheCreationTokens'),
      t('admin.usage.inputCost'), t('admin.usage.outputCost'),
      t('admin.usage.cacheReadCost'), t('admin.usage.cacheCreationCost'),
      t('usage.rate'), t('usage.accountMultiplier'), t('usage.original'), t('usage.actualCost'), t('usage.accountBilled'),
      t('usage.firstToken'), t('usage.duration'),
      t('admin.usage.requestId'), t('usage.userAgent'), t('admin.usage.ipAddress')
    ]
    const ws = XLSX.utils.aoa_to_sheet([headers])
    while (true) {
      const res = await adminUsageAPI.list(
        buildUsageListParams(p, 100, true),
        { signal: c.signal }
      )
      if (c.signal.aborted) break; if (p === 1) { total = res.total; exportProgress.total = total }
      const rows = (res.items || []).map((log: AdminUsageLog) => [
        log.created_at, log.api_key?.name || '', log.account?.name || '', log.model,
        log.upstream_model || log.model, log.upstream_response_model || '', log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'), formatReasoningEffort(log.reasoning_effort), log.group?.name || '',
        log.inbound_endpoint || '', log.upstream_endpoint || '', getRequestTypeLabel(log),
        log.input_tokens, log.output_tokens, log.cache_read_tokens, log.cache_creation_tokens,
        log.input_cost?.toFixed(6) || '0.000000', log.output_cost?.toFixed(6) || '0.000000',
        log.cache_read_cost?.toFixed(6) || '0.000000', log.cache_creation_cost?.toFixed(6) || '0.000000',
        log.rate_multiplier?.toPrecision(4) || '1.00', (log.account_rate_multiplier ?? 1).toPrecision(4),
        log.total_cost?.toFixed(6) || '0.000000', log.actual_cost?.toFixed(6) || '0.000000',
        ((log.account_stats_cost ?? log.total_cost) * (log.account_rate_multiplier ?? 1)).toFixed(6), log.first_token_ms ?? '', log.duration_ms,
        log.request_id || '', log.user_agent || '', log.ip_address || ''
      ])
      if (rows.length) {
        XLSX.utils.sheet_add_aoa(ws, rows, { origin: -1 })
      }
      exportedCount += rows.length
      exportProgress.current = exportedCount
      exportProgress.progress = total > 0 ? Math.min(100, Math.round(exportedCount / total * 100)) : 0
      if (exportedCount >= total || res.items.length < 100) break; p++
    }
    if(!c.signal.aborted) {
      const wb = XLSX.utils.book_new()
      XLSX.utils.book_append_sheet(wb, ws, 'Usage')
      saveAs(new Blob([XLSX.write(wb, { bookType: 'xlsx', type: 'array' })], { type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' }), `usage_${filters.value.start_date}_to_${filters.value.end_date}.xlsx`)
      appStore.showSuccess(t('usage.exportSuccess'))
    }
  } catch (error) { console.error('Failed to export:', error); appStore.showError('Export Failed') }
  finally { if(exportAbortController === c) { exportAbortController = null; exporting.value = false; exportProgress.show = false } }
}

// Column visibility
const ALWAYS_VISIBLE = ['created_at']
const DEFAULT_HIDDEN_COLUMNS = ['reasoning_effort', 'request_id', 'user_agent']
const HIDDEN_COLUMNS_KEY = 'usage-hidden-columns'
const HIDDEN_COLUMNS_VERSION_KEY = 'usage-hidden-columns-version'
const HIDDEN_COLUMNS_CURRENT_VERSION = 'request-id-hidden-by-default'

const allColumns = computed(() => [
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'account', label: t('admin.usage.account'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'request_id', label: t('admin.usage.requestId'), sortable: false },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
  { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false }
])

const hiddenColumns = reactive<Set<string>>(new Set())

const toggleableColumns = computed(() =>
  allColumns.value.filter(col => !ALWAYS_VISIBLE.includes(col.key))
)

const visibleColumns = computed(() =>
  allColumns.value.filter(col =>
    ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key)
  )
)

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
    localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

// ---- 错误请求 tab 列设置(与用量明细同机制,独立存储) ----
const ERR_ALWAYS_VISIBLE = ['status', 'created_at', 'actions']
const ERR_DEFAULT_HIDDEN_COLUMNS = ['user_agent']
const ERR_HIDDEN_COLUMNS_KEY = 'usage-error-hidden-columns'

// key 集合须与 OpsErrorLogTable 内部 allColumns 一致
const errAllColumns = computed(() => [
  { key: 'api_key', label: t('admin.ops.errorLog.apiKey') },
  { key: 'account', label: t('admin.ops.errorLog.account') },
  { key: 'platform', label: t('admin.ops.errorLog.platform') },
  { key: 'model', label: t('admin.ops.errorLog.model') },
  { key: 'endpoint', label: t('admin.ops.errorLog.endpoint') },
  { key: 'group', label: t('admin.ops.errorLog.group') },
  { key: 'type', label: t('admin.ops.errorLog.type') },
  { key: 'category', label: t('usage.errors.category') },
  { key: 'status', label: t('admin.ops.errorLog.status') },
  { key: 'message', label: t('admin.ops.errorLog.message') },
  { key: 'created_at', label: t('admin.ops.errorLog.time') },
  { key: 'user_agent', label: t('usage.userAgent') },
  { key: 'client_ip', label: t('admin.ops.errorLog.ip') },
  { key: 'actions', label: t('admin.ops.errorLog.action') },
])

const errHiddenColumns = reactive<Set<string>>(new Set())

const errToggleableColumns = computed(() =>
  errAllColumns.value.filter(col => !ERR_ALWAYS_VISIBLE.includes(col.key))
)

const errVisibleColumnKeys = computed(() =>
  errAllColumns.value
    .filter(col => ERR_ALWAYS_VISIBLE.includes(col.key) || !errHiddenColumns.has(col.key))
    .map(col => col.key)
)

const toggleErrColumn = (key: string) => {
  if (errHiddenColumns.has(key)) {
    errHiddenColumns.delete(key)
  } else {
    errHiddenColumns.add(key)
  }
  try {
    localStorage.setItem(ERR_HIDDEN_COLUMNS_KEY, JSON.stringify([...errHiddenColumns]))
  } catch (e) {
    console.error('Failed to save error columns:', e)
  }
}

const loadSavedErrColumns = () => {
  try {
    const saved = localStorage.getItem(ERR_HIDDEN_COLUMNS_KEY)
    const keys = saved ? (JSON.parse(saved) as string[]) : ERR_DEFAULT_HIDDEN_COLUMNS
    keys.forEach((key) => errHiddenColumns.add(key))
  } catch {
    ERR_DEFAULT_HIDDEN_COLUMNS.forEach((key) => errHiddenColumns.add(key))
  }
}

// 列设置下拉按当前 tab 分发
const currentToggleableColumns = computed(() =>
  activeTab.value === 'errors' ? errToggleableColumns.value : toggleableColumns.value
)
const isCurrentColumnVisible = (key: string) =>
  activeTab.value === 'errors' ? !errHiddenColumns.has(key) : isColumnVisible(key)
const toggleCurrentColumn = (key: string) =>
  activeTab.value === 'errors' ? toggleErrColumn(key) : toggleColumn(key)

const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      (JSON.parse(saved) as string[]).forEach((key) => {
        hiddenColumns.add(key)
      })
      if (localStorage.getItem(HIDDEN_COLUMNS_VERSION_KEY) !== HIDDEN_COLUMNS_CURRENT_VERSION) {
        hiddenColumns.add('request_id')
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
        localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        hiddenColumns.add(key)
      })
      localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
    }
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
      hiddenColumns.add(key)
    })
  }
}

// Detail tabs
type DetailTab = 'usage' | 'errors'
const activeTab = ref<DetailTab>('usage')
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  { key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const },
])

const switchTab = (tab: DetailTab) => {
  activeTab.value = tab
  if (tab === 'errors' && errRows.value.length === 0) loadAdminErrors()
}

// Error tab state
const errRows = ref<OpsErrorLog[]>([])
const errLoading = ref(false)
const errPage = ref(1)
const errPageSize = ref(20)
const errTotal = ref(0)
const errSortBy = ref('created_at')
const errSortOrder = ref<'asc' | 'desc'>('desc')
const showErrorModal = ref(false)
const selectedErrorId = ref<number | null>(null)

// 注意：'YYYY-MM-DDT00:00:00' 无时区后缀，按本地时区解析后再转 UTC——与页面其它日期处理语义一致，刻意如此，勿改成 'T00:00:00Z'
const toRFC3339 = (d: string | undefined, endOfDay = false): string | undefined =>
  d ? new Date(d + (endOfDay ? 'T23:59:59.999' : 'T00:00:00')).toISOString() : undefined

const loadAdminErrors = async () => {
  errLoading.value = true
  try {
    const resp = await listErrorLogs({
      page: errPage.value,
      page_size: errPageSize.value,
      view: 'all',
      start_time: toRFC3339(filters.value.start_date),
      end_time: toRFC3339(filters.value.end_date, true),
      api_key_id: filters.value.api_key_id ?? undefined,
      account_id: filters.value.account_id ?? undefined,
      group_id: filters.value.group_id ?? undefined,
      model: filters.value.model || undefined,
      phase: filters.value.error_phase || undefined,
      category: filters.value.error_category || undefined,
      status_codes: filters.value.status_code != null ? String(filters.value.status_code) : undefined,
      sort_by: errSortBy.value,
      sort_order: errSortOrder.value,
    })
    errRows.value = resp.items
    errTotal.value = resp.total
  } catch (error) {
    console.error('Failed to load admin errors:', error)
    appStore.showError(t('usage.errors.failedToLoad'))
  } finally {
    errLoading.value = false
  }
}

/**
 * Build the exact query used by the error tab for an export page.  Keep this
 * in one place so a downloaded workbook cannot silently differ from the rows
 * currently visible in the table (especially date, category, and sort
 * filters).
 */
const buildErrorExportParams = (
  page: number,
  pageSize: number,
  filterSource: AdminUsageQueryParams = filters.value,
  sortBy: string = errSortBy.value,
  sortOrder: 'asc' | 'desc' = errSortOrder.value,
  includeDetails = false,
): OpsErrorListQueryParams => ({
  page,
  page_size: pageSize,
  view: 'all',
  start_time: toRFC3339(filterSource.start_date),
  end_time: toRFC3339(filterSource.end_date, true),
  api_key_id: filterSource.api_key_id ?? undefined,
  account_id: filterSource.account_id ?? undefined,
  group_id: filterSource.group_id ?? undefined,
  model: filterSource.model || undefined,
  phase: filterSource.error_phase || undefined,
  category: filterSource.error_category || undefined,
  status_codes: filterSource.status_code != null ? String(filterSource.status_code) : undefined,
  sort_by: sortBy,
  sort_order: sortOrder,
  ...(includeDetails ? { include_details: true } : {}),
})

function stringifyErrorExportValue(value: unknown): string | number | boolean {
  if (value == null) return ''
  if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') return value
  try {
    // JSON.stringify returns undefined for values such as functions and
    // symbols.  Keep the worksheet cell type stable even for malformed or
    // legacy payloads received from a gateway.
    return JSON.stringify(value) ?? String(value)
  } catch {
    return String(value)
  }
}

// Diagnostic rows can contain a sizeable request snapshot.  Keep each
// response bounded while still doubling the old 100-row page size; the
// include_details projection removes the much more expensive N+1 lookups.
const ERROR_EXPORT_PAGE_SIZE = 200

// Detail requests are independent primary-key lookups. Keep a bounded worker
// fan-out so a large export does not wait for eight serial waves while still
// avoiding an unbounded burst against the browser/DB connection pools.
const ERROR_EXPORT_DETAIL_CONCURRENCY = 32

// XLSX worksheet cells are limited to 32,767 UTF-16 code units.  Request
// snapshots are intentionally retained by the backend up to a much larger
// bound, so keep oversized values lossless by placing chunks on a companion
// sheet while leaving a short, discoverable marker in the main table.
const ERROR_EXPORT_CELL_MAX_CHARS = 32767

type ErrorRequestDetailChunk = [number, string, number, number, string]

// Other diagnostic text fields (notably upstream_errors) can also exceed the
// XLSX per-cell limit. Keep them on a separate companion sheet so the request
// details sheet and its existing five-column contract remain unchanged.
type ErrorLargeTextChunk = [number, string, string, number, number, string]

/**
 * Split text on the XLSX UTF-16 code-unit boundary without separating a
 * surrogate pair.  JavaScript's `String#slice` counts code units, so a plain
 * fixed-width slice can otherwise leave an emoji split across two cells.
 */
function splitErrorExportText(value: string): string[] {
  if (value.length <= ERROR_EXPORT_CELL_MAX_CHARS) return [value]

  const chunks: string[] = []
  let offset = 0
  while (offset < value.length) {
    let end = Math.min(offset + ERROR_EXPORT_CELL_MAX_CHARS, value.length)
    if (
      end < value.length
      && end > offset
      && value.charCodeAt(end - 1) >= 0xd800
      && value.charCodeAt(end - 1) <= 0xdbff
      && value.charCodeAt(end) >= 0xdc00
      && value.charCodeAt(end) <= 0xdfff
    ) {
      end -= 1
    }
    // A surrogate pair is at most two code units, so this guard is only a
    // defensive fallback for malformed strings or future limit changes.
    if (end <= offset) end = Math.min(offset + ERROR_EXPORT_CELL_MAX_CHARS, value.length)
    chunks.push(value.slice(offset, end))
    offset = end
  }
  return chunks
}

function requestDetailsForExcel(
  value: unknown,
  errorId: number,
  requestId: string,
  chunks: ErrorRequestDetailChunk[]
): string | number | boolean {
  const serialized = stringifyErrorExportValue(value)
  if (typeof serialized !== 'string' || serialized.length <= ERROR_EXPORT_CELL_MAX_CHARS) {
    return serialized
  }

  const textChunks = splitErrorExportText(serialized)
  const chunkCount = textChunks.length
  for (let index = 0; index < chunkCount; index += 1) {
    chunks.push([
      errorId,
      requestId,
      index + 1,
      chunkCount,
      textChunks[index],
    ])
  }

  const marker = t('admin.ops.errorExport.requestDetailsChunked', { count: chunkCount })
  // Keep the marker visible even if a translated marker is unusually long.
  const safeMarker = marker.slice(0, ERROR_EXPORT_CELL_MAX_CHARS)
  const prefixLength = Math.max(0, ERROR_EXPORT_CELL_MAX_CHARS - safeMarker.length)
  return serialized.slice(0, prefixLength) + safeMarker
}

function largeTextForExcel(
  value: unknown,
  errorId: number,
  requestId: string,
  fieldLabel: string,
  chunks: ErrorLargeTextChunk[]
): string | number | boolean {
  const serialized = stringifyErrorExportValue(value)
  if (typeof serialized !== 'string' || serialized.length <= ERROR_EXPORT_CELL_MAX_CHARS) {
    return serialized
  }

  const textChunks = splitErrorExportText(serialized)
  const chunkCount = textChunks.length
  for (let index = 0; index < chunkCount; index += 1) {
    chunks.push([
      errorId,
      requestId,
      fieldLabel,
      index + 1,
      chunkCount,
      textChunks[index],
    ])
  }

  const marker = t('admin.ops.errorExport.fieldChunked', { field: fieldLabel, count: chunkCount })
  const safeMarker = marker.slice(0, ERROR_EXPORT_CELL_MAX_CHARS)
  const prefixLength = Math.max(0, ERROR_EXPORT_CELL_MAX_CHARS - safeMarker.length)
  return serialized.slice(0, prefixLength) + safeMarker
}

function formatErrorRequestType(type: number | null | undefined): string {
  switch (type) {
    case 1: return t('admin.ops.errorLog.requestTypeSync')
    case 2: return t('admin.ops.errorLog.requestTypeStream')
    case 3: return t('admin.ops.errorLog.requestTypeWs')
    default: return type == null ? '' : String(type)
  }
}

function formatBooleanForErrorExport(value: boolean | null | undefined): string {
  if (value == null) return ''
  return t(value ? 'common.yes' : 'common.no')
}

/**
 * Older gateways may omit diagnostic fields on paginated rows. Resolve those
 * legacy rows with a bounded worker pool; one failed detail must not discard
 * the rest of an otherwise valid export. The abort signal is passed through
 * to axios so cancelling the progress dialog stops in-flight calls.
 */
async function loadErrorExportDetails(
  rows: OpsErrorLog[],
  controller: AbortController
): Promise<Map<number, OpsErrorDetail>> {
  const details = new Map<number, OpsErrorDetail>()
  if (rows.length === 0 || controller.signal.aborted) return details

  // Keep a fixed number of workers busy instead of waiting for a whole batch
  // to finish before starting the next one.  A single slow detail lookup then
  // no longer stalls the other rows, while the bounded worker count still
  // protects the browser and database connection pools.
  let nextIndex = 0
  const workerCount = Math.min(ERROR_EXPORT_DETAIL_CONCURRENCY, rows.length)
  const loadNext = async (): Promise<void> => {
    while (!controller.signal.aborted) {
      const index = nextIndex
      nextIndex += 1
      if (index >= rows.length) return

      const row = rows[index]
      if (!row || !Number.isFinite(Number(row.id)) || Number(row.id) <= 0) continue

      // Future gateways may opt to include details in an export/list response;
      // use those values directly and avoid an unnecessary round trip.
      const embedded = row.request_details
      // New gateways return a marker when the list query projected the full
      // diagnostic columns.  In that mode an empty request snapshot is still
      // a complete result and must not trigger a fallback GET. For older
      // compatible gateways without the marker, accept a non-empty
      // diagnostic value as evidence that the projection is already inline.
      const hasText = (value: unknown): boolean => typeof value === 'string' && value.length > 0
      const hasInlineDetails = row.details_included === true
        || (embedded !== undefined && embedded !== null && embedded !== '')
        || hasText(row.error_body)
        || row.upstream_status_code != null
        || hasText(row.upstream_error_message)
        || hasText(row.upstream_error_detail)
        || hasText(row.upstream_errors)
        || row.auth_latency_ms != null
        || row.routing_latency_ms != null
        || row.upstream_latency_ms != null
        || row.response_latency_ms != null
        || row.time_to_first_token_ms != null
        || hasText(row.api_key_prefix)
      if (hasInlineDetails) {
        details.set(row.id, row as OpsErrorDetail)
        continue
      }

      try {
        const detail = await getErrorLogDetail(row.id, { signal: controller.signal })
        if (detail) details.set(row.id, detail)
      } catch (error) {
        if (controller.signal.aborted) throw error
        // Keep the summary row in the workbook even if an individual detail
        // endpoint fails (for example, a concurrently cleaned-up log).
        console.warn(`[UsageView] Failed to load error detail ${row.id}`, error)
      }
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => loadNext()))

  return details
}

/** Export the currently filtered error log list, including full request snapshots. */
async function exportErrorsToExcel() {
  if (exporting.value) return
  exporting.value = true
  exportProgress.show = true
  const controller = new AbortController()
  exportAbortController = controller

  exportProgress.current = 0
  exportProgress.progress = 0
  exportProgress.total = errTotal.value

  // Use the server's shared admin limit.  The export asks for the bounded
  // diagnostic projection in the same query, so there is no per-row detail
  // round trip; the server's ingestion bound keeps each snapshot finite.
  const pageSize = ERROR_EXPORT_PAGE_SIZE
  // Freeze filters/sort for the whole operation.  Changing a control while
  // requests are in flight must not produce a workbook containing mixed
  // result sets or a filename that no longer describes its contents.
  const filterSnapshot = { ...filters.value }
  const sortBySnapshot = errSortBy.value
  const sortOrderSnapshot = errSortOrder.value
  const fileStart = filterSnapshot.start_date || 'start'
  const fileEnd = filterSnapshot.end_date || 'end'

  try {
    // Start the first list request while the code-split XLSX module is being
    // downloaded. These operations are independent and this removes the
    // module-download latency from the critical path on a cold browser cache.
    const firstPageRequest = listErrorLogs(
      buildErrorExportParams(1, pageSize, filterSnapshot, sortBySnapshot, sortOrderSnapshot, true),
      { signal: controller.signal }
    )
    const [XLSX, firstResponse] = await Promise.all([
      import('xlsx'),
      firstPageRequest,
    ])
    const headers = [
      t('admin.ops.errorExport.id'),
      t('admin.ops.errorExport.time'),
      t('admin.ops.errorExport.phase'),
      t('admin.ops.errorExport.type'),
      t('admin.ops.errorExport.category'),
      t('admin.ops.errorExport.owner'),
      t('admin.ops.errorExport.source'),
      t('admin.ops.errorExport.severity'),
      t('admin.ops.errorExport.status'),
      t('admin.ops.errorExport.platform'),
      t('admin.ops.errorExport.model'),
      t('admin.ops.errorExport.requestedModel'),
      t('admin.ops.errorExport.upstreamModel'),
      t('admin.ops.errorExport.inboundEndpoint'),
      t('admin.ops.errorExport.upstreamEndpoint'),
      t('admin.ops.errorExport.apiKey'),
      t('admin.ops.errorExport.account'),
      t('admin.ops.errorExport.group'),
      // User identity is intentionally not exported: the admin Ops contract
      // keeps user_id/user_email out of responses; request attribution remains
      // available through the request IDs and captured request snapshot.
      t('admin.ops.errorExport.clientRequestId'),
      t('admin.ops.errorExport.requestId'),
      t('admin.ops.errorExport.requestPath'),
      t('admin.ops.errorExport.requestType'),
      t('admin.ops.errorExport.stream'),
      t('admin.ops.errorExport.message'),
      t('admin.ops.errorExport.errorBody'),
      t('admin.ops.errorExport.upstreamStatus'),
      t('admin.ops.errorExport.upstreamMessage'),
      t('admin.ops.errorExport.upstreamDetail'),
      t('admin.ops.errorExport.upstreamEvents'),
      t('admin.ops.errorExport.authLatency'),
      t('admin.ops.errorExport.routingLatency'),
      t('admin.ops.errorExport.upstreamLatency'),
      t('admin.ops.errorExport.responseLatency'),
      t('admin.ops.errorExport.timeToFirstToken'),
      t('admin.ops.errorExport.userAgent'),
      t('admin.ops.errorExport.clientIp'),
      t('admin.ops.errorExport.resolved'),
      t('admin.ops.errorExport.businessLimited'),
      // Keep the complete JSON snapshot as a dedicated field.  This is the
      // canonical export value; the summary columns above aid filtering.
      t('admin.ops.errorExport.requestDetails'),
    ]
    const ws = XLSX.utils.aoa_to_sheet([headers])
    const requestDetailChunks: ErrorRequestDetailChunk[] = []
    const largeTextChunks: ErrorLargeTextChunk[] = []

    let page = 1
    let total = errTotal.value
    let totalKnown = false
    let reportedPages: number | null = null
    let effectivePageSize = pageSize
    let exportedCount = 0
    let pendingResponse: Awaited<typeof firstPageRequest> | null = firstResponse

    while (true) {
      if (controller.signal.aborted) break
      const response = pendingResponse ?? await listErrorLogs(
        buildErrorExportParams(page, pageSize, filterSnapshot, sortBySnapshot, sortOrderSnapshot, true),
        { signal: controller.signal }
      )
      pendingResponse = null
      if (controller.signal.aborted) break

      const summaryRows = response.items || []
      const parsedPageSize = Number(response.page_size)
      if (Number.isFinite(parsedPageSize) && parsedPageSize > 0) {
        // A compatible gateway may cap page_size below what we requested.
        // Remember the effective value so a full capped page is not mistaken
        // for the end of an otherwise paginated result set.
        effectivePageSize = parsedPageSize
      }
      if (page === 1) {
        const parsedTotal = Number(response.total)
        const parsedPages = Number(response.pages)
        // Treat a zero total with a non-empty first page as malformed/legacy
        // pagination metadata rather than stopping after that page. A valid
        // `pages` value remains useful as a second termination signal.
        totalKnown = response.total !== undefined && response.total !== null && Number.isFinite(parsedTotal) && (parsedTotal > 0 || summaryRows.length === 0)
        reportedPages = Number.isFinite(parsedPages) && parsedPages > 0 ? parsedPages : null
        total = totalKnown ? parsedTotal : 0
        exportProgress.total = total
      }
      if (summaryRows.length === 0) break

      const detailById = await loadErrorExportDetails(summaryRows, controller)
      if (controller.signal.aborted) break

      const rows = summaryRows.map((summary) => {
        const detail = detailById.get(summary.id)
        // The list DTO is intentionally lightweight while the detail DTO
        // carries diagnostic fields; normalize both shapes for the exporter.
        const row = { ...summary, ...(detail || {}) } as OpsErrorDetail & Partial<OpsErrorLog>
        const embeddedRequestDetails = summary.request_details
        const detailRequestDetails = detail?.request_details
        // Prefer a non-empty detail value, but retain an embedded snapshot if
        // a compatible gateway returns an empty detail field.
        const requestDetails = detailRequestDetails !== undefined && detailRequestDetails !== null && detailRequestDetails !== ''
          ? detailRequestDetails
          : embeddedRequestDetails
        // is_business_limited is tagged with omitempty on the lightweight
        // list DTO, so a projected false value may be absent from JSON.  The
        // details marker makes that absence unambiguous for the export.
        const businessLimited = row.is_business_limited == null && summary.details_included === true
          ? false
          : row.is_business_limited

        return [
          row.id,
          row.created_at,
          row.phase || '',
          row.type || '',
          t(`usage.errors.categories.${mapErrorCategory(row.phase, row.type)}`),
          row.error_owner || '',
          row.error_source || '',
          row.severity || '',
          row.status_code ?? '',
          row.platform || '',
          row.model || '',
          row.requested_model || '',
          row.upstream_model || '',
          row.inbound_endpoint || '',
          row.upstream_endpoint || '',
          row.api_key_name || (row.api_key_id != null ? `#${row.api_key_id}` : ''),
          row.account_name || (row.account_id != null ? `#${row.account_id}` : ''),
          row.group_name || (row.group_id != null ? `#${row.group_id}` : ''),
          row.client_request_id || '',
          row.request_id || '',
          row.request_path || '',
          formatErrorRequestType(row.request_type),
          formatBooleanForErrorExport(row.stream),
          row.message || '',
          largeTextForExcel(row.error_body, row.id, row.request_id || row.client_request_id || '', t('admin.ops.errorExport.errorBody'), largeTextChunks),
          row.upstream_status_code ?? '',
          largeTextForExcel(row.upstream_error_message, row.id, row.request_id || row.client_request_id || '', t('admin.ops.errorExport.upstreamMessage'), largeTextChunks),
          largeTextForExcel(row.upstream_error_detail, row.id, row.request_id || row.client_request_id || '', t('admin.ops.errorExport.upstreamDetail'), largeTextChunks),
          largeTextForExcel(row.upstream_errors, row.id, row.request_id || row.client_request_id || '', t('admin.ops.errorExport.upstreamEvents'), largeTextChunks),
          row.auth_latency_ms ?? '',
          row.routing_latency_ms ?? '',
          row.upstream_latency_ms ?? '',
          row.response_latency_ms ?? '',
          row.time_to_first_token_ms ?? '',
          row.user_agent || '',
          row.client_ip || '',
          formatBooleanForErrorExport(row.resolved),
          formatBooleanForErrorExport(businessLimited),
          requestDetailsForExcel(requestDetails, row.id, row.request_id || row.client_request_id || '', requestDetailChunks),
        ]
      })

      if (rows.length) XLSX.utils.sheet_add_aoa(ws, rows, { origin: -1 })
      exportedCount += rows.length
      exportProgress.current = exportedCount
      exportProgress.progress = total > 0
        ? Math.min(100, Math.round((exportedCount / total) * 100))
        : 0

      // A known total is authoritative. Without one, use an explicit page
      // count first, then the effective page size reported by the gateway;
      // this avoids truncating results when a server caps our requested size.
      // If a legacy gateway omits all pagination metadata, keep probing until
      // an empty page instead of treating a short page as the end: some
      // gateways silently cap page_size and would otherwise lose rows.
      const reachedKnownTotal = totalKnown && exportedCount >= total
      const reachedReportedPages = !totalKnown && reportedPages !== null && page >= reportedPages
      const reachedShortPage = !totalKnown
        && reportedPages === null
        && Number.isFinite(parsedPageSize)
        && parsedPageSize > 0
        && summaryRows.length < effectivePageSize
      if (reachedKnownTotal || reachedReportedPages || reachedShortPage) break
      page += 1
    }

    if (!controller.signal.aborted) {
      const workbook = XLSX.utils.book_new()
      XLSX.utils.book_append_sheet(workbook, ws, 'Error Logs')
      if (requestDetailChunks.length > 0) {
        const detailSheetHeaders = [
          t('admin.ops.errorExport.detailSheetErrorId'),
          t('admin.ops.errorExport.detailSheetRequestId'),
          t('admin.ops.errorExport.detailSheetChunk'),
          t('admin.ops.errorExport.detailSheetChunkCount'),
          t('admin.ops.errorExport.detailSheetContent'),
        ]
        const detailWs = XLSX.utils.aoa_to_sheet([detailSheetHeaders])
        XLSX.utils.sheet_add_aoa(detailWs, requestDetailChunks, { origin: -1 })
        XLSX.utils.book_append_sheet(workbook, detailWs, 'Request Details')
      }
      if (largeTextChunks.length > 0) {
        const largeTextSheetHeaders = [
          t('admin.ops.errorExport.detailSheetErrorId'),
          t('admin.ops.errorExport.detailSheetRequestId'),
          t('admin.ops.errorExport.detailSheetField'),
          t('admin.ops.errorExport.detailSheetChunk'),
          t('admin.ops.errorExport.detailSheetChunkCount'),
          t('admin.ops.errorExport.detailSheetContent'),
        ]
        const largeTextWs = XLSX.utils.aoa_to_sheet([largeTextSheetHeaders])
        XLSX.utils.sheet_add_aoa(largeTextWs, largeTextChunks, { origin: -1 })
        XLSX.utils.book_append_sheet(workbook, largeTextWs, 'Oversized Fields')
      }
      saveAs(
        new Blob([XLSX.write(workbook, { bookType: 'xlsx', type: 'array' })], {
          type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
        }),
        `error_logs_${fileStart}_to_${fileEnd}.xlsx`
      )
      appStore.showSuccess(t('admin.ops.errorExport.success'))
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      // If module loading or a list request fails while the other operation is
      // still in flight, stop it before tearing down the progress state.
      controller.abort()
      console.error('Failed to export error logs:', error)
      appStore.showError(t('admin.ops.errorExport.failed'))
    }
  } finally {
    if (exportAbortController === controller) {
      exportAbortController = null
      exporting.value = false
      exportProgress.show = false
    }
  }
}

const onErrSort = (sortBy: string, sortOrder: 'asc' | 'desc') => {
  errSortBy.value = sortBy
  errSortOrder.value = sortOrder
  errPage.value = 1
  loadAdminErrors()
}
const onErrPage = (p: number) => { errPage.value = p; loadAdminErrors() }
const onErrPageSize = (s: number) => { errPageSize.value = s; errPage.value = 1; loadAdminErrors() }
const openError = (id: number) => { selectedErrorId.value = id; showErrorModal.value = true }

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}

onMounted(() => {
  applyRouteQueryFilters()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  window.setTimeout(() => {
    void loadChartData()
  }, 120)
  loadSavedColumns()
  loadSavedErrColumns()
  document.addEventListener('click', handleColumnClickOutside)
})
onUnmounted(() => { abortController?.abort(); exportAbortController?.abort(); document.removeEventListener('click', handleColumnClickOutside) })

watch(modelDistributionSource, (source) => {
  void loadModelStats(source)
})

defineExpose({ requestedModelStats, refreshData })
</script>
