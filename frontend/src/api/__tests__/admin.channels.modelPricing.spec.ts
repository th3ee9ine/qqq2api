import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get }
}))

import { getModelDefaultPricing } from '@/api/admin/channels'

describe('admin model default pricing API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('uses the legacy read-only path with a retained platform discriminator', async () => {
    const response = {
      found: true,
      input_price: 3e-6,
      output_price: 15e-6,
      cache_write_price: 3.75e-6,
      cache_read_price: 0.3e-6
    }
    get.mockResolvedValue({ data: response })

    await expect(getModelDefaultPricing('claude-sonnet-4', 'anthropic')).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/channels/model-pricing', {
      params: { model: 'claude-sonnet-4', platform: 'anthropic' }
    })
  })
})
