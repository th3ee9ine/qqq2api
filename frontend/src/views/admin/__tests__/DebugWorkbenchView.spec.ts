import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import DebugWorkbenchView from '../DebugWorkbenchView.vue'

const { defaults, accounts, models, proxies, runTest, writeText } = vi.hoisted(() => ({
  defaults: vi.fn(), accounts: vi.fn(), models: vi.fn(), proxies: vi.fn(), runTest: vi.fn(), writeText: vi.fn()
}))
vi.mock('@/components/layout/AppLayout.vue', () => ({ default: { template: '<main><slot /></main>' } }))
vi.mock('@/components/common/ProxySelector.vue', () => ({ default: { template: '<button>不使用代理</button>' } }))
vi.mock('@/api/admin/debugWorkbench', () => ({ getUpstreamTestDefaults: defaults, runDebugWorkbench: runTest }))
vi.mock('@/api/admin/accounts', () => ({ list: accounts, getAvailableModels: models }))
vi.mock('@/api/admin/proxies', () => ({ proxiesAPI: { getAll: proxies } }))

let wrapper: VueWrapper
async function render() {
  wrapper = mount(DebugWorkbenchView, {
    attachTo: document.body,
    global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, ProxySelector: { template: '<button>不使用代理</button>' } } }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  defaults.mockRejectedValue(new Error('offline'))
  accounts.mockResolvedValue({ items: [{ id: 1, name: 'GPT One', type: 'oauth' }, { id: 2, name: 'GPT Two', type: 'apikey' }] })
  models.mockResolvedValue([])
  proxies.mockResolvedValue([])
  writeText.mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
})
afterEach(() => wrapper?.unmount())

describe('debug workbench presentation', () => {
  it('identifies local previews without claiming upstream connectivity', async () => {
    accounts.mockResolvedValue({ items: [] })
    await render()
    expect(wrapper.get('.session-badge').text()).toBe('模板预览')
    expect(wrapper.get('.send-button').attributes('disabled')).toBeDefined()
    expect(wrapper.get('.preview-only-note').text()).toContain('不会模拟成功响应')
    expect(wrapper.text()).not.toContain('调试服务在线')
    expect(wrapper.findAll('.trace-tabs [role="tab"]')).toHaveLength(4)
    await wrapper.get('#trace-tab-upstream-response').trigger('click')
    expect(wrapper.get('.response-empty').text()).toContain('等待第一条响应')
    expect(runTest).not.toHaveBeenCalled()
  })

  it('preserves custom model input and synchronizes the JSON editor', async () => {
    await render()
    await wrapper.get('#debug-model').setValue('my-custom-model')
    await wrapper.get('#debug-model').trigger('keydown', { key: 'Enter' })
    expect(JSON.parse((wrapper.get('#debug-api-params').element as HTMLTextAreaElement).value).model).toBe('my-custom-model')
    expect(wrapper.get('#debug-model-options').text()).toContain('my-custom-model')
  })

  it('supports arrow-key editor navigation with focus and ARIA selection', async () => {
    await render()
    await wrapper.get('#editor-tab-message').trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(wrapper.get('#editor-tab-json').attributes('aria-selected')).toBe('true')
    expect(document.activeElement?.id).toBe('editor-tab-json')
    expect(wrapper.get('#editor-panel-json').isVisible()).toBe(true)
    expect(wrapper.get('#editor-panel-message').isVisible()).toBe(false)
  })

  it('disables image fields until enabled and opens image controls for image endpoints', async () => {
    await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper.get('#debug-endpoint').setValue('images/generations')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('#editor-tab-image').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('#editor-panel-image').isVisible()).toBe(true)
  })

  it('shows invalid JSON errors in the editor and uses error status styling', async () => {
    await render()
    await wrapper.get('#debug-headers').setValue('{invalid')
    await wrapper.get('.send-button').trigger('click')
    expect(wrapper.get('[role="alert"]').text()).toContain('请求头格式有误')
    expect(wrapper.get('.session-badge').classes()).toContain('session-error')
    expect(runTest).not.toHaveBeenCalled()
  })

  it('provides copy success and failure feedback', async () => {
    await render()
    await wrapper.get('.copy-button').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('"method": "POST"'))
    expect(wrapper.get('.copy-button').text()).toBe('已复制')
    writeText.mockRejectedValueOnce(new Error('denied'))
    await wrapper.get('.copy-button').trigger('click')
    await flushPromises()
    expect(wrapper.get('.copy-button').text()).toContain('复制失败')
  })
})


