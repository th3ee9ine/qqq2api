/**
 * System group options used by the global API Key manager.
 */

import { apiClient } from './client'
import type { Group } from '@/types'

/** Get all active groups that a system-wide API Key can use. */
export async function getAvailable(): Promise<Group[]> {
  const { data } = await apiClient.get<Group[]>('/groups/available')
  return data
}

export const groupsAPI = {
  getAvailable
}

export default groupsAPI
