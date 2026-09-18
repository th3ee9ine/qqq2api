import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DebugStateComparison, { type StateVerificationRun, type StateVerificationStage } from '../DebugStateComparison.vue'

function verificationRun(
  requestID: string,
  model: string,
  options: {
    stage?: StateVerificationStage
    requestState?: boolean
    responseState?: boolean
    accountID?: number
    apiKeyID?: number
    usageAPIKeyID?: number
    usageRequestedModel?: string
    proxy?: string
    requestedModel?: string
    stateSource?: 'native' | 'automatic' | 'none'
    stateMatchesCapture?: boolean
    usageLogStateSent?: boolean
    usageLogVerified?: boolean
    statePublished?: boolean
    dailyRouteVerified?: boolean
    success?: boolean
    attemptStatus?: number
    omitAttempts?: boolean
    omitResponse?: boolean
  } = {}
): StateVerificationRun {
  const accountID = options.accountID ?? 7
  const apiKeyID = options.apiKeyID ?? 19
  const requestedModel = options.requestedModel ?? 'gpt-6-astra'
  const snapshot = (extra: Record<string, unknown> = {}) => ({
    headers: {}, body_bytes: 1, captured_bytes: 1, truncated: false, complete: true, ...extra
  })
  const responseHeaders = options.responseState ? { 'X-Codex-Turn-State': ['[redacted:332]'] } : {}
  const requestHeaders = options.requestState ? { 'X-Codex-Turn-State': ['[redacted]'] } : {}
  const attemptStatus = options.attemptStatus ?? 200
  const attempt = {
    index: 1,
    transport: 'http',
    account_id: accountID,
    proxy: options.proxy || 'direct',
    duration_ms: 45,
    request: snapshot({ method: 'POST', headers: requestHeaders }),
    ...(!options.omitResponse ? {
      response: snapshot({
        status_code: attemptStatus,
        headers: responseHeaders,
        body_text: `data: {"type":"response.created","response":{"model":"${model}"}}\n\ndata: {"type":"response.completed","response":{"model":"${model}"}}\n\n`
      })
    } : {}),
    header_changes: []
  }
  return {
    requestID,
    stage: options.stage ?? 'baseline',
    requestedModel,
    selectedAccountID: accountID,
    apiKeyID,
    result: {
      request_id: requestID,
      success: options.success ?? true,
      endpoint: 'responses',
      transport: 'http',
      duration_ms: 50,
      session: { id: 'debug-session', session_id: 'session', thread_id: 'thread', turn_id: 'turn', window_id: 'window', turn_index: 1, turn_state_available: Boolean(options.responseState) },
      inbound: snapshot(),
      outbound: snapshot({ status_code: attemptStatus }),
      attempts: options.omitAttempts ? [] : [attempt],
      warnings: [],
      state_verification: {
        requested_model: requestedModel,
        response_created_model: model,
        response_completed_model: model,
        state_sent: Boolean(options.requestState),
        state_received: Boolean(options.responseState),
        state_length: options.responseState ? 332 : undefined,
        state_source: options.stateSource || 'none',
        actual_account_id: accountID,
        usage_log_account_id: accountID,
        usage_log_api_key_id: options.usageAPIKeyID ?? apiKeyID,
        usage_log_requested_model: options.usageRequestedModel ?? requestedModel,
        upstream_response_model: model,
        usage_log_state_sent: options.usageLogStateSent ?? Boolean(options.requestState),
        usage_log_verified: options.usageLogVerified ?? true,
        state_matches_capture: options.stateMatchesCapture ?? false,
        state_published: options.statePublished ?? true,
        daily_route_verified: options.dailyRouteVerified ?? true
      }
    }
  }
}