describe('upstream default headers', () => {
  const fullHeaders = {
    Host: 'chatgpt.com',
    'Content-Type': 'application/json',
    Accept: 'text/event-stream',
    Authorization: 'Bearer ••••••••',
    'User-Agent': 'configured-account-client/1.2.3',
    Originator: 'configured-account-client',
    Version: '1.2.3',
    'Chatgpt-Account-Id': '••••••••',
    'X-Custom-Header': '••••••••'
  }
  function serverDefaults() {
    defaults.mockResolvedValue({
      endpoint: 'responses', source: 'AccountTestService', account_type: 'oauth',
      body: { model: 'gpt-5.4', input: [], stream: true },
      headers: fullHeaders, url: 'https://chatgpt.com/backend-api/codex/responses',
      notes: ['Host 来自 Request.Host；动态请求头由后端生成。'],
      header_details: [
        { name: 'Authorization', purpose: '鉴别上游账号', requirement: 'required', condition: '所有请求', source: '账号凭据', default_included: true },
        { name: 'X-Codex-Turn-State', purpose: '延续上游轮次状态', requirement: 'conditional', condition: '仅存在前序响应时', source: '前序响应头', default_included: false },
        { name: 'X-Codex-Routing-Hint', purpose: '引导模型路由', requirement: 'conditional', condition: 'OAuth Codex 请求', source: '最终上游模型', default_included: true }
      ]
    })
  }

  it('loads every backend header and explanatory note without a frontend allowlist', async () => {
    serverDefaults()
    await render()
    await wrapper.get('#editor-tab-headers').trigger('click')
    expect(JSON.parse((wrapper.get('#debug-headers').element as HTMLTextAreaElement).value)).toEqual(fullHeaders)
    expect(wrapper.get('#editor-panel-headers .format-tag').text()).toBe('9 HEADERS')
    expect(wrapper.get('.headers-source-row').text()).toContain('来自项目实际请求构造')
    expect(wrapper.get('#headers-default-notes').text()).toContain('Host 来自 Request.Host')
    expect(wrapper.get('.header-guide').text()).toContain('鉴别上游账号')
    expect(wrapper.get('[data-header="Authorization"] .header-presence').text()).toBe('默认包含')
    expect(wrapper.get('[data-header="X-Codex-Turn-State"] .header-presence').text()).toBe('未配置')
    await wrapper.get('#trace-tab-upstream-request').trigger('click')
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('configured-account-client/1.2.3')
  })

  it('restores all default headers without resetting other request edits', async () => {
    serverDefaults()
    await render()
    await wrapper.get('#debug-api-params').setValue('{"model":"edited-model"}')
    await wrapper.get('#debug-headers').setValue('{"X-Only":"edited"}')
    await wrapper.get('.headers-source-row button').trigger('click')
    expect(JSON.parse((wrapper.get('#debug-headers').element as HTMLTextAreaElement).value)).toEqual(fullHeaders)
    expect((wrapper.get('#debug-api-params').element as HTMLTextAreaElement).value).toContain('edited-model')
  })

  it('clears the previous account headers when the next defaults request fails', async () => {
    serverDefaults()
    await render()
    defaults.mockRejectedValue(new Error('new account unavailable'))
    await wrapper.get('#debug-account').setValue('2')
    await flushPromises()
    const headers = JSON.parse((wrapper.get('#debug-headers').element as HTMLTextAreaElement).value)
    expect(headers).not.toHaveProperty('Chatgpt-Account-Id')
    expect(headers).not.toHaveProperty('X-Custom-Header')
    expect(wrapper.get('.headers-source-row').text()).toContain('基础示例')
    expect(wrapper.get('#headers-default-notes').text()).toContain('默认配置同步失败')
    expect(wrapper.findAll('.header-guide-item')).toHaveLength(0)
    expect(wrapper.get('.header-guide').text()).not.toContain('延续上游轮次状态')
  })

  it('keeps older backend responses compatible when header metadata is absent', async () => {
    defaults.mockResolvedValue({ endpoint: 'responses', source: 'AccountTestService', account_type: 'apikey', body: { model: 'gpt-5.4' }, headers: fullHeaders })
    await render()
    expect(JSON.parse((wrapper.get('#debug-headers').element as HTMLTextAreaElement).value)).toEqual(fullHeaders)
    expect(wrapper.get('.header-guide').text()).toContain('本次后端未返回请求头说明')
    expect(wrapper.find('.routing-hint-note').exists()).toBe(false)
  })

  it('keeps the routing hint explanation visible without rewriting edited headers or custom models', async () => {
    serverDefaults()
    await render()
    await wrapper.get('#editor-tab-headers').trigger('click')
    expect(wrapper.get('.routing-hint-note').isVisible()).toBe(true)
    expect(wrapper.get('.routing-hint-note').text()).toContain('按最终上游模型重新生成')
    expect(wrapper.get('.routing-hint-note').text()).toContain('静态值不会直接发送')
    expect(wrapper.get('.header-guide').attributes('open')).toBeUndefined()
    await wrapper.get('#debug-headers').setValue('{"X-Codex-Routing-Hint":"model=static-preview"}')
    await wrapper.get('#debug-model').setValue('my-custom-model')
    expect((wrapper.get('#debug-headers').element as HTMLTextAreaElement).value).toBe('{"X-Codex-Routing-Hint":"model=static-preview"}')
    expect((wrapper.get('#debug-model').element as HTMLInputElement).value).toBe('my-custom-model')
    expect(JSON.parse((wrapper.get('#debug-api-params').element as HTMLTextAreaElement).value).model).toBe('my-custom-model')
  })

  it('clears the previous guide while fetching defaults for another account', async () => {
    serverDefaults()
    await render()
    defaults.mockImplementation(() => new Promise(() => {}))
    await wrapper.get('#debug-account').setValue('2')
    await flushPromises()
    expect(wrapper.findAll('.header-guide-item')).toHaveLength(0)
    expect(wrapper.get('.header-guide').text()).toContain('正在同步当前账号与接口')
  })

  it('clears metadata on reset until the new defaults finish loading', async () => {
    serverDefaults()
    await render()
    defaults.mockImplementation(() => new Promise(() => {}))
    await wrapper.get('.header-actions button').trigger('click')
    expect(wrapper.findAll('.header-guide-item')).toHaveLength(0)
    expect(wrapper.get('.header-guide').text()).toContain('正在同步当前账号与接口')
  })
})

