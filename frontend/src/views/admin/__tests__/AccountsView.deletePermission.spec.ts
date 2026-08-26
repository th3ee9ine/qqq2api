import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/views/admin/AccountsView.vue'),
  'utf8',
)

describe('account deletion permissions', () => {
  it('only exposes single and bulk account deletion to the super administrator', () => {
    expect(source).toContain(':can-delete="authStore.isAdmin"')
    expect(source).toMatch(
      /<button v-if="authStore\.isAdmin" @click="handleDelete\(row\)"/,
    )
    expect(source).toContain('<ConfirmDialog v-if="authStore.isAdmin" :show="showDeleteDialog"')
  })

  it('fails closed if a restricted role triggers a delete handler indirectly', () => {
    expect(source).toMatch(
      /const handleBulkDelete = async \(\) => \{\s+if \(!authStore\.isAdmin\) return/,
    )
    expect(source).toMatch(
      /const handleDelete = \(a: Account\) => \{\s+if \(!authStore\.isAdmin\) return/,
    )
    expect(source).toMatch(
      /const confirmDelete = async \(\) => \{\s+if \(!authStore\.isAdmin \|\| !deletingAcc\.value\) return/,
    )
  })
})
