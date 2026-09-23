import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UsageView from '../UsageView.vue'
import Pagination from '@/components/common/Pagination.vue'

const { list } = vi.hoisted(() => ({ list: vi.fn() }))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { list, getStats: vi.fn().mockResolvedValue({ total_requests: 42 }) },
    dashboard: {
      getSnapshotV2: vi.fn().mockResolvedValue({ trend: [], groups: [] }),
      getModelStats: vi.fn().mockResolvedValue({ models: [] }),
    },
  },
}))
vi.mock('@/api/admin/ops', () => ({ listErrorLogs: vi.fn(), getErrorLogDetail: vi.fn() }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params?.page ? `${key} ${params.page}` : key,
  }),
}))

enableAutoUnmount(afterEach)

function response(page: number, total = 42, pageSize = 20) {
  const offset = (page - 1) * pageSize
  return {
    items: Array.from({ length: Math.max(0, Math.min(pageSize, total - offset)) }, (_, i) => ({ id: offset + i + 1 })),
    page,
    page_size: pageSize,
    total,
    pages: Math.ceil(total / pageSize),
  }
}

async function mountUsageView() {
  const wrapper = mount(UsageView, {
    global: { stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      UsageStatsCards: true, UsageFilters: true,
      UsageTable: { name: 'UsageTable', props: ['data'], template: '<div />' },
      UsageExportProgress: true, UsageCleanupDialog: true,
      OpsErrorLogTable: true, OpsErrorDetailModal: true,
      Select: true, DateRangePicker: true, Icon: true,
      TokenUsageTrend: true, ModelDistributionChart: true,
      GroupDistributionChart: true, EndpointDistributionChart: true,
    } },
  })
  await flushPromises()
  return wrapper
}

describe('admin usage pagination', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    localStorage.clear()
    delete window.__APP_CONFIG__
    list.mockReset().mockImplementation(async (params) => response(params.page, 42, params.page_size))
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
    localStorage.clear()
    delete window.__APP_CONFIG__
  })

  it('requests exact totals so every page is reachable and the total stays stable', async () => {
    list.mockImplementation(async (params) => {
      const result = response(params.page)
      return params.exact_total ? result : { ...result, total: params.page * 20 + 1 }
    })
    const wrapper = await mountUsageView()
    const pager = wrapper.getComponent(Pagination)

    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ exact_total: true }), expect.anything())
    expect(pager.props('total')).toBe(42)
    await pager.get('[aria-label="pagination.goToPage 3"]').trigger('click')
    await flushPromises()

    expect(pager.props('page')).toBe(3)
    expect(pager.props('total')).toBe(42)
    expect(wrapper.getComponent({ name: 'UsageTable' }).props('data')).toHaveLength(2)
    expect(pager.get('nav [aria-label="pagination.next"]').attributes('disabled')).toBeDefined()
  })

  it('returns to the first page when the page size changes', async () => {
    const wrapper = await mountUsageView()
    const pager = wrapper.getComponent(Pagination)
    await pager.get('[aria-label="pagination.goToPage 2"]').trigger('click')
    await flushPromises()
    pager.vm.$emit('update:pageSize', 50)
    await flushPromises()

    expect(list).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: 1, page_size: 50, exact_total: true }), expect.anything(),
    )
    expect(pager.props('page')).toBe(1)
    expect(pager.props('pageSize')).toBe(50)
  })

  it('uses the server page size for the displayed range and subsequent requests', async () => {
    localStorage.setItem('table-page-size', '100')
    list.mockImplementation(async (params) => response(params.page, 120, 50))
    const wrapper = await mountUsageView()
    const pager = wrapper.getComponent(Pagination)

    expect(pager.props('pageSize')).toBe(50)
    await pager.get('[aria-label="pagination.goToPage 2"]').trigger('click')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 2, page_size: 50 }), expect.anything())
  })

  it('reloads the last valid page when records were removed while viewing a later page', async () => {
    const wrapper = await mountUsageView()
    const pager = wrapper.getComponent(Pagination)
    await pager.get('[aria-label="pagination.goToPage 3"]').trigger('click')
    await flushPromises()
    list.mockClear().mockImplementation(async (params) => response(params.page, 21))

    wrapper.vm.refreshData()
    await flushPromises()

    expect(list.mock.calls.map(([params]) => params.page)).toEqual([3, 2])
    expect(pager.props('page')).toBe(2)
    expect(pager.props('total')).toBe(21)
    expect(wrapper.getComponent({ name: 'UsageTable' }).props('data')).toEqual([{ id: 21 }])
  })

  it('resets to page one without another request when all records were removed', async () => {
    const wrapper = await mountUsageView()
    await wrapper.getComponent(Pagination).get('[aria-label="pagination.goToPage 3"]').trigger('click')
    await flushPromises()
    list.mockClear().mockResolvedValue(response(3, 0))
    wrapper.vm.refreshData()
    await flushPromises()

    expect(list).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(Pagination).exists()).toBe(false)
    expect(wrapper.getComponent({ name: 'UsageTable' }).props('data')).toEqual([])

    list.mockClear().mockImplementation(async (params) => response(params.page))
    wrapper.vm.refreshData()
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ page: 1 }), expect.anything())
  })

  it('does not let an earlier response overwrite the latest page', async () => {
    const wrapper = await mountUsageView()
    const pager = wrapper.getComponent(Pagination)
    let resolveEarlier!: (value: ReturnType<typeof response>) => void
    list.mockImplementationOnce(() => new Promise((resolve) => { resolveEarlier = resolve }))
    await pager.get('[aria-label="pagination.goToPage 2"]').trigger('click')
    const earlierSignal = list.mock.calls.at(-1)?.[1].signal as AbortSignal
    await pager.get('[aria-label="pagination.goToPage 3"]').trigger('click')
    await flushPromises()
    resolveEarlier(response(2, 100))
    await flushPromises()

    expect(earlierSignal.aborted).toBe(true)
    expect(pager.props('page')).toBe(3)
    expect(pager.props('total')).toBe(42)
    expect(wrapper.getComponent({ name: 'UsageTable' }).props('data')).toEqual([{ id: 41 }, { id: 42 }])
  })
})
