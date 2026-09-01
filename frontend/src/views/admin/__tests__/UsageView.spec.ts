import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

import UsageView from '../UsageView.vue'

const { list, exportList, getStats, getSnapshotV2, getModelStats, listErrorLogs, getErrorLogDetail, routeQuery, aoaToSheet, sheetAddAoa, saveAs, xlsxWrite } = vi.hoisted(() => {
  vi.stubGlobal('localStorage', {
    getItem: vi.fn(() => null),
    setItem: vi.fn(),
    removeItem: vi.fn(),
  })

  return {
    list: vi.fn(),
		exportList: vi.fn(),
    getStats: vi.fn(),
    getSnapshotV2: vi.fn(),
    getModelStats: vi.fn(),
    listErrorLogs: vi.fn(),
    getErrorLogDetail: vi.fn(),
    routeQuery: {} as Record<string, string>,
		aoaToSheet: vi.fn(() => ({})),
		sheetAddAoa: vi.fn(),
		saveAs: vi.fn(),
		xlsxWrite: vi.fn(() => new Uint8Array([1, 2, 3])),
  }
})

const messages: Record<string, string> = {
  'admin.dashboard.timeRange': 'Time Range',
  'admin.dashboard.day': 'Day',
  'admin.dashboard.hour': 'Hour',
	'admin.usage.requestId': 'Request ID',
	'usage.requestedModel': 'Requested model',
	'usage.sentUpstreamModel': 'Sent upstream model',
	'usage.upstreamResponseModel': 'Upstream response model',
	'usage.upstreamModelMismatch': 'Upstream model mismatch',
	'usage.actualCost': 'Actual cost',
	'usage.userBilled': 'User billed',
	'common.yes': 'Yes',
	'common.no': 'No',
}

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: {
      list,
      getStats,
    },
    dashboard: {
      getSnapshotV2,
      getModelStats,
    },
  },
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: {
		list: exportList,
  },
}))

vi.mock('file-saver', () => ({ saveAs }))

vi.mock('xlsx', () => ({
	utils: {
		aoa_to_sheet: aoaToSheet,
		sheet_add_aoa: sheetAddAoa,
		book_new: vi.fn(() => ({})),
		book_append_sheet: vi.fn(),
	},
	write: xlsxWrite,
}))

vi.mock('@/api/admin/ops', () => ({
  listErrorLogs,
  getErrorLogDetail,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showWarning: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/utils/format', () => ({
  formatReasoningEffort: (value: string | null | undefined) => value ?? '-',
}))

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: routeQuery }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const UsageFiltersStub = defineComponent({
  template: '<div><slot name="after-reset" /></div>',
})
const UsageTableStub = {
  props: ['columns'],
  template: '<div data-test="usage-table" />',
}
const ModelDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="model-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}
const GroupDistributionChartStub = {
  props: ['metric'],
  emits: ['update:metric'],
  template: `
    <div data-test="group-chart">
      <span class="metric">{{ metric }}</span>
      <button class="switch-metric" @click="$emit('update:metric', 'actual_cost')">switch</button>
    </div>
  `,
}

const mountRouteFilteredUsageView = () => mount(UsageView, {
  global: { stubs: {
    AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
    UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
    Pagination: true, Select: true,
    DateRangePicker: true, Icon: true, TokenUsageTrend: true,
    ModelDistributionChart: true, GroupDistributionChart: true,
    EndpointDistributionChart: true,
  } },
})

describe('admin UsageView retained route filters', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    vi.useRealTimers()
  })

  it('preserves routed date ranges without restoring the retired user filter', async () => {
    routeQuery.start_date = '2026-08-01'
    routeQuery.end_date = '2026-08-07'

    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    expect((wrapper.vm as any).startDate).toBe('2026-08-01')
    expect((wrapper.vm as any).endDate).toBe('2026-08-07')
    expect(list).toHaveBeenCalledWith(
      expect.objectContaining({ start_date: '2026-08-01', end_date: '2026-08-07' }),
      expect.anything(),
    )
    expect(list.mock.calls[0]?.[0]).not.toHaveProperty('user_id')
  })
})

