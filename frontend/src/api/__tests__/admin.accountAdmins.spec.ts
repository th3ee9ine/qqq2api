import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put, del } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  del: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, put, delete: del },
}))

import accountAdminsAPI from '@/api/admin/accountAdmins'

describe('account administrators API', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('lists only through the dedicated account-admin endpoint', async () => {
    const response = { items: [], total: 0, page: 2, page_size: 10, pages: 0 }
    get.mockResolvedValue({ data: response })

    await expect(accountAdminsAPI.list(2, 10, {
      search: 'operator',
      status: 'active',
    })).resolves.toEqual(response)

    expect(get).toHaveBeenCalledWith('/admin/account-admins', {
      params: {
        page: 2,
        page_size: 10,
        search: 'operator',
        status: 'active',
      },
    })
  })

  it('creates without accepting a caller-controlled role', async () => {
    const payload = { email: 'operator@example.com', password: 'secret12' }
    post.mockResolvedValue({ data: { id: 7, ...payload, role: 'account_admin' } })

    await accountAdminsAPI.create(payload)

    expect(post).toHaveBeenCalledWith('/admin/account-admins', payload)
    expect(payload).not.toHaveProperty('role')
  })

  it('updates and deletes by id', async () => {
    put.mockResolvedValue({ data: { id: 7, status: 'disabled' } })
    del.mockResolvedValue({ data: { message: 'ok' } })

    await accountAdminsAPI.update(7, { status: 'disabled' })
    await accountAdminsAPI.remove(7)

    expect(put).toHaveBeenCalledWith('/admin/account-admins/7', { status: 'disabled' })
    expect(del).toHaveBeenCalledWith('/admin/account-admins/7')
  })
})
