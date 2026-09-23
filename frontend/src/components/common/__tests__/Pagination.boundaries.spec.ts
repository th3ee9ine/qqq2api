import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import Pagination from '../Pagination.vue'
import Select from '../Select.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: { page: number; total: number }) =>
      key === 'pagination.pageOf' ? `${params?.page}/${params?.total}` : key
  })
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  delete window.__APP_CONFIG__
  window.localStorage.clear()
})

function mountPagination(props: Partial<InstanceType<typeof Pagination>['$props']> = {}) {
  return mount(Pagination, {
    props: { total: 200, page: 1, pageSize: 20, ...props },
    global: { stubs: { Icon: true, Teleport: true } }
  })
}

describe('pagination boundaries', () => {
  it('shows one empty page and disables navigation on mobile and desktop', () => {
    const wrapper = mountPagination({ total: 0, showPageSizeSelector: false })

    expect(wrapper.text()).toContain('1/1')
    expect(wrapper.findAll('p .font-medium').map((item) => item.text())).toEqual(['0', '0', '0'])
    expect(wrapper.get('[aria-current="page"]').text()).toBe('1')
    const navigation = wrapper.findAll('button').filter((button) =>
      button.attributes('aria-label') !== 'pagination.goToPage'
    )
    expect(navigation).toHaveLength(4)
    expect(navigation.every((button) => (button.element as HTMLButtonElement).disabled)).toBe(true)
  })

  it('disables next navigation when the current page exceeds a reduced total', () => {
    const wrapper = mountPagination({ page: 10, total: 20 })
    const nextButtons = wrapper.findAll('button').filter((button) =>
      button.attributes('aria-label') === 'pagination.next' || button.text() === 'pagination.next'
    )

    expect(nextButtons).toHaveLength(2)
    expect(nextButtons.every((button) => (button.element as HTMLButtonElement).disabled)).toBe(true)
  })

  it('does not emit a page change for the current page', async () => {
    const wrapper = mountPagination()
    await wrapper.get('[aria-current="page"]').trigger('click')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })
})

describe('pagination page size', () => {
  it('uses explicit options and emits the selected size without global rounding', async () => {
    window.__APP_CONFIG__ = { table_page_size_options: [20, 50, 1000] } as any
    const wrapper = mountPagination({ pageSize: 50, pageSizeOptions: [25, 50, 100] })
    await wrapper.get('.select-trigger').trigger('click')

    const options = wrapper.findAll('[role="option"]')
    expect(options.map((option) => option.text())).toEqual(['25', '50', '100'])
    await options[0].trigger('click')

    expect(wrapper.emitted('update:pageSize')).toEqual([[25]])
    expect(window.localStorage.getItem('table-page-size')).toBe('25')
    expect(wrapper.emitted('update:page')).toBeUndefined()
  })

  it('uses global options when no explicit options are provided', async () => {
    window.__APP_CONFIG__ = { table_page_size_options: [20, 50, 1000] } as any
    const wrapper = mountPagination()
    await wrapper.get('.select-trigger').trigger('click')

    expect(wrapper.findAll('[role="option"]').map((option) => option.text())).toEqual(['20', '50', '1000'])
  })

  it.each([undefined, [20, 50, 1000]])('preserves the current server page size with options %s', async (pageSizeOptions) => {
    window.__APP_CONFIG__ = { table_page_size_options: [20, 50, 1000] } as any
    const wrapper = mountPagination({ pageSize: 100, pageSizeOptions })

    expect(wrapper.get('.select-value').text()).toBe('100')
    await wrapper.get('.select-trigger').trigger('click')
    expect(wrapper.get('[role="option"][aria-selected="true"]').text()).toBe('100')
  })

  it('removes duplicate and invalid explicit options', async () => {
    const wrapper = mountPagination({ pageSizeOptions: [0, -1, NaN, Infinity, 25.5, 50, 25, 25] })
    await wrapper.get('.select-trigger').trigger('click')

    expect(wrapper.findAll('[role="option"]').map((option) => option.text())).toEqual(['20', '25', '50'])
  })

  it('ignores invalid, unavailable, and unchanged selection values', () => {
    const wrapper = mountPagination({ pageSizeOptions: [20, 25, 50] })
    const select = wrapper.getComponent(Select)

    for (const value of [null, true, false, '', '25oops', 0, -1, NaN, Infinity, 25.5, 999, 20, '20']) {
      select.vm.$emit('update:modelValue', value)
    }

    expect(wrapper.emitted('update:pageSize')).toBeUndefined()
    expect(window.localStorage.getItem('table-page-size')).toBeNull()
  })

  it('accepts a numeric string only when it identifies a selectable size', () => {
    const wrapper = mountPagination({ pageSizeOptions: [20, 25, 50] })
    wrapper.getComponent(Select).vm.$emit('update:modelValue', '25')

    expect(wrapper.emitted('update:pageSize')).toEqual([[25]])
    expect(window.localStorage.getItem('table-page-size')).toBe('25')
  })
})