describe('admin UsageView distribution metric toggles', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()

    list.mockResolvedValue({
      items: [],
      total: 0,
      pages: 0,
    })
    getStats.mockResolvedValue({
      total_requests: 0,
      total_input_tokens: 0,
      total_output_tokens: 0,
      total_cache_tokens: 0,
      total_tokens: 0,
      total_cost: 0,
      total_actual_cost: 0,
      average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({
      trend: [],
      models: [],
      groups: [],
    })
    getModelStats.mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps previous model stats visible during refresh until new data arrives', async () => {
    // 首次加载返回 A
    getModelStats.mockResolvedValueOnce({ models: [{ model: 'A', total_tokens: 10 }] })

    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        AuditLogModal: true, Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: ModelDistributionChartStub, GroupDistributionChart: GroupDistributionChartStub,
        EndpointDistributionChart: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'A', total_tokens: 10 }])

    // 刷新:让第二次 getModelStats 处于 pending,断言旧数据 A 仍在(不被清空成 [])
    let resolveSecond: (v: any) => void = () => {}
    getModelStats.mockReturnValueOnce(new Promise((res) => { resolveSecond = res }))
    ;(wrapper.vm as any).refreshData()
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'A', total_tokens: 10 }])

    // 新数据到达后替换为 B
    resolveSecond({ models: [{ model: 'B', total_tokens: 20 }] })
    await flushPromises()
    expect((wrapper.vm as any).requestedModelStats).toEqual([{ model: 'B', total_tokens: 20 }])
  })

  it('keeps model and group metric toggles independent without refetching chart data', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: true,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: ModelDistributionChartStub,
          GroupDistributionChart: GroupDistributionChartStub,
        },
      },
    })

    vi.advanceTimersByTime(120)
    await flushPromises()

    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)
    expect(getSnapshotV2).toHaveBeenCalledWith(expect.objectContaining({
      start_date: formatLocalDate(yesterday),
      end_date: formatLocalDate(now),
      granularity: 'hour'
    }))

    const modelChart = wrapper.find('[data-test="model-chart"]')
    const groupChart = wrapper.find('[data-test="group-chart"]')

    expect(modelChart.find('.metric').text()).toBe('tokens')
    expect(groupChart.find('.metric').text()).toBe('tokens')

    await modelChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('tokens')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)

    await groupChart.find('.switch-metric').trigger('click')
    await flushPromises()

    expect(modelChart.find('.metric').text()).toBe('actual_cost')
    expect(groupChart.find('.metric').text()).toBe('actual_cost')
    expect(getSnapshotV2).toHaveBeenCalledTimes(1)
  })
})

describe('admin UsageView request ID column visibility', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.mocked(localStorage.getItem).mockReset().mockReturnValue(null)
    vi.mocked(localStorage.setItem).mockReset()
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('keeps request ID hidden by default and allows enabling it from column settings', async () => {
    const wrapper = mount(UsageView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          UsageStatsCards: true,
          UsageFilters: UsageFiltersStub,
          UsageTable: UsageTableStub,
          UsageExportProgress: true,
          UsageCleanupDialog: true,
          AuditLogModal: true,
          Pagination: true,
          Select: true,
          DateRangePicker: true,
          Icon: true,
          TokenUsageTrend: true,
          ModelDistributionChart: true,
          GroupDistributionChart: true,
          EndpointDistributionChart: true,
        },
      },
    })
    await wrapper.vm.$nextTick()

    const usageTable = wrapper.findComponent(UsageTableStub)
    expect(usageTable.props('columns')).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ key: 'request_id' })]),
    )

    await wrapper.get('button[title="admin.users.columnSettings"]').trigger('click')
    const requestIdToggle = wrapper.findAll('button').find((button) => button.text() === 'Request ID')
    expect(requestIdToggle).toBeDefined()
    await requestIdToggle!.trigger('click')

    expect(usageTable.props('columns')).toEqual(
      expect.arrayContaining([expect.objectContaining({ key: 'request_id', label: 'Request ID' })]),
    )
    expect(localStorage.setItem).toHaveBeenCalledWith(
      'usage-hidden-columns-version',
      'request-id-hidden-by-default',
    )
  })
})

