import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ProxiesView from '../ProxiesView.vue'

const {
  listProxies,
  getAllWithCount,
  createProxy,
  batchCreateProxies,
  updateProxy,
  showError,
  showSuccess
} = vi.hoisted(() => ({
  listProxies: vi.fn(),
  getAllWithCount: vi.fn(),
  createProxy: vi.fn(),
  batchCreateProxies: vi.fn(),
  updateProxy: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    proxies: {
      list: listProxies,
      getAllWithCount,
      create: createProxy,
      batchCreate: batchCreateProxies,
      update: updateProxy
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
    showInfo: vi.fn()
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key === 'admin.proxies.unlimited' ? 'Unlimited' : key
    })
  }
})

const proxies = [
  {
    id: 1,
    name: 'limited',
    protocol: 'http',
    host: '127.0.0.1',
    port: 8080,
    username: null,
    status: 'active',
    account_count: 2,
    max_accounts: 5,
    expires_at: null,
    fallback_mode: 'none',
    expiry_warn_days: 7,
    created_at: '2026-08-26T00:00:00Z',
    updated_at: '2026-08-26T00:00:00Z'
  },
  {
    id: 2,
    name: 'unlimited',
    protocol: 'socks5',
    host: '127.0.0.2',
    port: 1080,
    username: null,
    status: 'active',
    account_count: 3,
    max_accounts: 0,
    expires_at: null,
    fallback_mode: 'none',
    expiry_warn_days: 7,
    created_at: '2026-08-26T00:00:00Z',
    updated_at: '2026-08-26T00:00:00Z'
  }
] as const

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id" data-testid="proxy-row">
        <slot name="cell-account_count" :row="row" :value="row.account_count" />
        <slot name="cell-actions" :row="row" />
      </div>
      <slot v-if="data.length === 0" name="empty" />
    </div>
  `
}

const BaseDialogStub = {
  props: ['show', 'title'],
  template: `
    <section v-if="show" data-testid="base-dialog">
      <h2>{{ title }}</h2>
      <slot />
      <slot name="footer" />
    </section>
  `
}

const SelectStub = {
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: `
    <select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `
}

const mountView = async () => {
  const wrapper = mount(ProxiesView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: {
          template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>'
        },
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Pagination: true,
        ConfirmDialog: true,
        EmptyState: true,
        ImportDataModal: true,
        ProxyAdBanner: true,
        Icon: true,
        PlatformTypeBadge: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

describe('admin proxy account assignment limits', () => {
  beforeEach(() => {
    listProxies.mockReset()
    getAllWithCount.mockReset()
    createProxy.mockReset()
    batchCreateProxies.mockReset()
    updateProxy.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    listProxies.mockResolvedValue({
      items: proxies,
      total: proxies.length,
      page: 1,
      page_size: 20,
      pages: 1
    })
    getAllWithCount.mockResolvedValue(proxies)
    createProxy.mockResolvedValue(proxies[0])
    batchCreateProxies.mockResolvedValue({ created: 1, skipped: 0 })
    updateProxy.mockResolvedValue(proxies[0])
  })

  it('shows assigned accounts as count / limit, including unlimited proxies', async () => {
    const wrapper = await mountView()

    const capacities = wrapper.findAll('[data-testid="proxy-account-capacity"]')
    expect(capacities.map((item) => item.text())).toEqual(['2 / 5', '3 / Unlimited'])
  })

  it('includes max_accounts when creating a proxy', async () => {
    const wrapper = await mountView()
    const createButton = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'admin.proxies.createProxy'
    )
    expect(createButton).toBeTruthy()
    await createButton!.trigger('click')

    const form = wrapper.get('#create-proxy-form')
    const textInputs = form.findAll('input[type="text"]')
    await textInputs[0]!.setValue('new proxy')
    await textInputs[1]!.setValue('proxy.example.com')
    await wrapper.get('#create-proxy-max-accounts').setValue(8)
    await form.trigger('submit.prevent')
    await flushPromises()

    expect(createProxy).toHaveBeenCalledWith(expect.objectContaining({
      name: 'new proxy',
      host: 'proxy.example.com',
      max_accounts: 8
    }))
  })

  it('applies a shared max_accounts value when creating proxies in a batch', async () => {
    const wrapper = await mountView()
    const createButton = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'admin.proxies.createProxy'
    )
    await createButton!.trigger('click')

    const batchTab = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'admin.proxies.batchAdd'
    )
    await batchTab!.trigger('click')
    await wrapper.get('textarea').setValue('http://proxy.example.com:8080')
    await wrapper.get('textarea').trigger('input')
    await wrapper.get('#batch-proxy-max-accounts').setValue(6)

    const importButton = wrapper.findAll('button').find(
      (button) => button.text().trim() === 'admin.proxies.importProxies'
    )
    expect(importButton).toBeTruthy()
    await importButton!.trigger('click')
    await flushPromises()

    expect(batchCreateProxies).toHaveBeenCalledWith([
      expect.objectContaining({
        host: 'proxy.example.com',
        port: 8080,
        max_accounts: 6
      })
    ])
  })

  it('prefills and updates max_accounts when editing a proxy', async () => {
    const wrapper = await mountView()
    const firstRow = wrapper.findAll('[data-testid="proxy-row"]')[0]!
    const editButton = firstRow.findAll('button').find(
      (button) => button.text().trim() === 'common.edit'
    )
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')

    const input = wrapper.get('#edit-proxy-max-accounts')
    expect((input.element as HTMLInputElement).value).toBe('5')
    await input.setValue(9)
    await wrapper.get('#edit-proxy-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateProxy).toHaveBeenCalledWith(1, expect.objectContaining({
      max_accounts: 9
    }))
  })
})
