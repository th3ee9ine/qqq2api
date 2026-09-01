import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import UsageView from '../UsageView.vue'

/**
 * Regression coverage for the normal (non-ops) usage export.  The old
 * implementation appended every row to a SheetJS worksheet and only created
 * the download Blob after the complete result set had been materialised.  A
 * few tens of thousands of rows could therefore exhaust the renderer heap.
 * Large exports are expected to use the bounded CSV writer instead.
 */
const {
  usageList,
  exportList,
  getStats,
  getSnapshotV2,
  getModelStats,
  aoaToSheet,
  sheetAddAoa,
  saveAs,
  xlsxWrite,
  bookNew,
  bookAppendSheet,
  routeQuery,
} = vi.hoisted(() => {
  const usageList = vi.fn()
  const exportList = vi.fn()
  const getStats = vi.fn()
  const getSnapshotV2 = vi.fn()
  const getModelStats = vi.fn()
  const aoaToSheet = vi.fn(() => ({}))
  const sheetAddAoa = vi.fn()
  const saveAs = vi.fn()
  const xlsxWrite = vi.fn(() => new Uint8Array([1, 2, 3]))
  const bookNew = vi.fn(() => ({}))
  const bookAppendSheet = vi.fn()
  const routeQuery: Record<string, string> = {}

  return {
    usageList,
    exportList,
    getStats,
    getSnapshotV2,
    getModelStats,
    aoaToSheet,
    sheetAddAoa,
    saveAs,
    xlsxWrite,
    bookNew,
    bookAppendSheet,
    routeQuery,
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { list: usageList, getStats },
    dashboard: { getSnapshotV2, getModelStats },
  },
}))

vi.mock('@/api/admin/usage', () => ({
  adminUsageAPI: { list: exportList },
}))

vi.mock('@/api/admin/ops', () => ({
  listErrorLogs: vi.fn().mockResolvedValue({ items: [], total: 0, pages: 0 }),
  getErrorLogDetail: vi.fn(),
}))

vi.mock('file-saver', () => ({ saveAs }))

vi.mock('xlsx', () => ({
  utils: {
    aoa_to_sheet: aoaToSheet,
    sheet_add_aoa: sheetAddAoa,
    book_new: bookNew,
    book_append_sheet: bookAppendSheet,
  },
  write: xlsxWrite,
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
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const baseStats = {
  total_requests: 0,
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_tokens: 0,
  total_tokens: 0,
  total_cost: 0,
  total_actual_cost: 0,
  average_duration_ms: 0,
}

const stubs = {
  AppLayout: { template: '<div><slot /></div>' },
  UsageStatsCards: true,
  UsageFilters: { template: '<div><slot name="after-reset" /></div>' },
  UsageTable: true,
  UsageExportProgress: true,
  UsageCleanupDialog: true,
  OpsErrorLogTable: true,
  OpsErrorDetailModal: true,
  Pagination: true,
  Select: true,
  DateRangePicker: true,
  Icon: true,
  TokenUsageTrend: true,
  ModelDistributionChart: true,
  GroupDistributionChart: true,
  EndpointDistributionChart: true,
}

function mountView() {
  return mount(UsageView, { global: { stubs } })
}

function usageRow(id: number) {
  return {
    id,
    created_at: `2026-08-01T00:00:${String(id % 60).padStart(2, '0')}Z`,
    model: 'gpt-test',
    upstream_model: 'gpt-test',
    input_tokens: id,
    output_tokens: id + 1,
    cache_read_tokens: 0,
    cache_creation_tokens: 0,
    duration_ms: 10,
    total_cost: 0,
    actual_cost: 0,
  }
}

function readBlob(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result ?? ''))
    reader.onerror = () => reject(reader.error)
    reader.readAsText(blob)
  })
}

