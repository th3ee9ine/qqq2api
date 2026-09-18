<template>
  <section class="state-comparison" aria-labelledby="state-comparison-title">
    <header class="comparison-header">
      <div>
        <span class="comparison-kicker">MODEL / STATE ACCEPTANCE</span>
        <h2 id="state-comparison-title">模型与 State 验收</h2>
        <p class="comparison-note">阶段标签只归档真实执行结果，不改写 model 或 state；自动缓存来源需服务端证据。</p>
      </div>
      <div class="overall-status" :class="`status-${overall.tone}`" role="status">
        <span class="status-dot" aria-hidden="true"></span>
        <span>{{ overall.label }}</span>
      </div>
    </header>

    <div class="phase-selector" role="tablist" aria-label="选择本次请求的验收阶段">
      <button
        v-for="(stage, index) in stages"
        :id="`state-stage-tab-${stage.key}`"
        :key="stage.key"
        type="button"
        role="tab"
        :aria-selected="activeStage === stage.key"
        :tabindex="activeStage === stage.key ? 0 : -1"
        :class="{ active: activeStage === stage.key }"
        :disabled="running"
        @click="selectStage(stage.key)"
        @keydown="moveStage($event, index)"
      >
        <span>{{ String(index + 1).padStart(2, '0') }}</span>
        {{ stage.label }}
      </button>
    </div>

    <div class="comparison-grid">
      <article v-for="(stage, index) in stages" :key="stage.key" class="comparison-stage" :data-stage="stage.key">
        <div class="stage-heading">
          <div><span>{{ String(index + 1).padStart(2, '0') }}</span><h3>{{ stage.label }}</h3></div>
          <span class="stage-verdict" :class="`verdict-${recordStatus(stage.key).tone}`">{{ recordStatus(stage.key).label }}</span>
        </div>
        <template v-if="records[stage.key]">
          <dl class="evidence-list">
            <dt>请求模型</dt><dd>{{ records[stage.key]?.requestedModel || '未记录' }}</dd>
            <dt>预期上游模型</dt><dd>{{ records[stage.key]?.expectedModel || '未确认' }}</dd>
            <dt>response.created</dt><dd :class="modelClass(records[stage.key], 'created')">{{ records[stage.key]?.models.created || '缺失' }}</dd>
            <dt>response.completed</dt><dd :class="modelClass(records[stage.key], 'completed')">{{ records[stage.key]?.models.completed || '缺失' }}</dd>
            <dt>实际账号</dt><dd>{{ records[stage.key]?.actualAccountID ? `#${records[stage.key]?.actualAccountID}` : '未确认' }}</dd>
            <dt>API Key</dt><dd>{{ records[stage.key]?.apiKeyID ? `#${records[stage.key]?.apiKeyID}` : '未确认' }}</dd>
            <dt>usage 账号</dt><dd>{{ records[stage.key]?.usageLogAccountID ? `#${records[stage.key]?.usageLogAccountID}` : '未确认' }}</dd>
            <dt>usage API Key</dt><dd>{{ records[stage.key]?.usageLogAPIKeyID ? `#${records[stage.key]?.usageLogAPIKeyID}` : '未确认' }}</dd>
            <dt>实际出口</dt><dd>{{ records[stage.key]?.proxy || '未确认' }}</dd>
            <dt>发送 State</dt><dd>{{ stateLabel(records[stage.key]?.stateSent) }}</dd>
            <dt>响应 State</dt><dd>{{ stateLabel(records[stage.key]?.stateReceived, true) }}</dd>
            <dt>usage 请求模型</dt><dd>{{ records[stage.key]?.usageLogRequestedModel || '未确认' }}</dd>
            <dt>usage 实际模型</dt><dd>{{ records[stage.key]?.upstreamResponseModel || '未确认' }}</dd>
            <dt>HTTP</dt><dd>{{ records[stage.key]?.httpStatus || '未返回' }}</dd>
            <template v-if="stage.key === 'replay'">
              <dt>日常出口复验</dt><dd>{{ records.replay?.dailyRouteVerified ? '已通过' : '未通过' }}</dd>
              <dt>日常出口模型</dt><dd>{{ records.replay?.dailyModel || '同采集出口，无需切换' }}</dd>
              <dt>原子替换</dt><dd>{{ records.replay?.statePublished ? '已发布' : '未发布，保留旧值' }}</dd>
            </template>
          </dl>
          <p class="stage-reason" :class="`reason-${records[stage.key]?.models.verdict}`">{{ records[stage.key]?.models.reason }}</p>
          <p v-if="stage.key === 'replay' && !records[stage.key]?.stateMatchesCapture" class="source-warning">后端尚未证明回放 State 与本次采集候选完全一致。</p>
          <p v-if="stage.key === 'automatic' && !records[stage.key]?.automaticSourceConfirmed" class="source-warning">自动缓存来源尚未由后端证据确认。</p>
          <p v-if="stage.key === 'automatic' && !records[stage.key]?.usageLogConfirmed" class="source-warning">usage_logs 尚未闭合本次 request、账号、API Key、请求模型、实际模型和 State 证据。</p>
        </template>
        <div v-else class="stage-empty">
          <span>待执行</span>
          <p>{{ activeStage === stage.key ? '下一次请求将记录到此阶段' : '尚无真实执行记录' }}</p>
        </div>
      </article>
    </div>

    <footer class="comparison-footer">
      <span>当前记录阶段：<strong>{{ activeStageLabel }}</strong></span>
      <span>HTTP 200 不代表模型匹配；缺少真实响应模型时保持未确认。</span>
      <button type="button" :disabled="running || !hasRecords" @click="clearRecords">清空对比</button>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, reactive, watch } from 'vue'
