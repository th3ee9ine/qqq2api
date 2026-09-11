import { defineComponent } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Proxy, ProxyAccountSummary } from '@/types'

const { listAccounts, listProxies, bulkUpdate } = vi.hoisted(() => ({
  listAccounts: vi.fn(), listProxies: vi.fn(), bulkUpdate: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { proxies: { getProxyAccounts: listAccounts, getAllWithCount: listProxies }, accounts: { bulkUpdate } },
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${Object.values(params).join(':')}` : key}),
}))

import ProxyAccountsModal from '../ProxyAccountsModal.vue'

const proxy = (id: number, extra: Partial<Proxy> = {}): Proxy => ({
  id, name: `Proxy ${id}`, protocol: 'http', host: `192.0.2.${id}`, port: 8080, username: null,
  status: 'active', max_accounts: 2, account_count: 1, expires_at: null, fallback_mode: 'none',
  expiry_warn_days: 7, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...extra,
})
const source = proxy(7, {account_count: 3})
const accounts: ProxyAccountSummary[] = [
  {id: 11, name: 'Primary', platform: 'openai', type: 'oauth'},
  {id: 12, name: 'Secondary', platform: 'anthropic', type: 'oauth'},
  {id: 13, name: 'Shadow', platform: 'openai', type: 'oauth', parent_account_id: 11},
]
const BaseDialogStub = defineComponent({
  props: {show:Boolean}, emits:['close'],
  template: '<section v-if="show"><button data-testid="dialog-close" @click="$emit(\'close\')">Close</button><slot/><slot name="footer"/></section>',
})
const SelectStub = defineComponent({
  props:['modelValue','options','disabled'], emits:['update:modelValue'],
  template: '<select :value="modelValue" :disabled="disabled" @change="$emit(\'update:modelValue\', Number($event.target.value))"><option value="">Select</option><option v-for="option in options" :key="option.value" :value="option.value" :disabled="option.disabled">{{ option.label }}</option></select>',
})
let wrappers: VueWrapper[] = []
function render() {
  const wrapper = mount(ProxyAccountsModal, {
    props: {show: true, proxy: source},
    global: {stubs: {BaseDialog: BaseDialogStub, Select: SelectStub, Icon: true, PlatformTypeBadge: true}},
  })
  wrappers.push(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  listAccounts.mockResolvedValue(accounts.map(a => ({...a})))
  listProxies.mockResolvedValue([source, proxy(8), proxy(9, {status: 'inactive'}), proxy(10, {expires_at:'2000-01-01T00:00:00Z'})])
  bulkUpdate.mockResolvedValue({success:1,failed:0,success_ids:[11],failed_ids:[],results:[{account_id:11,success:true}]})
})
afterEach(() => { wrappers.forEach(w => w.unmount()); wrappers = [] })

describe('ProxyAccountsModal binding actions', () => {
  it('loads source accounts and prevents independent shadow changes', async () => {
    const wrapper = render()
    await flushPromises()
    expect(listAccounts).toHaveBeenCalledWith(7)
    expect(wrapper.get('[data-testid="select-account-13"]').attributes('disabled')).toBeDefined()
    const unlinkShadow = wrapper.find('[data-testid="unlink-account-13"]')
    expect(!unlinkShadow.exists() || unlinkShadow.attributes('disabled') !== undefined).toBe(true)
    expect(bulkUpdate).not.toHaveBeenCalled()
  })

  it('requires confirmation then unbinds only the chosen account with a source precondition', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="unlink-account-11"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).toHaveBeenCalledTimes(1)
    expect(bulkUpdate).toHaveBeenCalledWith([11], {proxy_id:0,expected_proxy_id:7})
    expect(listAccounts.mock.calls.length).toBeGreaterThanOrEqual(3)
    expect(wrapper.emitted('updated')).toBeTruthy()
  })

  it('rebinds selected parents and excludes inactive, expired and source proxies', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="select-account-11"]').setValue(true)
    await wrapper.get('[data-testid="select-account-12"]').setValue(true)
    await wrapper.get('[data-testid="bulk-rebind"]').trigger('click')
    await flushPromises()
    const values = wrapper.findAll('select option').map(option => option.attributes('value')).filter(Boolean)
    expect(values).toEqual(['8'])
    await wrapper.get('select').setValue('8')
    expect(wrapper.find('[data-testid="capacity-warning"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="confirm-binding"]').attributes('disabled')).toBeUndefined()
    bulkUpdate.mockResolvedValue({success:2,failed:0,success_ids:[11,12],failed_ids:[],results:[{account_id:11,success:true},{account_id:12,success:true}]})
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).toHaveBeenCalledTimes(1)
    expect(bulkUpdate).toHaveBeenCalledWith([11,12], {proxy_id:8,expected_proxy_id:7})
    expect(wrapper.emitted('updated')).toBeTruthy()
  })

  it('does not overwrite an account removed from the source after the dialog opened', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="unlink-account-11"]').trigger('click')
    await flushPromises()
    listAccounts.mockResolvedValue([accounts[1]])
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="binding-error"]').text()).toContain('admin.proxies.bindings.sourceChanged')
  })

  it('rechecks target availability immediately before a rebind', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="rebind-account-11"]').trigger('click')
    await flushPromises()
    await wrapper.get('select').setValue('8')
    listProxies.mockResolvedValue([source, proxy(8, {status:'inactive'})])
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="binding-error"]').text()).toContain('admin.proxies.bindings.targetUnavailable')
  })

  it('refreshes source and parent totals after a failed mutation without reporting success', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="unlink-account-11"]').trigger('click')
    await flushPromises()
    bulkUpdate.mockRejectedValue({code:'PROXY_BINDING_CHANGED',message:'Changed'})
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="binding-outcome"]').text()).toContain('admin.proxies.bindings.unknownResult')
    expect(wrapper.get('[data-testid="binding-outcome"]').text()).toContain('admin.proxies.bindings.sourceChanged')
    expect(wrapper.emitted('updated')).toBeTruthy()
    expect(listAccounts.mock.calls.length).toBeGreaterThanOrEqual(3)
  })

  it('allows unbinding when loading destination proxies fails', async () => {
    listProxies.mockRejectedValue(new Error('Targets unavailable'))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="unlink-account-11"]').trigger('click')
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(bulkUpdate).toHaveBeenCalledWith([11], {proxy_id:0,expected_proxy_id:7})
    expect(wrapper.get('[data-testid="binding-outcome"]').text()).toContain('admin.proxies.bindings.unlinkSuccess')
  })

  it('prevents duplicate submissions and closing while a write is pending', async () => {
    let finish!: (value: unknown) => void
    bulkUpdate.mockImplementation(() => new Promise(resolve => {finish = resolve}))
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="unlink-account-11"]').trigger('click')
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="confirm-binding"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="binding-back"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await wrapper.get('[data-testid="dialog-close"]').trigger('click')
    expect(bulkUpdate).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    finish({success:1,failed:0,success_ids:[11],failed_ids:[],results:[]})
    await flushPromises()
    expect(wrapper.emitted('updated')).toBeTruthy()
  })

  it('keeps only failed accounts selected after a partial result', async () => {
    const wrapper = render()
    await flushPromises()
    await wrapper.get('[data-testid="select-all-accounts"]').setValue(true)
    await wrapper.get('[data-testid="bulk-unlink"]').trigger('click')
    bulkUpdate.mockResolvedValue({success:1,failed:1,success_ids:[11],failed_ids:[12],results:[{account_id:11,success:true},{account_id:12,success:false,error:'Try again'}]})
    await wrapper.get('[data-testid="confirm-binding"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="binding-outcome"]').text()).toContain('admin.proxies.bindings.partialResult:1:1')
    expect((wrapper.get('[data-testid="select-account-11"]').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-testid="select-account-12"]').element as HTMLInputElement).checked).toBe(true)
  })

  it('ignores an older response after switching the source proxy', async () => {
    let resolveOld!: (value: ProxyAccountSummary[]) => void
    listAccounts.mockImplementation((id:number) => id === 7 ? new Promise(resolve => {resolveOld = resolve}) : Promise.resolve([{...accounts[1],name:'New source account'}]))
    const wrapper = render()
    await wrapper.setProps({proxy:proxy(8)})
    await flushPromises()
    expect(wrapper.text()).toContain('New source account')
    resolveOld(accounts)
    await flushPromises()
    expect(wrapper.text()).toContain('New source account')
    expect(wrapper.find('[data-testid="proxy-account-11"]').exists()).toBe(false)
  })
})
