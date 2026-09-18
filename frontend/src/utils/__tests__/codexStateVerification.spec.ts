import { describe, expect, it } from 'vitest'
import {
  compareResponseModels,
  expectedUpstreamModel,
  extractResponseModelEvidence,
  extractTurnStateEvidence,
  modelMatchesExpected
} from '../codexStateVerification'

describe('codex state verification evidence', () => {
  it('requires both created and completed models for a streaming pass', () => {
    const evidence = extractResponseModelEvidence({
      body_text: [
        'data: {"type":"response.created","response":{"model":"gpt-6-astra"}}',
        '',
        'data: {"type":"response.completed","response":{"model":"gpt-6-astra"}}',
        '',
        'data: [DONE]'
      ].join('\n')
    })
    expect(evidence).toMatchObject({ source: 'sse', created: 'gpt-6-astra', completed: 'gpt-6-astra' })
    expect(compareResponseModels('gpt-6-astra', evidence)).toMatchObject({ verdict: 'pass' })
  })

  it('reports an actual Luna response as a mismatch even when HTTP is successful', () => {
    const evidence = extractResponseModelEvidence({
      status_code: 200,
      body_text: 'data: {"type":"response.created","response":{"model":"gpt-5.6-luna"}}\n\ndata: {"type":"response.completed","response":{"model":"gpt-5.6-luna"}}\n'
    })
    const comparison = compareResponseModels('gpt-6-astra', evidence)
    expect(comparison.verdict).toBe('mismatch')
    expect(comparison.reason).toContain('gpt-5.6-luna')
  })

  it('accepts dated Astra response variants but not a different family', () => {
    const dated = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.created","response":{"model":"gpt-6-astra-2026-09-01"}}\n\ndata: {"type":"response.completed","response":{"model":"gpt-6-astra-2026-09-01"}}\n'
    })
    expect(compareResponseModels('gpt-6-astra', dated).verdict).toBe('pass')
    expect(modelMatchesExpected('gpt-6-astra', 'gpt-6-astra-2026-09-01')).toBe(true)
    expect(modelMatchesExpected('gpt-6-astra', 'openai/gpt-6-astra-build-42')).toBe(true)
    expect(modelMatchesExpected('gpt-6-astra', 'gpt-6')).toBe(true)

    const sol = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.created","response":{"model":"gpt-5.6-sol"}}\n\ndata: {"type":"response.completed","response":{"model":"gpt-5.6-sol"}}\n'
    })
    expect(compareResponseModels('gpt-6-astra', sol).verdict).toBe('mismatch')
    expect(modelMatchesExpected('gpt-6-astra', 'gpt-5.6-terra')).toBe(false)
  })

  it('compares Astra family variants symmetrically across lifecycle events', () => {
    expect(modelMatchesExpected('gpt-6-astra-2026-09-01', 'openai/gpt-6')).toBe(true)
    expect(modelMatchesExpected('openai/gpt-6', 'gpt-6-astra-build-42')).toBe(true)

    const evidence = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.created","response":{"model":"openai/gpt-6-astra-2026-09-01"}}\n\ndata: {"type":"response.completed","response":{"model":"gpt-6"}}\n'
    })
    expect(compareResponseModels('gpt-6-astra', evidence).verdict).toBe('pass')
  })

  it('compares codex-auto-review with the same reported model', () => {
    const evidence = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.created","response":{"model":"codex-auto-review"}}\n\ndata: {"type":"response.completed","response":{"model":"codex-auto-review"}}\n'
    })
    expect(expectedUpstreamModel('codex-auto-review')).toBe('codex-auto-review')
    expect(compareResponseModels('codex-auto-review', evidence)).toMatchObject({ verdict: 'pass', expected: 'codex-auto-review' })

    const astra = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.created","response":{"model":"gpt-6-astra"}}\n\ndata: {"type":"response.completed","response":{"model":"gpt-6-astra"}}\n'
    })
    expect(compareResponseModels('codex-auto-review', astra).verdict).toBe('mismatch')
    expect(expectedUpstreamModel('provider/codex-auto-review')).toBe('codex-auto-review')
    expect(modelMatchesExpected('codex-auto-review', 'provider/codex-auto-review')).toBe(true)
    expect(modelMatchesExpected('codex-auto-review', 'provider/codex-auto-review-2026-09-18')).toBe(true)
    expect(modelMatchesExpected('provider/codex-auto-review', 'provider/codex-auto-review')).toBe(true)
  })

  it('does not pass a partial stream', () => {
    const evidence = extractResponseModelEvidence({
      body_text: 'data: {"type":"response.completed","response":{"model":"gpt-6-astra"}}\n'
    })
    expect(compareResponseModels('gpt-6-astra', evidence).verdict).toBe('unknown')
  })

  it('supports non-stream JSON model evidence without borrowing the request model', () => {
    const evidence = extractResponseModelEvidence({ body: { model: 'gpt-5.6-luna', output: [] } })
    expect(compareResponseModels('gpt-6-astra', evidence)).toMatchObject({ verdict: 'mismatch', body: 'gpt-5.6-luna' })
  })

  it('keeps a redacted state length unknown unless the backend exposes a diagnostic length', () => {
    expect(extractTurnStateEvidence({ headers: { 'X-Codex-Turn-State': ['[redacted]'] } })).toEqual({ status: 'unknown', redacted: true })
    expect(extractTurnStateEvidence({ headers: { 'X-Codex-Turn-State': ['[redacted:332]'] } })).toEqual({ status: 'unknown', redacted: true, length: 332 })
    expect(extractTurnStateEvidence({ headers: { 'X-Codex-Turn-State': ['abc123'] } })).toEqual({ status: 'present', redacted: false, length: 6 })
    expect(extractTurnStateEvidence({ headers: {} })).toEqual({ status: 'absent', redacted: false })
  })
})
