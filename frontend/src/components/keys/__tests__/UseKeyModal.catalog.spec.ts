import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import UseKeyModal from '../UseKeyModal.vue'
import type { GroupPlatform } from '@/types'

const { saveAsMock } = vi.hoisted(() => ({ saveAsMock: vi.fn() }))

vi.mock('file-saver', () => ({ saveAs: saveAsMock }))
vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, params?: { count?: number }) => `${key}${params?.count == null ? '' : ` ${params.count}`}` })
}))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn().mockResolvedValue(true) })
}))

enableAutoUnmount(afterEach)
afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

function renderModal(platform: GroupPlatform = 'openai') {
  return mount(UseKeyModal, {
    props: { show: true, platform, apiKey: 'sk-current-key', baseUrl: 'https://example.com/v1/' },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Icon: true
      }
    }
  })
}

function responseFor(models: unknown[]) {
  return { ok: true, json: async () => ({ models }) }
}

describe('UseKeyModal Codex catalog', () => {
  it('fetches the selected key catalog, updates HTTP and WS setup, and downloads its exact contents', async () => {
    const models = [{
      slug: 'custom-model',
      default_reasoning_level: 'high',
      supported_reasoning_levels: [{ effort: 'high' }, { effort: 'max' }]
    }]
    const fetchMock = vi.fn().mockResolvedValue(responseFor(models))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = renderModal()
    const config = () => wrapper.findAll('pre code').map(code => code.text()).join('\n')

    expect(fetchMock).not.toHaveBeenCalled()
    expect(config()).not.toContain('model_catalog_json')
    expect(config()).toContain('model = "gpt-5.5"')
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith(
      'https://example.com/v1/models?client_version=0.147.0',
      expect.objectContaining({ headers: { Accept: 'application/json', Authorization: 'Bearer sk-current-key' } })
    )
    expect(wrapper.get('[data-testid="codex-model-catalog"]').text()).toContain('modelsCount 1')
    expect(config()).toContain('model = "custom-model"')
    expect(config()).toContain('review_model = "custom-model"')
    expect(config()).toContain('model_reasoning_effort = "high"')
    expect(config()).toContain('model_catalog_json = "~/.codex/codex-models.json"')
    expect(config()).toContain('requires_openai_auth = true')

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.findAll('button').find(button => button.text() === 'keys.useKeyModal.cliTabs.codexCliWs')!.trigger('click')
    expect(config()).toContain('supports_websockets = true')
    expect(config()).toContain('requires_openai_auth = false')
    expect(config()).toContain('x-openai-actor-authorization')
    expect(config()).toContain('model = "custom-model"')
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await wrapper.findAll('button').find(button => button.text() === 'Windows')!.trigger('click')
    expect(config()).toContain('model_catalog_json = "%userprofile%\\\\.codex\\\\codex-models.json"')
    await wrapper.get('[data-testid="codex-model-catalog-download"]').trigger('click')
    expect(saveAsMock).toHaveBeenCalledWith(expect.any(Blob), 'codex-models.json')
    const blob = saveAsMock.mock.calls[0]![0] as Blob
    const content = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(String(reader.result))
      reader.onerror = reject
      reader.readAsText(blob)
    })
    expect(JSON.parse(content)).toEqual({ models })
    expect(content).not.toContain('sk-current-key')
  })

  it('shows loading and retry feedback without discarding the original setup on failure', async () => {
    let rejectRequest!: (reason: Error) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise((_, reject) => { rejectRequest = reject }))
      .mockResolvedValue(responseFor([{ slug: 'gpt-5.5', supported_reasoning_levels: [{ effort: 'none' }] }]))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = renderModal()
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    expect(wrapper.get('[data-testid="codex-model-catalog-fetch"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="codex-model-catalog-fetch"]').text()).toBe('common.loading')
    rejectRequest(new Error('Request failed'))
    await flushPromises()
    expect(wrapper.get('[data-testid="codex-model-catalog"]').text()).toContain('errorDescription')
    expect(wrapper.get('[data-testid="codex-model-catalog-fetch"]').text()).toContain('retry')
    expect(wrapper.find('pre code').text()).toContain('model_reasoning_effort = "xhigh"')
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('pre code').text()).not.toContain('model_reasoning_effort')
    expect(wrapper.find('[data-testid="codex-model-catalog-download"]').exists()).toBe(true)
  })

  it('aborts an old key request and ignores its late response after a new key catalog loads', async () => {
    let resolveOld!: (value: ReturnType<typeof responseFor>) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValue(responseFor([{ slug: 'new-key-model' }]))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = renderModal()
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    const oldSignal = fetchMock.mock.calls[0]![1].signal as AbortSignal
    await wrapper.setProps({ apiKey: 'sk-next-key', baseUrl: 'https://next.example.com' })
    expect(oldSignal.aborted).toBe(true)
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()
    resolveOld(responseFor([{ slug: 'stale-key-model' }]))
    await flushPromises()
    expect(wrapper.find('pre code').text()).toContain('model = "new-key-model"')
    expect(wrapper.text()).not.toContain('stale-key-model')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.find('[data-testid="codex-model-catalog-download"]').exists()).toBe(false)
    expect(wrapper.find('pre code').text()).toContain('model = "gpt-5.5"')
  })

  it('supports the retained Grok Codex setup while keeping its preferred model and authentication', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(responseFor([{ slug: 'other-model' }, { slug: 'grok-4.7' }])))
    const wrapper = renderModal('grok')
    expect(wrapper.find('[data-testid="codex-model-catalog"]').exists()).toBe(false)
    await wrapper.findAll('button').find(button => button.text() === 'keys.useKeyModal.cliTabs.codexCli')!.trigger('click')
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    await flushPromises()
    const config = wrapper.findAll('pre code').map(code => code.text()).join('\n')
    expect(config).toContain('model = "grok-4.7"')
    expect(config).toContain('env_key = "XAI_API_KEY"')
    expect(config).toContain('requires_openai_auth = false')
    expect(config).toContain('supports_websockets = false')
    expect(config).toContain('model_catalog_json = "~/.codex/codex-models.json"')
    expect(config).not.toContain('model_reasoning_effort')
  })

  it('aborts on unmount and does not show catalog controls in other client tabs', async () => {
    const fetchMock = vi.fn().mockImplementation(() => new Promise(() => {}))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = renderModal()
    await wrapper.get('[data-testid="codex-model-catalog-fetch"]').trigger('click')
    const signal = fetchMock.mock.calls[0]![1].signal as AbortSignal
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
    expect(renderModal('anthropic').find('[data-testid="codex-model-catalog"]').exists()).toBe(false)
    expect(renderModal('composite').find('[data-testid="codex-model-catalog"]').exists()).toBe(false)
  })
})
