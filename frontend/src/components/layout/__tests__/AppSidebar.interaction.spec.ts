import { mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useAdminSettingsStore, useAppStore, useAuthStore } from '@/stores'
import type { UserRole } from '@/types'
import AppSidebar from '../AppSidebar.vue'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

let wrapper: VueWrapper | undefined

beforeEach(() => {
  localStorage.clear()
  document.documentElement.classList.remove('dark')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
  document.body.classList.remove('sidebar-open')
  vi.restoreAllMocks()
})

async function mountSidebar({
  desktop = true,
  collapsed = false,
  role = 'admin' as UserRole,
  path = '/admin/dashboard'
} = {}) {
  vi.spyOn(window, 'matchMedia').mockImplementation((query) => ({
    matches: query === '(min-width: 1024px)' && desktop,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn()
  }))

  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore()
  app.setSidebarCollapsed(collapsed)
  app.$patch({ cachedPublicSettings: { risk_control_enabled: true } })
  const auth = useAuthStore()
  auth.user = {
    id: 1,
    username: 'operator',
    email: 'operator@example.com',
    role,
    balance: 0,
    concurrency: 0,
    status: 'active',
    allowed_groups: null,
    balance_notify_enabled: false,
    balance_notify_threshold: null,
    balance_notify_extra_emails: [],
    created_at: '2026-09-23',
    updated_at: '2026-09-23'
  }
  const settings = useAdminSettingsStore()
  vi.spyOn(settings, 'fetch').mockResolvedValue()

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }]
  })
  await router.push(path)
  await router.isReady()
  wrapper = mount(AppSidebar, {
    attachTo: document.body,
    global: {
      plugins: [pinia, router],
      stubs: { Icon: true, VersionBadge: true }
    }
  })
  await nextTick()
  return { app, router, sidebar: wrapper.get('aside'), view: wrapper }
}

describe('AppSidebar interactions', () => {
  it('expands a collapsed desktop sidebar and exposes all security audit children', async () => {
    const { app, router, sidebar, view } = await mountSidebar({ collapsed: true })
    const group = view.get('button[aria-controls="sidebar-group-security-audit"]')
    expect(sidebar.classes()).toContain('w-[72px]')
    expect(group.attributes('aria-expanded')).toBe('false')
    expect(view.find('#sidebar-group-security-audit').exists()).toBe(false)

    await group.trigger('click')

    expect(app.sidebarCollapsed).toBe(false)
    expect(sidebar.classes()).toContain('w-64')
    expect(group.attributes('aria-expanded')).toBe('true')
    expect(view.get('#sidebar-group-security-audit').findAll('a').map(link => link.attributes('href')))
      .toEqual(['/admin/risk-control', '/admin/prompt-audit', '/admin/jailbreak-guard'])
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('uses the full sidebar on mobile while preserving the desktop collapse preference', async () => {
    const { app, sidebar, view } = await mountSidebar({ desktop: false, collapsed: true })
    app.setMobileOpen(true)
    await nextTick()
    await nextTick()

    expect(sidebar.classes()).toContain('w-64')
    expect(sidebar.classes()).not.toContain('w-[72px]')
    expect(sidebar.find('.sidebar-label-collapsed').exists()).toBe(false)
    await view.get('button[aria-controls="sidebar-group-security-audit"]').trigger('click')
    expect(view.get('#sidebar-group-security-audit').findAll('a')).toHaveLength(3)
    expect(app.sidebarCollapsed).toBe(true)
    expect(app.mobileOpen).toBe(true)
  })

  it('closes the mobile sidebar with Escape and restores focus to its opener', async () => {
    const { app, sidebar } = await mountSidebar({ desktop: false })
    const opener = document.createElement('button')
    opener.textContent = 'Open navigation'
    document.body.append(opener)
    opener.focus()
    app.setMobileOpen(true)
    await nextTick()
    await nextTick()

    expect(sidebar.element.contains(document.activeElement)).toBe(true)
    expect(document.body.classList.contains('sidebar-open')).toBe(true)
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown', {
      key: 'Escape', bubbles: true, cancelable: true
    }))
    await nextTick()
    await nextTick()

    expect(app.mobileOpen).toBe(false)
    expect(document.body.classList.contains('sidebar-open')).toBe(false)
    expect(document.activeElement).toBe(opener)
  })

  it('makes a hidden mobile sidebar inert and accessible only while open', async () => {
    const { app, sidebar } = await mountSidebar({ desktop: false })
    expect(sidebar.attributes('aria-hidden')).toBe('true')
    expect(sidebar.attributes('inert')).toBeDefined()

    app.setMobileOpen(true)
    await nextTick()
    expect(sidebar.attributes('aria-hidden')).toBeUndefined()
    expect(sidebar.attributes('inert')).toBeUndefined()

    app.setMobileOpen(false)
    await nextTick()
    expect(sidebar.attributes('aria-hidden')).toBe('true')
    expect(sidebar.attributes('inert')).toBeDefined()
  })

  it('keeps the desktop sidebar interactive when the mobile drawer is closed', async () => {
    const { app, sidebar } = await mountSidebar()
    expect(app.mobileOpen).toBe(false)
    expect(sidebar.attributes('aria-hidden')).toBeUndefined()
    expect(sidebar.attributes('inert')).toBeUndefined()
  })

  it.each([true, false])('limits account administrators to accounts and proxies (desktop: %s)', async (desktop) => {
    const { view } = await mountSidebar({ desktop, role: 'account_admin', path: '/admin/accounts' })
    expect(view.get('nav').findAll('a').map(link => link.attributes('href')))
      .toEqual(['/admin/accounts', '/admin/proxies'])
    expect(view.find('button[aria-controls="sidebar-group-security-audit"]').exists()).toBe(false)
    expect(view.get('.sidebar-logo').attributes('href')).toBe('/admin/accounts')
  })
})
