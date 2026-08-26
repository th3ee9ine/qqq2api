import { describe, expect, it } from 'vitest'
import {
  defaultPanelPath,
  hasPanelPermission,
  isAccountAdminRole,
  isPanelRole,
  isSuperAdminRole,
} from '@/utils/accessControl'

describe('panel access control', () => {
  it('keeps admin as the only super administrator role', () => {
    expect(isSuperAdminRole('admin')).toBe(true)
    expect(isSuperAdminRole('account_admin')).toBe(false)
    expect(isAccountAdminRole('account_admin')).toBe(true)
  })

  it('accepts only administrative roles into the panel', () => {
    expect(isPanelRole('admin')).toBe(true)
    expect(isPanelRole('account_admin')).toBe(true)
    expect(isPanelRole('user')).toBe(false)
  })

  it('limits account administrators to accounts and proxies', () => {
    expect(hasPanelPermission('account_admin', 'accounts.manage')).toBe(true)
    expect(hasPanelPermission('account_admin', 'proxies.manage')).toBe(true)
    expect(hasPanelPermission('account_admin', 'super_admin')).toBe(false)
    expect(hasPanelPermission('admin', 'super_admin')).toBe(true)
  })

  it('returns a role-specific panel home path', () => {
    expect(defaultPanelPath('admin')).toBe('/admin/dashboard')
    expect(defaultPanelPath('account_admin')).toBe('/admin/accounts')
    expect(defaultPanelPath('user')).toBe('/login')
  })
})
