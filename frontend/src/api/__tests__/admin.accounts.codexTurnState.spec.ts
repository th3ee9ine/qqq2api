import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import {
  cancelCodexTurnStateTask,
  collectCodexTurnState,
  getCodexTurnStateTask,
  listCodexTurnStateTasks,
  retryCodexTurnStateTask,
} from '@/api/admin/accounts'

describe('admin account Codex Turn State API', () => {
  beforeEach(() => {
    get.mockReset()
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

  it('forwards an optional abort signal to the collection request', async () => {
    const controller = new AbortController()
    post.mockResolvedValueOnce({ data: { status: 'queued', account_id: 7 } })

    await collectCodexTurnState(7, controller.signal)

    expect(post).toHaveBeenCalledWith(
      '/admin/accounts/7/codex-turn-state/collect',
      undefined,
      { timeout: 120_000, signal: controller.signal },
    )
  })

  it('marks a bulk collection in the request body', async () => {
    const controller = new AbortController()
    post.mockResolvedValueOnce({ data: { status: 'queued', account_id: 7 } })

    await collectCodexTurnState(7, 'bulk', controller.signal)

    expect(post).toHaveBeenCalledWith(
      '/admin/accounts/7/codex-turn-state/collect',
      { source: 'bulk' },
      { timeout: 120_000, signal: controller.signal },
    )
  })

  it('lists task summaries and loads one encoded task ID', async () => {
    const task = {
      id: 'task/id with spaces',
      account_id: 7,
      account_name: 'Account 7',
      request_model: 'gpt-5.5',
      owner_model: 'gpt-5.5',
      source: 'manual',
      status: 'running',
      stage: 'collecting',
      progress: 40,
      progress_current: 2,
      progress_total: 5,
      created_at_ms: 1,
      updated_at_ms: 2,
      can_cancel: true,
      can_retry: false,
    }
    get.mockResolvedValueOnce({ data: [task] }).mockResolvedValueOnce({ data: { ...task, events: [] } })

    await expect(listCodexTurnStateTasks()).resolves.toEqual([task])
    await expect(getCodexTurnStateTask(task.id)).resolves.toEqual({ ...task, events: [] })
    expect(get).toHaveBeenNthCalledWith(1, '/admin/accounts/codex-turn-state/tasks')
    expect(get).toHaveBeenNthCalledWith(2, '/admin/accounts/codex-turn-state/tasks/task%2Fid%20with%20spaces')
  })

  it('cancels and retries a task through the task action endpoints', async () => {
    const canceled = { id: 'task-1', status: 'canceled' }
    const retried = { id: 'task-2', status: 'queued', retry_of: 'task-1' }
    post.mockResolvedValueOnce({ data: canceled }).mockResolvedValueOnce({ data: retried })

    await expect(cancelCodexTurnStateTask('task-1')).resolves.toEqual(canceled)
    await expect(retryCodexTurnStateTask('task-1')).resolves.toEqual(retried)
    expect(post).toHaveBeenNthCalledWith(1, '/admin/accounts/codex-turn-state/tasks/task-1/cancel')
    expect(post).toHaveBeenNthCalledWith(2, '/admin/accounts/codex-turn-state/tasks/task-1/retry')
  })
})
