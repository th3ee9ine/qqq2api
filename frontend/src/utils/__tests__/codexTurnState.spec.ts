import { describe, expect, it } from 'vitest'
import {
  CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES,
  CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES,
  CODEX_TURN_STATE_PROXY_URL_LIMIT,
  isCodexTurnStateEligibleAccount,
  normalizeCodexTurnStateDefaultModel,
  normalizeCodexTurnStateModels,
  normalizeCodexTurnStateProxyUrls,
  parseCodexTurnStateProxyUrlBatch,
  validateCodexTurnStateProxyUrl,
} from '../codexTurnState'
import type { AccountListItem } from '@/types'

function account(overrides: Partial<AccountListItem> = {}): AccountListItem {
  return {
    id: 1,
    name: 'Healthy account',
    platform: 'openai',
    type: 'oauth',
    proxy_id: null,
    concurrency: 1,
    priority: 0,
    status: 'active',
    error_message: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null,
    codex_turn_state_auto: {
      configured: false,
      due: true,
      recovery_pending: false,
    },
    ...overrides,
  } as AccountListItem
}

describe('Codex Turn State system settings helpers', () => {
  it('normalizes model scopes', () => {
    expect(normalizeCodexTurnStateModels(' GPT-5 , gpt-5\n Codex/*, codex/* ')).toBe('gpt-5,codex/*')
    expect(normalizeCodexTurnStateModels('')).toBe('')
    expect(normalizeCodexTurnStateModels('*')).toBe('*')
  })

  it.each(['foo**', 'bad value', 'foo*bar', 'gpt-5\tfoo', 'a"b', 'a'.repeat(1025), Array.from({ length: 10 }, (_, i) => String(i) + 'x'.repeat(110)).join(',')])(
    'rejects malformed scopes: %s',
    input => {
      expect(() => normalizeCodexTurnStateModels(input)).toThrow('invalid_models')
    },
  )

  it.each(['gpt-😀', '中文', '日本語', '한국어', 'gpt@5', '<model>', 'model+fast', 'model\\path'])(
    'rejects characters outside the backend whitelist: %s',
    input => {
      expect(() => normalizeCodexTurnStateModels(input)).toThrow('invalid_models')
    },
  )

  it('accepts every backend model character and the normalized length boundary', () => {
    expect(normalizeCodexTurnStateModels(' ORG/GPT-5.4_FAST:V1*, * ')).toBe('org/gpt-5.4_fast:v1*,*')
    expect(normalizeCodexTurnStateModels('a'.repeat(1024))).toHaveLength(1024)
  })

  it('normalizes the default probe model and restores the built-in default', () => {
    expect(normalizeCodexTurnStateDefaultModel(' custom/probe-model ')).toBe('custom/probe-model')
    expect(normalizeCodexTurnStateDefaultModel('')).toBe('gpt-5.5')
    expect(normalizeCodexTurnStateDefaultModel('   ')).toBe('gpt-5.5')
  })

  it.each(['gpt-5*', 'bad value', 'gpt@5', 'a'.repeat(129)])(
    'rejects an invalid default probe model: %s',
    input => {
      expect(() => normalizeCodexTurnStateDefaultModel(input)).toThrow('invalid_default_model')
    },
  )

  it('validates and normalizes dedicated SOCKS5 URLs', () => {
    expect(validateCodexTurnStateProxyUrl('socks5://collector:secret@PROXY.EXAMPLE:1080')).toEqual({
      valid: true,
      normalized: 'socks5://collector:secret@proxy.example:1080',
    })
    expect(normalizeCodexTurnStateProxyUrls([
      'socks5://collector:secret@PROXY.EXAMPLE:1080',
      'socks5://collector:secret@proxy.example:1080',
      'socks5://other:secret@proxy.example:1081',
    ])).toEqual([
      'socks5://collector:secret@proxy.example:1080',
      'socks5://other:secret@proxy.example:1081',
    ])
    expect(normalizeCodexTurnStateProxyUrls([])).toEqual([])
  })

  it('matches backend credential encoding and deduplicates equivalent percent escapes', () => {
    expect(validateCodexTurnStateProxyUrl('socks5://%75ser:p%2fss@PROXY.EXAMPLE:01080')).toEqual({
      valid: true,
      normalized: 'socks5://user:p%2Fss@proxy.example:1080',
    })
    expect(validateCodexTurnStateProxyUrl("socks5://u!$&'()*+,;=:p!$&'()*+,;=:@proxy.example:1080")).toEqual({
      valid: true,
      normalized: 'socks5://u%21$&%27%28%29%2A+,;=:p%21$&%27%28%29%2A+,;=%3A@proxy.example:1080',
    })
    expect(normalizeCodexTurnStateProxyUrls([
      'socks5://%75ser:%70ass@proxy.example:1080',
      'socks5://user:pass@PROXY.EXAMPLE:1080',
    ])).toEqual(['socks5://user:pass@proxy.example:1080'])
    expect(validateCodexTurnStateProxyUrl('socks5://user:pass@TÄST.example:1080')).toEqual({
      valid: true,
      normalized: 'socks5://user:pass@t%C3%A4st.example:1080',
    })
  })

  it.each([
    'socks5://collector:secret@foo%09bar:1080',
    'socks5://collector:secret@foo%C2%85bar:1080',
    'socks5://collector:secret@foo%C2%A0bar:1080',
    'socks5://collector:secret@foo%E2%80%A8bar:1080',
    'socks5://collector:secret@foo%2Ebar:1080',
  ])('rejects a backend-invalid encoded host: %s', (value) => {
    expect(validateCodexTurnStateProxyUrl(value)).toEqual({ valid: false, error: 'host' })
  })

  it.each([
    '',
    'http://collector:secret@proxy.example:1080',
    'socks5://proxy.example:1080',
    'socks5://collector@proxy.example:1080',
    'socks5://collector:secret@:1080',
    'socks5://collector:secret@proxy.example',
    'socks5://collector:secret@proxy.example:0',
    'socks5://collector:secret@proxy.example:65536',
    'socks5://collector:secret@proxy.example:1080/path',
    'socks5://collector:secret@proxy.example:1080?query=1',
    'socks5://collector:secret@proxy.example:1080#fragment',
    'socks5://collector:%0Asecret@proxy.example:1080',
    'socks5://collector:%09secret@proxy.example:1080',
    'socks5://collector:%01secret@proxy.example:1080',
    'socks5://collector:%7Fsecret@proxy.example:1080',
    'socks5://collector:%C2%85secret@proxy.example:1080',
    'socks5://collector:sec\tret@proxy.example:1080',
    'socks5://collector:secret@proxy\t.example:1080',
    `socks5://collector:${'中'.repeat(225)}@proxy.example:1080`,
    ' socks5://collector:secret@proxy.example:1080',
  ])('rejects malformed dedicated proxy URL: %s', (value) => {
    expect(validateCodexTurnStateProxyUrl(value).valid).toBe(false)
    expect(() => normalizeCodexTurnStateProxyUrls([value])).toThrow('invalid_proxy_urls')
  })

  it('enforces the dedicated proxy pool limit', () => {
    const values = Array.from(
      { length: CODEX_TURN_STATE_PROXY_URL_LIMIT + 1 },
      (_, index) => `socks5://user:password@proxy-${index}.example:1080`,
    )
    expect(() => normalizeCodexTurnStateProxyUrls(values)).toThrow('invalid_proxy_urls')
  })

  it('parses, normalizes, and stably deduplicates a batch against the current pool', () => {
    const result = parseCodexTurnStateProxyUrlBatch([
      'socks5://existing:secret@PROXY.EXAMPLE:1080',
      ' socks5://new:secret@NEW.EXAMPLE:1081 ',
      'socks5://new:secret@new.example:1081',
      '',
      'http://invalid:secret@proxy.example:1080',
      'socks5://missing-password@proxy.example:1080',
    ].join('\r\n'), ['socks5://existing:secret@proxy.example:1080'])

    expect(result).toEqual({
      total: 5,
      urls: ['socks5://new:secret@new.example:1081'],
      duplicates: 2,
      overflow: 0,
      errors: [
        { line: 5, error: 'scheme' },
        { line: 6, error: 'credentials' },
      ],
    })
  })

  it('reports batch overflow without silently dropping valid URLs', () => {
    const existing = Array.from(
      { length: CODEX_TURN_STATE_PROXY_URL_LIMIT - 1 },
      (_, index) => `socks5://user:password@existing-${index}.example:1080`,
    )
    const result = parseCodexTurnStateProxyUrlBatch([
      'socks5://new:password@new-1.example:1080',
      'socks5://new:password@new-2.example:1080',
    ].join('\n'), existing)

    expect(result.urls).toHaveLength(2)
    expect(result.overflow).toBe(1)
    expect(result.errors).toEqual([])
  })

  it('uses the canonical unique size of the current pool for batch capacity', () => {
    const existing = Array.from(
      { length: CODEX_TURN_STATE_PROXY_URL_LIMIT - 2 },
      (_, index) => `socks5://user:password@existing-${index}.example:1080`,
    )
    existing.push('socks5://%75ser:password@EXISTING-0.EXAMPLE:1080')
    const result = parseCodexTurnStateProxyUrlBatch([
      'socks5://new:password@new-1.example:1080',
      'socks5://new:password@new-2.example:1080',
    ].join('\n'), existing)

    expect(existing).toHaveLength(CODEX_TURN_STATE_PROXY_URL_LIMIT - 1)
    expect(result.urls).toHaveLength(2)
    expect(result.overflow).toBe(0)
  })

  it('rejects a batch with too many physical lines before per-line parsing', () => {
    const result = parseCodexTurnStateProxyUrlBatch(
      '\n'.repeat(CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES),
      [],
    )

    expect(result).toEqual({
      total: 0,
      urls: [],
      duplicates: 0,
      overflow: 0,
      errors: [],
      inputError: 'tooManyLines',
    })
  })

  it('enforces the batch byte limit with UTF-8 byte length', () => {
    const raw = '中'.repeat(Math.floor(CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES / 2))
    expect(raw.length).toBeLessThan(CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES)

    expect(parseCodexTurnStateProxyUrlBatch(raw, [])).toEqual({
      total: 0,
      urls: [],
      duplicates: 0,
      overflow: 0,
      errors: [],
      inputError: 'tooManyBytes',
    })
  })

  it('keeps only currently schedulable OpenAI Codex accounts', () => {
    const now = Date.parse('2026-09-19T12:00:00Z')
    expect(isCodexTurnStateEligibleAccount(account())).toBe(true)
    expect(isCodexTurnStateEligibleAccount(account({ platform: 'anthropic' }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ type: 'apikey' }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ status: 'inactive', codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ status: 'error', codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ schedulable: false, codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ rate_limit_reset_at: '2026-09-19T12:01:00Z', codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ overload_until: '2026-09-19T12:01:00Z', codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ temp_unschedulable_until: '2026-09-19T12:01:00Z', codex_turn_state_auto: null }))).toBe(false)
    expect(isCodexTurnStateEligibleAccount(account({ auto_pause_on_expired: true, expires_at: Math.floor(now / 1000), codex_turn_state_auto: null }))).toBe(false)
    // The marker also preserves backend-only exceptions, such as an OAuth paid
    // credit snapshot superseding a local quota-threshold pause.
    expect(isCodexTurnStateEligibleAccount(account({ temp_unschedulable_until: '2026-09-19T12:01:00Z' }))).toBe(true)
  })
})
