import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAuthStore } from '@/stores/auth'

const mockLogin = vi.fn()
const mockLogin2FA = vi.fn()
const mockLogout = vi.fn()
const mockGetCurrentUser = vi.fn()
const mockRefreshToken = vi.fn()

vi.mock('@/api', () => ({
  authAPI: {
    login: (...args: unknown[]) => mockLogin(...args),
    login2FA: (...args: unknown[]) => mockLogin2FA(...args),
    logout: (...args: unknown[]) => mockLogout(...args),
    getCurrentUser: (...args: unknown[]) => mockGetCurrentUser(...args),
    refreshToken: (...args: unknown[]) => mockRefreshToken(...args),
  },
  isTotp2FARequired: (response: { requires_2fa?: boolean }) => response?.requires_2fa === true,
}))

const administrator = {
  id: 1,
  username: 'admin',
  email: 'admin@example.com',
  role: 'admin' as const,
  balance: 0,
  concurrency: 0,
  status: 'active' as const,
  allowed_groups: null,
  created_at: '2024-01-01',
  updated_at: '2024-01-01',
}

const authResponse = {
  access_token: 'admin-access-token',
  refresh_token: 'admin-refresh-token',
  expires_in: 3600,
  token_type: 'Bearer',
  user: { ...administrator },
}

describe('single-administrator auth store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
    vi.useFakeTimers()
    vi.clearAllMocks()
    mockGetCurrentUser.mockResolvedValue({ data: administrator })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('stores a successful administrator password login', async () => {
    mockLogin.mockResolvedValue(authResponse)
    const store = useAuthStore()

    await store.login({ email: 'admin@example.com', password: 'secret' })

    expect(store.isAuthenticated).toBe(true)
    expect(store.isAdmin).toBe(true)
    expect(store.token).toBe('admin-access-token')
    expect(store.user).toEqual(administrator)
    expect(localStorage.getItem('auth_token')).toBe('admin-access-token')
  })

  it('rejects a non-administrator response and clears stale credentials', async () => {
    mockLogin.mockResolvedValue({
      ...authResponse,
      user: { ...administrator, role: 'user' },
    })
    const store = useAuthStore()

    await expect(
      store.login({ email: 'legacy@example.com', password: 'secret' }),
    ).rejects.toThrow('Administrator account required')

    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_token')).toBeNull()
    expect(localStorage.getItem('auth_user')).toBeNull()
  })

  it('returns a TOTP challenge without creating a session', async () => {
    const challenge = {
      requires_2fa: true,
      temp_token: 'temporary-token',
      user_email_masked: 'a***@example.com',
    }
    mockLogin.mockResolvedValue(challenge)
    const store = useAuthStore()

    await expect(
      store.login({ email: 'admin@example.com', password: 'secret' }),
    ).resolves.toEqual(challenge)
    expect(store.isAuthenticated).toBe(false)
  })

  it('creates the administrator session after TOTP verification', async () => {
    mockLogin2FA.mockResolvedValue(authResponse)
    const store = useAuthStore()

    await expect(store.login2FA('temporary-token', '654321')).resolves.toEqual(administrator)
    expect(mockLogin2FA).toHaveBeenCalledWith({
      temp_token: 'temporary-token',
      totp_code: '654321',
    })
    expect(store.isAuthenticated).toBe(true)
  })

  it('clears local state even if server logout fails', async () => {
    mockLogin.mockResolvedValue(authResponse)
    mockLogout.mockRejectedValue(new Error('offline'))
    const store = useAuthStore()
    await store.login({ email: 'admin@example.com', password: 'secret' })

    await store.logout()

    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_token')).toBeNull()
    expect(localStorage.getItem('refresh_token')).toBeNull()
  })

  it('restores only an administrator session and drops retired pending auth state', () => {
    localStorage.setItem('auth_token', 'saved-token')
    localStorage.setItem('auth_user', JSON.stringify(administrator))
    localStorage.setItem('pending_auth_session', JSON.stringify({ provider: 'oidc' }))
    const store = useAuthStore()

    store.checkAuth()

    expect(store.isAuthenticated).toBe(true)
    expect(localStorage.getItem('pending_auth_session')).toBeNull()
  })

  it('removes a persisted non-administrator session', () => {
    localStorage.setItem('auth_token', 'legacy-token')
    localStorage.setItem('auth_user', JSON.stringify({ ...administrator, role: 'user' }))
    const store = useAuthStore()

    store.checkAuth()

    expect(store.isAuthenticated).toBe(false)
    expect(localStorage.getItem('auth_token')).toBeNull()
  })

  it('refreshes and persists administrator session data', async () => {
    mockLogin.mockResolvedValue(authResponse)
    const store = useAuthStore()
    await store.login({ email: 'admin@example.com', password: 'secret' })
    const updated = { ...administrator, username: 'system-admin' }
    mockGetCurrentUser.mockResolvedValue({ data: updated })

    await expect(store.refreshUser()).resolves.toEqual(updated)
    expect(store.user).toEqual(updated)
    expect(JSON.parse(localStorage.getItem('auth_user')!)).toEqual(updated)
  })

  it('supports the administrator simple run mode', async () => {
    mockLogin.mockResolvedValue({
      ...authResponse,
      user: { ...administrator, run_mode: 'simple' as const },
    })
    const store = useAuthStore()

    await store.login({ email: 'admin@example.com', password: 'secret' })

    expect(store.isSimpleMode).toBe(true)
  })
})
