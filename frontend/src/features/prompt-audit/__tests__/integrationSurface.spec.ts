import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')

describe('Prompt Audit integration surface', () => {
  it('registers an admin and risk-control guarded route', () => {
    const router = read('../../../router/index.ts')
    expect(router).toContain("path: '/admin/prompt-audit'")
    const route = router.slice(router.indexOf("path: '/admin/prompt-audit'"), router.indexOf("path: '/admin/usage'"))
    expect(route).toContain('requiresAuth: true')
    expect(route).toContain('requiresAdmin: true')
    expect(route).toContain('requiresRiskControl: true')
  })

  it('keeps the legacy content moderation route and adds both pages under an expand-only security group', () => {
    const sidebar = read('../../../components/layout/AppSidebar.vue')
    const group = sidebar.slice(sidebar.indexOf("path: '/admin/security-audit'"), sidebar.indexOf("path: '/admin/redeem'"))
    expect(group).toContain('expandOnly: true')
    expect(group).toContain("path: '/admin/risk-control'")
    expect(group).toContain("path: '/admin/prompt-audit'")
    expect(group).toContain("path: '/admin/jailbreak-guard'")
    expect(group).toContain('nav.jailbreakGuard')
  })

  it('registers the dedicated jailbreak guard route with the same admin and risk-control gates', () => {
    const router = read('../../../router/index.ts')
    expect(router).toContain("path: '/admin/jailbreak-guard'")
    const route = router.slice(router.indexOf("path: '/admin/jailbreak-guard'"), router.indexOf("path: '/admin/usage'"))
    expect(route).toContain("JailbreakGuardView.vue")
    expect(route).toContain('requiresAuth: true')
    expect(route).toContain('requiresAdmin: true')
    expect(route).toContain('requiresRiskControl: true')
  })

  it('keeps Prompt Audit locale trees symmetric and all operational controls named', () => {
    expect(Object.keys(zh.admin.promptAudit)).toEqual(Object.keys(en.admin.promptAudit))
    expect(zh.nav.securityAudit).toBeTruthy()
    expect(en.nav.securityAudit).toBeTruthy()
    const endpoint = read('../components/EndpointPool.vue')
    const events = read('../components/EventWorkspace.vue')
    expect(endpoint).toContain('aria-label')
    expect(events).toContain('aria-label')
    expect(events).toContain('overflow-x-auto')
    expect(events).toContain('sm:grid-cols-2')
  })
})
