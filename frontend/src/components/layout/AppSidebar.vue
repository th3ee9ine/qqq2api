<template>
  <aside
    class="sidebar"
    :class="[
      sidebarCollapsed ? 'w-[72px]' : 'w-64',
      { '-translate-x-full lg:translate-x-0': !mobileOpen }
    ]"
  >
    <!-- Logo/Brand -->
    <div class="sidebar-header" :class="{ 'sidebar-header-collapsed': sidebarCollapsed }">
      <!-- Custom Logo or Default Logo -->
      <router-link
        :to="homePath"
        class="sidebar-logo flex h-9 w-9 items-center justify-center overflow-hidden rounded-xl shadow-glow transition-opacity hover:opacity-80"
        @click="handleMenuItemClick(homePath)"
      >
        <img v-if="settingsLoaded" :src="siteLogo || '/logo.svg'" alt="Logo" class="h-full w-full object-contain" />
      </router-link>
      <div class="sidebar-brand" :class="{ 'sidebar-brand-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">
        <router-link
          :to="homePath"
          class="sidebar-brand-title text-lg font-bold text-gray-900 transition-colors hover:text-primary-600 dark:text-white dark:hover:text-primary-400"
          @click="handleMenuItemClick(homePath)"
        >
          {{ siteName }}
        </router-link>
        <!-- Version Badge -->
        <VersionBadge :version="siteVersion" />
      </div>
    </div>

    <!-- Navigation -->
    <nav ref="sidebarNavRef" class="sidebar-nav scrollbar-hide">
      <!-- Administrator navigation. User/self-service sections were removed. -->
      <div v-if="isPanelOperator" class="sidebar-section">
        <template v-for="item in adminNavItems" :key="item.path">
          <!-- Collapsible group (has children) -->
          <template v-if="item.children?.length">
            <button
              type="button"
              class="sidebar-link mb-1 w-full"
              :class="{
                'sidebar-link-active': isGroupActive(item) && !isGroupExpanded(item),
                'sidebar-link-collapsed': sidebarCollapsed
              }"
              :title="sidebarCollapsed ? item.label : undefined"
              @click="handleGroupClick(item)"
            >
              <Icon :name="item.icon" size="md" class="flex-shrink-0" />
              <span
                class="sidebar-label sidebar-label-flex"
                :class="{ 'sidebar-label-collapsed': sidebarCollapsed }"
                :aria-hidden="sidebarCollapsed ? 'true' : 'false'"
              >
                <span class="min-w-0 truncate">{{ item.label }}</span>
                <Icon
                  name="chevronDown"
                  size="sm"
                  class="flex-shrink-0 transition-transform duration-200"
                  :class="isGroupExpanded(item) ? 'rotate-180' : ''"
                />
              </span>
            </button>
            <!-- Children -->
            <div v-if="!sidebarCollapsed && isGroupExpanded(item)" class="mb-1 ml-4 border-l border-gray-200 pl-2 dark:border-dark-600">
              <router-link
                v-for="child in item.children"
                :key="child.path"
                :to="child.path"
                class="sidebar-link mb-0.5 py-1.5 text-sm"
                :class="{ 'sidebar-link-active': route.path === child.path }"
                @click="handleMenuItemClick(child.path)"
              >
                <Icon :name="child.icon" size="sm" class="flex-shrink-0" />
                <span>{{ child.label }}</span>
              </router-link>
            </div>
          </template>
          <!-- Normal item (no children) -->
          <router-link
            v-else
            :to="item.path"
            class="sidebar-link mb-1"
            :class="{ 'sidebar-link-active': isActive(item.path), 'sidebar-link-collapsed': sidebarCollapsed }"
            :title="sidebarCollapsed ? item.label : undefined"
            :data-tour="item.path === '/keys' ? 'sidebar-my-keys' : undefined"
            :id="
              item.path === '/admin/accounts'
                ? 'sidebar-channel-manage'
                : item.path === '/admin/groups'
                  ? 'sidebar-group-manage'
                  : undefined
            "
            @click="handleMenuItemClick(item.path)"
          >
            <span v-if="item.iconSvg" class="h-5 w-5 flex-shrink-0 sidebar-svg-icon" v-html="sanitizeSvg(item.iconSvg)"></span>
            <Icon v-else :name="item.icon" size="md" class="flex-shrink-0" />
            <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ item.label }}</span>
          </router-link>
        </template>
      </div>
    </nav>

    <!-- Bottom Section -->
    <div class="mt-auto border-t border-gray-100 p-3 dark:border-dark-800">
      <!-- Theme Toggle -->
      <button
        @click="toggleTheme"
        class="sidebar-link mb-2 w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        :title="sidebarCollapsed ? (isDark ? t('nav.lightMode') : t('nav.darkMode')) : undefined"
      >
        <Icon v-if="isDark" name="sun" size="md" class="flex-shrink-0 text-amber-500" />
        <Icon v-else name="moon" size="md" class="flex-shrink-0" />
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{
          isDark ? t('nav.lightMode') : t('nav.darkMode')
        }}</span>
      </button>

      <!-- Collapse Button -->
      <button
        @click="toggleSidebar"
        class="sidebar-link w-full"
        :class="{ 'sidebar-link-collapsed': sidebarCollapsed }"
        :title="sidebarCollapsed ? t('nav.expand') : t('nav.collapse')"
      >
        <Icon v-if="!sidebarCollapsed" name="chevronDoubleLeft" size="md" class="flex-shrink-0" />
        <Icon v-else name="chevronDoubleRight" size="md" class="flex-shrink-0" />
        <span class="sidebar-label" :class="{ 'sidebar-label-collapsed': sidebarCollapsed }" :aria-hidden="sidebarCollapsed ? 'true' : 'false'">{{ t('nav.collapse') }}</span>
      </button>
    </div>
  </aside>

  <!-- Mobile Overlay -->
  <transition name="fade">
    <div
      v-if="mobileOpen"
      class="fixed inset-0 z-30 bg-black/50 lg:hidden"
      @click="closeMobile"
    ></div>
  </transition>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { useAdminSettingsStore, useAppStore, useAuthStore, useOnboardingStore } from '@/stores'