describe('admin UsageView errors tab filter forwarding', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()
    listErrorLogs.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
    listErrorLogs.mockResolvedValue({ items: [], total: 0, pages: 0 })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('forwards model/account_id/group_id to listErrorLogs on the errors tab', async () => {
    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        AuditLogModal: true, Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: true, GroupDistributionChart: true, EndpointDistributionChart: true,
        OpsErrorLogTable: true, OpsErrorDetailModal: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()

    // 模拟用户在过滤器里选择了模型/账户/分组
    const vm = wrapper.vm as any
    vm.filters.model = 'gpt-5.3-codex'
    vm.filters.account_id = 7
    vm.filters.group_id = 3
    await flushPromises()

    // 切换到「错误请求」标签（第二个 tab 按钮）触发 loadAdminErrors
    const tabs = wrapper.findAll('[data-testid="usage-detail-tab"]')
    await tabs[1].trigger('click')
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledWith(expect.objectContaining({
      view: 'all',
      model: 'gpt-5.3-codex',
      account_id: 7,
      group_id: 3,
    }))
  })
})

describe('admin UsageView error-log export', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
    listErrorLogs.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getErrorLogDetail.mockReset()
    aoaToSheet.mockClear()
    sheetAddAoa.mockClear()
    saveAs.mockClear()
    xlsxWrite.mockClear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('exports filtered error rows with detail snapshots and downloads an xlsx workbook', async () => {
    const summary = {
      id: 42,
      created_at: '2026-08-20T01:02:03Z',
      phase: 'request',
      type: 'invalid_request',
      error_owner: 'client',
      error_source: 'client_request',
      severity: 'P2',
      status_code: 400,
      platform: 'openai',
      model: 'gpt-test',
      resolved: false,
      client_request_id: 'client-42',
      request_id: 'request-42',
      message: 'invalid request',
      api_key_id: 7,
      api_key_name: 'fixture-key',
      account_id: 8,
      account_name: 'fixture-account',
      group_id: 9,
      group_name: 'fixture-group',
      request_type: 1,
      stream: false,
    }
    const detail = {
      ...summary,
      error_body: '{"error":"bad request"}',
      request_details: {
        method: 'POST',
        path: '/v1/chat/completions',
        headers: { 'content-type': ['application/json'] },
        body: { model: 'gpt-test', messages: [{ role: 'user', content: 'hello' }] },
      },
      is_business_limited: false,
    }
    listErrorLogs.mockResolvedValueOnce({ items: [summary], total: 1, pages: 1 })
    getErrorLogDetail.mockResolvedValueOnce(detail)

    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    const vm = wrapper.vm as any
    vm.activeTab = 'errors'
    vm.filters.model = 'gpt-test'
    vm.filters.account_id = 8
    vm.filters.group_id = 9
    vm.filters.error_phase = 'request'
    vm.filters.error_category = 'invalid_request'
    vm.filters.status_code = 400

    await vm.exportToExcel()
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledWith(expect.objectContaining({
      page: 1,
      page_size: 100,
      view: 'all',
      model: 'gpt-test',
      account_id: 8,
      group_id: 9,
      phase: 'request',
      category: 'invalid_request',
      status_codes: '400',
    }), expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(getErrorLogDetail).toHaveBeenCalledWith(42, expect.objectContaining({ signal: expect.any(AbortSignal) }))

    const headers = aoaToSheet.mock.calls[0][0][0] as string[]
    const requestDetailsIndex = headers.indexOf('admin.ops.errorExport.requestDetails')
    expect(requestDetailsIndex).toBeGreaterThan(-1)
    expect(headers).toContain('admin.ops.errorExport.errorBody')
    const row = sheetAddAoa.mock.calls[0][1][0] as unknown[]
    expect(row[requestDetailsIndex]).toBe(JSON.stringify(detail.request_details))
    expect(row[headers.indexOf('admin.ops.errorExport.errorBody')]).toBe(detail.error_body)
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^error_logs_.*\.xlsx$/))
  })

  it('walks all filtered pages when exporting more than one hundred errors', async () => {
    const firstPage = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1,
      created_at: '2026-08-20T01:02:03Z',
      phase: 'request',
      type: 'invalid_request',
      error_owner: 'client',
      error_source: 'client_request',
      severity: 'P2',
      status_code: 400,
      platform: 'openai',
      model: 'gpt-test',
      resolved: false,
      client_request_id: `client-${index + 1}`,
      request_id: `request-${index + 1}`,
      message: 'invalid request',
      request_details: { body: { index: index + 1 } },
    }))
    const secondPage = [{
      ...firstPage[0],
      id: 101,
      client_request_id: 'client-101',
      request_id: 'request-101',
      request_details: { body: { index: 101 } },
    }]
    listErrorLogs
      .mockResolvedValueOnce({ items: firstPage, total: 101, pages: 2 })
      .mockResolvedValueOnce({ items: secondPage, total: 101, pages: 2 })

    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()
    ;(wrapper.vm as any).activeTab = 'errors'

    await (wrapper.vm as any).exportToExcel()
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledTimes(2)
    expect(listErrorLogs.mock.calls[0][0]).toEqual(expect.objectContaining({ page: 1, page_size: 100 }))
    expect(listErrorLogs.mock.calls[1][0]).toEqual(expect.objectContaining({ page: 2, page_size: 100 }))
    expect(listErrorLogs.mock.calls[0][1]).toEqual(expect.objectContaining({ signal: expect.any(AbortSignal) }))
    // Embedded snapshots are used directly, so no N detail requests are needed.
    expect(getErrorLogDetail).not.toHaveBeenCalled()
    expect(sheetAddAoa).toHaveBeenCalledTimes(2)
    expect((sheetAddAoa.mock.calls[1][1] as unknown[]).length).toBe(1)
    expect(saveAs).toHaveBeenCalledTimes(1)
  })

  it('honors reported pages when the server caps page size and omits total', async () => {
    const makePage = (offset: number) => Array.from({ length: 50 }, (_, index) => ({
      id: offset + index + 1,
      created_at: '2026-08-20T01:02:03Z',
      phase: 'request',
      type: 'invalid_request',
      error_owner: 'client',
      error_source: 'client_request',
      severity: 'P2',
      status_code: 400,
      platform: 'openai',
      model: 'gpt-test',
      resolved: false,
      client_request_id: `client-${offset + index + 1}`,
      request_id: `request-${offset + index + 1}`,
      message: 'invalid request',
      // Embedded snapshots avoid 100 detail lookups while exercising only
      // the pagination termination behavior under test.
      request_details: { body: { index: offset + index + 1 } },
    }))

    // The gateway enforces a 50-row maximum despite the requested page_size
    // of 100.  It omits total but reports the effective page count.
    listErrorLogs
      .mockResolvedValueOnce({ items: makePage(0), pages: 2 })
      .mockResolvedValueOnce({ items: makePage(50), pages: 2 })

    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()
    ;(wrapper.vm as any).activeTab = 'errors'

    await (wrapper.vm as any).exportToExcel()
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledTimes(2)
    expect(listErrorLogs.mock.calls[0][0]).toEqual(expect.objectContaining({ page: 1, page_size: 100 }))
    expect(listErrorLogs.mock.calls[1][0]).toEqual(expect.objectContaining({ page: 2, page_size: 100 }))
    expect(saveAs).toHaveBeenCalledTimes(1)
  })

  it('uses response page_size when pagination metadata is absent', async () => {
    const makeRows = (offset: number, count: number) => Array.from({ length: count }, (_, index) => ({
      id: offset + index + 1,
      created_at: '2026-08-20T01:02:03Z',
      phase: 'request',
      type: 'invalid_request',
      error_owner: 'client',
      error_source: 'client_request',
      severity: 'P2',
      status_code: 400,
      platform: 'openai',
      model: 'gpt-test',
      resolved: false,
      client_request_id: `client-${offset + index + 1}`,
      request_id: `request-${offset + index + 1}`,
      message: 'invalid request',
      request_details: { body: { index: offset + index + 1 } },
    }))

    // No total/pages are returned. The effective server page size is 50,
    // so the first 50-row page must be followed by a second request.
    listErrorLogs
      .mockResolvedValueOnce({ items: makeRows(0, 50), page_size: 50 })
      .mockResolvedValueOnce({ items: makeRows(50, 1), page_size: 50 })

    const wrapper = mountRouteFilteredUsageView()
    vi.advanceTimersByTime(120)
    await flushPromises()
    ;(wrapper.vm as any).activeTab = 'errors'

    await (wrapper.vm as any).exportToExcel()
    await flushPromises()

    expect(listErrorLogs).toHaveBeenCalledTimes(2)
    expect(listErrorLogs.mock.calls[1][0]).toEqual(expect.objectContaining({ page: 2, page_size: 100 }))
    expect(saveAs).toHaveBeenCalledTimes(1)
  })
})

