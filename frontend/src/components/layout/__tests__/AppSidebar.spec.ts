import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar administrator navigation', () => {
  it('links to the system-wide usage records page', () => {
    expect(componentSource).toContain("{ path: '/admin/usage', label: t('nav.usage'), icon: 'chart' }")
  })

  it('marks only account and proxy navigation as available to restricted operators', () => {
    expect(componentSource).toContain("path: '/admin/accounts'")
    expect(componentSource).toContain("requiredPermission: 'accounts.manage'")
    expect(componentSource).toContain("path: '/admin/proxies'")
    expect(componentSource).toContain("requiredPermission: 'proxies.manage'")
    expect(componentSource).toContain("path: '/admin/account-admins'")
    expect(componentSource).toContain(': authStore.isAdmin)')

    const restrictedOperatorPaths = Array.from(
      componentSource.matchAll(/\{ path: '([^']+)'[^\n]*requiredPermission: '[^']+'/g),
      (match) => match[1],
    )
    expect(restrictedOperatorPaths).toEqual(['/admin/accounts', '/admin/proxies'])
  })

  it('keeps the original retained navigation visuals and ordering', () => {
    expect(componentSource).toContain("{ path: '/admin/groups', label: t('nav.groups'), icon: 'folder'")
    expect(componentSource).toContain('name="chevronDoubleLeft"')
    expect(componentSource).toContain('name="chevronDoubleRight"')
    expect(componentSource).toContain(':aria-hidden="sidebarCollapsed ? \'true\' : \'false\'"')
    expect(componentSource).toContain(":data-tour=\"item.path === '/keys' ? 'sidebar-my-keys' : undefined\"")

    const usageIndex = componentSource.indexOf("path: '/admin/usage'")
    const keysIndex = componentSource.indexOf("path: '/keys'")
    const settingsIndex = componentSource.indexOf("path: '/admin/settings'")
    expect(usageIndex).toBeGreaterThan(-1)
    expect(keysIndex).toBeGreaterThan(usageIndex)
    expect(settingsIndex).toBeGreaterThan(keysIndex)
  })

  it('does not restore retired self-service navigation', () => {
    for (const path of [
      '/admin/users',
      '/admin/subscriptions',
      '/admin/channels',
      '/admin/announcements',
      '/admin/redeem',
      '/admin/promo-codes',
      '/subscriptions',
      '/redeem',
      '/profile'
    ]) {
      expect(componentSource).not.toContain(`path: '${path}'`)
    }
  })
})

describe('AppSidebar header styles', () => {
  it('keeps the original collapse animation styles', () => {
    expect(componentSource).toContain('.sidebar-header-collapsed {')
    expect(componentSource).toContain('.sidebar-brand-collapsed {')
    expect(componentSource).toContain('.sidebar-link-collapsed {')
    expect(componentSource).toContain('.sidebar-label-collapsed {')
  })

  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})