import VersionBadge from '@/components/common/VersionBadge.vue'
import Icon, { type IconName } from '@/components/icons/Icon.vue'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, makeSidebarFlag } from '@/utils/featureFlags'
import type { PanelPermission } from '@/utils/accessControl'

interface NavItem {
  path: string
  label: string
  icon: IconName
  iconSvg?: string
  hideInSimpleMode?: boolean
  featureFlag?: () => boolean | undefined
  requiredPermission?: PanelPermission
  children?: NavItem[]
  expandOnly?: boolean
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()
const onboardingStore = useOnboardingStore()

const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const mobileOpen = computed(() => appStore.mobileOpen)
const isAdmin = computed(() => authStore.isAdmin)
const isPanelOperator = computed(() => authStore.isPanelOperator)
const sidebarNavRef = ref<HTMLElement | null>(null)
const isDark = ref(document.documentElement.classList.contains('dark'))
const homePath = computed(() => authStore.panelHomePath)
const siteName = computed(() => appStore.siteName)
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteVersion = computed(() => appStore.siteVersion)
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)
const flagRiskControl = makeSidebarFlag(FeatureFlags.riskControl)
const flagOpsMonitoring = () => adminSettingsStore.opsMonitoringEnabled

const baseAdminNavItems = computed<NavItem[]>(() => [
  { path: '/admin/dashboard', label: t('nav.dashboard'), icon: 'grid' },
  { path: '/admin/ops', label: t('nav.ops'), icon: 'chart', featureFlag: flagOpsMonitoring },
  { path: '/admin/groups', label: t('nav.groups'), icon: 'folder', hideInSimpleMode: true },
  { path: '/admin/accounts', label: t('nav.accounts'), icon: 'globe', requiredPermission: 'accounts.manage' },
  { path: '/admin/proxies', label: t('nav.proxies'), icon: 'server', requiredPermission: 'proxies.manage' },
  { path: '/admin/account-admins', label: t('nav.accountAdmins'), icon: 'users' },
  {
    path: '/admin/security-audit', label: t('nav.securityAudit'), icon: 'shield', expandOnly: true, featureFlag: flagRiskControl,
    children: [
      { path: '/admin/risk-control', label: t('nav.contentModeration'), icon: 'shield' },
      { path: '/admin/prompt-audit', label: t('nav.promptAudit'), icon: 'shield' },
      { path: '/admin/jailbreak-guard', label: t('nav.jailbreakGuard'), icon: 'shield' },
    ],
  },
  { path: '/admin/usage', label: t('nav.usage'), icon: 'chart' },
  { path: '/admin/audit-logs', label: t('nav.auditLogs'), icon: 'shield', hideInSimpleMode: true },
  { path: '/keys', label: t('nav.apiKeys'), icon: 'key' },
  { path: '/admin/settings', label: t('nav.settings'), icon: 'cog' },
])

