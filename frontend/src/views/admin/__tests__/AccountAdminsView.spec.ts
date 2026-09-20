import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const viewPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AccountAdminsView.vue')
const viewSource = readFileSync(viewPath, 'utf8')

describe('AccountAdminsView security-sensitive actions', () => {
  it('generates credentials with browser cryptography rather than Math.random', () => {
    expect(viewSource).toContain('globalThis.crypto.getRandomValues')
    expect(viewSource).toContain('maxUnbiasedByte')
    expect(viewSource).not.toContain('Math.random')
  })

  it('requires TOTP step-up for create, update, status, and delete mutations', () => {
    expect(viewSource).toContain('stepUp.run(() => adminAPI.accountAdmins.create(')
    expect(viewSource).toContain('stepUp.run(() => adminAPI.accountAdmins.update(editingAccountAdmin.value!.id')
    expect(viewSource).toContain('stepUp.run(() => adminAPI.accountAdmins.update(accountAdmin.id, { status }))')
    expect(viewSource).toContain('stepUp.run(() => adminAPI.accountAdmins.remove(target.id))')
  })

  it('edits and displays the account administrator supply rate multiplier', () => {
    expect(viewSource).toContain('v-model.number="form.supply_rate_multiplier"')
    expect(viewSource).toContain("supply_rate_multiplier: accountAdmin.supply_rate_multiplier ?? 1")
    expect(viewSource).toContain("{ key: 'supply_rate_multiplier'")
    expect(viewSource).toContain('formatMultiplier(Number(value ?? 1))')
    expect(viewSource).toContain('supply_rate_multiplier: supplyRateMultiplier')
  })

  it('rejects invalid supply rate multipliers before submitting', () => {
    expect(viewSource).toContain('Number.isFinite(supplyRateMultiplier)')
    expect(viewSource).toContain('supplyRateMultiplier < 0')
    expect(viewSource).toContain("showError(t('admin.accountAdmins.supplyRateMultiplierInvalid'))")
  })
})