import type { DebugVerificationStage, DebugWorkbenchResult } from '@/api/admin/debugWorkbench'
import {
  compareResponseModels,
  expectedUpstreamModel,
  extractRequestModel,
  extractResponseModelEvidence,
  extractTurnStateEvidence,
  modelMatchesExpected,
  type ModelComparison,
  type StateHeaderEvidence
} from '@/utils/codexStateVerification'

export type StateVerificationStage = DebugVerificationStage

export interface StateVerificationRun {
  requestID: string
  stage: StateVerificationStage
  requestedModel?: string
  selectedAccountID?: number
  apiKeyID?: number
  result: DebugWorkbenchResult
}

type BackendVerificationEvidence = {
  requested_model?: string
  response_created_model?: string
  response_completed_model?: string
  response_model?: string
  state_sent?: boolean
  state_received?: boolean
  state_length?: number
  state_source?: string
  actual_account_id?: number
  usage_log_account_id?: number
  usage_log_api_key_id?: number
  usage_log_requested_model?: string
  upstream_response_model?: string
  usage_log_state_sent?: boolean
  usage_log_verified?: boolean
  state_matches_capture?: boolean
  state_published?: boolean
  daily_route_verified?: boolean
}

type VerificationRecord = {
  requestID: string
  requestedModel?: string
  expectedModel?: string
  actualAccountID?: number
  usageLogAccountID?: number
  apiKeyID?: number
  usageLogAPIKeyID?: number
  usageLogRequestedModel?: string
  upstreamResponseModel?: string
  proxy?: string
  httpStatus?: number
  executionSucceeded: boolean
  upstreamResponseObserved: boolean
  upstreamHTTPConfirmed: boolean
  models: ModelComparison
  stateSent: StateHeaderEvidence
  stateReceived: StateHeaderEvidence
  lifecycleComplete: boolean
  stateMatchesCapture: boolean
  automaticSourceConfirmed: boolean
  usageLogStateSent: boolean
  usageLogConfirmed: boolean
  statePublished: boolean
  dailyRouteVerified: boolean
  dailyModel?: string
}

