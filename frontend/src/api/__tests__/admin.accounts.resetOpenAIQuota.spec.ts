import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { OpenAIQuotaResetResult } from '@/api/admin/accounts'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

const resetResult: OpenAIQuotaResetResult = {
  code: 'ok',
  windows_reset: 2,
  cache_refreshed: true,
  account_state_recovered: true
}

const firstUUID = '11111111-1111-4111-8111-111111111111'
const secondUUID = '22222222-2222-4222-8222-222222222222'
const thirdUUID = '33333333-3333-4333-8333-333333333333'

function storageKey(id: number): string {
  return `sub2api:admin:openai-quota-reset:${id}`
}

function requestKey(index: number): string {
  return post.mock.calls[index][2].headers['Idempotency-Key']
}

function deferredResponse() {
  let resolve!: (value: { data: OpenAIQuotaResetResult }) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<{ data: OpenAIQuotaResetResult }>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

describe('admin OpenAI quota reset idempotency', () => {
  beforeEach(() => {
    vi.resetModules()
    sessionStorage.clear()
    post.mockReset()
    post.mockResolvedValue({ data: resetResult })
    vi.spyOn(globalThis.crypto, 'randomUUID')
      .mockReturnValueOnce(firstUUID)
      .mockReturnValueOnce(secondUUID)
      .mockReturnValue(thirdUUID)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('shares one operation key for concurrent requests to the same account', async () => {
    const firstResponse = deferredResponse()
    const secondResponse = deferredResponse()
    post.mockReturnValueOnce(firstResponse.promise).mockReturnValueOnce(secondResponse.promise)
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')

    const first = resetOpenAIQuota(42)
    const second = resetOpenAIQuota(42)

    expect(post).toHaveBeenCalledTimes(2)
    expect(post).toHaveBeenNthCalledWith(1, '/admin/openai/accounts/42/reset-quota', undefined, {
      timeout: 90_000,
      headers: { 'Idempotency-Key': `openai-quota-reset-42-${firstUUID}` }
    })
    expect(requestKey(1)).toBe(requestKey(0))
    expect(globalThis.crypto.randomUUID).toHaveBeenCalledTimes(1)
    expect(sessionStorage.getItem(storageKey(42))).toBe(requestKey(0))

    secondResponse.resolve({ data: resetResult })
    firstResponse.resolve({ data: resetResult })
    await expect(Promise.all([first, second])).resolves.toEqual([resetResult, resetResult])
    expect(sessionStorage.getItem(storageKey(42))).toBeNull()
  })

  it('reuses a failed operation key and creates a new key only after success', async () => {
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')
    post.mockRejectedValueOnce(new Error('network timeout after upstream reset'))

    await expect(resetOpenAIQuota(42)).rejects.toThrow('network timeout after upstream reset')
    expect(sessionStorage.getItem(storageKey(42))).toBe(requestKey(0))
    await expect(resetOpenAIQuota(42)).resolves.toEqual(resetResult)

    expect(requestKey(1)).toBe(requestKey(0))
    expect(globalThis.crypto.randomUUID).toHaveBeenCalledTimes(1)
    expect(sessionStorage.getItem(storageKey(42))).toBeNull()

    await resetOpenAIQuota(42)
    expect(requestKey(2)).toBe(`openai-quota-reset-42-${secondUUID}`)
    expect(requestKey(2)).not.toBe(requestKey(0))
  })

  it('restores an ambiguous operation from session storage after the module reloads', async () => {
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')
    post.mockRejectedValueOnce(new Error('connection lost'))
    await expect(resetOpenAIQuota(42)).rejects.toThrow('connection lost')
    const originalKey = requestKey(0)

    vi.resetModules()
    const { resetOpenAIQuota: resetAfterReload } = await import('@/api/admin/accounts')
    await expect(resetAfterReload(42)).resolves.toEqual(resetResult)

    expect(requestKey(1)).toBe(originalKey)
    expect(globalThis.crypto.randomUUID).toHaveBeenCalledTimes(1)
    expect(sessionStorage.getItem(storageKey(42))).toBeNull()
  })

  it('does not let a delayed success clear a newer operation retry key', async () => {
    const firstResponse = deferredResponse()
    const delayedResponse = deferredResponse()
    const newerResponse = deferredResponse()
    post
      .mockReturnValueOnce(firstResponse.promise)
      .mockReturnValueOnce(delayedResponse.promise)
      .mockReturnValueOnce(newerResponse.promise)
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')

    const first = resetOpenAIQuota(42)
    const delayed = resetOpenAIQuota(42)
    expect(requestKey(1)).toBe(requestKey(0))
    firstResponse.resolve({ data: resetResult })
    await first

    const newer = resetOpenAIQuota(42)
    const newerKey = requestKey(2)
    expect(newerKey).not.toBe(requestKey(0))
    delayedResponse.resolve({ data: resetResult })
    await delayed
    expect(sessionStorage.getItem(storageKey(42))).toBe(newerKey)

    const newerFailure = expect(newer).rejects.toThrow('new operation timeout')
    newerResponse.reject(new Error('new operation timeout'))
    await newerFailure
    post.mockResolvedValueOnce({ data: resetResult })
    await expect(resetOpenAIQuota(42)).resolves.toEqual(resetResult)

    expect(requestKey(3)).toBe(newerKey)
    expect(globalThis.crypto.randomUUID).toHaveBeenCalledTimes(2)
    expect(sessionStorage.getItem(storageKey(42))).toBeNull()
  })

  it('keeps independent accounts and their retry state isolated', async () => {
    const firstResponse = deferredResponse()
    const otherResponse = deferredResponse()
    post.mockReturnValueOnce(firstResponse.promise).mockReturnValueOnce(otherResponse.promise)
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')

    const first = resetOpenAIQuota(42)
    const other = resetOpenAIQuota(43)
    const firstKey = requestKey(0)
    const otherKey = requestKey(1)
    expect(firstKey).toBe(`openai-quota-reset-42-${firstUUID}`)
    expect(otherKey).toBe(`openai-quota-reset-43-${secondUUID}`)

    firstResponse.resolve({ data: resetResult })
    await first
    expect(sessionStorage.getItem(storageKey(42))).toBeNull()
    expect(sessionStorage.getItem(storageKey(43))).toBe(otherKey)

    const otherFailure = expect(other).rejects.toThrow('account 43 timeout')
    otherResponse.reject(new Error('account 43 timeout'))
    await otherFailure
    await resetOpenAIQuota(43)
    expect(requestKey(2)).toBe(otherKey)
    expect(sessionStorage.getItem(storageKey(43))).toBeNull()

    await resetOpenAIQuota(42)
    expect(requestKey(3)).not.toBe(firstKey)
    expect(requestKey(3)).not.toBe(otherKey)
    expect(globalThis.crypto.randomUUID).toHaveBeenCalledTimes(3)
  })

  it('keeps in-memory retry protection when storage reads and writes throw', async () => {
    const inaccessibleStorage = {
      getItem: vi.fn(() => { throw new Error('storage access denied') }),
      setItem: vi.fn(() => { throw new Error('storage quota exceeded') }),
      removeItem: vi.fn(() => { throw new Error('storage access denied') })
    }
    vi.stubGlobal('sessionStorage', inaccessibleStorage)
    const { resetOpenAIQuota } = await import('@/api/admin/accounts')
    post.mockRejectedValueOnce(new Error('network timeout'))

    await expect(resetOpenAIQuota(42)).rejects.toThrow('network timeout')
    await expect(resetOpenAIQuota(42)).resolves.toEqual(resetResult)

    expect(requestKey(1)).toBe(requestKey(0))
    expect(inaccessibleStorage.getItem).toHaveBeenCalled()
    expect(inaccessibleStorage.setItem).toHaveBeenCalled()
    expect(inaccessibleStorage.removeItem).toHaveBeenCalled()
    await expect(resetOpenAIQuota(42)).resolves.toEqual(resetResult)
    expect(requestKey(2)).not.toBe(requestKey(0))
  })
})
