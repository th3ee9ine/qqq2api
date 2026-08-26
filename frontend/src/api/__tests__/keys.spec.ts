import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('../client', () => ({
  apiClient: { post }
}))

import { create } from '../keys'

describe('keys API concurrency payload', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: {} })
  })

  it('preserves an explicit zero as unlimited', async () => {
    await create('unlimited', 1, undefined, [], [], 0, undefined, undefined, 0)

    expect(post).toHaveBeenCalledWith('/keys', expect.objectContaining({
      name: 'unlimited',
      concurrency: 0
    }))
  })

  it('serializes a configured integer limit', async () => {
    await create('limited', 1, undefined, [], [], 0, undefined, undefined, 12)

    expect(post).toHaveBeenCalledWith('/keys', expect.objectContaining({
      name: 'limited',
      concurrency: 12
    }))
  })
})
