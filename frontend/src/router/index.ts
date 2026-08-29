/**
 * Vue Router configuration for Sub2API frontend
 * Defines all application routes with lazy loading and navigation guards
 */

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { useNavigationLoadingState } from '@/composables/useNavigationLoading'
import { useRoutePrefetch } from '@/composables/useRoutePrefetch'
import { getSetupStatus } from '@/api/setup'
import { resolveCompletedSetupRedirectPath } from './setupRedirect'
import { resolveRouteDocumentTitle } from './title'

/**
 * Route definitions with lazy loading
 */
const routes: RouteRecordRaw[] = [
  // ==================== Setup Routes ====================
  {
    path: '/setup',
    name: 'Setup',
    component: () => import('@/views/setup/SetupWizardView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Setup'
    }
  },

  // ==================== Public Routes ====================
  {
    path: '/home',
    name: 'Home',
    component: () => import('@/views/HomeView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Home'
    }
  },
  {
    path: '/login',
    name: 'Login',
    component: () => import('@/views/auth/LoginView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Login',
      titleKey: 'home.login'
    }
  },
  {
    path: '/key-usage',
    name: 'KeyUsage',
    component: () => import('@/views/KeyUsageView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Key Usage',
    }
  },
  {
    path: '/legal/:documentId',
    name: 'LegalDocument',
    component: () => import('@/views/public/LegalDocumentView.vue'),
    meta: {
      requiresAuth: false,
      title: 'Legal Document'
    }
  },
  // ==================== Shared administrator tools ====================
  {
    path: '/',
    redirect: '/home'
  },
  {
    path: '/keys',
    name: 'Keys',
    component: () => import('@/views/user/KeysView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'API Keys',
      titleKey: 'keys.title',
      descriptionKey: 'keys.description'
    }
  },
  // ==================== Admin Routes ====================
  {
    path: '/admin',
    redirect: '/admin/dashboard'
  },
  {
    path: '/admin/dashboard',
    name: 'AdminDashboard',
    component: () => import('@/views/admin/DashboardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Admin Dashboard',
      titleKey: 'admin.dashboard.title',
      descriptionKey: 'admin.dashboard.description'
    }
  },
  {
    path: '/admin/ops',
    name: 'AdminOps',
    component: () => import('@/views/admin/ops/OpsDashboard.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Ops Monitoring',
      titleKey: 'admin.ops.title',
      descriptionKey: 'admin.ops.description'
    }
  },
  {
    path: '/admin/audit-logs',
    name: 'AdminAuditLogs',
    component: () => import('@/views/admin/AuditLogView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Audit Logs',
      titleKey: 'admin.audit.title',
      descriptionKey: 'admin.audit.description'
    }
  },
  {
    path: '/admin/groups',
    name: 'AdminGroups',
    component: () => import('@/views/admin/GroupsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Group Management',
      titleKey: 'admin.groups.title',
      descriptionKey: 'admin.groups.description'
    }
  },
  {
    path: '/admin/accounts',
    name: 'AdminAccounts',
    component: () => import('@/views/admin/AccountsView.vue'),
    meta: {
      requiresAuth: true,
      requiredPermission: 'accounts.manage',
      title: 'Account Management',
      titleKey: 'admin.accounts.title',
      descriptionKey: 'admin.accounts.description'
    }
  },
  {
    path: '/admin/proxies',
    name: 'AdminProxies',
    component: () => import('@/views/admin/ProxiesView.vue'),
    meta: {
      requiresAuth: true,
      requiredPermission: 'proxies.manage',
      title: 'Proxy Management',
      titleKey: 'admin.proxies.title',
      descriptionKey: 'admin.proxies.description'
    }
  },
  {
    path: '/admin/settings',
    name: 'AdminSettings',
    component: () => import('@/views/admin/SettingsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'System Settings',
      titleKey: 'admin.settings.title',
      descriptionKey: 'admin.settings.description'
    }
  },
  {
    path: '/admin/account-admins',
    name: 'AdminAccountAdmins',
    component: () => import('@/views/admin/AccountAdminsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Account Administrators',
      titleKey: 'admin.accountAdmins.title',
      descriptionKey: 'admin.accountAdmins.description'
    }
  },
  {
    path: '/admin/risk-control',
    name: 'AdminRiskControl',
    component: () => import('@/views/admin/RiskControlView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Risk Control',
      titleKey: 'admin.riskControl.title',
      descriptionKey: 'admin.riskControl.description',
      requiresRiskControl: true
    }
  },
  {
    path: '/admin/prompt-audit',
    name: 'AdminPromptAudit',
    component: () => import('@/features/prompt-audit/PromptAuditView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Prompt Audit',
      titleKey: 'admin.promptAudit.title',
      descriptionKey: 'admin.promptAudit.description',
      requiresRiskControl: true
    }
  },
  {
    path: '/admin/jailbreak-guard',
    name: 'AdminJailbreakGuard',
    component: () => import('@/features/prompt-audit/JailbreakGuardView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Jailbreak Guard',
      titleKey: 'admin.promptAudit.localGuard.title',
      descriptionKey: 'admin.promptAudit.localGuard.pageDescription',
      requiresRiskControl: true
    }
  },
  {
    path: '/admin/usage',
    name: 'AdminUsage',
    component: () => import('@/views/admin/UsageView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Usage Records',
      titleKey: 'admin.usage.title',
      descriptionKey: 'admin.usage.description'
    }
  },
  // ==================== 404 Not Found ====================
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: () => import('@/views/NotFoundView.vue'),
    meta: {
      title: '404 Not Found'
    }
  }
]

