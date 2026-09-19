import type { AccountListItem } from '@/types'

export const CODEX_TURN_STATE_PROXY_URL_LIMIT = 256
export const CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES = 1024 * 1024
export const CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES = 1024

export type CodexTurnStateProxyUrlError =
  | 'required'
  | 'tooLong'
  | 'scheme'
  | 'credentials'
  | 'host'
  | 'port'
  | 'suffix'
  | 'invalid'

export interface CodexTurnStateProxyUrlValidation {
  valid: boolean
  normalized?: string
  error?: CodexTurnStateProxyUrlError
}

export interface CodexTurnStateProxyUrlBatchError {
  line: number
  error: CodexTurnStateProxyUrlError
}

export type CodexTurnStateProxyUrlBatchInputError = 'tooManyBytes' | 'tooManyLines'

export interface CodexTurnStateProxyUrlBatchResult {
  total: number
  urls: string[]
  duplicates: number
  overflow: number
  errors: CodexTurnStateProxyUrlBatchError[]
  inputError?: CodexTurnStateProxyUrlBatchInputError
}

const textEncoder = new TextEncoder()

function encodeCodexTurnStateProxyCredential(value: string): string {
  let encoded = ''
  for (const byte of textEncoder.encode(value)) {
    const unreserved =
      (byte >= 0x41 && byte <= 0x5a)
      || (byte >= 0x61 && byte <= 0x7a)
      || (byte >= 0x30 && byte <= 0x39)
      || byte === 0x2d
      || byte === 0x2e
      || byte === 0x5f
      || byte === 0x7e
    const goUserInfoSubDelimiter =
      byte === 0x24
      || byte === 0x26
      || byte === 0x2b
      || byte === 0x2c
      || byte === 0x3b
      || byte === 0x3d
    encoded += unreserved || goUserInfoSubDelimiter
      ? String.fromCharCode(byte)
      : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`
  }
  return encoded
}

function encodeCodexTurnStateProxyHost(value: string): string {
  let encoded = ''
  for (const byte of textEncoder.encode(value)) {
    const unreserved =
      (byte >= 0x41 && byte <= 0x5a)
      || (byte >= 0x61 && byte <= 0x7a)
      || (byte >= 0x30 && byte <= 0x39)
      || byte === 0x2d
      || byte === 0x2e
      || byte === 0x5f
      || byte === 0x7e
    const goHostDelimiter =
      byte === 0x21
      || byte === 0x22
      || byte === 0x24
      || byte === 0x26
      || byte === 0x27
      || byte === 0x28
      || byte === 0x29
      || byte === 0x2a
      || byte === 0x2b
      || byte === 0x2c
      || byte === 0x3a
      || byte === 0x3b
      || byte === 0x3c
      || byte === 0x3d
      || byte === 0x3e
      || byte === 0x5b
      || byte === 0x5d
    encoded += unreserved || goHostDelimiter
      ? String.fromCharCode(byte)
      : `%${byte.toString(16).toUpperCase().padStart(2, '0')}`
  }
  return encoded
}

function hasDisallowedASCIIHostEscape(value: string): boolean {
  for (const match of value.matchAll(/%([0-9a-f]{2})/gi)) {
    const byte = Number.parseInt(match[1], 16)
    if (byte < 0x80 && byte !== 0x25) return true
  }
  return false
}

function exceedsCodexTurnStateProxyBatchLineLimit(raw: string): boolean {
  if (!raw) return false
  let lines = 1
  for (let index = 0; index < raw.length; index += 1) {
    const current = raw.charCodeAt(index)
    if (current !== 0x0a && current !== 0x0d) continue
    if (current === 0x0d && raw.charCodeAt(index + 1) === 0x0a) index += 1
    lines += 1
    if (lines > CODEX_TURN_STATE_PROXY_BATCH_MAX_LINES) return true
  }
  return false
}

/** Canonicalize the system-wide model scope; never silently broaden invalid input. */
export function normalizeCodexTurnStateModels(raw: string): string {
  const encoder = new TextEncoder()
  const seen = new Set<string>()
  for (const part of raw.trim().split(/[,\r\n]/)) {
    const model = part.trim().toLowerCase()
    if (!model) continue
    // Keep the same model-character whitelist as the backend; '*' by itself
    // is a valid all-model prefix, but wildcards elsewhere are not accepted.
    if (!/^[a-z0-9_.:/-]*\*?$/.test(model)) {
      throw new Error('invalid_models')
    }
    seen.add(model)
  }
  const value = [...seen].join(',')
  if (encoder.encode(value).length > 1024) throw new Error('invalid_models')
  return value
}

/** Exact model ID for automatic probes; blank restores the built-in default. */
export function normalizeCodexTurnStateDefaultModel(raw: string): string {
  const model = raw.trim() || 'gpt-5.5'
  if (model.length > 128 || !/^[a-zA-Z0-9_.:/-]+$/.test(model)) {
    throw new Error('invalid_default_model')
  }
  return model
}

/** Validate the credential-bearing, Turn-State-only SOCKS5 proxy URL. */
export function validateCodexTurnStateProxyUrl(raw: string): CodexTurnStateProxyUrlValidation {
  if (!raw) return { valid: false, error: 'required' }
  if (textEncoder.encode(raw).length > 2048) return { valid: false, error: 'tooLong' }
  if (raw !== raw.trim() || /\p{Cc}/u.test(raw)) return { valid: false, error: 'invalid' }

  let parsed: URL
  try {
    parsed = new URL(raw)
  } catch {
    return { valid: false, error: 'invalid' }
  }
  if (parsed.protocol.toLowerCase() !== 'socks5:') return { valid: false, error: 'scheme' }
  let username: string
  let password: string
  try {
    username = decodeURIComponent(parsed.username)
    password = decodeURIComponent(parsed.password)
  } catch {
    return { valid: false, error: 'credentials' }
  }
  if (!username || !password || /\p{Cc}/u.test(`${username}${password}`)) return { valid: false, error: 'credentials' }
  const bracketedHost = parsed.hostname.startsWith('[') && parsed.hostname.endsWith(']')
  const encodedHost = bracketedHost ? parsed.hostname.slice(1, -1) : parsed.hostname
  let host: string
  try {
    if (hasDisallowedASCIIHostEscape(encodedHost)) return { valid: false, error: 'host' }
    host = decodeURIComponent(encodedHost).toLowerCase()
  } catch {
    return { valid: false, error: 'host' }
  }
  if (!host || textEncoder.encode(host).length > 253 || /[\p{Cc}\p{White_Space}]/u.test(host)) {
    return { valid: false, error: 'host' }
  }
  const port = Number(parsed.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return { valid: false, error: 'port' }
  if (parsed.pathname || parsed.search || parsed.hash || parsed.href.endsWith('?') || parsed.href.endsWith('#')) {
    return { valid: false, error: 'suffix' }
  }

  const canonicalHost = encodeCodexTurnStateProxyHost(host)
  const normalizedHost = bracketedHost ? `[${canonicalHost}]` : canonicalHost
  const normalized = `socks5://${encodeCodexTurnStateProxyCredential(username)}:${encodeCodexTurnStateProxyCredential(password)}@${normalizedHost}:${port}`
  if (textEncoder.encode(normalized).length > 2048) return { valid: false, error: 'tooLong' }
  return { valid: true, normalized }
}

/** Validate, canonicalize, and stably de-duplicate the dedicated proxy pool. */
export function normalizeCodexTurnStateProxyUrls(values: string[]): string[] {
  if (!Array.isArray(values) || values.length > CODEX_TURN_STATE_PROXY_URL_LIMIT) {
    throw new Error('invalid_proxy_urls')
  }
  const seen = new Set<string>()
  const result: string[] = []
  for (const value of values) {
    const validation = validateCodexTurnStateProxyUrl(value)
    if (!validation.valid || !validation.normalized) throw new Error('invalid_proxy_urls')
    if (seen.has(validation.normalized)) continue
    seen.add(validation.normalized)
    result.push(validation.normalized)
  }
  return result
}

/** Parse newline-delimited URLs for an atomic append to the current pool. */
export function parseCodexTurnStateProxyUrlBatch(
  raw: string,
  existingValues: string[],
): CodexTurnStateProxyUrlBatchResult {
  const result: CodexTurnStateProxyUrlBatchResult = {
    total: 0,
    urls: [],
    duplicates: 0,
    overflow: 0,
    errors: [],
  }
  if (raw.length > CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES) {
    return { ...result, inputError: 'tooManyBytes' }
  }
  if (exceedsCodexTurnStateProxyBatchLineLimit(raw)) {
    return { ...result, inputError: 'tooManyLines' }
  }
  if (textEncoder.encode(raw).length > CODEX_TURN_STATE_PROXY_BATCH_MAX_BYTES) {
    return { ...result, inputError: 'tooManyBytes' }
  }

  const seen = new Set<string>()
  for (const value of existingValues) {
    const validation = validateCodexTurnStateProxyUrl(value)
    if (validation.valid && validation.normalized) seen.add(validation.normalized)
  }

  raw.split(/\r\n?|\n/).forEach((line, index) => {
    const value = line.trim()
    if (!value) return
    result.total += 1

    const validation = validateCodexTurnStateProxyUrl(value)
    if (!validation.valid || !validation.normalized) {
      result.errors.push({ line: index + 1, error: validation.error || 'invalid' })
      return
    }
    if (seen.has(validation.normalized)) {
      result.duplicates += 1
      return
    }
    seen.add(validation.normalized)
    result.urls.push(validation.normalized)
  })

  result.overflow = Math.max(0, seen.size - CODEX_TURN_STATE_PROXY_URL_LIMIT)

  return result
}

/** The backend emits diagnostics only after its authoritative IsSchedulable check. */
export function isCodexTurnStateEligibleAccount(account: AccountListItem): boolean {
  if (account.platform !== 'openai' || (account.type !== 'oauth' && account.type !== 'setup-token')) return false
  return account.codex_turn_state_auto != null
}