const props = defineProps<{ stage?: StateVerificationStage; run?: StateVerificationRun | null; running?: boolean; scopeKey?: string }>()
const emit = defineEmits<{ 'update:stage': [stage: StateVerificationStage] }>()
const stages: { key: StateVerificationStage; label: string }[] = [
  { key: 'baseline', label: '原始请求' },
  { key: 'capture', label: '新出口采集' },
  { key: 'replay', label: '同账号回放' },
  { key: 'automatic', label: '自动注入验收' }
]
const activeStage = computed<StateVerificationStage>(() => props.stage || 'baseline')
const records = reactive<Record<StateVerificationStage, VerificationRecord | null>>({ baseline: null, capture: null, replay: null, automatic: null })
const recordedRequestIDs = new Set<string>()
let recordedScope = ''

const activeStageLabel = computed(() => stages.find(stage => stage.key === activeStage.value)?.label || '')
const hasRecords = computed(() => stages.some(stage => records[stage.key]))
const overall = computed(() => {
  if (!hasRecords.value) return { tone: 'idle', label: '等待真实请求' }
  const baseline = records.baseline
  const capture = records.capture
  const replay = records.replay
  const automatic = records.automatic
  if ([capture, replay, automatic].some(record => record?.models.verdict === 'mismatch')) return { tone: 'error', label: '模型不匹配' }
  if (!baseline || !capture || !replay || !automatic) return { tone: 'pending', label: '验收未完成' }
  const required = [baseline, capture, replay, automatic]
  const acceptance = [capture, replay, automatic]
  const accountIDs = required.map(record => record.actualAccountID)
  const usageAccountIDs = required.map(record => record.usageLogAccountID)
  const apiKeyIDs = required.map(record => record.apiKeyID)
  const usageAPIKeyIDs = required.map(record => record.usageLogAPIKeyID)
  const sameAccount = accountIDs.every(id => id != null && id === accountIDs[0]) && usageAccountIDs.every(id => id != null && id === accountIDs[0])
  const sameAPIKey = apiKeyIDs.every(id => id != null && id === apiKeyIDs[0]) && usageAPIKeyIDs.every(id => id != null && id === apiKeyIDs[0])
  const modelsPass = acceptance.every(record => record.models.verdict === 'pass' && record.lifecycleComplete)
  const executionsPass = acceptance.every(record => record.executionSucceeded && record.upstreamHTTPConfirmed)
  const captured = capture.stateReceived.status === 'present'
  const replayed = replay.stateSent.status === 'present' && replay.stateMatchesCapture && replay.dailyRouteVerified && replay.statePublished
  const usageVerified = acceptance.every(usageEvidenceMatches) && replay.usageLogStateSent && automatic.usageLogStateSent
  if (baselineEvidenceComplete(baseline) && sameAccount && sameAPIKey && modelsPass && executionsPass && captured && replayed && automatic.automaticSourceConfirmed && usageVerified) return { tone: 'success', label: '自动注入已验收' }
  return { tone: 'pending', label: '证据未闭合' }
})

function optionalBackendEvidence(result: DebugWorkbenchResult): BackendVerificationEvidence {
  const value = (result as DebugWorkbenchResult & { state_verification?: BackendVerificationEvidence }).state_verification
  return value && typeof value === 'object' ? value : {}
}