describe('admin UsageView removed user surface', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    list.mockReset()
    getStats.mockReset()
    getSnapshotV2.mockReset()
    getModelStats.mockReset()

    list.mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockResolvedValue({
      total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
      total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
    })
    getSnapshotV2.mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockResolvedValue({ models: [] })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('has no user column, user filter, or ranking tab', async () => {
    const wrapper = mount(UsageView, {
      global: { stubs: {
        AppLayout: AppLayoutStub, UsageStatsCards: true, UsageFilters: UsageFiltersStub,
        UsageTable: true, UsageExportProgress: true, UsageCleanupDialog: true,
        Pagination: true, Select: true,
        DateRangePicker: true, Icon: true, TokenUsageTrend: true,
        ModelDistributionChart: true, GroupDistributionChart: true, EndpointDistributionChart: true,
        OpsErrorLogTable: true, OpsErrorDetailModal: true,
      } },
    })
    vi.advanceTimersByTime(120)
    await flushPromises()

    const tabs = wrapper.findAll('[data-testid="usage-detail-tab"]')
    expect(tabs).toHaveLength(2)
    expect((wrapper.vm as any).filters).not.toHaveProperty('user_id')
    expect((wrapper.vm as any).allColumns).not.toEqual(
      expect.arrayContaining([expect.objectContaining({ key: 'user' })]),
    )
    expect(list.mock.calls[0]?.[0]).not.toHaveProperty('user_id')
  })
})

