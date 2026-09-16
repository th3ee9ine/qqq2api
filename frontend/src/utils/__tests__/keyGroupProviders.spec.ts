import { describe, expect, it } from 'vitest'
import { getKeyGroupProvider, KEY_GROUP_PROVIDERS, KEY_GROUP_PROVIDER_ICONS } from '../keyGroupProviders'

describe('custom API key provider filters', () => {
  it('classifies retained groups by platform rather than their display name', () => {
    expect(getKeyGroupProvider('anthropic')).toBe('anthropic')
    expect(getKeyGroupProvider('openai')).toBe('openai')
    expect(getKeyGroupProvider('composite')).toBe('other')
    expect(KEY_GROUP_PROVIDERS).toEqual(['anthropic', 'openai', 'other'])
    expect(KEY_GROUP_PROVIDER_ICONS.other).toEqual(['anthropic', 'openai'])
  })

  it.each(['gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'minimax', 'opencode_go'] as const)(
    'keeps legacy %s groups out of the provider selector',
    (platform) => expect(getKeyGroupProvider(platform)).toBeNull()
  )
})