function recordRun(run: StateVerificationRun): void {
  if (!run.requestID || recordedRequestIDs.has(run.requestID)) return
  if (!recordedScope) recordedScope = props.scopeKey || ''
  const attempts = run.result.attempts || []
  const attempt = [...attempts].reverse().find(item => item.response) || attempts[attempts.length - 1]
  const backend = optionalBackendEvidence(run.result)
  const extracted = extractResponseModelEvidence(attempt?.response)
  if (backend.response_created_model) extracted.created = backend.response_created_model
  if (backend.response_completed_model) extracted.completed = backend.response_completed_model
  if (backend.response_model && !extracted.body) extracted.body = backend.response_model
  if ((backend.response_created_model || backend.response_completed_model) && extracted.source === 'none') extracted.source = 'sse'
  if (backend.response_model && extracted.source === 'none') extracted.source = 'json'
  const requestedModel = backend.requested_model || run.requestedModel
  const stateSent = extractTurnStateEvidence(attempt?.request)
  const stateReceived = extractTurnStateEvidence(attempt?.response)
  if (typeof backend.state_sent === 'boolean') stateSent.status = backend.state_sent ? 'present' : 'absent'
  if (typeof backend.state_received === 'boolean') stateReceived.status = backend.state_received ? 'present' : 'absent'
  if (typeof backend.state_length === 'number' && backend.state_length >= 0) stateReceived.length = backend.state_length
  const expectedModel = expectedUpstreamModel(requestedModel, extractRequestModel(attempt?.request))
  const actualAccountID = backend.actual_account_id || attempt?.account_id || run.selectedAccountID
  const comparison = compareResponseModels(requestedModel, extracted, expectedModel)
  records[run.stage] = {
    requestID: run.requestID,
    requestedModel,
    expectedModel,
    actualAccountID,
    usageLogAccountID: backend.usage_log_account_id,
    apiKeyID: run.apiKeyID,
    usageLogAPIKeyID: backend.usage_log_api_key_id,
    usageLogRequestedModel: backend.usage_log_requested_model,
    upstreamResponseModel: backend.upstream_response_model,
    proxy: attempt?.proxy,
    httpStatus: attempt?.response?.status_code,
    executionSucceeded: run.result.success === true,
    upstreamResponseObserved: Boolean(attempt?.response),
    upstreamHTTPConfirmed: Boolean(attempt?.response?.status_code && attempt.response.status_code >= 200 && attempt.response.status_code < 300),
    models: comparison,
    stateSent,
    stateReceived,
    lifecycleComplete: Boolean(extracted.created && extracted.completed),
    stateMatchesCapture: backend.state_matches_capture === true,
    automaticSourceConfirmed: backend.state_source === 'automatic',
    usageLogStateSent: backend.usage_log_state_sent === true,
    usageLogConfirmed: backend.usage_log_verified === true,
    statePublished: backend.state_published === true,
    dailyRouteVerified: backend.daily_route_verified === true,
    dailyModel: run.result.daily_replay?.state_verification?.response_completed_model
  }
  recordedRequestIDs.add(run.requestID)
  const current = stages.findIndex(stage => stage.key === run.stage)
	const mayAdvance = run.stage === 'baseline'
		? baselineEvidenceComplete(records.baseline)
		: recordStatus(run.stage).tone === 'success'
  if (mayAdvance && current >= 0 && current < stages.length - 1) emit('update:stage', stages[current + 1].key)
}

function clearRecords(): void {
  for (const stage of stages) records[stage.key] = null
  emit('update:stage', 'baseline')
  recordedRequestIDs.clear()
}

function recordStatus(stage: StateVerificationStage) {
  const record = records[stage]
  if (!record) return { tone: 'idle', label: '待执行' }
  if (!record.executionSucceeded) return { tone: 'error', label: '执行失败' }
  if (!record.upstreamHTTPConfirmed) return record.upstreamResponseObserved
    ? { tone: 'error', label: '上游 HTTP 失败' }
    : { tone: 'pending', label: '上游响应未确认' }
  if (record.models.verdict === 'mismatch') return { tone: 'error', label: '模型不匹配' }
  if (record.models.verdict === 'unknown' || !record.lifecycleComplete) return { tone: 'pending', label: '模型未确认' }
  if (stage === 'capture' && record.stateReceived.status !== 'present') return { tone: 'error', label: '未采集 State' }
  if (stage === 'replay' && (record.stateSent.status !== 'present' || !record.stateMatchesCapture)) return { tone: 'error', label: '回放未验证' }
  if (stage === 'replay' && (!record.dailyRouteVerified || !record.statePublished)) return { tone: 'pending', label: '替换未完成' }
  if (stage === 'automatic' && (!record.automaticSourceConfirmed || !record.usageLogStateSent)) return { tone: 'pending', label: '注入未确认' }
  if (!usageEvidenceMatches(record)) return { tone: 'pending', label: 'usage 未确认' }
  return { tone: 'success', label: '模型匹配' }
}

