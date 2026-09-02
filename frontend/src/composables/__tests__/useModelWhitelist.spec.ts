import { describe, expect, it } from 'vitest'

import { buildModelMappingObject, getModelsByPlatform, splitModelMappingObject } from '../useModelWhitelist'

describe('useModelWhitelist', () => {
  it('returns the retained OpenAI model catalog', () => {
    const models = getModelsByPlatform('openai')

    expect(models).toContain('gpt-5.4')
    expect(models).toContain('gpt-5.4-mini')
    expect(models).toContain('gpt-5.4-2026-03-05')
    expect(models).toContain('codex-auto-review')
    expect(models).toContain('gpt-5.6')
  })

  it('returns the retained Anthropic model catalog', () => {
    const models = getModelsByPlatform('anthropic')

    expect(models).toContain('claude-fable-5')
    expect(models).toContain('claude-fable-5-1')
    expect(models).toContain('claude-opus-4-8')
  })

  it('returns no models for unsupported platforms', () => {
    expect(getModelsByPlatform('unsupported')).toEqual([])
  })

  it('whitelist mode ignores wildcard entries', () => {
    const mapping = buildModelMappingObject('whitelist', ['claude-*', 'gpt-5.4'], [])
    expect(mapping).toEqual({ 'gpt-5.4': 'gpt-5.4' })
  })

  it('whitelist mode preserves exact mappings', () => {
    const mapping = buildModelMappingObject('whitelist', ['gpt-5.4-2026-03-05'], [])

    expect(mapping).toEqual({
      'gpt-5.4-2026-03-05': 'gpt-5.4-2026-03-05'
    })
  })

  it('combined mode preserves whitelist and explicit mappings', () => {
    const mapping = buildModelMappingObject(
      'combined',
      ['gpt-5.4', 'claude-*'],
      [
        { from: 'gpt-latest', to: 'gpt-5.4' },
        { from: 'gpt-5.4', to: 'gpt-5.4-mini' }
      ]
    )

    expect(mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-mini',
      'gpt-latest': 'gpt-5.4'
    })
  })

  it('splitModelMappingObject restores identity mappings to the whitelist', () => {
    const parsed = splitModelMappingObject({
      'gpt-5.4': 'gpt-5.4',
      'gpt-latest': 'gpt-5.4',
      ' ': 'gpt-empty',
      broken: 123
    })

    expect(parsed).toEqual({
      allowedModels: ['gpt-5.4'],
      modelMappings: [{ from: 'gpt-latest', to: 'gpt-5.4' }]
    })
  })
})