/**
 * Create router instance
 */
const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes,
  scrollBehavior(_to, _from, savedPosition) {
    // Scroll to saved position when using browser back/forward
    if (savedPosition) {
      return savedPosition
    }
    // Scroll to top for new routes
    return { top: 0 }
  }
})

/**
 * Navigation guard: Authentication check
 */
let authInitialized = false

// 初始化导航加载状态和预加载
const navigationLoading = useNavigationLoadingState()
// 延迟初始化预加载，传入 router 实例
let routePrefetch: ReturnType<typeof useRoutePrefetch> | null = null
const BACKEND_MODE_ALLOWED_PATHS = ['/login', '/key-usage', '/setup', '/legal']
// These legacy paths are intentionally kept out of the route table. Redirect
// them for users with old bookmarks instead of exposing a dead page.
const DISABLED_FEATURE_PATHS = [
  '/admin/subscriptions',
  '/admin/promo-codes',
  '/admin/announcements',
  '/admin/channels',
  '/admin/channels/pricing',
  '/admin/channels/monitor',
  '/admin/users',
  '/admin/redeem',
  '/admin/affiliates',
  '/admin/plugins',
  '/model-plaza',
  '/redeem',
  '/subscriptions',
  '/monitor',
]

// Keep retired self-service URLs recognizable so old bookmarks fail closed
// instead of rendering the removed end-user surface.
const REMOVED_USER_PATHS = [
  '/dashboard',
  '/usage',
  '/affiliate',
  '/available-channels',
  '/profile',
  '/purchase',
  '/orders',
  '/payment',
  '/custom',
]
const REMOVED_AUTH_PATHS = [
  '/register',
  '/email-verify',
  '/forgot-password',
  '/reset-password',
  '/auth',
]

function isDisabledFeaturePath(path: string): boolean {
  const normalizedPath = path.length > 1 ? path.replace(/\/+$/, '') : path
  return DISABLED_FEATURE_PATHS.some((disabledPath) => {
    return normalizedPath === disabledPath || normalizedPath.startsWith(`${disabledPath}/`)
  })
}

function isBackendModePublicRouteAllowed(path: string): boolean {
  if (BACKEND_MODE_ALLOWED_PATHS.some((allowedPath) => path === allowedPath || path.startsWith(`${allowedPath}/`))) {
    return true
  }
  return false
}

function isPathInList(path: string, prefixes: string[]): boolean {
  return prefixes.some((prefix) => path === prefix || path.startsWith(`${prefix}/`))
}

function adminOrLoginRedirect(authStore: ReturnType<typeof useAuthStore>): string {
  return authStore.isAuthenticated ? authStore.panelHomePath : '/login'
}

