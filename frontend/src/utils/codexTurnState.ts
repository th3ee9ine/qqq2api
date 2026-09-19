import type { AccountListItem } from '@/types'

export const CODEX_TURN_STATE_PROXY_URL_LIMIT = 256

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
  if (raw.length > 2048) return { valid: false, error: 'tooLong' }
  if (raw !== raw.trim() || /[\r\n\0]/.test(raw)) return { valid: false, error: 'invalid' }

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
  if (!username || !password || /[\r\n\0]/.test(`${username}${password}`)) return { valid: false, error: 'credentials' }
  if (!parsed.hostname || parsed.hostname.length > 253 || /\s/.test(parsed.hostname)) return { valid: false, error: 'host' }
  const port = Number(parsed.port)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return { valid: false, error: 'port' }
  if (parsed.pathname || parsed.search || parsed.hash || parsed.href.endsWith('?') || parsed.href.endsWith('#')) {
    return { valid: false, error: 'suffix' }
  }

  const normalized = `socks5://${parsed.username}:${parsed.password}@${parsed.hostname.toLowerCase()}:${port}`
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

/** The backend emits diagnostics only after its authoritative IsSchedulable check. */
export function isCodexTurnStateEligibleAccount(account: AccountListItem): boolean {
  if (account.platform !== 'openai' || (account.type !== 'oauth' && account.type !== 'setup-token')) return false
  return account.codex_turn_state_auto != null
}
