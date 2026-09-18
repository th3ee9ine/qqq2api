import { describe, expect, it } from 'vitest'
import { formatCodexTurnStateDuration, inspectCodexTurnState, isValidCodexTurnStateToken, normalizeCodexTurnStateModels, refreshCodexTurnStateStatus } from '../codexTurnState'
const now = 1_800_000_000_000
function token(seconds: number, blocks = 10): string { const raw = new Uint8Array(57 + blocks * 16); raw[0] = 128; new DataView(raw.buffer).setBigUint64(1, BigInt(seconds)); return btoa(String.fromCharCode(...raw)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '') }
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
  it('validates printable ASCII token safety', () => { expect(isValidCodexTurnStateToken(' opaque=value / future format ')).toBe(true); expect(isValidCodexTurnStateToken('x'.repeat(4096))).toBe(true); expect(isValidCodexTurnStateToken('x'.repeat(4097))).toBe(false); expect(isValidCodexTurnStateToken('bad\r\nheader')).toBe(false); expect(isValidCodexTurnStateToken('不可')).toBe(false) })
  it('inspects Fernet envelope without authenticity checks', () => { expect(inspectCodexTurnState(token(now / 1000), now)).toMatchObject({ active: true, reason: 'ready', verdict: 'normal', blocks: 10, issued_at: now / 1000, expires_at: now / 1000 + 3600 }); expect(inspectCodexTurnState(token(now / 1000 - 3600), now)).toMatchObject({ active: true, reason: 'expired' }); expect(inspectCodexTurnState(token(now / 1000 + 120), now)).toMatchObject({ active: true, reason: 'future' }); expect(inspectCodexTurnState(token(now / 1000, 11), now)).toMatchObject({ active: true, reason: 'suspect' }); expect(inspectCodexTurnState('unknown', now)).toMatchObject({ active: true, reason: 'unknown' }) })
  it('gates only disabled, missing and unsafe headers', () => { expect(inspectCodexTurnState('opaque', now, false)).toMatchObject({ active: false, reason: 'disabled' }); expect(inspectCodexTurnState('', now)).toMatchObject({ active: false, reason: 'missing' }); expect(inspectCodexTurnState('bad\r\nheader', now)).toMatchObject({ active: false, reason: 'invalid' }) })
  it('refreshes expiry diagnostics as warning only', () => { const saved = inspectCodexTurnState(token(now / 1000), now); expect(refreshCodexTurnStateStatus(saved, true, now + 3_600_000)).toMatchObject({ active: true, reason: 'expired' }) })
  it('formats countdown durations', () => { expect(formatCodexTurnStateDuration(65_000)).toBe('01:05'); expect(formatCodexTurnStateDuration(-3_661_000)).toBe('1:01:01') })
})
