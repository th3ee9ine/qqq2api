import { describe, expect, it } from 'vitest'

import {
  ADMIN_UI_REQUEST_HEADER,
  USER_UI_REQUEST_HEADER,
  isUserTimingAPIPath,
  shouldMarkAdminUIRequest,
  shouldMarkUserUIRequest,
} from '@/api/adminUIRequest'

describe('Admin UI request marker', () => {
  it('uses the stable request header name', () => {
    expect(ADMIN_UI_REQUEST_HEADER).toBe('X-Admin-UI-Request')
  })

  it.each([
    '/admin',
    '/admin/users',
    '/api/v1/admin',
    '/api/v1/admin/accounts?status=active',
    'https://api.example.test/api/v1/admin/dashboard',
  ])('marks Admin API request %s before page navigation', (requestURL) => {
    expect(shouldMarkAdminUIRequest(requestURL, '/login')).toBe(true)
  })

  it.each(['/keys', '/groups/available', '/auth/me'])(
    'marks shared request %s while an Admin page is active',
    (requestURL) => {
      expect(shouldMarkAdminUIRequest(requestURL, '/admin/dashboard')).toBe(true)
    }
  )

  it.each([
    ['/keys', '/dashboard'],
    ['/api/v1/administer', '/dashboard'],
    ['/keys', '/administrator'],
    ['', '/'],
  ])('does not mark request %s on page %s', (requestURL, pagePath) => {
    expect(shouldMarkAdminUIRequest(requestURL, pagePath)).toBe(false)
  })
})

describe('authenticated panel request marker', () => {
  it('uses the stable request header name', () => {
    expect(USER_UI_REQUEST_HEADER).toBe('X-User-UI-Request')
  })

  it.each([
    '/auth/me',
    '/auth/revoke-all-sessions',
    '/user/totp/status',
    '/user/api-keys/12/usage/daily',
    '/keys',
    '/keys/12',
    '/groups/available',
    '/api/v1/auth/me',
    '/api/v1/keys?page=1',
  ])('marks user timing API %s', (requestURL) => {
    expect(shouldMarkUserUIRequest(requestURL)).toBe(true)
    expect(isUserTimingAPIPath(requestURL)).toBe(true)
  })

  it.each([
    '/auth/login',
    '/settings/public',
    '/admin/users',
    '/groups',
    '/channels',
    '/auth/oauth/bind-token',
    '/user/profile',
    '/groups/rates',
    '/usage',
    '/announcements',
    '/redeem',
    '/subscriptions',
    '/channel-monitors',
    '/payment/orders',
    '/payment/public/orders/verify',
    '/payment/webhook/stripe',
    '/api/v1/payment/public/orders/resolve',
    '',
  ])('does not mark non-user timing API %s', (requestURL) => {
    expect(shouldMarkUserUIRequest(requestURL)).toBe(false)
  })
})