function modelClass(record: VerificationRecord | null, key: 'created' | 'completed') {
  if (!record) return ''
  const actual = key === 'created' ? record.models.created : record.models.completed || record.models.body
  if (!actual) return 'value-unknown'
  return record.models.expected && modelMatchesExpected(record.models.expected, actual) ? 'value-pass' : 'value-error'
}

function usageEvidenceMatches(record: VerificationRecord): boolean {
  return record.usageLogConfirmed &&
    record.actualAccountID != null && record.usageLogAccountID === record.actualAccountID &&
    record.apiKeyID != null && record.usageLogAPIKeyID === record.apiKeyID &&
    Boolean(record.requestedModel && record.usageLogRequestedModel === record.requestedModel) &&
    Boolean(record.expectedModel && record.upstreamResponseModel && modelMatchesExpected(record.expectedModel, record.upstreamResponseModel))
}

function baselineEvidenceComplete(record: VerificationRecord | null): boolean {
	return Boolean(record && record.executionSucceeded && record.upstreamHTTPConfirmed &&
		record.lifecycleComplete && record.stateSent.status === 'absent' && record.usageLogConfirmed &&
		record.actualAccountID != null && record.usageLogAccountID === record.actualAccountID &&
		record.apiKeyID != null && record.usageLogAPIKeyID === record.apiKeyID &&
		record.requestedModel && record.usageLogRequestedModel === record.requestedModel &&
		record.models.completed && record.upstreamResponseModel === record.models.completed)
}

function stateLabel(evidence?: StateHeaderEvidence, includeLength = false): string {
  if (!evidence || evidence.status === 'absent') return '未观察到'
  const length = includeLength && evidence.length != null ? ` · ${evidence.length} 字节` : ''
  if (evidence.redacted) return `已观察到${length} · 内容脱敏`
  if (evidence.status === 'unknown') return `未确认${length}`
  return `已观察到${length}`
}

async function moveStage(event: KeyboardEvent, index: number) {
  if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? stages.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + stages.length) % stages.length
  selectStage(stages[next].key)
  await nextTick()
  document.getElementById(`state-stage-tab-${stages[next].key}`)?.focus()
}

function selectStage(stage: StateVerificationStage): void {
  if (!props.running) emit('update:stage', stage)
}

watch(() => props.scopeKey, value => {
  if (recordedScope && value !== recordedScope) clearRecords()
  recordedScope = value || ''
})
watch(() => props.run, value => { if (value) recordRun(value) }, { deep: false })
</script>

