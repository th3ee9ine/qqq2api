import type { OpenAICodexTurnStateStatus } from '@/api/admin/settings'

export const codexTurnStateMaxLength = 4096
export const codexTurnStateBaselineBlocks = 10
export const codexTurnStateTtlMs = 60 * 60 * 1000

/** Only HTTP header safety is enforced. Fernet inspection below is advisory. */
export function isValidCodexTurnStateToken(raw: string): boolean {
  const value = raw.trim()
  return value.length <= codexTurnStateMaxLength && /^[\x20-\x7e]*$/.test(value)
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

/** Refreshes warning diagnostics without treating the community heuristic as a gate. */
export function refreshCodexTurnStateStatus(
  saved: OpenAICodexTurnStateStatus, enabled: boolean, nowMs: number,
): OpenAICodexTurnStateStatus {
  const status = { ...saved, enabled }
  status.active = enabled && status.configured && status.reason !== 'invalid'
  if (!enabled) status.reason = 'disabled'
  else if (!status.configured) status.reason = 'missing'
  else if (saved.reason === 'invalid') status.reason = 'invalid'
  else if (!status.issued_at || !status.expires_at) status.reason = 'unknown'
  else if (status.issued_at * 1000 > nowMs + 60_000) status.reason = 'future'
  else if (nowMs >= status.expires_at * 1000) status.reason = 'expired'
  else status.reason = status.blocks > codexTurnStateBaselineBlocks ? 'suspect' : 'ready'
  return status
}

/** Envelope only: neither decrypts nor validates the signature, account, or efficacy. */
export function inspectCodexTurnState(
  token: string, nowMs: number, enabled = true,
): OpenAICodexTurnStateStatus {
  const value = token.trim()
  const status: OpenAICodexTurnStateStatus = {
    enabled, configured: value !== '', active: false,
    reason: isValidCodexTurnStateToken(value) ? 'unknown' : 'invalid',
    verdict: 'unknown', blocks: 0,
  }
  if (status.reason !== 'invalid' && /^[A-Za-z0-9_-]+={0,2}$/.test(value)) {
    const body = value.replace(/=+$/, '')
    try {
      if (body.length % 4 !== 1) {
        const raw = atob(body.replace(/-/g, '+').replace(/_/g, '/'))
        // Match the backend strict decoder's canonical trailing padding bits.
        const canonical = btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
        const cipherBytes = raw.length - 57
        if (canonical === body && raw.charCodeAt(0) === 128 && cipherBytes >= 16 && cipherBytes % 16 === 0) {
          let seconds = 0
          for (let i = 1; i < 9; i++) seconds = seconds * 256 + raw.charCodeAt(i)
          if (seconds >= 1577836800 && seconds < 4102444800) {
            status.blocks = cipherBytes / 16
            status.issued_at = seconds
            status.expires_at = seconds + codexTurnStateTtlMs / 1000
            status.verdict = status.blocks > codexTurnStateBaselineBlocks ? 'suspect' : 'normal'
          }
        }
      }
    } catch { /* A safe but unknown upstream format remains eligible for injection. */ }
  }
  return refreshCodexTurnStateStatus(status, enabled, nowMs)
}

export function formatCodexTurnStateDuration(ms: number): string {
  if (!Number.isFinite(ms)) return '00:00'
  const total = Math.floor(Math.abs(ms) / 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor(total / 60) % 60
  const seconds = total % 60
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`
}