const adminNavItems = computed(() => {
  const filter = (items: NavItem[]): NavItem[] => items
    .filter((item) => item.requiredPermission
      ? authStore.hasPermission(item.requiredPermission)
      : authStore.isAdmin)
    .filter((item) => item.featureFlag?.() !== false)
    .filter((item) => !authStore.isSimpleMode || !item.hideInSimpleMode)
    .map((item) => item.children ? { ...item, children: filter(item.children) } : item)
  return filter(baseAdminNavItems.value)
})

const expandedGroups = ref<Set<string>>(new Set())

function toggleSidebar() { appStore.toggleSidebar() }
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}
function closeMobile() { appStore.setMobileOpen(false) }
function handleMenuItemClick(path: string) {
  if (mobileOpen.value) setTimeout(() => appStore.setMobileOpen(false), 150)
  const selectors: Record<string, string> = {
    '/admin/groups': '#sidebar-group-manage',
    '/admin/accounts': '#sidebar-channel-manage',
    '/keys': '[data-tour="sidebar-my-keys"]'
  }
  const selector = selectors[path]
  if (selector && onboardingStore.isCurrentStep(selector)) onboardingStore.nextStep(500)
}
function isActive(path: string) { return route.path === path || route.path.startsWith(`${path}/`) }
function isGroupActive(item: NavItem) { return !!item.children?.some((child) => route.path === child.path) }
function isGroupExpanded(item: NavItem) { return expandedGroups.value.has(item.path) || isGroupActive(item) }
function handleGroupClick(item: NavItem) {
  if (sidebarCollapsed.value) return
  if (item.expandOnly) {
    if (expandedGroups.value.has(item.path)) expandedGroups.value.delete(item.path)
    else expandedGroups.value.add(item.path)
    return
  }
  if (route.path !== item.path) void router.push(item.path)
  expandedGroups.value.add(item.path)
}

const savedTheme = localStorage.getItem('theme')
if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
  isDark.value = true
  document.documentElement.classList.add('dark')
}

watch(isAdmin, (value) => { if (value) void adminSettingsStore.fetch() }, { immediate: true })
onMounted(() => {
  if (isAdmin.value) void adminSettingsStore.fetch()
  void nextTick(() => {
    if (sidebarNavRef.value && appStore.sidebarScrollTop > 0) {
      sidebarNavRef.value.scrollTop = appStore.sidebarScrollTop
    }
  })
})
onBeforeUnmount(() => { if (sidebarNavRef.value) appStore.sidebarScrollTop = sidebarNavRef.value.scrollTop })
</script>

<style scoped>
.sidebar-logo {
  flex: 0 0 2.25rem;
  min-width: 2.25rem;
}

.sidebar-header-collapsed {
  gap: 0;
  padding-left: 1.125rem;
  padding-right: 1.125rem;
}

.sidebar-brand {
  min-width: 0;
  flex: 1 1 auto;
  white-space: nowrap;
  transition:
    max-width 0.22s ease,
    opacity 0.14s ease,
    transform 0.14s ease;
  max-width: 12rem;
}

.sidebar-brand-collapsed {
  max-width: 0;
  overflow: hidden;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

.sidebar-brand-title {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sidebar-link-collapsed {
  gap: 0;
  padding-left: 0.875rem;
  padding-right: 0.875rem;
}

.sidebar-label {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition:
    max-width 0.2s ease,
    opacity 0.12s ease,
    transform 0.12s ease;
  max-width: 12rem;
}

.sidebar-label-flex {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.sidebar-label-collapsed {
  max-width: 0;
  opacity: 0;
  transform: translateX(-4px);
  pointer-events: none;
}

/* Custom SVG icon in sidebar: constrain size without overriding uploaded SVG colors */
.sidebar-svg-icon {
  color: currentColor;
}

.sidebar-svg-icon :deep(svg) {
  display: block;
  width: 1.25rem;
  height: 1.25rem;
}
</style>
