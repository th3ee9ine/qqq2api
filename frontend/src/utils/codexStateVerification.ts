/**
 * Small, transport-agnostic helpers for the admin state verification view.
 *
 * These functions intentionally inspect only the response snapshot.  The
 * request `model` is never used as a substitute for an upstream response
 * model, and a redacted Turn State header is reported as unknown rather than
 * being treated as proof that a state was accepted.
 */

export type VerificationSnapshot = {
  status_code?: number
  headers?: Record<string, string[] | string | undefined>
  body?: unknown
  body_text?: string
  complete?: boolean
  truncated?: boolean
}

export type ResponseModelEvidence = {
  created?: string
  completed?: string
  body?: string
  eventCount: number
  source: 'sse' | 'json' | 'none'
}

export type ModelVerdict = 'pass' | 'mismatch' | 'unknown'

export type ModelComparison = {
  verdict: ModelVerdict
  requested?: string
  expected?: string
  created?: string
  completed?: string
  body?: string
  reason: string
}

export type StateHeaderEvidence = {
  status: 'present' | 'absent' | 'unknown'
  length?: number
  redacted: boolean
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function modelFromEvent(value: unknown): string | undefined {
  const event = asRecord(value)
  if (!event) return undefined
  const response = asRecord(event.response)
  return stringValue(response?.model) || stringValue(event.model)
}

function eventType(value: unknown): string | undefined {
  return stringValue(asRecord(value)?.type)?.toLowerCase()
}

function consumeEvent(value: unknown, evidence: ResponseModelEvidence): void {
  const type = eventType(value)
  const model = modelFromEvent(value)
  if (type === 'response.created' && model) evidence.created = model
  if (type === 'response.completed' && model) evidence.completed = model
  if (type) evidence.eventCount += 1
}

function consumeJson(value: unknown, evidence: ResponseModelEvidence): void {
  if (Array.isArray(value)) {
    value.forEach(item => consumeEvent(item, evidence))
    return
  }
  const object = asRecord(value)
  if (!object) return
  if (eventType(object)) {
    consumeEvent(object, evidence)
    return
  }
  const model = stringValue(object.model)
  if (model) evidence.body = model
  // A few upstream wrappers put the response envelope under `response`.
  const nested = asRecord(object.response)
  if (nested && stringValue(nested.model)) evidence.body = stringValue(nested.model)
}

function parseSSE(text: string, evidence: ResponseModelEvidence): boolean {
  let foundEvent = false
  for (const line of text.split(/\r?\n/)) {
    const raw = line.trim()
    if (!raw.toLowerCase().startsWith('data:')) continue
    const payload = raw.slice(raw.indexOf(':') + 1).trim()
    if (!payload || payload === '[DONE]') continue
    try {
      const value = JSON.parse(payload) as unknown
      const before = evidence.eventCount
      consumeEvent(value, evidence)
      foundEvent = foundEvent || evidence.eventCount > before || Boolean(modelFromEvent(value))
    } catch {
      // A stream may contain comments or a partial frame.  Keep the evidence
      // already parsed and let the caller report missing events as unknown.
    }
  }
  return foundEvent
}

/** Extracts models from real response JSON or SSE frames. */
export function extractResponseModelEvidence(snapshot?: VerificationSnapshot | null): ResponseModelEvidence {
  const evidence: ResponseModelEvidence = { eventCount: 0, source: 'none' }
  if (!snapshot) return evidence
  if (typeof snapshot.body_text === 'string' && snapshot.body_text.trim()) {
    if (parseSSE(snapshot.body_text, evidence)) evidence.source = 'sse'
    // Some non-stream responses are captured as text even though they are JSON.
    if (evidence.source === 'none') {
      try {
        consumeJson(JSON.parse(snapshot.body_text), evidence)
        if (evidence.body || evidence.eventCount) evidence.source = 'json'
      } catch {
        // Preserve a no-evidence result instead of inferring from request data.
      }
    }
  }
  if (snapshot.body !== undefined && snapshot.body !== null) {
    consumeJson(snapshot.body, evidence)
    if (evidence.source === 'none' && (evidence.body || evidence.eventCount)) evidence.source = 'json'
  }
  return evidence
}

/** Reads a model from the actual upstream request snapshot, when captured. */
export function extractRequestModel(snapshot?: VerificationSnapshot | null): string | undefined {
  if (!snapshot) return undefined
  const object = asRecord(snapshot.body)
  const fromBody = stringValue(object?.model)
  if (fromBody) return fromBody
  if (typeof snapshot.body_text === 'string') {
    try { return stringValue(asRecord(JSON.parse(snapshot.body_text))?.model) } catch { /* request body was not a JSON object */ }
  }
  return undefined
}

/**
 * Resolves the exact model that a response is expected to report. Supported
 * Codex names are not rewritten for validation; in particular,
 * codex-auto-review must report codex-auto-review.
 */
export function expectedUpstreamModel(requestedModel?: string, observedUpstreamModel?: string): string | undefined {
  const requested = stringValue(requestedModel)
  const observed = stringValue(observedUpstreamModel)
  if (!requested) return observed
  if (isCodexAutoReviewVariant(requested)) return 'codex-auto-review'
  if (isAstraFamilyModel(requested)) return 'gpt-6-astra'
  return observed || requested
}

function normalizedModelSegment(model: string): string {
  const normalized = model.trim().toLowerCase().replace(/_/g, '-')
  const slash = normalized.lastIndexOf('/')
  return slash >= 0 ? normalized.slice(slash + 1).trim() : normalized
}

function isAstraFamilyModel(model: string): boolean {
  const normalized = normalizedModelSegment(model)
  return normalized === 'gpt-6' || normalized === 'gpt-6-astra' || normalized.startsWith('gpt-6-astra-')
}

function isCodexAutoReviewVariant(model: string): boolean {
  const normalized = normalizedModelSegment(model)
  return normalized === 'codex-auto-review' || normalized.startsWith('codex-auto-review-')
}

/**
 * Compares raw upstream identities while allowing only the documented Astra
 * family variants. Auto-review remains a separate family from Astra.
 */
function modelsEquivalent(expected: string, actual: string): boolean {
  const expectedValue = expected.trim()
  const actualValue = actual.trim()
  if (!expectedValue || !actualValue) return false
  if (isCodexAutoReviewVariant(expectedValue) || isCodexAutoReviewVariant(actualValue)) {
    return isCodexAutoReviewVariant(expectedValue) && isCodexAutoReviewVariant(actualValue)
  }
  if (expectedValue.toLowerCase() === actualValue.toLowerCase()) return true
  return isAstraFamilyModel(expectedValue) && isAstraFamilyModel(actualValue)
}

export function modelMatchesExpected(expected: string, actual: string): boolean {
  return modelsEquivalent(expected, actual)
}

/**
 * Compares a requested model with both response.created and response.completed.
 * A stream missing either event is intentionally not considered a pass.
 */
export function compareResponseModels(requestedModel: string | undefined, evidence: ResponseModelEvidence, expectedModel = expectedUpstreamModel(requestedModel)): ModelComparison {
  const requested = stringValue(requestedModel)
  const expected = stringValue(expectedModel)
  const created = stringValue(evidence.created)
  const completed = stringValue(evidence.completed)
  const body = stringValue(evidence.body)
  if (evidence.source === 'json' && body) {
    if (!expected) return { verdict: 'unknown', requested, body, reason: '预期上游模型缺失，无法比较响应模型。' }
    return modelMatchesExpected(expected, body)
      ? { verdict: 'pass', requested, expected, body, reason: `JSON 响应的 model 与预期上游模型 ${expected} 一致。` }
      : { verdict: 'mismatch', requested, expected, body, reason: `实际 JSON 响应模型为 ${body}，预期上游模型为 ${expected}。` }
  }
  if (!created || !completed) {
    return { verdict: 'unknown', requested, expected, created, completed, reason: '缺少完整的 response.created / response.completed model 证据。' }
  }
  if (!expected) return { verdict: 'unknown', requested, created, completed, reason: '预期上游模型缺失，无法比较响应模型。' }
  if (!modelMatchesExpected(expected, created) || !modelMatchesExpected(expected, completed) || !modelMatchesExpected(created, completed)) {
    const actual = created === completed ? created : `${created} / ${completed}`
    return { verdict: 'mismatch', requested, expected, created, completed, reason: `实际响应模型为 ${actual}，预期上游模型为 ${expected}（客户端请求 ${requested || '未提供'}）。` }
  }
  return { verdict: 'pass', requested, expected, created, completed, reason: `response.created 与 response.completed 均为预期上游模型 ${expected}。` }
}

function headerValues(snapshot: VerificationSnapshot | null | undefined, name: string): string[] {
  if (!snapshot?.headers) return []
  const wanted = name.toLowerCase()
  for (const [key, raw] of Object.entries(snapshot.headers)) {
    if (key.toLowerCase() !== wanted) continue
    if (Array.isArray(raw)) return raw.filter((value): value is string => typeof value === 'string')
    return typeof raw === 'string' ? [raw] : []
  }
  return []
}

/** Reads only the diagnostic presence/length; redacted values stay unknown. */
export function extractTurnStateEvidence(snapshot?: VerificationSnapshot | null): StateHeaderEvidence {
  const values = headerValues(snapshot, 'x-codex-turn-state')
  if (!values.length) return { status: 'absent', redacted: false }
  const value = values[0].trim()
  if (!value) return { status: 'absent', redacted: false }
  if (/^\[redacted(?::(\d+))?\]$/i.test(value) || /[•*]/.test(value)) {
    const match = value.match(/:(\d+)\]?$/)
    return { status: 'unknown', redacted: true, ...(match ? { length: Number(match[1]) } : {}) }
  }
  return { status: 'present', redacted: false, length: value.length }
}
