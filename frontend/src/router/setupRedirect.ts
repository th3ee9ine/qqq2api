import type { UserRole } from '@/types'
import { defaultPanelPath } from '@/utils/accessControl'

export function resolveCompletedSetupRedirectPath(
  isAuthenticated: boolean,
  roleOrIsAdmin: UserRole | boolean | null | undefined,
): string {
  if (!isAuthenticated) {
    return '/login'
  }

  // Keep the boolean form compatible with callers/tests that predate roles.
  if (typeof roleOrIsAdmin === 'boolean') {
    return roleOrIsAdmin ? '/admin/dashboard' : '/login'
  }
  return defaultPanelPath(roleOrIsAdmin)
}
