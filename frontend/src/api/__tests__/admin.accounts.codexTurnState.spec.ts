import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { collectCodexTurnState } from '@/api/admin/accounts'

describe('admin account Codex Turn State API', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it('queues the full configured model scope without a client-selected model', async () => {
    const result = {
      status: 'queued',
      account_id: 7,
      target_models: ['gpt-6-astra-preview', 'model-b'],
      queued_models: ['gpt-6-astra', 'model-b'],
      already_valid_models: [],
      model_targets: [
        { model: 'gpt-6-astra-preview', owner: 'gpt-6-astra' },
        { model: 'model-b', owner: 'model-b' },
      ],
    }
    post.mockResolvedValueOnce({ data: result })

    await expect(collectCodexTurnState(7)).resolves.toEqual(result)
    expect(post).toHaveBeenCalledWith(
      '/admin/accounts/7/codex-turn-state/collect',
      undefined,
      { timeout: 120_000 },
    )
  })

  it('preserves a structured rejected batch returned with HTTP 409', async () => {
    const result = {
      status: 'rejected',
      account_id: 7,
      target_models: [],
      queued_models: [],
      already_valid_models: [],
      model_targets: [],
      reason: 'model_scope_empty',
    }
    post.mockRejectedValueOnce({ status: 409, data: result })

    await expect(collectCodexTurnState(7)).resolves.toEqual(result)
  })
})