<style scoped>
.state-comparison { margin-bottom: 24px; border: 1px solid var(--wb-border); border-radius: 8px; background: var(--wb-surface); overflow: hidden; }
.comparison-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 18px 22px; border-bottom: 1px solid var(--wb-border); }
.comparison-kicker { display: block; color: var(--wb-muted); font: 11px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
.comparison-header h2 { margin-top: 3px; font-size: 15px; font-weight: 600; }
.comparison-note { margin-top: 4px; color: var(--wb-muted); font-size: 11px; line-height: 1.5; }
.overall-status { display: inline-flex; align-items: center; gap: 7px; min-width: 0; font-size: 12px; font-weight: 600; white-space: nowrap; }
.status-idle, .status-pending { color: var(--wb-muted); }
.status-error { color: var(--wb-error); }
.status-success { color: #21835f; }
.status-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; flex-shrink: 0; }
.phase-selector { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); border-bottom: 1px solid var(--wb-border); background: var(--wb-subtle); }
.phase-selector button { display: flex; align-items: center; justify-content: center; gap: 8px; min-height: 44px; padding: 8px 10px; border-right: 1px solid var(--wb-border); color: var(--wb-muted); font-size: 12px; }
.phase-selector button:last-child { border-right: 0; }
.phase-selector button span { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.phase-selector button.active { color: var(--wb-accent); background: var(--wb-surface); box-shadow: inset 0 -2px currentColor; font-weight: 600; }
.comparison-grid { display: grid; grid-template-columns: repeat(4,minmax(0,1fr)); }
.comparison-stage { min-width: 0; min-height: 295px; padding: 16px; border-right: 1px solid var(--wb-border); }
.comparison-stage:last-child { border-right: 0; }
.stage-heading, .stage-heading > div { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.stage-heading > div { justify-content: flex-start; min-width: 0; }
.stage-heading > div > span { color: var(--wb-muted); font: 11px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
.stage-heading h3 { min-width: 0; font-size: 13px; font-weight: 600; overflow-wrap: anywhere; }
.stage-verdict { flex-shrink: 0; padding: 2px 5px; border-radius: 4px; background: var(--wb-subtle); font-size: 10px; font-weight: 600; }
.verdict-idle, .verdict-pending { color: var(--wb-muted); }
.verdict-error { color: var(--wb-error); }
.verdict-success { color: #21835f; }
.evidence-list { display: grid; grid-template-columns: minmax(80px,.8fr) minmax(0,1.2fr); gap: 7px 9px; margin-top: 16px; font-size: 11px; line-height: 1.55; }
.evidence-list dt { color: var(--wb-muted); }
.evidence-list dd { min-width: 0; text-align: right; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; overflow-wrap: anywhere; }
.value-pass { color: #21835f; }
.value-error { color: var(--wb-error); font-weight: 600; }
.value-unknown { color: var(--wb-warning); }
.stage-reason, .source-warning { margin-top: 13px; padding-top: 11px; border-top: 1px solid var(--wb-border); color: var(--wb-muted); font-size: 11px; line-height: 1.65; overflow-wrap: anywhere; }
.reason-mismatch, .source-warning { color: var(--wb-error); }
.reason-pass { color: #21835f; }
.source-warning { margin-top: 7px; padding-top: 0; border-top: 0; color: var(--wb-warning); }
.stage-empty { min-height: 224px; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; color: var(--wb-muted); }
.stage-empty span { font: 12px/1.5 ui-monospace, SFMono-Regular, Menlo, monospace; }
.stage-empty p { margin-top: 7px; font-size: 11px; }
.comparison-footer { display: flex; align-items: center; gap: 16px; padding: 12px 22px; border-top: 1px solid var(--wb-border); background: var(--wb-subtle); color: var(--wb-muted); font-size: 11px; }
.comparison-footer span:nth-child(2) { margin-left: auto; text-align: right; }
.comparison-footer button { flex-shrink: 0; padding: 4px 7px; border: 1px solid var(--wb-border); border-radius: 4px; color: var(--wb-muted); }
.comparison-footer button:hover:not(:disabled) { color: var(--wb-accent); border-color: var(--wb-accent); }
@media (max-width: 1120px) { .comparison-grid { grid-template-columns: repeat(2,minmax(0,1fr)); } .comparison-stage:nth-child(2) { border-right: 0; } .comparison-stage:nth-child(-n+2) { border-bottom: 1px solid var(--wb-border); } }
@media (max-width: 700px) { .comparison-header { align-items: flex-start; flex-direction: column; } .phase-selector { grid-template-columns: repeat(2,minmax(0,1fr)); } .phase-selector button:nth-child(2) { border-right: 0; } .phase-selector button:nth-child(-n+2) { border-bottom: 1px solid var(--wb-border); } .comparison-grid { grid-template-columns: minmax(0,1fr); } .comparison-stage, .comparison-stage:nth-child(2) { border-right: 0; border-bottom: 1px solid var(--wb-border); } .comparison-stage:last-child { border-bottom: 0; } .comparison-footer { align-items: flex-start; flex-direction: column; } .comparison-footer span:nth-child(2) { margin-left: 0; text-align: left; } }
</style>
