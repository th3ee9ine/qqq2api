<template>
  <AppLayout>
    <div class="workbench">
      <header class="workspace-header">
        <div>
          <div class="workspace-eyebrow"><span class="workspace-mark"><Icon name="terminal" size="sm" /></span> DEVELOPER WORKSPACE <span class="eyebrow-divider">/</span> PLAYGROUND</div>
          <h1>上游调试工作台<span class="heading-period">.</span></h1>
          <p class="workspace-description">构造请求，探索模型，在同一视图检查调试链路。</p>
        </div>
        <div class="header-actions">
          <span class="session-badge" :class="`session-${sessionState.tone}`" role="status"><span class="status-dot"></span>{{ sessionState.label }}</span>
          <button type="button" class="secondary-button" :disabled="running || defaultsLoading" @click="resetAll"><Icon name="refresh" size="sm" /> 重置参数</button>
        </div>
      </header>

      <section class="connection-panel" aria-labelledby="connection-title">
        <div class="panel-kicker"><h2 id="connection-title"><Icon name="server" size="sm" /> 连接配置</h2><span>01 <span class="kicker-divider">/</span> CONFIGURATION</span></div>
        <div class="connection-grid">
          <div class="connection-field">
            <label for="debug-account" class="field-label"><span>GPT 账号</span><span class="count-badge">{{ accounts.length }}</span></label>
            <div class="control-with-icon"><Icon name="userCircle" size="md" /><select id="debug-account" v-model="form.account" class="workbench-input" :disabled="running || accountsLoading || !accounts.length"><option v-if="accountsLoading" value="">正在加载账号…</option><option v-else-if="!accounts.length" value="">暂无可用 GPT 账号</option><option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }} · {{ a.typeLabel }} · {{ a.email }}</option></select></div>
            <p class="field-hint" :title="selectedAccount ? `${selectedAccount.email} · ${selectedAccount.platform} · ID ${selectedAccount.id}` : ''">{{ accountsLoading ? '正在加载账号…' : selectedAccount ? `${selectedAccount.typeLabel} · ${selectedAccount.email} · ID ${selectedAccount.id}` : '请选择调试账号' }}</p>
            <p v-if="accountsError" class="field-warning">{{ accountsError }}</p>
          </div>
          <div class="connection-field">
            <label for="debug-model" class="field-label"><span>模型</span><span class="field-label-note">支持自定义</span></label>
            <div class="control-with-icon"><Icon name="cpu" size="md" /><input id="debug-model" v-model="form.model" list="debug-model-options" class="workbench-input model-input" :disabled="running || modelsLoading || defaultsLoading" placeholder="选择或输入模型 ID" autocomplete="off" @change="commitCustomModel" @keydown.enter.prevent="commitCustomModel" /></div>
            <datalist id="debug-model-options"><option v-for="m in models" :key="m.id" :value="m.id">{{ m.display_name || m.id }}</option></datalist>
            <p class="field-hint">{{ modelsLoading ? '正在同步该账号的模型…' : selectedModel?.type === 'custom' || (!selectedModel && form.model) ? '自定义模型 · 按 Enter 确认' : `${models.length} 个候选模型 · 可直接输入模型 ID` }}</p>
            <p v-if="modelsError" class="field-warning">{{ modelsError }}</p>
          </div>
          <div class="connection-field">
            <label for="debug-endpoint" class="field-label">请求方式</label>
            <div class="control-with-icon"><Icon name="swap" size="md" /><select id="debug-endpoint" v-model="form.endpoint" class="workbench-input" :disabled="running"><option value="chat/completions">Chat Completions</option><option value="responses">Responses API</option><option value="images/generations">Images Generation</option></select></div>
            <p class="field-hint">{{ form.endpoint === 'images/generations' ? '图像生成接口' : '文本与多模态接口' }}</p>
          </div>
          <div class="connection-field proxy-field">
            <label for="debug-proxy" class="field-label">代理 IP</label>
            <select id="debug-proxy" v-model="form.proxyId" class="workbench-input" :disabled="running"><option :value="null">跟随账号配置</option><option :value="0">直连 · 不使用代理</option><option v-for="proxy in proxies" :key="proxy.id" :value="proxy.id">{{ proxy.name }} · {{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}</option></select>
            <p class="field-hint">{{ selectedProxy ? `${selectedProxy.protocol} · ${selectedProxy.host}:${selectedProxy.port}` : form.proxyId === 0 ? '仅本次请求直连，不修改账号配置' : '使用所选账号的默认代理' }}</p>
          </div>
        </div>
        <div class="debug-context">
          <div class="context-topline"><div><span class="context-label">会话上下文</span><span class="transport-badge">HTTP / SSE</span></div><button type="button" class="preset-button" :disabled="running" @click="resetSession">新建会话</button></div>
          <div class="context-controls"><label for="debug-turn-action">下一次请求</label><select id="debug-turn-action" v-model="sessionAction" class="workbench-input" :disabled="running || !debugSession"><option value="new_turn">新回合 · 保留会话 / 线程</option><option value="continue_turn">续接当前回合 · 复用上游状态</option></select><span class="context-state">{{ debugSession ? `第 ${debugSession.turn_index} 回合 · ${debugSession.turn_state_available ? '已有上游 Turn State' : '暂无上游 Turn State'}` : '首次发送时创建会话，动态 IDs 由服务端维护' }}</span></div>
          <details v-if="debugSession" class="context-ids"><summary>查看会话标识</summary><dl><template v-for="(value, key) in sessionIdentifiers" :key="key"><dt>{{ key }}</dt><dd>{{ value }}</dd></template></dl></details>
          <p class="context-note">{{ sessionAction === 'continue_turn' && debugSession ? '续接复用当前回合；服务端仅复用本账号、本会话收到的 Turn State。' : '新回合保留 Session / Thread / Window，生成新的 Turn ID，并清空上一回合状态。' }} 对话消息以完整 JSON 为准，不自动追加历史。</p>
        </div>
        <p v-if="isFixtureAccount && !accountsLoading" class="preview-only-note">当前仅预览参数模板。选择已配置的 GPT 账号后可发送真实请求；这里不会模拟成功响应。</p>
        <div class="request-bar">
          <div class="endpoint-address"><span class="method-badge">POST</span><code :title="upstreamUrl">{{ upstreamUrl }}</code></div>
          <button type="button" class="send-button" :disabled="running || defaultsLoading || accountsLoading || modelsLoading || isFixtureAccount" @click="runRequest"><Icon :name="running ? 'refresh' : 'play'" size="sm" :class="running && 'animate-spin'" /><span>{{ running ? '正在发送…' : isFixtureAccount ? '等待选择账号' : '发送请求' }}</span><Icon name="arrowRight" size="sm" class="send-arrow" /></button>
        </div>
      </section>

      <div class="workspace-columns">
        <section class="editor-panel" aria-labelledby="editor-title">
          <div class="section-heading"><div><span class="section-number">02</span><h2 id="editor-title">请求编辑器</h2></div><span class="section-caption">REQUEST</span></div>
          <div class="editor-tabs" role="tablist" aria-label="请求编辑方式">
            <button v-for="tab in editorTabs" :id="`editor-tab-${tab.key}`" :key="tab.key" type="button" role="tab" :aria-selected="editorTab === tab.key" :aria-controls="`editor-panel-${tab.key}`" :tabindex="editorTab === tab.key ? 0 : -1" :class="{ 'is-active': editorTab === tab.key }" @click="editorTab = tab.key" @keydown="navigateTabs($event, editorTabs, editorTab, selectEditorTab)"><Icon :name="tab.icon" size="sm" />{{ tab.label }}</button>
          </div>
          <div v-if="responseStatus && !responseSuccessful" class="request-error" role="alert"><Icon name="exclamationCircle" size="sm" /><span>{{ responseError || '本次请求未成功，请在响应详情中检查错误信息。' }}</span></div>
          <div v-show="editorTab === 'message'" id="editor-panel-message" role="tabpanel" aria-labelledby="editor-tab-message" class="editor-body">
            <div class="preset-row"><span>快捷场景</span><button v-for="preset in presets" :key="preset.label" type="button" class="preset-button" :disabled="running || defaultsLoading" :class="{ 'is-selected': form.prompt === preset.prompt }" @click="applyPreset(preset)">{{ preset.label }}</button></div>
            <label class="editor-field"><span class="field-label">系统提示词<span class="field-label-note">SYSTEM</span></span><textarea v-model="form.system" :disabled="running || defaultsLoading || form.endpoint === 'images/generations'" rows="3" class="workbench-input" placeholder="设定模型的角色与行为…"></textarea></label>
            <label class="editor-field"><span class="field-label">用户消息<span class="field-label-note">USER</span></span><textarea v-model="form.prompt" :disabled="running || defaultsLoading" rows="6" class="workbench-input prompt-input" placeholder="输入要调试的内容…"></textarea></label>
            <div class="parameter-grid"><label class="editor-field"><span class="field-label">Temperature</span><input v-model.number="form.temperature" :disabled="running || defaultsLoading || form.endpoint !== 'chat/completions'" type="number" min="0" max="2" step="0.1" class="workbench-input" /><span class="field-hint">{{ form.endpoint === 'chat/completions' ? '0 表示使用接口默认值' : '该接口请在 JSON 中配置支持的字段' }}</span></label><label class="editor-field"><span class="field-label">最大 Tokens</span><input v-model.number="form.maxTokens" :disabled="running || defaultsLoading || form.endpoint !== 'chat/completions'" type="number" min="0" class="workbench-input" /><span class="field-hint">{{ form.endpoint === 'chat/completions' ? '0 表示不指定上限' : '保留完整 JSON 中的参数' }}</span></label></div>
          </div>
          <div v-show="editorTab === 'json'" id="editor-panel-json" role="tabpanel" aria-labelledby="editor-tab-json" class="editor-body">
            <div class="editor-description"><span>完整 API 参数</span><span class="format-tag">JSON</span></div>
            <label class="sr-only" for="debug-api-params">完整 API 参数 JSON</label><textarea id="debug-api-params" v-model="form.apiParams" :disabled="running || defaultsLoading" rows="23" class="source-editor" spellcheck="false"></textarea>
            <p class="editor-note">此 JSON 是发送时的完整请求体。便捷控件只更新对应字段；自定义字段原样提交，由正式网关按账号规则处理。</p>
          </div>
          <div v-show="editorTab === 'headers'" id="editor-panel-headers" role="tabpanel" aria-labelledby="editor-tab-headers" class="editor-body">
            <div class="editor-description"><span>上游请求 Headers</span><span class="format-tag">{{ headerCount === null ? 'JSON 格式待修正' : `${headerCount} HEADERS` }}</span></div>
            <div class="headers-source-row"><span>{{ defaultsLoading ? '正在同步项目上游默认头…' : headersFromBackend ? '来自项目实际请求构造 · 已脱敏' : '基础示例 · 待同步后端配置' }}</span><button type="button" class="preset-button" :disabled="running || defaultsLoading" @click="restoreDefaultHeaders">恢复默认</button></div>
            <label class="sr-only" for="debug-headers">上游完整请求头 JSON</label><textarea id="debug-headers" v-model="form.headers" :disabled="running || defaultsLoading" rows="20" class="source-editor" spellcheck="false" aria-describedby="headers-default-notes"></textarea>
            <div id="headers-default-notes" class="headers-default-notes"><p class="editor-note"><Icon name="lock" size="sm" /> 鉴权值已隐藏；动态请求头以占位符展示，实际值由后端生成。</p><p v-if="hasRoutingHintDetail" class="editor-note routing-hint-note">OAuth Codex 的 X-Codex-Routing-Hint 由正式网关按最终上游模型重新生成，JSON 中的静态值不会直接发送。执行后可检查实际保留、重写、生成与过滤结果。</p><details v-if="defaultsNotes.length"><summary>默认头来源与动态字段说明</summary><ul><li v-for="(note, index) in defaultsNotes" :key="index">{{ note }}</li></ul></details></div>
            <DebugHeaderGuide :details="headerDetails" :headers="form.headers" :loading="defaultsLoading" />
          </div>
          <div v-show="editorTab === 'image'" id="editor-panel-image" role="tabpanel" aria-labelledby="editor-tab-image" class="editor-body">
            <div class="image-mode-row"><div><h3>生图测试</h3><p>提示词、生成参数与图像预览</p></div><label class="switch-control"><input v-model="imageMode" type="checkbox" :disabled="form.endpoint !== 'responses' || running || defaultsLoading" /><span class="switch-track" aria-hidden="true"></span><span>{{ imageMode ? '已启用' : '启用' }}</span></label></div>
            <p v-if="form.endpoint !== 'images/generations'" class="editor-note">{{ form.endpoint === 'responses' ? '启用后会在完整 JSON 中加入 image_generation 工具；工具参数请在 JSON 中编辑。' : '生图请切换到 Responses API 或 Images Generation。' }}</p><fieldset :disabled="!imageMode || running || defaultsLoading" class="image-fields"><legend class="sr-only">生图参数</legend><label class="editor-field"><span class="field-label">图像提示词</span><textarea v-model="imageForm.prompt" rows="4" class="workbench-input" placeholder="描述画面、光线、构图与风格…"></textarea></label><div class="parameter-grid"><label class="editor-field"><span class="field-label">生成数量</span><input :disabled="form.endpoint !== 'images/generations'" v-model.number="imageForm.n" type="number" min="1" max="10" class="workbench-input" /></label><label class="editor-field"><span class="field-label">响应格式</span><select :disabled="form.endpoint !== 'images/generations'" v-model="imageForm.responseFormat" class="workbench-input"><option value="b64_json">b64_json</option><option value="url">url</option></select></label></div></fieldset>
            <div class="image-preview"><img v-if="imagePreviewUrl && !imagePreviewFailed" :src="imagePreviewUrl" alt="生成结果预览" @error="imagePreviewFailed = true" /><div v-else class="preview-placeholder"><span class="preview-icon"><Icon name="beaker" size="lg" /></span><strong>{{ imagePreviewFailed ? '图像加载失败' : '让想法成为画面' }}</strong><p>{{ imagePreviewFailed ? '请在上游响应中检查图像地址。' : isFixtureAccount ? '尚未选择真实账号，仅显示参数模板。' : '启用生图后，生成结果将在这里展示。' }}</p></div></div>
          </div>
          <footer class="editor-footer"><label class="switch-control"><input v-model="form.stream" type="checkbox" :disabled="running || defaultsLoading || form.endpoint === 'images/generations'" /><span class="switch-track" aria-hidden="true"></span><span>流式响应</span></label><span class="format-tag">{{ form.endpoint === 'images/generations' ? 'JSON' : form.stream ? 'STREAM' : 'JSON' }}</span></footer>
        </section>

        <section class="inspector-panel" aria-labelledby="inspector-title">
          <div class="section-heading"><div><span class="section-number">03</span><h2 id="inspector-title">链路检查器</h2></div><span class="section-caption">INSPECTOR</span></div>
          <div class="trace-tabs" role="tablist" aria-label="调试链路阶段"><button v-for="(tab, index) in tabs" :id="`trace-tab-${tab.key}`" :key="tab.key" type="button" role="tab" :aria-selected="activeTab === tab.key" :aria-label="tab.label" :title="tab.label" :aria-controls="`trace-panel-${tab.key}`" :tabindex="activeTab === tab.key ? 0 : -1" :class="{ 'is-active': activeTab === tab.key }" @click="activeTab = tab.key" @keydown="navigateTabs($event, tabs, activeTab, selectTraceTab)"><span class="trace-step">{{ String(index + 1).padStart(2, '0') }}</span><span>{{ tab.shortLabel }}</span><Icon v-if="index < 3" name="chevronRight" size="sm" class="trace-arrow" /></button></div>
          <div v-if="result" class="attempt-toolbar"><label for="debug-attempt">上游尝试</label><select v-if="result.attempts.length" id="debug-attempt" v-model.number="selectedAttemptIndex" class="workbench-input"><option v-for="(attempt, index) in result.attempts" :key="attempt.index" :value="index">#{{ attempt.index }} · {{ attempt.response?.status_code || (attempt.error ? '错误' : '无响应') }} · {{ attempt.duration_ms }} ms</option></select><span v-else>本次未发出上游请求</span><span v-if="selectedAttempt?.ttft_ms != null" class="ttft-label" title="从上游调用开始，到首次读到响应 Body 字节；不等同于模型首个文本 Token">首字节 {{ selectedAttempt.ttft_ms }} ms</span></div>
          <div v-if="result?.warnings?.length || selectedAttempt?.error" class="trace-warnings" role="status"><p v-for="(warning, index) in result?.warnings || []" :key="index">{{ warning }}</p><p v-if="selectedAttempt?.error">{{ selectedAttempt.error }}</p></div>
          <div :id="`trace-panel-${activeTab}`" role="tabpanel" :aria-labelledby="`trace-tab-${activeTab}`" class="trace-content">
            <div class="inspector-toolbar"><div><span class="file-dot"></span><span>{{ activeTab }}.json</span></div><button type="button" class="copy-button" @click="copyCurrent"><Icon :name="copyState === 'copied' ? 'check' : 'copy'" size="sm" />{{ copyState === 'copied' ? '已复制' : copyState === 'error' ? '复制失败，请重试' : '复制' }}</button><span class="sr-only" role="status">{{ copyState === 'copied' ? 'JSON 已复制' : copyState === 'error' ? '复制失败' : '' }}</span></div>
            <div v-if="activeTab === 'upstream-response' && !responseStatus && !running" class="response-empty"><span class="response-empty-icon"><Icon name="terminal" size="xl" /></span><h3>等待第一条响应</h3><p>发送请求后，在这里检查实际响应头与 JSON / SSE 原文。</p><span class="empty-key">POST <span>/v1/{{ form.endpoint }}</span></span></div>
            <div v-else-if="activeTab === 'upstream-response' && running" class="response-empty" role="status"><Icon name="refresh" size="xl" class="animate-spin" /><h3>请求处理中</h3><p>正在采集 HTTP / SSE 响应，结束后展示完整记录或明确的截断标记…</p></div>
            <template v-else><div v-if="captureNotice" class="capture-notice" :class="{ 'capture-warning': captureNotice.warning }" role="status">{{ captureNotice.text }}</div><DebugJsonViewer :value="currentPayload" :label="tabDescription || 'JSON 详情'" /></template>
            <div class="inspector-status"><span><span class="status-dot" :class="running ? 'dot-running' : responseStatus && !responseSuccessful ? 'dot-error' : ''"></span>{{ running ? '请求进行中' : responseStatus ? `${result ? '实际执行' : submitted ? '提交请求' : '参数校验'} · ${responseStatus}` : '参数模板 · 尚未执行' }}</span><span>{{ requestDuration === null ? '— ms' : `${requestDuration} ms` }}<span class="status-divider">/</span>{{ payloadSize }}</span></div>
          </div>
          <DebugHeaderChanges v-if="result" :changes="selectedAttempt?.header_changes || []" /><div class="trace-notes"><div><Icon name="infoCircle" size="sm" /><p>{{ result ? '当前展示最近一次执行的脱敏记录。上游请求采集自 HTTP 客户端实际提交，非网络抓包；传输层自动字段不伪造。' : submitted ? '请求已提交，正在等待服务端执行记录；若连接中断，上游结果仍需以服务端记录为准。' : '尚未执行：请求区仅为参数模板。发送时完整 Body 与 Headers 进入正式网关，响应区不使用连通性测试事件。' }} SSE 在请求结束后显示，非实时逐字展示；采集上限不改变真实转发内容。</p></div><div class="defaults-note"><span>参数来源</span><span>{{ defaultsLoading ? '同步默认参数…' : defaultsSource }}</span></div></div>
        </section>
      </div>
      <footer class="workspace-footer"><span><Icon name="terminal" size="sm" /> QQQ2API <span class="footer-slash">/</span> 开发者工作台</span><span>文本 · 图像 · 上游链路</span></footer>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon, { type IconName } from '@/components/icons/Icon.vue'
import DebugJsonViewer from '@/components/debug/DebugJsonViewer.vue'
import DebugHeaderGuide from '@/components/debug/DebugHeaderGuide.vue'
import DebugHeaderChanges from '@/components/debug/DebugHeaderChanges.vue'
import { getUpstreamTestDefaults, runDebugWorkbench, type DebugEndpoint, type DebugSessionAction, type DebugSession, type DebugWorkbenchResult, type DebugSnapshot, type UpstreamHeaderDetail } from '@/api/admin/debugWorkbench'
import { getAvailableModels, list as listAccounts } from '@/api/admin/accounts'
import { proxiesAPI } from '@/api/admin/proxies'
import type { Proxy } from '@/types'

type DebugAccount = { id: string; name: string; email: string; typeLabel: string; status: string; platform?: string }
type DebugModel = { id: string; display_name?: string; type?: string; created_at?: string }
const fallbackModels: DebugModel[] = [{ id: 'gpt-5.4', display_name: 'GPT-5.4' }, { id: 'gpt-4o', display_name: 'GPT-4o' }, { id: 'gpt-4.1', display_name: 'GPT-4.1' }, { id: 'o3-mini', display_name: 'o3-mini' }, { id: 'gpt-image-2', display_name: 'GPT Image 2' }]
const accounts = ref<DebugAccount[]>([])
const models = ref<DebugModel[]>(fallbackModels.map(model => ({ ...model })))
const proxies = ref<Proxy[]>([])
const defaultResponsesParams = { model: 'gpt-5.4', instructions: 'You are Codex, based on GPT-5.', input: [{ role: 'user', content: [{ type: 'input_text', text: 'hi' }] }], stream: true }
const baseRequestHeaders: Record<string, string> = { 'Content-Type': 'application/json', Accept: 'text/event-stream', Authorization: 'Bearer ••••••••' }
const form = reactive({ account: '', proxyId: null as number | null, model: 'gpt-5.4', endpoint: 'responses' as DebugEndpoint, system: defaultResponsesParams.instructions, prompt: 'hi', temperature: 0, maxTokens: 0, stream: true, headers: JSON.stringify(baseRequestHeaders, null, 2), apiParams: JSON.stringify(defaultResponsesParams, null, 2) })
const imageForm = reactive({ prompt: 'hi', n: 1, responseFormat: 'b64_json' })
const imageMode = ref(false)
const running = ref(false)
const responseStatus = ref('')
const responseError = ref('')
const submitted = ref(false)
const activeTab = ref('inbound')
const editorTab = ref('message')
const editorTabs: { key: string; label: string; icon: IconName }[] = [{ key: 'message', label: '消息', icon: 'chatBubble' }, { key: 'json', label: 'JSON', icon: 'terminal' }, { key: 'headers', label: 'Headers', icon: 'document' }, { key: 'image', label: '生图', icon: 'beaker' }]
const tabs = [{ key: 'inbound', label: '入站完整参数', shortLabel: '入站' }, { key: 'outbound', label: '出站完整参数', shortLabel: '出站' }, { key: 'upstream-request', label: '请求上游完整参数', shortLabel: '上游请求' }, { key: 'upstream-response', label: '上游完整响应参数', shortLabel: '上游响应' }]
const presets = [{ label: '健康检查', prompt: '返回 OK' }, { label: '长文本', prompt: '请总结这段文本：' }, { label: 'JSON 输出', prompt: '仅输出合法 JSON，对象包含 message 字段。' }]
const copyState = ref<'idle' | 'copied' | 'error'>('idle')
const requestDuration = ref<number | null>(null)
const imagePreviewFailed = ref(false)
const defaultsSource = ref('本地参数模板')
const defaultsLoading = ref(false)
const accountsLoading = ref(false)
const accountsError = ref('')
const modelsLoading = ref(false)
const modelsError = ref('')
const upstreamHeaders = ref<Record<string, string>>({ ...baseRequestHeaders })
const headersFromBackend = ref(false)
const defaultsNotes = ref<string[]>([])
const headerDetails = ref<UpstreamHeaderDetail[]>([])
const defaultUpstreamUrl = ref('https://api.openai.com/v1/responses')
const defaultUpstreamBody = ref<Record<string, unknown> | undefined>()
const result = ref<DebugWorkbenchResult | null>(null)
const selectedAttemptIndex = ref(0)
const debugSession = ref<DebugSession | null>(null)
const sessionAction = ref<Exclude<DebugSessionAction, 'new_session'>>('new_turn')
const selectedAttempt = computed(() => result.value?.attempts[selectedAttemptIndex.value])
const sessionIdentifiers = computed(() => debugSession.value ? { Session: debugSession.value.session_id, Thread: debugSession.value.thread_id, Turn: debugSession.value.turn_id, Window: debugSession.value.window_id } : {})
const isFixtureAccount = computed(() => !/^[1-9]\d*$/.test(form.account))
const selectedAccount = computed(() => accounts.value.find(account => account.id === form.account) || null)
const selectedModel = computed(() => models.value.find(model => model.id === form.model) || null)
const selectedProxy = computed(() => proxies.value.find(proxy => proxy.id === form.proxyId) || null)
const responseSuccessful = computed(() => /^2\d\d$/.test(responseStatus.value) && Boolean(result.value?.success))
const sessionState = computed(() => {
  if (running.value) return { label: '请求中', tone: 'busy' }
  if (accountsLoading.value || modelsLoading.value || defaultsLoading.value) return { label: '同步配置', tone: 'neutral' }
  if (responseStatus.value && !responseSuccessful.value) return { label: '请求失败', tone: 'error' }
  if (isFixtureAccount.value) return { label: '模板预览', tone: 'neutral' }
  if (responseStatus.value) return { label: '实际请求已返回', tone: 'success' }
  return { label: '准备就绪', tone: 'neutral' }
})
const hasRoutingHintDetail = computed(() => headerDetails.value.some(detail => detail.name.toLowerCase() === 'x-codex-routing-hint'))
const headerCount = computed(() => {
  try { return Object.keys(parseHeaders()).length } catch { return null }
})
const tabDescription = computed(() => tabs.find(tab => tab.key === activeTab.value)?.label)
const upstreamUrl = computed(() => selectedAttempt.value?.request.url || defaultUpstreamUrl.value)
const previewBody = computed(() => { try { return JSON.parse(form.apiParams) } catch { return { invalid_json: form.apiParams } } })
const previewHeaders = computed(() => {
  try {
    const headers = parseHeaders()
    return Object.fromEntries(Object.entries(headers).map(([key, value]) => [key, /authorization|cookie|token|secret|api[-_]key/i.test(key) ? '[redacted]' : value]))
  } catch { return { error: '请求头 JSON 待修正' } }
})
const payloads = computed<Record<string, unknown>>(() => result.value ? {
  inbound: result.value.inbound,
  outbound: result.value.outbound,
  'upstream-request': selectedAttempt.value?.request || { status: 'not_sent', message: '本次执行未记录到上游请求。' },
  'upstream-response': selectedAttempt.value?.response || { status: 'no_response', error: selectedAttempt.value?.error || result.value.error || '上游尚未返回响应。' }
} : {
  inbound: { stage: submitted.value ? '已提交 · 尚未收到服务端执行记录' : '参数模板 · 尚未执行', method: 'POST', path: `/v1/${form.endpoint}`, headers: previewHeaders.value, body: previewBody.value },
  outbound: { stage: submitted.value ? '未收到网关响应记录' : '尚未执行', message: submitted.value ? '提交已发起，实际执行状态以服务端记录为准。' : '发送后展示正式网关返回给调用方的真实状态、响应头与响应体。' },
  'upstream-request': { stage: '默认上游模板 · 非实际请求', url: defaultUpstreamUrl.value, method: 'POST', headers: previewHeaders.value, body: defaultUpstreamBody.value || previewBody.value },
  'upstream-response': submitted.value ? { stage: '未收到上游执行记录', error: responseError.value || undefined, message: '上游实际结果尚未确认；网络中断不表示请求没有执行。' } : { stage: '尚未执行', headers: {}, body: null }
})
const currentPayload = computed(() => JSON.stringify(payloads.value[activeTab.value], null, 2))
const currentSnapshot = computed<DebugSnapshot | undefined>(() => !result.value ? undefined : activeTab.value === 'inbound' ? result.value.inbound : activeTab.value === 'outbound' ? result.value.outbound : activeTab.value === 'upstream-request' ? selectedAttempt.value?.request : selectedAttempt.value?.response)
const captureNotice = computed(() => {
  const snapshot = currentSnapshot.value
  if (!snapshot) return null
  const bytes = `${snapshot.captured_bytes ?? 0} / ${snapshot.body_bytes ?? 0} B`
  if (snapshot.truncated) return { warning: true, text: `采集已截断 · ${bytes} · 仅展示已采集片段，真实转发内容未因此截断。` }
  if (!snapshot.complete) return { warning: true, text: `采集未完成 · ${bytes} · 请求中断或响应尚未读完，以下不是完整响应。` }
  return { warning: false, text: `应用层记录已采集 · ${bytes} · 敏感值已脱敏` }
})
const payloadSize = computed(() => { const bytes = new TextEncoder().encode(currentPayload.value).length; return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB` })
const imagePreviewUrl = computed(() => {
  // OAuth images can be synthesized by the gateway; inspect the true downstream response first.
  if (!imageMode.value || !result.value) return ''
  const downstream = result.value.outbound
  let body = downstream.body as { data?: { b64_json?: string; url?: string }[]; output?: { type?: string; result?: string }[] } | undefined
  if (!body && downstream.body_text && downstream.complete && !downstream.truncated) {
    for (const chunk of downstream.body_text.split(/\n\s*\n/).reverse()) {
      const data = chunk.match(/^data:\s*(.+)$/m)?.[1]
      if (!data) continue
      try { const event = JSON.parse(data); if (event?.type === 'response.completed') { body = event.response; break } } catch { /* Not every SSE frame contains JSON. */ }
    }
  }
  const firstImage = body?.data?.[0]
  const generatedImage = body?.output?.find(item => item.type === 'image_generation_call' && typeof item.result === 'string')?.result
  if (generatedImage) return `data:image/png;base64,${generatedImage}`
  if (typeof firstImage?.b64_json === 'string') return `data:image/png;base64,${firstImage.b64_json}`
  return typeof firstImage?.url === 'string' && /^https?:\/\//i.test(firstImage.url) ? firstImage.url : ''
})
watch(imagePreviewUrl, () => { imagePreviewFailed.value = false })
watch(currentPayload, () => { copyState.value = 'idle' })
let defaultsRequestSeq = 0
let modelsRequestSeq = 0
let runRequestSeq = 0
let requestController: AbortController | null = null
let syncingControls = false

function editBody(update: (body: Record<string, any>) => void) {
  if (syncingControls) return
  try {
    const body = JSON.parse(form.apiParams)
    if (!body || typeof body !== 'object' || Array.isArray(body)) return
    update(body)
    form.apiParams = JSON.stringify(body, null, 2)
  } catch { /* Preserve invalid JSON until the user repairs it. */ }
}
function updatePrompt(body: Record<string, any>, text: string) {
  if (form.endpoint === 'images/generations') { body.prompt = text; return }
  if (form.endpoint === 'chat/completions') {
    if (!Array.isArray(body.messages)) body.messages = []
    const user = [...body.messages].reverse().find(item => item?.role === 'user')
    if (user) user.content = text
    else body.messages.push({ role: 'user', content: text })
  } else {
    if (typeof body.input === 'string') { body.input = text; return }
    if (!Array.isArray(body.input)) body.input = []
    const user = [...body.input].reverse().find(item => item?.role === 'user')
    if (user) {
      if (typeof user.content === 'string') user.content = text
      else {
        if (!Array.isArray(user.content)) user.content = []
        const part = user.content.find((item: any) => item?.type === 'input_text')
        if (part) part.text = text
        else user.content.push({ type: 'input_text', text })
      }
    } else body.input.push({ role: 'user', content: [{ type: 'input_text', text }] })
  }
}
watch(() => form.apiParams, (raw) => {
  try {
    const body = JSON.parse(raw)
    if (!body || typeof body !== 'object' || Array.isArray(body)) return
    syncingControls = true
    if (typeof body.model === 'string') form.model = body.model
    if (typeof body.stream === 'boolean') form.stream = body.stream
    form.temperature = typeof body.temperature === 'number' ? body.temperature : 0
    form.maxTokens = typeof body.max_tokens === 'number' ? body.max_tokens : 0
    const messages = form.endpoint === 'chat/completions' ? body.messages : body.input
    const user = Array.isArray(messages) ? [...messages].reverse().find(item => item?.role === 'user') : undefined
    const text = typeof messages === 'string' ? messages : typeof user?.content === 'string' ? user.content : user?.content?.find?.((part: any) => part?.type === 'input_text')?.text
    if (typeof text === 'string') { form.prompt = text; imageForm.prompt = text }
    if (form.endpoint === 'responses') form.system = typeof body.instructions === 'string' ? body.instructions : ''
    if (form.endpoint === 'chat/completions') { const system = body.messages?.find?.((item: any) => item?.role === 'system'); form.system = typeof system?.content === 'string' ? system.content : '' }
    if (typeof body.prompt === 'string') imageForm.prompt = body.prompt
    if (typeof body.n === 'number') imageForm.n = body.n
    if (typeof body.response_format === 'string') imageForm.responseFormat = body.response_format
    imageMode.value = form.endpoint === 'images/generations' || Boolean(body.tools?.some?.((tool: any) => tool?.type === 'image_generation'))
  } catch { /* JSON stays the source of truth while being edited. */ } finally { syncingControls = false }
}, { flush: 'sync' })
watch(() => form.model, model => { if (model) editBody(body => { body.model = model }) }, { flush: 'sync' })
watch(() => form.prompt, text => editBody(body => updatePrompt(body, text)), { flush: 'sync' })
watch(() => form.system, value => editBody(body => {
  if (form.endpoint === 'responses') { body.instructions = value; return }
  if (form.endpoint !== 'chat/completions') return
  if (!Array.isArray(body.messages)) body.messages = []
  const index = body.messages.findIndex((item: any) => item?.role === 'system')
  if (value.trim()) { if (index >= 0) body.messages[index].content = value; else body.messages.unshift({ role: 'system', content: value }) }
  else if (index >= 0) body.messages.splice(index, 1)
}), { flush: 'sync' })
watch(() => form.stream, value => editBody(body => { if (form.endpoint !== 'images/generations') body.stream = value }), { flush: 'sync' })
watch(() => form.temperature, value => editBody(body => { if (form.endpoint === 'chat/completions') { if (value > 0) body.temperature = value; else delete body.temperature } }), { flush: 'sync' })
watch(() => form.maxTokens, value => editBody(body => { if (form.endpoint === 'chat/completions') { if (value > 0) body.max_tokens = value; else delete body.max_tokens } }), { flush: 'sync' })
watch(() => imageForm.prompt, text => editBody(body => updatePrompt(body, text)), { flush: 'sync' })
watch(() => imageForm.n, value => editBody(body => { if (form.endpoint === 'images/generations') body.n = value }), { flush: 'sync' })
watch(() => imageForm.responseFormat, value => editBody(body => { if (form.endpoint === 'images/generations') body.response_format = value }), { flush: 'sync' })
watch(imageMode, enabled => editBody(body => {
  if (form.endpoint !== 'responses') return
  const tools = Array.isArray(body.tools) ? body.tools.filter((tool: any) => tool?.type !== 'image_generation') : []
  if (enabled) tools.push({ type: 'image_generation' })
  body.tools = tools
}), { flush: 'sync' })
function clearExecution() {
  ++runRequestSeq
  requestController?.abort()
  requestController = null
  running.value = false
  result.value = null
  selectedAttemptIndex.value = 0
  responseStatus.value = ''
  responseError.value = ''
  submitted.value = false
  requestDuration.value = null
}
function resetSession() {
  clearExecution()
  debugSession.value = null
  sessionAction.value = 'new_turn'
}
watch(() => form.endpoint, async endpoint => {
  resetSession()
  defaultUpstreamBody.value = undefined
  defaultUpstreamUrl.value = `https://api.openai.com/v1/${endpoint}`
  if (endpoint === 'images/generations') {
    editorTab.value = 'image'
    form.apiParams = JSON.stringify({ model: 'gpt-image-2', prompt: imageForm.prompt, n: 1, response_format: 'b64_json' }, null, 2)
  } else form.apiParams = JSON.stringify(endpoint === 'responses' ? defaultResponsesParams : { model: 'gpt-5.4', messages: [{ role: 'user', content: 'hi' }], stream: true }, null, 2)
  await loadDefaults()
})
watch(() => form.account, async () => {
  resetSession()
  ++defaultsRequestSeq
  headerDetails.value = []
  defaultsLoading.value = true
  await loadAccountModels()
  await loadDefaults()
})
// Proxy overrides are submitted with the next request without resetting the editor.
function restoreDefaultHeaders() { form.headers = JSON.stringify(upstreamHeaders.value, null, 2) }
function selectEditorTab(key: string) { editorTab.value = key }
function selectTraceTab(key: string) { activeTab.value = key }
async function navigateTabs(event: KeyboardEvent, options: { key: string }[], current: string, select: (key: string) => void) {
  if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const index = options.findIndex(option => option.key === current)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? options.length - 1 : (index + (event.key === 'ArrowRight' ? 1 : -1) + options.length) % options.length
  const tablist = (event.currentTarget as HTMLElement).parentElement
  select(options[next].key)
  await nextTick()
  tablist?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
}
async function loadAccountModels() {
  const accountId = form.account
  const seq = ++modelsRequestSeq
  modelsError.value = ''
  if (!/^[1-9]\d*$/.test(accountId)) { models.value = fallbackModels.map(model => ({ ...model })); modelsLoading.value = false; return }
  modelsLoading.value = true
  try {
    const available = await getAvailableModels(Number(accountId))
    if (seq !== modelsRequestSeq) return
    const options = (available || []).map((model: any) => typeof model === 'string' ? { id: model, display_name: model } : { id: model?.id, display_name: model?.display_name || model?.id, type: model?.type })
      .filter((model: DebugModel) => typeof model.id === 'string' && model.id.length > 0)
      .filter((model, index, all) => all.findIndex(item => item.id === model.id) === index)
    models.value = options.length ? options : fallbackModels.map(model => ({ ...model }))
    if (!options.length) modelsError.value = '该账号暂无可用模型，当前显示内置模型'
  } catch {
    if (seq === modelsRequestSeq) { models.value = fallbackModels.map(model => ({ ...model })); modelsError.value = '该账号模型列表加载失败，当前显示内置模型' }
  } finally { if (seq === modelsRequestSeq) modelsLoading.value = false }
}
async function loadDefaults() {
  const seq = ++defaultsRequestSeq
  defaultsLoading.value = true
  headerDetails.value = []
  try {
    const defaults = await getUpstreamTestDefaults(form.endpoint, form.account, imageMode.value ? imageForm.prompt : undefined, form.proxyId)
    if (seq !== defaultsRequestSeq) return
    form.apiParams = JSON.stringify(defaults.body, null, 2)
    form.headers = JSON.stringify(defaults.headers, null, 2)
    upstreamHeaders.value = { ...defaults.headers }
    headersFromBackend.value = true
    defaultsNotes.value = (defaults.notes || []).filter((note): note is string => typeof note === 'string')
    headerDetails.value = defaults.header_details || []
    defaultUpstreamUrl.value = defaults.url || `https://api.openai.com/v1/${form.endpoint}`
    defaultUpstreamBody.value = defaults.upstream_body
    if (typeof defaults.body.model === 'string' && !models.value.some(model => model.id === defaults.body.model)) models.value.unshift({ id: defaults.body.model, display_name: defaults.body.model })
    defaultsSource.value = `后端默认 · ${defaults.account_type}`
  } catch {
    if (seq !== defaultsRequestSeq) return
    defaultsSource.value = '本地参数模板（默认配置同步失败）'
    headersFromBackend.value = false
    headerDetails.value = []
    defaultUpstreamBody.value = undefined
    defaultUpstreamUrl.value = `https://api.openai.com/v1/${form.endpoint}`
    defaultsNotes.value = ['默认配置同步失败，当前仅展示基础示例头；切换账号或重置后将重新同步。']
    const fallbackHeaders = { ...baseRequestHeaders }
    if (form.endpoint === 'images/generations') delete fallbackHeaders.Accept
    upstreamHeaders.value = fallbackHeaders
    restoreDefaultHeaders()
  } finally { if (seq === defaultsRequestSeq) defaultsLoading.value = false }
}
onMounted(async () => {
  accountsLoading.value = true
  try {
    const response = await listAccounts(1, 50, { platform: 'openai', status: 'active' })
    const labels: Record<string, string> = { oauth: 'OAuth', 'setup-token': 'Setup Token', apikey: 'API Key', 'api-key': 'API Key', upstream: 'Upstream' }
    accounts.value = (response.items ?? []).map((item: any) => ({ id: String(item.id), name: item.name || `GPT 账号 ${item.id}`, email: item.email || item.account || '已配置账号', status: item.status || 'active', typeLabel: labels[String(item.type || 'oauth').trim().toLowerCase()] || item.type || 'Account', platform: item.platform || 'openai' }))
    if (accounts.value.length) form.account = accounts.value[0].id
  } catch { accountsError.value = '账号列表加载失败；当前仅预览模板，未连接上游。' } finally { accountsLoading.value = false }
  try { proxies.value = ((await proxiesAPI.getAll()) || []).filter(proxy => proxy.status === 'active') } catch { proxies.value = [] }
  if (!form.account) { await loadAccountModels(); await loadDefaults() }
})
onUnmounted(() => { ++defaultsRequestSeq; ++modelsRequestSeq; ++runRequestSeq; requestController?.abort() })
function commitCustomModel() {
  const id = form.model.trim()
  if (!id) return
  form.model = id
  if (!models.value.some(model => model.id === id)) models.value.push({ id, display_name: id, type: 'custom' })
}
function applyPreset(preset: { prompt: string }) { editBody(body => updatePrompt(body, preset.prompt)) }
async function resetAll() { resetSession(); copyState.value = 'idle'; await loadDefaults() }
function parseHeaders(): Record<string, string> {
  const headers = JSON.parse(form.headers || '{}')
  if (!headers || typeof headers !== 'object' || Array.isArray(headers)) throw new Error('请求头必须是 JSON 对象')
  const names = new Set<string>()
  for (const [name, value] of Object.entries(headers)) {
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(name) || names.has(name.toLowerCase()) || typeof value !== 'string' || /[\r\n\0]/.test(value)) throw new Error('请求头名称需有效且不重复，值需为不含换行的字符串')
    names.add(name.toLowerCase())
  }
  return headers
}
async function runRequest() {
  if (running.value || isFixtureAccount.value) return
  result.value = null
  submitted.value = false
  requestDuration.value = null
  responseStatus.value = ''
  responseError.value = ''
  let headers: Record<string, string>
  let body: Record<string, unknown>
  try { headers = parseHeaders() } catch {
    responseStatus.value = '请求头错误'
    responseError.value = '请求头格式有误：请输入字符串键值 JSON 对象，名称不得重复，值不得包含换行。'
    return
  }
  try {
    body = JSON.parse(form.apiParams)
    if (!body || typeof body !== 'object' || Array.isArray(body)) throw new Error('参数必须是 JSON 对象')
  } catch {
    responseStatus.value = '参数错误'
    responseError.value = '请求参数格式有误，请检查完整 JSON 对象。'
    return
  }
  const accountId = form.account
  const seq = ++runRequestSeq
  const started = performance.now()
  const controller = new AbortController()
  requestController = controller
  result.value = null
  requestDuration.value = null
  submitted.value = true
  running.value = true
  activeTab.value = 'upstream-response'
  try {
    const response = await runDebugWorkbench(accountId, {
      endpoint: form.endpoint, headers, body,
      ...(form.proxyId != null ? { proxy_id: form.proxyId } : {}),
      session: debugSession.value ? { id: debugSession.value.id, action: sessionAction.value } : { action: 'new_session' }
    }, controller.signal)
    if (seq !== runRequestSeq || accountId !== form.account) return
    result.value = response
    debugSession.value = response.session?.id ? response.session : null
    selectedAttemptIndex.value = Math.max(0, response.attempts.length - 1)
    responseStatus.value = String(response.outbound.status_code || (response.success ? 200 : 502))
    responseError.value = response.error || (response.success ? '' : '上游或网关返回错误，请检查实际链路记录。')
    requestDuration.value = response.duration_ms
  } catch (error: any) {
    if (seq !== runRequestSeq || accountId !== form.account) return
    responseStatus.value = '请求失败'
    responseError.value = error?.message || '调试请求失败；尚未收到执行记录，请检查后端连接。'
  } finally {
    if (seq === runRequestSeq) {
      running.value = false
      requestController = null
      if (requestDuration.value === null) requestDuration.value = Math.round(performance.now() - started)
    }
  }
}
async function copyCurrent() {
  try { if (!navigator.clipboard) throw new Error('Clipboard unavailable'); await navigator.clipboard.writeText(currentPayload.value); copyState.value = 'copied' } catch { copyState.value = 'error' }
}
</script>

<style scoped src="./debug-workbench.css"></style>
