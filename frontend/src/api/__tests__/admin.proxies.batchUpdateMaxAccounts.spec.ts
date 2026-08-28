import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { post }
}))

import { batchUpdateMaxAccounts } from '@/api/admin/proxies'

describe('admin proxy batch account-limit API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { updated_ids: [11, 12], skipped: [] } })
  })

  it('sends selected proxy IDs and the shared limit', async () => {
    await batchUpdateMaxAccounts([11, 12], 8)

    expect(post).toHaveBeenCalledWith('/admin/proxies/batch-update-max-accounts', {
      ids: [11, 12],
      max_accounts: 8
    })
  })
})
