import type { UserRole } from '@/types'

export type PanelPermission =
  | 'super_admin'
  | 'accounts.manage'
  | 'proxies.manage'

const ACCOUNT_ADMIN_PERMISSIONS = new Set<PanelPermission>([
  'accounts.manage',
  'proxies.manage',
])

export function isSuperAdminRole(role: UserRole | null | undefined): boolean {
  return role === 'admin'
}

export function isAccountAdminRole(role: UserRole | null | undefined): boolean {
  return role === 'account_admin'
}

export function isPanelRole(role: UserRole | null | undefined): boolean {
  return isSuperAdminRole(role) || isAccountAdminRole(role)
}

export function hasPanelPermission(
  role: UserRole | null | undefined,
  permission: PanelPermission,
): boolean {
  if (isSuperAdminRole(role)) return true
  return isAccountAdminRole(role) && ACCOUNT_ADMIN_PERMISSIONS.has(permission)
}

export function defaultPanelPath(role: UserRole | null | undefined): string {
  if (isSuperAdminRole(role)) return '/admin/dashboard'
  if (isAccountAdminRole(role)) return '/admin/accounts'
  return '/login'
}
