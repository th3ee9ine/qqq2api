/**
 * Authentication state for super administrators and restricted account
 * administrators.
 */

import { defineStore } from 'pinia'
import { computed, readonly, ref } from 'vue'
import { authAPI, isTotp2FARequired, type LoginResponse } from '@/api'
import type { AuthResponse, LoginRequest, User } from '@/types'
import {
  defaultPanelPath,
  hasPanelPermission,
  isAccountAdminRole,
  isPanelRole,
  isSuperAdminRole,
  type PanelPermission,
} from '@/utils/accessControl'

const AUTH_TOKEN_KEY = 'auth_token'
const AUTH_USER_KEY = 'auth_user'
const REFRESH_TOKEN_KEY = 'refresh_token'
const TOKEN_EXPIRES_AT_KEY = 'token_expires_at'
const RETIRED_PENDING_AUTH_SESSION_KEY = 'pending_auth_session'
const AUTO_REFRESH_INTERVAL = 60 * 1000
const TOKEN_REFRESH_BUFFER = 120 * 1000

function isPanelOperator(user: User | null | undefined): user is User {
  return isPanelRole(user?.role)
}

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(null)
  const token = ref<string | null>(null)
  const refreshTokenValue = ref<string | null>(null)
  const tokenExpiresAt = ref<number | null>(null)
  const runMode = ref<'standard' | 'simple'>('standard')
  let refreshIntervalId: ReturnType<typeof setInterval> | null = null
  let tokenRefreshTimeoutId: ReturnType<typeof setTimeout> | null = null

  const isAuthenticated = computed(() => Boolean(token.value && isPanelOperator(user.value)))
  // Keep isAdmin narrowly scoped to the configured super administrator.
  const isAdmin = computed(() => isSuperAdminRole(user.value?.role))
  const isAccountAdmin = computed(() => isAccountAdminRole(user.value?.role))
  const isPanelOperatorRole = computed(() => isPanelOperator(user.value))
  const panelHomePath = computed(() => defaultPanelPath(user.value?.role))
  const isSimpleMode = computed(() => runMode.value === 'simple')

  function hasPermission(permission: PanelPermission): boolean {
    return hasPanelPermission(user.value?.role, permission)
  }

  function stopAutoRefresh(): void {
    if (refreshIntervalId) {
      clearInterval(refreshIntervalId)
      refreshIntervalId = null
    }
  }

  function startAutoRefresh(): void {
    stopAutoRefresh()
    refreshIntervalId = setInterval(() => {
      if (token.value) {
        refreshUser().catch((error) => {
          console.error('Failed to refresh administrator session:', error)
        })
      }
    }, AUTO_REFRESH_INTERVAL)
  }

  function stopTokenRefresh(): void {
    if (tokenRefreshTimeoutId) {
      clearTimeout(tokenRefreshTimeoutId)
      tokenRefreshTimeoutId = null
    }
  }

  function scheduleTokenRefreshAt(expiresAtMs: number): void {
    stopTokenRefresh()
    const refreshInMs = Math.max(0, expiresAtMs - Date.now() - TOKEN_REFRESH_BUFFER)
    if (refreshInMs === 0) {
      void performTokenRefresh()
      return
    }
    tokenRefreshTimeoutId = setTimeout(() => {
      void performTokenRefresh()
    }, refreshInMs)
  }

  function scheduleTokenRefresh(expiresInSeconds: number): void {
    const expiresAtMs = Date.now() + expiresInSeconds * 1000
    tokenExpiresAt.value = expiresAtMs
    localStorage.setItem(TOKEN_EXPIRES_AT_KEY, String(expiresAtMs))
    scheduleTokenRefreshAt(expiresAtMs)
  }

  async function performTokenRefresh(): Promise<void> {
    if (!refreshTokenValue.value) return
    try {
      const response = await authAPI.refreshToken()
      token.value = response.access_token
      refreshTokenValue.value = response.refresh_token
      scheduleTokenRefresh(response.expires_in)
    } catch (error) {
      console.error('Administrator token refresh failed:', error)
    }
  }

  function clearAuth(): void {
    stopAutoRefresh()
    stopTokenRefresh()
    user.value = null
    token.value = null
    refreshTokenValue.value = null
    tokenExpiresAt.value = null
    runMode.value = 'standard'
    localStorage.removeItem(AUTH_TOKEN_KEY)
    localStorage.removeItem(AUTH_USER_KEY)
    localStorage.removeItem(REFRESH_TOKEN_KEY)
    localStorage.removeItem(TOKEN_EXPIRES_AT_KEY)
    localStorage.removeItem(RETIRED_PENDING_AUTH_SESSION_KEY)
  }

  function setAuthFromResponse(response: AuthResponse): void {
    if (!isPanelOperator(response.user)) {
      clearAuth()
      throw new Error('Administrator account required')
    }

    token.value = response.access_token
    refreshTokenValue.value = response.refresh_token || null
    runMode.value = response.user.run_mode || 'standard'
    const { run_mode: _runMode, ...administrator } = response.user
    user.value = administrator

    localStorage.setItem(AUTH_TOKEN_KEY, response.access_token)
    localStorage.setItem(AUTH_USER_KEY, JSON.stringify(administrator))
    localStorage.removeItem(RETIRED_PENDING_AUTH_SESSION_KEY)
    if (response.refresh_token) {
      localStorage.setItem(REFRESH_TOKEN_KEY, response.refresh_token)
    } else {
      localStorage.removeItem(REFRESH_TOKEN_KEY)
    }

    startAutoRefresh()
    if (response.refresh_token && response.expires_in) {
      scheduleTokenRefresh(response.expires_in)
    }
  }

  function checkAuth(): void {
    localStorage.removeItem(RETIRED_PENDING_AUTH_SESSION_KEY)
    const savedToken = localStorage.getItem(AUTH_TOKEN_KEY)
    const savedUser = localStorage.getItem(AUTH_USER_KEY)
    if (!savedToken || !savedUser) return

    try {
      const parsedUser = JSON.parse(savedUser) as User
      if (!isPanelOperator(parsedUser)) {
        clearAuth()
        return
      }

      token.value = savedToken
      user.value = parsedUser
      refreshTokenValue.value = localStorage.getItem(REFRESH_TOKEN_KEY)
      const savedExpiresAt = localStorage.getItem(TOKEN_EXPIRES_AT_KEY)
      tokenExpiresAt.value = savedExpiresAt ? Number.parseInt(savedExpiresAt, 10) : null
      runMode.value = 'standard'

      void refreshUser().catch((error) => {
        console.error('Failed to restore administrator session:', error)
      })
      startAutoRefresh()
      if (refreshTokenValue.value && tokenExpiresAt.value !== null) {
        scheduleTokenRefreshAt(tokenExpiresAt.value)
      }
    } catch (error) {
      console.error('Failed to restore administrator session:', error)
      clearAuth()
    }
  }

  async function login(credentials: LoginRequest): Promise<LoginResponse> {
    try {
      const response = await authAPI.login(credentials)
      if (!isTotp2FARequired(response)) {
        setAuthFromResponse(response)
      }
      return response
    } catch (error) {
      clearAuth()
      throw error
    }
  }

  async function login2FA(tempToken: string, totpCode: string): Promise<User> {
    try {
      const response = await authAPI.login2FA({
        temp_token: tempToken,
        totp_code: totpCode,
      })
      setAuthFromResponse(response)
      return user.value!
    } catch (error) {
      clearAuth()
      throw error
    }
  }

  async function logout(): Promise<void> {
    try {
      await authAPI.logout()
    } catch (error) {
      console.warn('Logout API call failed, clearing local session anyway', error)
    } finally {
      clearAuth()
    }
  }

  async function refreshUser(): Promise<User> {
    if (!token.value) {
      throw new Error('Not authenticated')
    }

    try {
      const response = await authAPI.getCurrentUser()
      if (!isPanelOperator(response.data)) {
        clearAuth()
        throw new Error('Administrator account required')
      }
      runMode.value = response.data.run_mode || 'standard'
      const { run_mode: _runMode, ...administrator } = response.data
      user.value = administrator
      localStorage.setItem(AUTH_USER_KEY, JSON.stringify(administrator))
      return administrator
    } catch (error) {
      if ((error as { status?: number }).status === 401) {
        clearAuth()
      }
      throw error
    }
  }

  return {
    user,
    token,
    runMode: readonly(runMode),
    isAuthenticated,
    isAdmin,
    isAccountAdmin,
    isPanelOperator: isPanelOperatorRole,
    panelHomePath,
    hasPermission,
    isSimpleMode,
    login,
    login2FA,
    logout,
    checkAuth,
    refreshUser,
  }
})
