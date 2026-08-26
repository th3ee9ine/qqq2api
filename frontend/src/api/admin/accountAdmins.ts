/**
 * Super-administrator API for managing restricted account administrators.
 * The backend fixes the role to `account_admin`; callers cannot promote an
 * identity through this surface.
 */

import { apiClient } from '../client'
import type { AdminUser, PaginatedResponse } from '@/types'

export interface AccountAdminFilters {
  search?: string
  status?: '' | 'active' | 'disabled'
}

export interface CreateAccountAdminRequest {
  email: string
  password: string
  username?: string
  notes?: string
}

export interface UpdateAccountAdminRequest {
  email?: string
  password?: string
  username?: string
  notes?: string
  status?: 'active' | 'disabled'
}

export async function list(
  page = 1,
  pageSize = 20,
  filters: AccountAdminFilters = {},
): Promise<PaginatedResponse<AdminUser>> {
  const { data } = await apiClient.get<PaginatedResponse<AdminUser>>('/admin/account-admins', {
    params: {
      page,
      page_size: pageSize,
      search: filters.search || undefined,
      status: filters.status || undefined,
    },
  })
  return data
}

export async function create(payload: CreateAccountAdminRequest): Promise<AdminUser> {
  const { data } = await apiClient.post<AdminUser>('/admin/account-admins', payload)
  return data
}

export async function update(
  id: number,
  payload: UpdateAccountAdminRequest,
): Promise<AdminUser> {
  const { data } = await apiClient.put<AdminUser>(`/admin/account-admins/${id}`, payload)
  return data
}

export async function remove(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/account-admins/${id}`)
  return data
}

export default {
  list,
  create,
  update,
  remove,
}