describe('admin UsageView large usage export', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.keys(routeQuery).forEach((key) => delete routeQuery[key])
    usageList.mockReset().mockResolvedValue({ items: [], total: 0, pages: 0 })
    getStats.mockReset().mockResolvedValue(baseStats)
    getSnapshotV2.mockReset().mockResolvedValue({ trend: [], models: [], groups: [] })
    getModelStats.mockReset().mockResolvedValue({ models: [] })
    exportList.mockReset()
    aoaToSheet.mockClear()
    sheetAddAoa.mockClear()
    saveAs.mockClear()
    xlsxWrite.mockClear()
    bookNew.mockClear()
    bookAppendSheet.mockClear()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('uses the low-memory CSV path and fetches every page for a 10k-row result', async () => {
    const total = 10_000
    const pageSize = 1_000
    exportList.mockImplementation(async (params: { page?: number; page_size?: number }) => {
      const requestedPageSize = params.page_size || pageSize
      const page = params.page || 1
      const start = (page - 1) * requestedPageSize
      const count = start >= total ? 0 : Math.min(requestedPageSize, total - start)
      return {
        items: Array.from({ length: count }, (_, index) => usageRow(start + index + 1)),
        total,
        pages: Math.ceil(total / requestedPageSize),
        page_size: requestedPageSize,
      }
    })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await (wrapper.vm as { exportToExcel: () => Promise<void> }).exportToExcel()
    await flushPromises()

    expect(exportList).toHaveBeenCalled()
    expect(exportList.mock.calls[0][0]).toEqual(expect.objectContaining({
      page: 1,
      page_size: pageSize,
      exact_total: true,
    }))
    expect(exportList).toHaveBeenCalledTimes(Math.ceil(total / pageSize))
    expect(xlsxWrite).not.toHaveBeenCalled()
    expect(saveAs).toHaveBeenCalledTimes(1)
    expect(saveAs.mock.calls[0][1]).toMatch(/^usage_.*\.csv$/)

    const blob = saveAs.mock.calls[0][0] as Blob
    expect(blob.type).toBe('text/csv;charset=utf-8')
    // The page-call count verifies that the exporter traversed every page;
    // the non-trivial Blob size verifies that all emitted CSV chunks reached
    // the final download object.
    expect(blob.size).toBeGreaterThan(total)

    vi.useRealTimers()
    const csv = await readBlob(blob)
    expect(csv).toContain('2026-08-01T00:00:01Z')
    expect(csv).toContain('2026-08-01T00:00:40Z')
  })

  it('uses the count-free sentinel path when the table already reports a large result', async () => {
    // The regular table uses fast pagination too, so a non-final page reports
    // offset + page_size + 1 rather than an exact total.  The exporter should
    // recognize that hint and avoid a second COUNT(*) request.
    usageList.mockResolvedValue({ items: [usageRow(1)], total: 21, pages: 2, page_size: 20 })
    const total = 10_000
    const pageSize = 1_000
    exportList.mockImplementation(async (params: { page?: number; page_size?: number }) => {
      const page = params.page || 1
      const start = (page - 1) * pageSize
      const count = start >= total ? 0 : Math.min(pageSize, total - start)
      const hasMore = start + count < total
      return {
        items: Array.from({ length: count }, (_, index) => usageRow(start + index + 1)),
        total: hasMore ? start + pageSize + 1 : total,
        pages: hasMore ? page + 1 : page,
        page_size: pageSize,
      }
    })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await (wrapper.vm as { exportToExcel: () => Promise<void> }).exportToExcel()
    await flushPromises()

    expect(exportList.mock.calls[0][0]).toEqual(expect.objectContaining({
      exact_total: false,
      skip_count: true,
      page_size: pageSize,
    }))
    expect(exportList).toHaveBeenCalledTimes(10)
    expect(xlsxWrite).not.toHaveBeenCalled()
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^usage_.*\.csv$/))
  })

  it('marks subsequent filtered pages as count-free while retaining the filter snapshot', async () => {
    exportList.mockImplementation(async (params: { page?: number }) => {
      if ((params.page || 1) === 1) {
        return { items: [usageRow(1)], total: 6_000, pages: 6, page_size: 1_000 }
      }
      return { items: [], total: 6_000, pages: 6, page_size: 1_000 }
    })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()
    ;(wrapper.vm as any).filters.api_key_id = 42

    await (wrapper.vm as any).exportToExcel()
    await flushPromises()

    expect(exportList).toHaveBeenCalledTimes(2)
    expect(exportList.mock.calls[0][0]).toEqual(expect.objectContaining({
      page: 1,
      exact_total: true,
      api_key_id: 42,
    }))
    expect(exportList.mock.calls[0][0]).not.toHaveProperty('skip_count')
    expect(exportList.mock.calls[1][0]).toEqual(expect.objectContaining({
      page: 2,
      exact_total: false,
      skip_count: true,
      api_key_id: 42,
    }))
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^usage_.*\.csv$/))
  })

  it('retries a failed exact-count probe through the bounded fast path', async () => {
    exportList
      .mockRejectedValueOnce(new Error('count timeout'))
      .mockResolvedValueOnce({ items: [usageRow(1)], total: 6_000, pages: 6, page_size: 1_000 })
      .mockResolvedValueOnce({ items: [], total: 6_000, pages: 6, page_size: 1_000 })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await (wrapper.vm as any).exportToExcel()
    await flushPromises()

    expect(exportList).toHaveBeenCalledTimes(3)
    expect(exportList.mock.calls[0][0]).toEqual(expect.objectContaining({ exact_total: true }))
    expect(exportList.mock.calls[1][0]).toEqual(expect.objectContaining({
      page: 1,
      exact_total: false,
      skip_count: true,
    }))
    expect(saveAs).toHaveBeenCalledTimes(1)
  })

  it('uses reported pages when a compatible gateway omits the total', async () => {
    exportList
      .mockResolvedValueOnce({ items: [usageRow(1)], pages: 2, page_size: 10 })
      .mockResolvedValueOnce({ items: [usageRow(2)], pages: 2, page_size: 10 })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await (wrapper.vm as { exportToExcel: () => Promise<void> }).exportToExcel()
    await flushPromises()

    expect(exportList).toHaveBeenCalledTimes(2)
    expect(exportList.mock.calls.map((call) => call[0].page)).toEqual([1, 2])
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^usage_.*\.xlsx$/))
  })

  it('keeps paging when a later response repeats a stale earlier-page total', async () => {
    // A compatible gateway caps the effective page size at two. Page two
    // repeats a stale earlier-page total (3), even though four rows have
    // already been emitted. Treating that stale value as an exact total would
    // truncate the final row on page three.
    exportList
      .mockResolvedValueOnce({
        items: [usageRow(1), usageRow(2)],
        total: 5,
        pages: 3,
        page_size: 2,
      })
      .mockResolvedValueOnce({
        items: [usageRow(3), usageRow(4)],
        total: 3,
        pages: 2,
        page_size: 2,
      })
      .mockResolvedValueOnce({
        items: [usageRow(5)],
        total: 5,
        pages: 3,
        page_size: 2,
      })

    const wrapper = mountView()
    vi.advanceTimersByTime(120)
    await flushPromises()

    await (wrapper.vm as { exportToExcel: () => Promise<void> }).exportToExcel()
    await flushPromises()

    expect(exportList).toHaveBeenCalledTimes(3)
    expect(exportList.mock.calls.map((call) => call[0].page)).toEqual([1, 2, 3])
    expect(sheetAddAoa).toHaveBeenCalledTimes(3)
    expect(saveAs).toHaveBeenCalledWith(expect.any(Blob), expect.stringMatching(/^usage_.*\.xlsx$/))
  })
})
