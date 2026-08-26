import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/views/admin/ProxiesView.vue'),
  'utf8',
)

describe('proxy credential export step-up', () => {
  it('runs the sensitive export through TOTP step-up', () => {
    expect(source).toContain('const proxyExportStepUp = useStepUp()')
    expect(source).toContain('proxyExportStepUp.run(() =>')
    expect(source).toContain('adminAPI.proxies.exportData(')
    expect(source).toContain('<TotpStepUpDialog :controller="proxyExportStepUp" />')
  })

  it('handles cancelled and blocked step-up attempts without a generic export error', () => {
    expect(source).toContain('isStepUpCancelled(error)')
    expect(source).toContain('isStepUpBlocked(error)')
    expect(source).toContain("stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'")
  })
})