describe('actual gateway debug execution', () => {
  function execution(overrides: Record<string, unknown> = {}) {
    const snapshot = (body: unknown, extra = {}) => ({ headers: { 'Content-Type': ['application/json'] }, body, body_bytes: 42, captured_bytes: 42, truncated: false, complete: true, ...extra })
    return {
      request_id: 'debug-request-1', success: true, endpoint: 'responses', transport: 'http', duration_ms: 246,
      session: { id: 'session-handle-1', session_id: 'native-session', thread_id: 'native-thread', turn_id: 'native-turn', window_id: 'native-window', turn_index: 1, turn_state_available: true },
      inbound: snapshot({ input: 'actually received' }), outbound: snapshot({ output_text: 'actual result' }, { status_code: 200 }),
      attempts: [{ index: 1, transport: 'http', account_id: 1, proxy: 'direct', duration_ms: 231, ttft_ms: 53,
        request: snapshot({ model: 'actual-upstream-model' }, { method: 'POST', url: 'https://chatgpt.com/backend-api/codex/responses' }),
        response: snapshot(undefined, { status_code: 200, headers: { 'X-Codex-Turn-State': ['[redacted]'] }, body_text: 'data: {"type":"response.completed"}\n\n' }),
        header_changes: [{ name: 'X-Codex-Routing-Hint', action: 'rewritten', input_values: ['model=old'], output_values: ['model=actual-upstream-model'], reason: '由最终上游模型生成' }, { name: 'X-Unknown', action: 'filtered', reason: '不在正式网关允许范围' }]
      }], warnings: [], ...overrides
    }
  }

  it('submits the complete JSON and header editors to the real endpoint without dropping custom fields', async () => {
    runTest.mockResolvedValue(execution())
    await render()
    const body = { model: 'custom-native', input: [{ role: 'user', content: 'from JSON, not a stale prompt' }], stream: false, service_tier: 'priority', reasoning: { effort: 'high' }, metadata: { arbitrary: 'preserve' }, tools: [{ type: 'custom_tool' }] }
    const headers = { 'Content-Type': 'application/json', 'Accept-Language': 'zh-CN', 'X-Codex-Parent-Thread-Id': 'parent-thread', 'X-Unknown': 'a custom value' }
    await wrapper.get('#debug-api-params').setValue(JSON.stringify(body))
    await wrapper.get('#debug-headers').setValue(JSON.stringify(headers))
    await wrapper.get('.send-button').trigger('click')
    await flushPromises()
    expect(runTest).toHaveBeenCalledWith('1', { endpoint: 'responses', body, headers, session: { action: 'new_session' } }, expect.any(AbortSignal))
    expect(wrapper.get('.session-badge').text()).toBe('实际请求已返回')
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('response.completed')
    expect(wrapper.text()).toContain('首字节 53 ms')
    expect(wrapper.get('[data-header-change="X-Codex-Routing-Hint"]').text()).toContain('重写')
    expect(wrapper.get('[data-header-change="X-Unknown"]').text()).toContain('过滤')
    await wrapper.get('#trace-tab-upstream-request').trigger('click')
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('actual-upstream-model')
    await wrapper.get('#trace-tab-outbound').trigger('click')
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('actual result')
  })

  it('preserves JSON image fields rather than replacing them with stale image controls', async () => {
    runTest.mockResolvedValue(execution())
    await render()
    await wrapper.get('#debug-endpoint').setValue('images/generations')
    await flushPromises()
    const body = { model: 'image-custom', prompt: 'from JSON', n: 3, response_format: 'url', size: '1024x1536', quality: 'high', background: 'transparent' }
    await wrapper.get('#debug-api-params').setValue(JSON.stringify(body))
    await wrapper.get('.send-button').trigger('click')
    await flushPromises()
    expect(runTest.mock.calls[0][1]).toMatchObject({ endpoint: 'images/generations', body })
  })

  it('keeps a session across new turns, offers continuation, and resets explicitly', async () => {
    runTest.mockResolvedValue(execution())
    await render()
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(wrapper.get('.context-state').text()).toContain('已有上游 Turn State')
    expect(wrapper.get('.context-ids').text()).toContain('native-thread')
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[1][1].session).toEqual({ id: 'session-handle-1', action: 'new_turn' })
    await wrapper.get('#debug-turn-action').setValue('continue_turn')
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[2][1].session).toEqual({ id: 'session-handle-1', action: 'continue_turn' })
    await wrapper.get('.context-topline button').trigger('click')
    expect(wrapper.find('.context-ids').exists()).toBe(false)
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[3][1].session).toEqual({ action: 'new_session' })
  })

  it('resets session on account changes and ignores stale in-flight results', async () => {
    let resolveRequest!: (value: ReturnType<typeof execution>) => void
    runTest.mockImplementation(() => new Promise(resolve => { resolveRequest = resolve }))
    await render()
    await wrapper.get('.send-button').trigger('click')
    const signal = runTest.mock.calls[0][2] as AbortSignal
    // Simulate account switch from external state while the disabled control is in flight.
    const accountSelect = wrapper.get('#debug-account').element as HTMLSelectElement
    accountSelect.disabled = false
    await wrapper.get('#debug-account').setValue('2')
    await flushPromises()
    resolveRequest(execution())
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.find('.context-ids').exists()).toBe(false)
    expect(wrapper.find('[data-header-change]').exists()).toBe(false)
    expect(wrapper.get('.session-badge').text()).toBe('准备就绪')
  })

  it('selects actual retry attempts and identifies truncated and incomplete captures', async () => {
    const response = execution()
    response.attempts.unshift({ ...response.attempts[0], index: 0, error: 'first failed attempt', response: { ...response.attempts[0].response, status_code: 429, body_text: 'retry-first', captured_bytes: 8, body_bytes: 100, truncated: true, complete: false } })
    runTest.mockResolvedValue(response)
    await render()
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('response.completed')
    await wrapper.get('#debug-attempt').setValue('0')
    expect(wrapper.get('.capture-notice').text()).toContain('采集已截断')
    expect(wrapper.get('.capture-notice').text()).toContain('8 / 100 B')
    expect(wrapper.findComponent({ name: 'DebugJsonViewer' }).props('value')).toContain('retry-first')
  })

  it('distinguishes account default, direct and explicit proxy without resetting edited body', async () => {
    proxies.mockResolvedValue([{ id: 9, name: 'Proxy', protocol: 'socks5', host: '127.0.0.1', port: 1080, status: 'active' }])
    runTest.mockResolvedValue(execution())
    await render()
    const body = { model: 'native-custom', input: 'preserve-network-edits', metadata: { keep: true } }
    await wrapper.get('#debug-api-params').setValue(JSON.stringify(body))
    await wrapper.get('#debug-proxy').setValue('0')
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[0][1]).toMatchObject({ body, proxy_id: 0 })
    await wrapper.get('#debug-proxy').setValue('9')
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[1][1]).toMatchObject({ body, proxy_id: 9 })
    await wrapper.get('#debug-proxy').setValue('')
    // Null option has the browser string "" but Vue preserves the actual null value.
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(runTest.mock.calls[2][1]).not.toHaveProperty('proxy_id')
  })

  it('rejects malformed or duplicate case-insensitive headers before calling the API', async () => {
    await render()
    for (const headers of [{ 'X-Header': 'a', 'x-header': 'b' }, { 'X-Header': 'value\r\nInjected: bad' }, { 'Bad Header': 'value' }, { Accept: 7 }]) {
      await wrapper.get('#debug-headers').setValue(JSON.stringify(headers))
      await wrapper.get('.send-button').trigger('click')
      expect(wrapper.get('[role="alert"]').text()).toContain('请求头格式有误')
    }
    expect(runTest).not.toHaveBeenCalled()
  })

  it('shows upstream errors with actual failed trace rather than inferring HTTP success from an event', async () => {
    runTest.mockResolvedValue(execution({ success: false, error: 'The selected account quota is exhausted.', outbound: { headers: {}, status_code: 429, body: { error: 'quota' }, complete: true, truncated: false, captured_bytes: 1, body_bytes: 1 } }))
    await render()
    await wrapper.get('.send-button').trigger('click'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('quota is exhausted')
    expect(wrapper.get('.session-badge').classes()).toContain('session-error')
    expect(wrapper.get('.inspector-status').text()).toContain('429')
  })
})
