import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post }
}))

import {
  getConfig,
  testConnection,
  updateConfig,
  type ImageStorageConfig
} from '@/api/admin/imageStorage'

const config: ImageStorageConfig = {
  enabled: true,
  endpoint: 'https://s3.example.com',
  region: 'auto',
  bucket: 'generated-images',
  access_key_id: 'access-key',
  secret_access_key: 'secret-key',
  prefix: 'images/',
  public_base_url: 'https://cdn.example.com',
  force_path_style: true,
  presign_expiry_hours: 24,
  max_download_bytes: 33_554_432
}

describe('admin image storage API', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
  })

  it('loads the configuration and secret status', async () => {
    const response = { config, secret_configured: true }
    get.mockResolvedValue({ data: response })

    await expect(getConfig()).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/settings/image-storage')
  })

  it('updates the configuration using the bare response contract', async () => {
    put.mockResolvedValue({ data: config })

    await expect(updateConfig(config)).resolves.toEqual(config)
    expect(put).toHaveBeenCalledWith('/admin/settings/image-storage', config)
  })

  it('accepts a wrapped update response for compatibility', async () => {
    put.mockResolvedValue({ data: { config, secret_configured: true } })

    await expect(updateConfig(config)).resolves.toEqual(config)
  })

  it('tests the supplied configuration', async () => {
    const response = { ok: true, message: 'connected' }
    post.mockResolvedValue({ data: response })

    await expect(testConnection(config)).resolves.toEqual(response)
    expect(post).toHaveBeenCalledWith('/admin/settings/image-storage/test', config)
  })
})
