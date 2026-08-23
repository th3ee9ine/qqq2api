import { describe, expect, it } from 'vitest'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'

describe('GroupsView Composite route options', () => {
  it('offers only retained platforms as route targets', () => {
    expect(CONCRETE_PLATFORM_OPTIONS.map((option) => option.value)).toEqual([
      'anthropic',
      'openai',
    ])
  })
})
