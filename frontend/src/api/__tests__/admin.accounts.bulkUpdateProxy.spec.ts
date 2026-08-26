import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { post }
}))

import { bulkUpdate } from '@/api/admin/accounts'

describe('admin account bulk proxy assignment API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { success: 2, failed: 0, results: [] } })
  })

  it('keeps auto_assign_proxy at the top level beside selected account IDs', async () => {
    await bulkUpdate([11, 12], { auto_assign_proxy: true })

    expect(post).toHaveBeenCalledWith('/admin/accounts/bulk-update', {
      account_ids: [11, 12],
      auto_assign_proxy: true
    })
  })

  it('keeps auto_assign_proxy at the top level in filtered-results mode', async () => {
    await bulkUpdate({
      filters: { platform: 'openai', status: 'active' },
      auto_assign_proxy: true
    })

    expect(post).toHaveBeenCalledWith('/admin/accounts/bulk-update', {
      filters: { platform: 'openai', status: 'active' },
      auto_assign_proxy: true
    })
  })
})
