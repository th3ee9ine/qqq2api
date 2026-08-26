/**
 * Authentication API for the administrator control panel.
 *
 * Registration, password recovery, passkeys and end-user OAuth/SSO are
 * intentionally absent. Interactive sign-in supports the configured super
 * administrator and restricted account administrators, with optional TOTP.
 */

import { apiClient } from './client'
import { refreshAuthTokens, type RefreshTokenResponse } from './tokenRefresh'
export type { RefreshTokenResponse } from './tokenRefresh'
import type {
  AuthResponse,
  CurrentUserResponse,
  LoginRequest,
  PublicSettings,
  TotpLogin2FARequest,
  TotpLoginResponse,
} from '@/types'

export type LoginResponse = AuthResponse | TotpLoginResponse

export function isTotp2FARequired(response: LoginResponse): response is TotpLoginResponse {
  return 'requires_2fa' in response && response.requires_2fa === true
}

export function setAuthToken(token: string): void {
  localStorage.setItem('auth_token', token)
}

export function setRefreshToken(token: string): void {
  localStorage.setItem('refresh_token', token)
}

export function setTokenExpiresAt(expiresIn: number): void {
  localStorage.setItem('token_expires_at', String(Date.now() + expiresIn * 1000))
}

export function getAuthToken(): string | null {
  return localStorage.getItem('auth_token')
}

export function getRefreshToken(): string | null {
  return localStorage.getItem('refresh_token')
}

export function getTokenExpiresAt(): number | null {
  const value = localStorage.getItem('token_expires_at')
  return value ? Number.parseInt(value, 10) : null
}

export function clearAuthToken(): void {
  localStorage.removeItem('auth_token')
  localStorage.removeItem('refresh_token')
  localStorage.removeItem('auth_user')
  localStorage.removeItem('token_expires_at')
  localStorage.removeItem('pending_auth_session')
}

function persistAuthenticatedResponse(response: AuthResponse): void {
  setAuthToken(response.access_token)
  if (response.refresh_token) {
    setRefreshToken(response.refresh_token)
  }
  if (response.expires_in) {
    setTokenExpiresAt(response.expires_in)
  }
  localStorage.setItem('auth_user', JSON.stringify(response.user))
}

export async function login(credentials: LoginRequest): Promise<LoginResponse> {
  const { data } = await apiClient.post<LoginResponse>('/auth/login', credentials)
  if (!isTotp2FARequired(data)) {
    persistAuthenticatedResponse(data)
  }
  return data
}

export async function login2FA(request: TotpLogin2FARequest): Promise<AuthResponse> {
  const { data } = await apiClient.post<AuthResponse>('/auth/login/2fa', request)
  persistAuthenticatedResponse(data)
  return data
}

export async function getCurrentUser() {
  return apiClient.get<CurrentUserResponse>('/auth/me')
}

export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken()
  if (refreshToken) {
    try {
      await apiClient.post('/auth/logout', { refresh_token: refreshToken })
    } catch {
      // Local logout must remain available when the server is unreachable.
    }
  }
  clearAuthToken()
}

export async function refreshToken(): Promise<RefreshTokenResponse> {
  return refreshAuthTokens()
}

export async function revokeAllSessions(): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>('/auth/revoke-all-sessions')
  return data
}

export function isAuthenticated(): boolean {
  return getAuthToken() !== null
}

export async function getPublicSettings(): Promise<PublicSettings> {
  const { data } = await apiClient.get<PublicSettings>('/settings/public')
  return data
}

export const authAPI = {
  login,
  login2FA,
  getCurrentUser,
  logout,
  refreshToken,
  revokeAllSessions,
  isAuthenticated,
  getPublicSettings,
  setAuthToken,
  setRefreshToken,
  setTokenExpiresAt,
  getAuthToken,
  getRefreshToken,
  getTokenExpiresAt,
  clearAuthToken,
}

export default authAPI
