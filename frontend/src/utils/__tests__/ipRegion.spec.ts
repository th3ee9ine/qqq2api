import { describe, expect, it } from 'vitest'
import { getIpArea } from '@/utils/ipRegion'

describe('getIpArea', () => {
  it('classifies North and South American addresses', () => {
    expect(getIpArea('US')).toBe('northAmerica')
    expect(getIpArea('br')).toBe('southAmerica')
  })

  it('returns undefined for missing or unknown country codes', () => {
    expect(getIpArea()).toBeUndefined()
    expect(getIpArea('XX')).toBeUndefined()
  })
})