router.beforeEach(async (to, _from, next) => {
  // 开始导航加载状态
  navigationLoading.startNavigation()

  const authStore = useAuthStore()

  // Restore auth state from localStorage on first navigation (page refresh)
  if (!authInitialized) {
    authStore.checkAuth()
    authInitialized = true
  }

  // Set page title
  const appStore = useAppStore()
  const adminSettingsStore = useAdminSettingsStore()
  const customMenuItems = [
    ...(appStore.cachedPublicSettings?.custom_menu_items ?? []),
    ...(authStore.isAdmin ? adminSettingsStore.customMenuItems : []),
  ]
  document.title = resolveRouteDocumentTitle(to, appStore.siteName, customMenuItems)

  // Check if route requires authentication
  const requiresAuth = to.meta.requiresAuth !== false // Default to true
  const requiresAdmin = to.meta.requiresAdmin === true
  const requiredPermission = to.meta.requiredPermission

  // Removed user and self-service authentication pages are redirected before
  // their lazy components can load. This also handles legacy deep links.
  if (isDisabledFeaturePath(to.path) || isPathInList(to.path, REMOVED_USER_PATHS) || isPathInList(to.path, REMOVED_AUTH_PATHS)) {
    next(adminOrLoginRedirect(authStore))
    return
  }

  if (to.path === '/setup') {
    try {
      const status = await getSetupStatus()
      if (!status.needs_setup) {
        next(resolveCompletedSetupRedirectPath(authStore.isAuthenticated, authStore.user?.role))
        return
      }
    } catch {
      // If setup status cannot be determined, keep the setup page reachable.
    }
  }

  // If route doesn't require auth, allow access
  if (!requiresAuth) {
    // If already authenticated and trying to access login/register, redirect to appropriate dashboard
    if (authStore.isAuthenticated && to.path === '/login') {
      // A stale legacy non-admin token must not expose a panel route. Leave the
      // login form reachable so the administrator can sign in again.
      next(authStore.panelHomePath)
      return
    }
    // Backend mode: block public pages for unauthenticated users (except login, key-usage, setup)
    if (appStore.backendModeEnabled && !authStore.isAuthenticated) {
      const isAllowed = isBackendModePublicRouteAllowed(to.path)
      if (!isAllowed) {
        next('/login')
        return
      }
    }
    next()
    return
  }

  // Route requires authentication
  if (!authStore.isAuthenticated) {
    // Not authenticated, redirect to login
    next({
      path: '/login',
      query: { redirect: to.fullPath } // Save intended destination
    })
    return
  }

  // Ordinary users and unknown roles must never enter the management panel.
  if (!authStore.isPanelOperator) {
    next('/login')
    return
  }

  // Check admin requirement
  if (requiresAdmin && !authStore.isAdmin) {
    next(authStore.panelHomePath)
    return
  }

  if (requiredPermission && !authStore.hasPermission(requiredPermission)) {
    next(authStore.panelHomePath)
    return
  }

  // 公共设置可能尚未加载（App.vue 的 onMounted 异步拉取晚于首次导航，且纯静态部署
  // 无 __APP_CONFIG__ 注入）。此时 cachedPublicSettings 为空会把 payment/risk_control
  // 误判为“未启用”而错误拦截，故这里先确保设置加载完成。
  if ((to.meta.requiresPayment || to.meta.requiresRiskControl) && !appStore.publicSettingsLoaded) {
    try {
      await appStore.fetchPublicSettings()
    } catch (error) {
      console.warn('Failed to load public settings in route guard', error)
    }
  }

  // Only an explicit value from successfully loaded settings can disable a route.
  // A transient settings failure is unknown state, not a confirmed feature toggle.
  if (
    to.meta.requiresPayment &&
    appStore.publicSettingsLoaded &&
    appStore.cachedPublicSettings?.payment_enabled === false
  ) {
    next('/admin/dashboard')
    return
  }

  if (
    to.meta.requiresRiskControl &&
    appStore.publicSettingsLoaded &&
    appStore.cachedPublicSettings?.risk_control_enabled === false
  ) {
    next('/admin/settings')
    return
  }

  // 简易模式下限制访问某些页面
  if (authStore.isSimpleMode) {
    const restrictedPaths = ['/admin/groups']

    if (restrictedPaths.some((path) => to.path.startsWith(path))) {
      // 简易模式下访问受限页面,重定向到仪表板
      next('/admin/dashboard')
      return
    }
  }

  // Backend mode: authenticated panel operators may use their permitted routes.
  if (appStore.backendModeEnabled) {
    if (authStore.isAuthenticated && authStore.isPanelOperator) {
      next()
      return
    }
    const isAllowed = isBackendModePublicRouteAllowed(to.path)
    if (!isAllowed) {
      next('/login')
      return
    }
  }

  // All checks passed, allow navigation
  next()
})

/**
 * Navigation guard: End loading and trigger prefetch
 */
router.afterEach((to) => {
  // 结束导航加载状态
  navigationLoading.endNavigation()

  // 懒初始化预加载（首次导航时创建，传入 router 实例）
  if (!routePrefetch) {
    routePrefetch = useRoutePrefetch(router)
  }
  // 触发路由预加载（在浏览器空闲时执行）
  routePrefetch.triggerPrefetch(to)
})

/**
 * Navigation guard: Error handling
 * Handles dynamic import failures caused by deployment updates
 */
router.onError((error) => {
  console.error('Router error:', error)

  // Check if this is a dynamic import failure (chunk loading error)
  const isChunkLoadError =
    error.message?.includes('Failed to fetch dynamically imported module') ||
    error.message?.includes('Loading chunk') ||
    error.message?.includes('Loading CSS chunk') ||
    error.name === 'ChunkLoadError'

  if (isChunkLoadError) {
    // Avoid infinite reload loop by checking sessionStorage
    const reloadKey = 'chunk_reload_attempted'
    const lastReload = sessionStorage.getItem(reloadKey)
    const now = Date.now()

    // Allow reload if never attempted or more than 10 seconds ago
    if (!lastReload || now - parseInt(lastReload) > 10000) {
      sessionStorage.setItem(reloadKey, now.toString())
      console.warn('Chunk load error detected, reloading page to fetch latest version...')
      window.location.reload()
    } else {
      console.error('Chunk load error persists after reload. Please clear browser cache.')
    }
  }
})

export default router
