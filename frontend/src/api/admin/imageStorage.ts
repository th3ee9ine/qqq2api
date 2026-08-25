import { apiClient } from '../client'

/**
 * Object storage used by asynchronous OpenAI image tasks.
 *
 * This configuration is independent from the retired database-backup feature.
 * Leaving `secret_access_key` empty keeps the secret already stored by the
 * backend.
 */
export interface ImageStorageConfig {
  enabled: boolean
  endpoint: string
  region: string
  bucket: string
  access_key_id: string
  secret_access_key?: string
  prefix: string
  public_base_url: string
  force_path_style: boolean
  presign_expiry_hours: number
  max_download_bytes: number
}

export interface ImageStorageConfigResponse {
  config: ImageStorageConfig
  secret_configured: boolean
}

export interface ImageStorageTestResponse {
  ok: boolean
  message: string
}

type ImageStorageUpdateResponse = ImageStorageConfig | ImageStorageConfigResponse

export async function getConfig(): Promise<ImageStorageConfigResponse> {
  const { data } = await apiClient.get<ImageStorageConfigResponse>(
    '/admin/settings/image-storage',
  )
  return data
}

export async function updateConfig(config: ImageStorageConfig): Promise<ImageStorageConfig> {
  const { data } = await apiClient.put<ImageStorageUpdateResponse>(
    '/admin/settings/image-storage',
    config,
  )
  return 'config' in data ? data.config : data
}

export async function testConnection(
  config: ImageStorageConfig,
): Promise<ImageStorageTestResponse> {
  const { data } = await apiClient.post<ImageStorageTestResponse>(
    '/admin/settings/image-storage/test',
    config,
  )
  return data
}

export const imageStorageAPI = {
  getConfig,
  updateConfig,
  testConnection,
}

export default imageStorageAPI
