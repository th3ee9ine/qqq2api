import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

type NavigationGuard = (
  to: Record<string, any>,
  from: Record<string, any>,
  next: ReturnType<typeof vi.fn>
) => Promise<void>

const routerHarness = vi.hoisted(() => ({
  guard: null as NavigationGuard | null,
  routes: [] as Array<Record<string, any>>,
}))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: true,
  isAdmin: false,
  isAccountAdmin: false,
  isPanelOperator: true,
  panelHomePath: '/admin/dashboard',
  user: { role: 'admin' },
  hasPermission: vi.fn(() => true),
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  publicSettingsLoaded: false,
  cachedPublicSettings: null as null | {
    payment_enabled?: boolean
    risk_control_enabled?: boolean
    custom_menu_items?: []
  },
  fetchPublicSettings: vi.fn(),
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn((options: { routes: Array<Record<string, any>> }) => {
    routerHarness.routes = options.routes
    return {
      beforeEach: vi.fn((guard: NavigationGuard) => {
        routerHarness.guard = guard
      }),
      afterEach: vi.fn(),
      onError: vi.fn(),
    }
  }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

function runGuard(meta: Record<string, unknown>, path: string) {
  if (!routerHarness.guard) {
    throw new Error('router guard was not registered')
  }

  const next = vi.fn()
  const navigation = routerHarness.guard(
    {
      path,
      fullPath: path,
      name: 'FeatureRoute',
      params: {},
      meta: { requiresAuth: true, ...meta },
    },
    {},
    next
  )
  return { navigation, next }
}

describe('feature route guard', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = true
    authStore.isAccountAdmin = false
    authStore.isPanelOperator = true
    authStore.panelHomePath = '/admin/dashboard'
    authStore.user = { role: 'admin' }
    authStore.hasPermission.mockReturnValue(true)
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = false
    appStore.cachedPublicSettings = null
    appStore.fetchPublicSettings.mockReset()
  })

  it('does not register model plaza and redirects its legacy URL', async () => {
    expect(routerHarness.routes.some((route) => route.path === '/model-plaza')).toBe(false)

    const { navigation, next } = runGuard({ requiresAuth: false }, '/model-plaza')
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith('/admin/dashboard')
  })

  it('registers system-wide admin usage as an administrator-only route', async () => {
    const usageRoute = routerHarness.routes.find((route) => route.path === '/admin/usage')

    expect(usageRoute).toMatchObject({
      name: 'AdminUsage',
      meta: {
        requiresAuth: true,
        requiresAdmin: true,
        titleKey: 'admin.usage.title',
        descriptionKey: 'admin.usage.description',
      },
    })

    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/usage')
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['/admin/accounts', 'accounts.manage'],
    ['/admin/proxies', 'proxies.manage'],
  ])('allows an account administrator to access %s', async (path, permission) => {
    authStore.isAdmin = false
    authStore.isAccountAdmin = true
    authStore.panelHomePath = '/admin/accounts'
    authStore.user = { role: 'account_admin' }
    authStore.hasPermission.mockImplementation((value: string) => value === permission)

    const route = routerHarness.routes.find((item) => item.path === path)
    expect(route?.meta?.requiredPermission).toBe(permission)

    const { navigation, next } = runGuard({ requiredPermission: permission }, path)
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it('keeps account-administrator management exclusive to super administrators', async () => {
    authStore.isAdmin = false
    authStore.isAccountAdmin = true
    authStore.panelHomePath = '/admin/accounts'
    authStore.user = { role: 'account_admin' }

    const route = routerHarness.routes.find((item) => item.path === '/admin/account-admins')
    expect(route).toMatchObject({
      name: 'AdminAccountAdmins',
      meta: { requiresAuth: true, requiresAdmin: true },
    })

    const { navigation, next } = runGuard({ requiresAdmin: true }, '/admin/account-admins')
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith('/admin/accounts')
  })

  it.each([
    ['risk control', { requiresRiskControl: true }, '/admin/risk-control'],
  ])('does not treat a failed %s settings load as explicitly disabled', async (_name, meta, path) => {
    appStore.fetchPublicSettings.mockResolvedValue(null)

    const { navigation, next } = runGuard(meta, path)
    await navigation

    expect(appStore.publicSettingsLoaded).toBe(false)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    [
      'risk control',
      { requiresRiskControl: true },
      { risk_control_enabled: false },
      '/admin/settings',
    ],
  ])('redirects when loaded settings explicitly disable %s', async (_name, meta, settings, target) => {
    authStore.isAdmin = true
    appStore.cachedPublicSettings = settings
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(meta, '/feature')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
  })
})
