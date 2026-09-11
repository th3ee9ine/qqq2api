import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getUpstreamTestDefaults, runDebugWorkbench } from '../debugWorkbench'
const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('../../client', () => ({ apiClient: { get, post } }))
beforeEach(() => vi.clearAllMocks())
describe('debug workbench API', () => {
  it('submits the entire payload as JSON to the debug executor rather than connectivity testing', async () => {
    const result = { request_id: 'request-1', success: true }
    post.mockResolvedValue({ data: result })
    const controller = new AbortController()
    const payload = { endpoint: 'responses' as const, headers: { 'Accept-Language': 'zh-CN' }, body: { model: 'custom', input: 'body input', metadata: { keep: true } }, proxy_id: 0, session: { action: 'new_session' as const } }
    expect(await runDebugWorkbench('12', payload, controller.signal)).toEqual(result)
    expect(post).toHaveBeenCalledWith('/admin/accounts/12/debug', payload, { timeout: 180000, signal: controller.signal })
  })
  it('requests account-aware defaults and preserves explicit direct proxy value', async () => {
    get.mockResolvedValue({ data: { headers: {} } })
    await getUpstreamTestDefaults('responses', '12', 'hello', 0)
    expect(get).toHaveBeenCalledWith('/admin/accounts/test-defaults', { params: { endpoint: 'responses', account_id: '12', prompt: 'hello', proxy_id: 0 } })
  })
})