describe('admin UsageView model audit export', () => {
	beforeEach(() => {
		vi.useFakeTimers()
		list.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
		exportList.mockReset().mockResolvedValue({
			items: [{
				id: 1,
				created_at: '2026-08-04T00:00:00Z',
				model: 'gpt-5.6-sol',
				upstream_model: 'gpt-5.5',
				upstream_response_model: 'gpt-5.4',
				upstream_model_mismatch: true,
				request_type: 'sync',
				input_tokens: 1,
				output_tokens: 1,
				cache_read_tokens: 0,
				cache_creation_tokens: 0,
				duration_ms: 10,
			}],
			total: 1,
			pages: 1,
		})
		getStats.mockReset().mockResolvedValue({
			total_requests: 0, total_input_tokens: 0, total_output_tokens: 0,
			total_cache_tokens: 0, total_tokens: 0, total_cost: 0, total_actual_cost: 0, average_duration_ms: 0,
		})
		getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
		getModelStats.mockReset().mockResolvedValue({ models: [] })
		aoaToSheet.mockClear()
		sheetAddAoa.mockClear()
		saveAs.mockClear()
		xlsxWrite.mockClear()
	})

	afterEach(() => {
		vi.useRealTimers()
	})

	it('exports requested, sent, response, and mismatch as separate admin columns', async () => {
		const wrapper = mountRouteFilteredUsageView()
		vi.advanceTimersByTime(120)
		await flushPromises()

		await (wrapper.vm as any).exportToExcel()
		await flushPromises()

		const headers = aoaToSheet.mock.calls[0][0][0]
		expect(headers.slice(3, 7)).toEqual([
			'Requested model',
			'Sent upstream model',
			'Upstream response model',
			'Upstream model mismatch',
		])
		expect(headers).toContain('Actual cost')
		expect(headers).not.toContain('User billed')
		const row = sheetAddAoa.mock.calls[0][1][0]
		expect(row.slice(3, 7)).toEqual(['gpt-5.6-sol', 'gpt-5.5', 'gpt-5.4', 'Yes'])
		expect(saveAs).toHaveBeenCalledTimes(1)
	})
})
