import { describe, expect, it } from 'vitest'
import { normalizeCodexTurnStateModels } from '../codexTurnState'
describe('Codex Turn State system settings helpers', () => {
  it('normalizes model scopes', () => { expect(normalizeCodexTurnStateModels(' GPT-5 , gpt-5\n Codex/*, codex/* ')).toBe('gpt-5,codex/*'); expect(normalizeCodexTurnStateModels('')).toBe(''); expect(normalizeCodexTurnStateModels('*')).toBe('*') })
  it.each(['foo**', 'bad value', 'foo*bar', 'gpt-5\tfoo', 'a"b', 'a'.repeat(1025), Array.from({ length: 10 }, (_, i) => String(i) + 'x'.repeat(110)).join(',')])('rejects malformed scopes', input => { expect(() => normalizeCodexTurnStateModels(input)).toThrow('invalid_models') })
  it.each(['gpt-😀', '中文', '日本語', '한국어', 'gpt@5', '<model>', 'model+fast', 'model\\path'])('rejects characters outside the backend whitelist: %s', input => {
    expect(() => normalizeCodexTurnStateModels(input)).toThrow('invalid_models')
  })
  it('accepts every backend model character and the normalized length boundary', () => {
    expect(normalizeCodexTurnStateModels(' ORG/GPT-5.4_FAST:V1*, * ')).toBe('org/gpt-5.4_fast:v1*,*')
    expect(normalizeCodexTurnStateModels('a'.repeat(1024))).toHaveLength(1024)
  })
})
