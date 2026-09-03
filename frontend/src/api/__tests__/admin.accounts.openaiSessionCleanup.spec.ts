import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, put },
}))

import {
  getOpenAISessionCleanup,
  runOpenAISessionCleanup,
  updateOpenAISessionCleanup,
} from '@/api/admin/accounts'

describe('admin OpenAI session cleanup API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
  })

  it('reads and updates account cleanup settings', async () => {
    const settings = { enabled: true, interval_minutes: 15, state: null }
    get.mockResolvedValueOnce({ data: settings })
    put.mockResolvedValueOnce({ data: settings })

    await expect(getOpenAISessionCleanup(42)).resolves.toEqual(settings)
    await expect(updateOpenAISessionCleanup(42, { enabled: true, interval_minutes: 15 })).resolves.toEqual(settings)

    expect(get).toHaveBeenCalledWith('/admin/openai/accounts/42/sessions/cleanup')
    expect(put).toHaveBeenCalledWith('/admin/openai/accounts/42/sessions/cleanup', {
      enabled: true,
      interval_minutes: 15,
    })
  })

  it('uses the dedicated run endpoint with an extended timeout', async () => {
    post.mockResolvedValueOnce({ data: { message: 'ok' } })

    await expect(runOpenAISessionCleanup(42)).resolves.toEqual({ message: 'ok' })
    expect(post).toHaveBeenCalledWith(
      '/admin/openai/accounts/42/sessions/cleanup/run',
      undefined,
      { timeout: 130000 }
    )
  })
})
