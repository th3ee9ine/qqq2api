import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import JailbreakGuardView from '../JailbreakGuardView.vue'

const mocks = vi.hoisted(() => ({ getConfig: vi.fn(), updateConfig: vi.fn() }))

vi.mock('../api', () => ({ default: mocks }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const AppLayoutStub = { template: '<div><slot /></div>' }

describe('JailbreakGuardView', () => {
  beforeEach(() => {
    mocks.getConfig.mockReset()
    mocks.updateConfig.mockReset()
  })

  it('loads the server policy metadata and renders the dedicated security-audit page', async () => {
    mocks.getConfig.mockResolvedValue({ local_jailbreak_guard_enabled: true, local_jailbreak_policy_id: 'local-jailbreak-v1' })
    const wrapper = mount(JailbreakGuardView, { global: { stubs: { AppLayout: AppLayoutStub } } })
    await flushPromises()

    expect(mocks.getConfig).toHaveBeenCalledOnce()
    expect(wrapper.get('[data-test="local-jailbreak-guard"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="local-jailbreak-guard-status"]').text()).toBe('admin.promptAudit.localGuard.enabled')
    expect(wrapper.text()).toContain('local-jailbreak-v1')
    expect(wrapper.text()).toContain('admin.promptAudit.localGuard.remoteIndependent')
    expect(wrapper.get('[data-test="local-jailbreak-guard-toggle"]').attributes('aria-checked')).toBe('true')
  })

  it('edits and persists the local guard switch through the complete config payload', async () => {
    const config = { local_jailbreak_guard_enabled: true, local_jailbreak_policy_id: 'local-jailbreak-v1' }
    mocks.getConfig.mockResolvedValue(config)
    mocks.updateConfig.mockResolvedValue({ ...config, local_jailbreak_guard_enabled: false })
    const wrapper = mount(JailbreakGuardView, { global: { stubs: { AppLayout: AppLayoutStub } } })
    await flushPromises()

    await wrapper.get('[data-test="local-jailbreak-guard-toggle"]').trigger('click')
    expect(wrapper.get('[data-test="local-jailbreak-guard-toggle"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-test="save-local-jailbreak-guard"]').attributes('disabled')).toBeUndefined()

    await wrapper.get('[data-test="save-local-jailbreak-guard"]').trigger('click')
    await flushPromises()
    expect(mocks.updateConfig).toHaveBeenCalledWith(expect.objectContaining({ local_jailbreak_guard_enabled: false }))
    expect(wrapper.get('[data-test="local-jailbreak-guard-status"]').text()).toBe('admin.promptAudit.localGuard.disabled')
    expect(wrapper.get('[data-test="local-jailbreak-guard-toggle"]').attributes('aria-checked')).toBe('false')
    expect(wrapper.get('[data-test="save-local-jailbreak-guard"]').attributes('disabled')).toBeDefined()
  })

  it('resets an unsaved switch change without calling the API', async () => {
    mocks.getConfig.mockResolvedValue({ local_jailbreak_guard_enabled: true, local_jailbreak_policy_id: 'local-jailbreak-v1' })
    const wrapper = mount(JailbreakGuardView, { global: { stubs: { AppLayout: AppLayoutStub } } })
    await flushPromises()

    await wrapper.get('[data-test="local-jailbreak-guard-toggle"]').trigger('click')
    await wrapper.get('[data-test="reset-local-jailbreak-guard"]').trigger('click')
    expect(wrapper.get('[data-test="local-jailbreak-guard-toggle"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.get('[data-test="save-local-jailbreak-guard"]').attributes('disabled')).toBeDefined()
    expect(mocks.updateConfig).not.toHaveBeenCalled()
  })

  it('keeps the local guard shown as enabled when optional config metadata is unavailable', async () => {
    mocks.getConfig.mockRejectedValue(new Error('config unavailable'))
    const wrapper = mount(JailbreakGuardView, { global: { stubs: { AppLayout: AppLayoutStub } } })
    await flushPromises()

    expect(wrapper.get('[data-test="local-jailbreak-guard-status"]').text()).toBe('admin.promptAudit.localGuard.enabled')
    expect(wrapper.text()).toContain('local-jailbreak-v1')
    expect(wrapper.find('[data-test="local-jailbreak-guard-toggle"]').exists()).toBe(false)
  })
})
