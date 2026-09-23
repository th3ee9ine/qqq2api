import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'

const { getAvailableModels, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  const messages: Record<string, string> = {
    'admin.accounts.imagePromptDefault': 'Generate a cute orange cat astronaut sticker on a clean pastel background.'
  }
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.accounts.imageReceived' && params?.count) {
          return `received-${params.count}`
        }
        if (key === 'admin.accounts.audioReceived' && params?.count) {
          return `received-audio-${params.count}`
        }
        if (key === 'admin.accounts.imagePreviewAlt' && params?.index) {
          return `test-image-${params.index}`
        }
        return messages[key] || key
      }
    })
  }
})

function createStreamResponse(lines: string[]) {
  const encoder = new TextEncoder()
  const chunks = lines.map((line) => encoder.encode(line))
  let index = 0

  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function mountModal(account: Record<string, unknown> = {
  id: 42,
  name: 'OpenAI Test',
  platform: 'openai',
  type: 'apikey',
  status: 'active'
}) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    copyToClipboard.mockReset()
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_start","model":"gpt-5.4"}\n',
        'data: {"type":"content","text":"ok"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('OpenAI Compact 探测会携带 compact 测试模式', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect(global.fetch).toHaveBeenCalledTimes(1)
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '',
      mode: 'compact'
    })
  })

  it('defaults to a text model when image and text models are both available', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-image-1', display_name: 'GPT Image 1' },
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    const wrapper = mountModal()

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).selectedModelId).toBe('gpt-5.4')
  })

  it('prefers gpt-5.5 for OAuth account probes when available', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' },
      { id: 'gpt-5.6-sol', display_name: 'GPT-5.6 Sol' },
      { id: 'gpt-5.5', display_name: 'GPT-5.5' }
    ])
    const wrapper = mountModal()

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).selectedModelId).toBe('gpt-5.5')
  })

  it('falls back to the model id when the upstream omits display_name', async () => {
    // OpenAI-compatible /models endpoints commonly return only `id`.
    getAvailableModels.mockResolvedValue([
      { id: 'custom-model', object: 'model' }
    ])
    const wrapper = mountModal()

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).availableModels).toEqual([
      expect.objectContaining({ id: 'custom-model', display_name: 'custom-model' })
    ])
    expect((wrapper.vm as any).selectedModelId).toBe('custom-model')
  })

  it('restores the Claude model-first test flow and omits OpenAI mode from the request', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'claude-haiku-4-5', display_name: 'Claude Haiku 4.5' },
      { id: 'claude-sonnet-4-6', display_name: 'Claude Sonnet 4.6' }
    ])
    const wrapper = mountModal({
      id: 7,
      name: 'Claude OAuth',
      platform: 'anthropic',
      type: 'oauth',
      status: 'active'
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).selectedModelId).toBe('claude-sonnet-4-6')
    expect(wrapper.text()).not.toContain('admin.accounts.openai.testMode')
    expect(wrapper.text()).toContain('admin.accounts.testModel')
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({
      model_id: 'claude-sonnet-4-6',
      prompt: ''
    })
  })

  it('keeps the baseline empty selector state when model loading fails', async () => {
    getAvailableModels.mockRejectedValue(new Error('models unavailable'))
    const wrapper = mountModal()

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.text()).not.toContain('common.error')
    expect((wrapper.vm as any).selectedModelId).toBe('')
  })

  it('does not send a stale image prompt after switching back to a text model', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-image-1', display_name: 'GPT Image 1' },
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-image-1'
    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body).prompt).toBe('')
  })

  it('sends Grok search through its explicit standalone mode without a text model', async () => {
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    ;(wrapper.vm as any).grokTestMode = 'search'
    await flushPromises()
    ;(wrapper.vm as any).testPrompt = 'xAI Grok latest'
    await (wrapper.vm as any).startTest()
    await flushPromises()
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({ model_id: '', mode: 'search', prompt: 'xAI Grok latest' })
  })

  it('switches Grok video tests to an Imagine model', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'grok-4.7', display_name: 'Grok 4.7' },
      { id: 'grok-imagine-video-1.5', display_name: 'Imagine Video 1.5' }
    ])
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    ;(wrapper.vm as any).grokTestMode = 'video'
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()
    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({ model_id: 'grok-imagine-video-1.5', mode: 'video' })
  })

  it('renders Grok TTS audio from data and HTTPS URLs with controls and keeps its request and logs', async () => {
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      'data: {"type":"status","text":"Synthesizing speech"}\n',
      'data: {"type":"content","text":"tts ok"}\n',
      'data: {"type":"audio","audio_url":"data:audio/mpeg;base64,YXVkaW8=","mime_type":"audio/mpeg"}\n',
      'data: {"type":"audio","audio_url":"https://example.com/voice.wav","mime_type":"audio/wav"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    ;(wrapper.vm as any).grokTestMode = 'tts'
    await flushPromises()
    ;(wrapper.vm as any).testPrompt = 'Read this test sentence.'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toEqual({ model_id: '', mode: 'tts', prompt: 'Read this test sentence.' })
    const players = wrapper.findAll('audio')
    expect(players).toHaveLength(2)
    expect(players[0].attributes()).toMatchObject({ controls: '', preload: 'none', 'aria-label': 'received-audio-1' })
    expect(players[0].attributes('autoplay')).toBeUndefined()
    expect(players[0].get('source').attributes()).toMatchObject({ src: 'data:audio/mpeg;base64,YXVkaW8=', type: 'audio/mpeg' })
    expect(players[1].get('source').attributes('src')).toBe('https://example.com/voice.wav')
    expect(wrapper.text()).toContain('Synthesizing speech')
    expect(wrapper.text()).toContain('tts ok')
    expect(wrapper.text()).toContain('received-audio-2')
    expect((wrapper.vm as any).status).toBe('success')
  })

  it('rejects non-audio data URLs and unsafe or malformed audio URLs', async () => {
    const urls: unknown[] = [
      'javascript:alert(1)', 'data:text/html;base64,PGgxPmhpPC9oMT4=',
      'data:image/svg+xml;base64,PHN2Zz4=', 'data:audio/mpeg;base64,invalid!',
      'file:///tmp/audio.mp3', '//example.com/audio.mp3', 42, ''
    ]
    global.fetch = vi.fn().mockResolvedValue(createStreamResponse([
      ...urls.map(audio_url => `data: ${JSON.stringify({ type: 'audio', audio_url })}\n`),
      'data: {"type":"test_complete","success":true}\n'
    ])) as any
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()
    expect(wrapper.find('audio').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('received-audio-')
  })

  it('clears previous audio when retrying, closing, and changing accounts', async () => {
    const audioResponse = () => createStreamResponse([
      'data: {"type":"audio","audio_url":"data:audio/wav;base64,YXVkaW8=","mime_type":"audio/wav"}\n',
      'data: {"type":"test_complete","success":true}\n'
    ])
    global.fetch = vi.fn()
      .mockResolvedValueOnce(audioResponse())
      .mockResolvedValueOnce(createStreamResponse(['data: {"type":"test_complete","success":true}\n']))
      .mockImplementation(async () => audioResponse()) as any
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    expect(wrapper.findAll('audio')).toHaveLength(1)
    await (wrapper.vm as any).startTest()
    expect(wrapper.find('audio').exists()).toBe(false)
    await (wrapper.vm as any).startTest()
    expect(wrapper.findAll('audio')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text() === 'common.close')!.trigger('click')
    expect(wrapper.find('audio').exists()).toBe(false)
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    expect(wrapper.findAll('audio')).toHaveLength(1)
    await wrapper.setProps({ account: { id: 99, name: 'Other Grok', platform: 'grok', type: 'oauth', status: 'active' } as any })
    await flushPromises()
    expect(wrapper.find('audio').exists()).toBe(false)
  })

  it.each(['close', 'unmount'] as const)('aborts streaming on %s and ignores a late audio event', async (action) => {
    let resolveRead!: (chunk: { done: boolean; value?: Uint8Array }) => void
    global.fetch = vi.fn().mockResolvedValue({
      ok: true,
      body: { getReader: () => ({ read: () => new Promise(resolve => { resolveRead = resolve }) }) }
    }) as any
    const wrapper = mountModal({ id: 88, name: 'Grok', platform: 'grok', type: 'oauth', status: 'active' })
    await wrapper.setProps({ show: true })
    await flushPromises()
    const request = (wrapper.vm as any).startTest()
    await flushPromises()
    const signal = (global.fetch as any).mock.calls[0][1].signal as AbortSignal
    if (action === 'close') await wrapper.setProps({ show: false })
    else wrapper.unmount()
    expect(signal.aborted).toBe(true)
    resolveRead({ done: false, value: new TextEncoder().encode('data: {"type":"audio","audio_url":"data:audio/mpeg;base64,YXVkaW8="}\n') })
    await request
    await flushPromises()
    if (action === 'close') expect(wrapper.find('audio').exists()).toBe(false)
  })

  it('closes without loading models for a retired platform account', async () => {
    const wrapper = mountModal({
      id: 88,
      name: 'Retired',
      platform: 'gemini',
      type: 'oauth',
      status: 'active'
    })

    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(getAvailableModels).not.toHaveBeenCalled()
  })

  it('uses the backend SSE error field as the visible failure message', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"error","error":"upstream rejected credentials"}\n'
      ])
    ) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect((wrapper.vm as any).status).toBe('error')
    expect((wrapper.vm as any).errorMessage).toBe('upstream rejected credentials')
  })

  it('keeps OpenAI compact status events in the visible test output', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"status","text":"Compacting conversation context"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).testMode = 'compact'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect((wrapper.vm as any).outputLines).toEqual(expect.arrayContaining([
      expect.objectContaining({ text: 'Compacting conversation context' })
    ]))
    expect(wrapper.text()).toContain('Compacting conversation context')
  })

  it('renders image events when an OpenAI image model is selected', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.4', display_name: 'GPT-5.4' },
      { id: 'gpt-image-1', display_name: 'GPT Image 1' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        'data: {"type":"image","image_url":"data:image/png;base64,aW1hZ2U=","mime_type":"image/png"}\n',
        'data: {"type":"test_complete","success":true}\n'
      ])
    ) as any
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()
    ;(wrapper.vm as any).selectedModelId = 'gpt-image-1'
    await flushPromises()

    expect((wrapper.vm as any).testPrompt).toBe(
      'Generate a cute orange cat astronaut sticker on a clean pastel background.'
    )

    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect((wrapper.vm as any).generatedImages).toEqual([
      { url: 'data:image/png;base64,aW1hZ2U=', mimeType: 'image/png' }
    ])
    expect(wrapper.get('img').attributes('src')).toBe('data:image/png;base64,aW1hZ2U=')
  })
})