describe('DebugStateComparison', () => {
  it('does not claim recovery when daily replay or atomic publication is missing', async () => {
    const wrapper = mount(DebugStateComparison)
    await wrapper.setProps({ run: verificationRun('baseline', 'gpt-5.6-luna') })
    await wrapper.setProps({ run: verificationRun('capture', 'gpt-6-astra', { stage: 'capture', responseState: true }) })
    await wrapper.setProps({ run: verificationRun('replay', 'gpt-6-astra', { stage: 'replay', requestState: true, stateMatchesCapture: true, statePublished: false, dailyRouteVerified: false }) })
    await wrapper.setProps({ run: verificationRun('automatic', 'gpt-6-astra', { stage: 'automatic', requestState: true, stateSource: 'automatic' }) })
    expect(wrapper.get('.overall-status').text()).toContain('证据未闭合')
    expect(wrapper.get('[data-stage="replay"]').text()).toContain('未发布，保留旧值')
  })
	it('shows HTTP 200 plus Luna as a model mismatch and advances to the next stage', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('baseline-1', 'gpt-5.6-luna') })
    const baseline = wrapper.get('[data-stage="baseline"]')
    expect(baseline.text()).toContain('模型不匹配')
    expect(baseline.text()).toContain('gpt-5.6-luna')
    expect(baseline.text()).toContain('200')
    expect(wrapper.emitted('update:stage')?.at(-1)).toEqual(['capture'])
    expect(wrapper.get('.overall-status').text()).toContain('验收未完成')
	})

	it('does not advance a non-durable baseline', async () => {
		const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:gpt-6-astra' } })
		await wrapper.setProps({ run: verificationRun('baseline-no-usage', 'gpt-5.6-luna', { usageLogVerified: false }) })
		expect(wrapper.get('[data-stage="baseline"]').text()).toContain('模型不匹配')
		expect(wrapper.emitted('update:stage')).toBeUndefined()
	})

  it('requires codex-auto-review to report its own model identity', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:codex-auto-review' } })
    await wrapper.setProps({ run: verificationRun('alias-1', 'codex-auto-review', { requestedModel: 'codex-auto-review' }) })
    const baseline = wrapper.get('[data-stage="baseline"]')
    expect(baseline.text()).toContain('模型匹配')
    expect(baseline.text()).toContain('预期上游模型codex-auto-review')
  })

  it('keeps automatic injection unverified even after matching manual state runs', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('baseline-1', 'gpt-5.6-luna') })
    await wrapper.setProps({ run: verificationRun('capture-1', 'gpt-6-astra', { stage: 'capture', responseState: true, proxy: 'socks5://us.proxy.local:3000' }) })
    await wrapper.setProps({ run: verificationRun('replay-1', 'gpt-6-astra', { stage: 'replay', requestState: true, responseState: true, proxy: 'socks5://us.proxy.local:3000', stateMatchesCapture: true }) })
    await wrapper.setProps({ run: verificationRun('automatic-1', 'gpt-6-astra', { stage: 'automatic', requestState: true, responseState: true, stateSource: 'none', usageLogStateSent: true, usageLogVerified: false }) })

    expect(wrapper.get('[data-stage="capture"]').text()).toContain('模型匹配')
    expect(wrapper.get('[data-stage="capture"]').text()).toContain('332 字节')
    expect(wrapper.get('[data-stage="replay"]').text()).toContain('已观察到')
    expect(wrapper.get('[data-stage="automatic"]').text()).toContain('注入未确认')
    expect(wrapper.get('[data-stage="automatic"]').text()).toContain('自动缓存来源尚未由后端证据确认')
    expect(wrapper.get('.overall-status').text()).toContain('证据未闭合')
    expect(wrapper.text()).not.toContain('自动注入已验收')
  })

  it('requires capture, exact replay, automatic source and same account/API key usage evidence', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:19:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('baseline-ok', 'gpt-5.6-luna') })
    await wrapper.setProps({ run: verificationRun('capture-ok', 'openai/gpt-6-astra-2026-09-18', { stage: 'capture', responseState: true }) })
    await wrapper.setProps({ run: verificationRun('replay-ok', 'gpt-6-astra-build-42', { stage: 'replay', requestState: true, stateMatchesCapture: true }) })
    await wrapper.setProps({ run: verificationRun('automatic-ok', 'gpt-6', { stage: 'automatic', requestState: true, stateSource: 'automatic', stateMatchesCapture: false, usageLogStateSent: true }) })

    expect(wrapper.get('.overall-status').text()).toContain('自动注入已验收')
    expect(wrapper.get('[data-stage="automatic"]').text()).toContain('gpt-6')
    expect(wrapper.get('[data-stage="capture"]').text()).toContain('openai/gpt-6-astra-2026-09-18')
    expect(wrapper.text()).not.toContain('candidate-secret')
  })

  it('does not claim automatic acceptance without a baseline record from this comparison run', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:19:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('capture-ok', 'gpt-6-astra', { stage: 'capture', responseState: true }) })
    await wrapper.setProps({ run: verificationRun('replay-ok', 'gpt-6-astra', { stage: 'replay', requestState: true, stateMatchesCapture: true }) })
    await wrapper.setProps({ run: verificationRun('automatic-ok', 'gpt-6-astra', { stage: 'automatic', requestState: true, stateSource: 'automatic', usageLogStateSent: true }) })

    expect(wrapper.get('[data-stage="baseline"]').text()).toContain('待执行')
    expect(wrapper.get('.overall-status').text()).toContain('验收未完成')
    expect(wrapper.text()).not.toContain('自动注入已验收')
  })

  it.each<StateVerificationStage>(['baseline', 'capture', 'replay', 'automatic'])(
    'does not mark %s successful when the workbench result reports failure',
    async stage => {
      const wrapper = mount(DebugStateComparison)
      await wrapper.setProps({ run: verificationRun(`failed-${stage}`, 'gpt-6-astra', {
        stage,
        success: false,
        responseState: stage === 'capture',
        requestState: stage === 'replay' || stage === 'automatic',
        stateMatchesCapture: stage === 'replay',
        stateSource: stage === 'automatic' ? 'automatic' : 'none',
        usageLogStateSent: stage === 'replay' || stage === 'automatic'
      }) })

      expect(wrapper.get(`[data-stage="${stage}"]`).text()).toContain('执行失败')
    }
  )

  it('does not let model evidence hide a non-2xx upstream attempt', async () => {
    const wrapper = mount(DebugStateComparison)
    await wrapper.setProps({ run: verificationRun('http-failed', 'gpt-6-astra', { stage: 'capture', responseState: true, attemptStatus: 429 }) })

    const capture = wrapper.get('[data-stage="capture"]')
    expect(capture.text()).toContain('上游 HTTP 失败')
    expect(capture.text()).toContain('429')
    expect(capture.text()).not.toContain('模型匹配')
  })

  it('keeps a model-rich record unconfirmed when no upstream attempt exists', async () => {
    const wrapper = mount(DebugStateComparison)
    await wrapper.setProps({ run: verificationRun('missing-attempt', 'gpt-6-astra', { stage: 'capture', responseState: true, omitAttempts: true }) })

    expect(wrapper.get('[data-stage="capture"]').text()).toContain('上游响应未确认')
    expect(wrapper.get('[data-stage="capture"]').text()).not.toContain('模型匹配')
  })

  it('keeps a model-rich record unconfirmed when the attempt has no response', async () => {
    const wrapper = mount(DebugStateComparison)
    await wrapper.setProps({ run: verificationRun('missing-response', 'gpt-6-astra', { stage: 'automatic', requestState: true, stateSource: 'automatic', usageLogStateSent: true, omitResponse: true }) })

    expect(wrapper.get('[data-stage="automatic"]').text()).toContain('上游响应未确认')
    expect(wrapper.get('[data-stage="automatic"]').text()).not.toContain('模型匹配')
  })

  it('does not accept a replay that merely sent some state without exact capture equality', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:19:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('capture-ok', 'gpt-6-astra', { stage: 'capture', responseState: true }) })
    await wrapper.setProps({ run: verificationRun('replay-wrong', 'gpt-6-astra', { stage: 'replay', requestState: true, stateMatchesCapture: false }) })

    expect(wrapper.get('[data-stage="replay"]').text()).toContain('回放未验证')
    expect(wrapper.get('[data-stage="replay"]').text()).toContain('完全一致')
    expect(wrapper.get('.overall-status').text()).toContain('验收未完成')
  })

  it('does not trust usage_log_verified when the durable API key evidence belongs to another key', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:19:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('baseline-ok', 'gpt-5.6-luna') })
    await wrapper.setProps({ run: verificationRun('capture-ok', 'gpt-6-astra', { stage: 'capture', responseState: true }) })
    await wrapper.setProps({ run: verificationRun('replay-ok', 'gpt-6-astra', { stage: 'replay', requestState: true, stateMatchesCapture: true }) })
    await wrapper.setProps({ run: verificationRun('automatic-wrong-key', 'gpt-6-astra', { stage: 'automatic', requestState: true, stateSource: 'automatic', usageLogStateSent: true, usageAPIKeyID: 20 }) })

    expect(wrapper.get('[data-stage="automatic"]').text()).toContain('usage 未确认')
    expect(wrapper.get('.overall-status').text()).toContain('证据未闭合')
    expect(wrapper.text()).not.toContain('自动注入已验收')
  })

  it('clears records when account or requested model scope changes', async () => {
    const wrapper = mount(DebugStateComparison, { props: { scopeKey: '7:gpt-6-astra' } })
    await wrapper.setProps({ run: verificationRun('baseline-1', 'gpt-5.6-luna') })
    expect(wrapper.get('[data-stage="baseline"]').text()).toContain('gpt-5.6-luna')
    await wrapper.setProps({ scopeKey: '8:gpt-6-astra' })
    expect(wrapper.get('[data-stage="baseline"]').text()).toContain('待执行')
    expect(wrapper.get('.overall-status').text()).toContain('等待真实请求')
  })
})
