<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] space-y-5">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div class="flex items-center gap-2">
            <Icon name="terminal" size="lg" class="text-primary-500" />
            <h1 class="text-2xl font-bold text-gray-900 dark:text-white">上游接口调试工作台</h1>
          </div>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">选择账号，实时查看入站、出站与上游请求的完整链路</p>
        </div>
        <div class="flex items-center gap-2">
          <span class="inline-flex items-center gap-1.5 rounded-full bg-emerald-50 px-3 py-1.5 text-xs font-medium text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-400">
            <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span> 调试服务在线
          </span>
          <button type="button" class="btn btn-secondary" @click="resetAll"><Icon name="refresh" size="sm" /> 重置</button>
        </div>
      </div>

      <section class="card p-4 md:p-5">
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-[1.2fr_1fr_1fr_1fr_1fr_auto] lg:items-end">
          <label class="block"><span class="field-label">GPT 账号 <span class="font-normal text-gray-400">（{{ accounts.length }} 个）</span></span><select v-model="form.account" class="input mt-1.5" :disabled="accountsLoading || !accounts.length"><option v-if="accountsLoading" value="">正在加载账号…</option><option v-else-if="!accounts.length" value="">暂无可用 GPT 账号</option><option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.status === 'active' ? '●' : '○' }} {{ a.name }} · {{ a.typeLabel }} · {{ a.email }}</option></select><span v-if="selectedAccount" class="mt-1 block truncate text-[11px] text-gray-400">{{ selectedAccount.platform || 'openai' }} · ID {{ selectedAccount.id }}</span><span v-if="accountsLoading" class="mt-1 block text-[11px] text-gray-400">正在加载账号…</span><span v-else-if="accountsError" class="mt-1 block text-[11px] text-amber-600">{{ accountsError }}</span></label>
          <label class="block"><span class="field-label">模型 <span class="font-normal text-gray-400">（{{ models.length }} 个）</span></span><select v-model="form.model" class="input mt-1.5" :disabled="modelsLoading || !models.length"><option v-if="modelsLoading" value="">正在加载模型…</option><option v-else-if="!models.length" value="">暂无可用模型</option><option v-for="m in models" :key="m.id" :value="m.id">{{ m.display_name || m.id }}{{ m.id !== (m.display_name || m.id) ? ` · ${m.id}` : '' }}</option></select><span v-if="selectedModel" class="mt-1 block truncate text-[11px] text-gray-400">模型 ID：{{ selectedModel.id }}<span v-if="selectedModel.type"> · {{ selectedModel.type }}</span></span><span v-if="modelsLoading" class="mt-1 block text-[11px] text-gray-400">正在同步该账号的模型…</span><span v-else-if="modelsError" class="mt-1 block text-[11px] text-amber-600">{{ modelsError }}</span></label>
          <label class="block"><span class="field-label">请求方式</span><select v-model="form.endpoint" class="input mt-1.5"><option value="chat/completions">Chat Completions</option><option value="responses">Responses API</option><option value="images/generations">Images Generation</option></select></label>
          <label class="block"><span class="field-label">代理 IP</span><ProxySelector v-model="form.proxyId" :proxies="proxies" :disabled="running" /></label>
          <button type="button" class="btn btn-primary h-10 justify-center px-6" :disabled="running" @click="runRequest"><Icon :name="running ? 'refresh' : 'play'" size="sm" :class="running && 'animate-spin'" /> {{ running ? '请求中…' : '发送请求' }}</button>
        </div>
        <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
          <span class="text-xs text-gray-500 dark:text-dark-300">快捷场景</span>
          <button v-for="preset in presets" :key="preset.label" type="button" class="rounded-md border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-dark-200" @click="applyPreset(preset)">{{ preset.label }}</button>
          <label class="ml-auto inline-flex cursor-pointer items-center gap-2 text-xs text-gray-500 dark:text-dark-300"><input v-model="form.stream" type="checkbox" class="rounded border-gray-300 text-primary-600" /> 流式响应</label>
        </div>
        <div class="mt-3 flex items-center gap-2 text-[11px] text-gray-400"><span class="font-medium uppercase tracking-wide">上游 URL</span><code class="rounded bg-gray-50 px-2 py-1 text-gray-600 dark:bg-dark-800 dark:text-dark-300">{{ upstreamUrl }}</code><span class="ml-auto">默认参数来源：{{ defaultsSource }}</span></div>
      </section>

      <div class="grid grid-cols-1 gap-5 xl:grid-cols-2">
        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"><h2 class="section-title"><Icon name="upload" size="sm" class="text-primary-500" /> 请求参数</h2><span class="badge-muted">JSON</span></div>
          <div class="space-y-4 p-4">
            <label class="block"><span class="field-label">系统提示词</span><textarea v-model="form.system" rows="2" class="input mt-1.5 resize-y" placeholder="You are a helpful assistant…"></textarea></label>
            <label class="block"><span class="field-label">用户消息</span><textarea v-model="form.prompt" rows="4" class="input mt-1.5 resize-y" placeholder="输入要调试的内容…"></textarea></label>
            <div class="grid grid-cols-2 gap-3"><label><span class="field-label">Temperature</span><input v-model.number="form.temperature" type="number" min="0" max="2" step="0.1" class="input mt-1.5" /></label><label><span class="field-label">最大 Tokens</span><input v-model.number="form.maxTokens" type="number" min="1" class="input mt-1.5" /></label></div>
            <label class="block"><span class="field-label">自定义请求头</span><textarea v-model="form.headers" rows="3" class="input mt-1.5 font-mono text-xs" spellcheck="false"></textarea></label>
            <label class="block"><span class="field-label">完整 API 参数 <span class="font-normal text-gray-400">（可直接编辑 JSON）</span></span><textarea v-model="form.apiParams" rows="10" class="input mt-1.5 resize-y font-mono text-xs leading-relaxed" spellcheck="false"></textarea></label>
          </div>
        </section>

        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"><h2 class="section-title"><Icon name="beaker" size="sm" class="text-fuchsia-500" /> 生图测试</h2><label class="inline-flex items-center gap-2 text-xs text-gray-500"><input v-model="imageMode" type="checkbox" :disabled="form.endpoint === 'images/generations'" class="rounded border-gray-300 text-fuchsia-600" /> 启用</label></div>
          <div class="space-y-4 p-4" :class="!imageMode && 'opacity-50 pointer-events-none'"><label class="block"><span class="field-label">图像提示词</span><textarea v-model="imageForm.prompt" rows="3" class="input mt-1.5 resize-y" placeholder="描述你想生成的图像…"></textarea></label><div class="grid grid-cols-2 gap-3"><label><span class="field-label">数量 n</span><input v-model.number="imageForm.n" type="number" min="1" max="10" class="input mt-1.5" /></label><label><span class="field-label">响应格式</span><select v-model="imageForm.responseFormat" class="input mt-1.5"><option value="b64_json">b64_json</option><option value="url">url</option></select></label></div><div class="flex min-h-24 items-center justify-center overflow-hidden rounded-lg border border-dashed border-gray-300 bg-gray-50 text-xs text-gray-400 dark:border-dark-600 dark:bg-dark-800/50"><img v-if="imagePreviewUrl" :src="imagePreviewUrl" alt="生成结果预览" class="max-h-48 max-w-full rounded object-contain" /><span v-else>生成结果将在此处预览</span></div></div>
        </section>
      </div>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center gap-1 border-b border-gray-100 px-2 dark:border-dark-700"><button v-for="tab in tabs" :key="tab.key" type="button" class="tab-button" :class="activeTab === tab.key && 'tab-button-active'" @click="activeTab = tab.key">{{ tab.label }}<span v-if="tab.key === 'upstream-response' && responseStatus" class="ml-1.5 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] text-emerald-600">{{ responseStatus }}</span></button></div>
        <div class="p-4"><div class="mb-3 flex items-center justify-between"><div class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-300"><span>{{ tabDescription }}</span><span class="rounded bg-gray-100 px-2 py-0.5 text-[10px] text-gray-500 dark:bg-dark-800 dark:text-dark-300">{{ defaultsLoading ? '同步默认参数…' : defaultsSource }}</span></div><button type="button" class="btn btn-ghost px-2 py-1 text-xs" @click="copyCurrent"><Icon name="copy" size="sm" /> 复制 JSON</button></div><pre class="max-h-[380px] overflow-auto rounded-lg bg-gray-900 p-4 font-mono text-xs leading-relaxed text-gray-100">{{ currentPayload }}</pre></div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { getUpstreamTestDefaults, runAccountConnectivityTest } from '@/api/admin/debugWorkbench'
import { getAvailableModels, list as listAccounts } from '@/api/admin/accounts'
import { proxiesAPI } from '@/api/admin/proxies'
import ProxySelector from '@/components/common/ProxySelector.vue'
import type { Proxy } from '@/types'

const proxies = ref<Proxy[]>([])
type DebugAccount = { id: string; name: string; email: string; typeLabel: string; status: string; platform?: string }
type DebugModel = { id: string; display_name?: string; type?: string; created_at?: string }
const fallbackModels: DebugModel[] = [
  { id: 'gpt-5.4', display_name: 'GPT-5.4' },
  { id: 'gpt-4o', display_name: 'GPT-4o' },
  { id: 'gpt-4.1', display_name: 'GPT-4.1' },
  { id: 'o3-mini', display_name: 'o3-mini' },
  { id: 'gpt-image-2', display_name: 'GPT Image 2' }
]
const accounts = ref<DebugAccount[]>([
  { id: 'acc-01', name: 'GPT Team · 主账号', email: 'team@example.com', typeLabel: 'OAuth', status: 'active', platform: 'openai' },
  { id: 'acc-02', name: 'GPT Plus · 备用', email: 'backup@example.com', typeLabel: 'API Key', status: 'active', platform: 'openai' }
])
const models = ref<DebugModel[]>(fallbackModels.map((model) => ({ ...model })))
const defaultChatParams = {
  model: 'gpt-5.4',
  messages: [{ role: 'user', content: 'hi' }],
  stream: true
}
const defaultResponsesParams = {
  model: 'gpt-5.4',
  instructions: 'You are Codex, based on GPT-5.',
  input: [{ role: 'user', content: [{ type: 'input_text', text: 'hi' }] }],
  stream: true
}
const form = reactive({ account: 'acc-01', proxyId: null as number | null, model: 'gpt-5.4', endpoint: 'responses', system: defaultResponsesParams.instructions, prompt: 'hi', temperature: 0, maxTokens: 0, stream: true, headers: '{\n  "Content-Type": "application/json",\n  "Accept": "text/event-stream",\n  "Authorization": "Bearer ••••••••"\n}', apiParams: JSON.stringify(defaultResponsesParams, null, 2) })
const imageForm = reactive({ prompt: 'hi', n: 1, responseFormat: 'b64_json' })
const imageMode = ref(false); const running = ref(false); const responseStatus = ref(''); const activeTab = ref('inbound')
const defaultsSource = ref('本地样本默认值')
const defaultsLoading = ref(false)
const accountsLoading = ref(false)
const accountsError = ref('')
const modelsLoading = ref(false)
const modelsError = ref('')
const upstreamHeaders = ref<Record<string, string>>({ Authorization: 'Bearer ••••••••', 'Content-Type': 'application/json' })
const defaultUpstreamUrl = ref('https://api.openai.com/v1/chat/completions')
const selectedAccount = computed(() => accounts.value.find((account) => account.id === form.account) || null)
const selectedModel = computed(() => models.value.find((model) => model.id === form.model) || null)
let defaultsRequestSeq = 0
let modelsRequestSeq = 0
watch(() => form.endpoint, async (endpoint) => {
  if (endpoint === 'images/generations') {
    imageMode.value = true
    form.model = 'gpt-image-2'
    form.apiParams = JSON.stringify({ model: 'gpt-image-2', prompt: imageForm.prompt, n: 1, response_format: 'b64_json' }, null, 2)
  } else if (endpoint === 'responses') {
    imageMode.value = false
    form.model = 'gpt-5.4'
    form.apiParams = JSON.stringify({ model: 'gpt-5.4', instructions: 'You are Codex, based on GPT-5.', input: [{ role: 'user', content: [{ type: 'input_text', text: 'hi' }] }], stream: true }, null, 2)
  } else {
    imageMode.value = false
    form.model = 'gpt-5.4'
    form.apiParams = JSON.stringify(defaultChatParams, null, 2)
  }
  await loadDefaults()
})
watch(() => form.account, async () => { await loadAccountModels(); await loadDefaults() })
watch(() => form.model, (model) => {
  if (!model) return
  try {
    const body = JSON.parse(form.apiParams)
    if (body && typeof body === 'object' && body.model !== model) {
      body.model = model
      form.apiParams = JSON.stringify(body, null, 2)
    }
  } catch { /* the JSON editor will show the validation error on send */ }
})
watch(() => form.prompt, (value) => {
  try {
    const body = JSON.parse(form.apiParams)
    if (form.endpoint === 'images/generations' || imageMode.value) body.prompt = value
    else if (Array.isArray(body.messages)) {
      const user = [...body.messages].reverse().find((item: any) => item?.role === 'user')
      if (user) user.content = value
    } else if (Array.isArray(body.input)) {
      const item = [...body.input].reverse().find((entry: any) => entry?.role === 'user')
      if (item?.content?.[0]) item.content[0].text = value
    }
    form.apiParams = JSON.stringify(body, null, 2)
  } catch { /* keep invalid JSON visible for the send-time validation */ }
})
watch(() => form.system, (value) => {
  if (form.endpoint !== 'chat/completions') return
  try {
    const body = JSON.parse(form.apiParams)
    if (!Array.isArray(body.messages)) return
    const index = body.messages.findIndex((item: any) => item?.role === 'system')
    if (value.trim()) {
      if (index >= 0) body.messages[index].content = value
      else body.messages.unshift({ role: 'system', content: value })
    } else if (index >= 0) body.messages.splice(index, 1)
    form.apiParams = JSON.stringify(body, null, 2)
  } catch { /* keep invalid JSON visible */ }
})
watch(() => form.stream, (value) => {
  try { const body = JSON.parse(form.apiParams); if (body && typeof body === 'object') { body.stream = value; form.apiParams = JSON.stringify(body, null, 2) } } catch { /* validation on send */ }
})
watch(() => form.temperature, (value) => {
  if (form.endpoint !== 'chat/completions') return
  try { const body = JSON.parse(form.apiParams); if (value > 0) body.temperature = value; else delete body.temperature; form.apiParams = JSON.stringify(body, null, 2) } catch { /* validation on send */ }
})
watch(() => form.maxTokens, (value) => {
  if (form.endpoint !== 'chat/completions') return
  try { const body = JSON.parse(form.apiParams); if (value > 0) body.max_tokens = value; else delete body.max_tokens; form.apiParams = JSON.stringify(body, null, 2) } catch { /* validation on send */ }
})
const tabs = [{ key: 'inbound', label: '入站完整参数' }, { key: 'outbound', label: '出站完整参数' }, { key: 'upstream-request', label: '请求上游完整参数' }, { key: 'upstream-response', label: '上游完整响应参数' }]
const presets = [{ label: '健康检查', prompt: '返回 OK' }, { label: '长文本', prompt: '请总结这段文本：' }, { label: 'JSON 输出', prompt: '仅输出合法 JSON，对象包含 message 字段。' }]
const payloads = reactive<Record<string, unknown>>({ inbound: { method: 'POST', path: '/v1/responses', headers: { 'content-type': 'application/json', authorization: 'Bearer ••••••••' }, body: defaultResponsesParams }, outbound: { status: 'pending', transformed_model: form.model, route: 'account://acc-01', proxy: null, body: defaultResponsesParams }, 'upstream-request': { url: 'https://api.openai.com/v1/responses', method: 'POST', headers: { authorization: 'Bearer ••••••••', 'content-type': 'application/json' }, proxy: null, body: defaultResponsesParams }, 'upstream-response': { status: '—', headers: {}, body: null } })
const selectedProxy = computed(() => form.proxyId == null ? null : proxies.value.find((p) => p.id === form.proxyId) || null)
watch(() => form.proxyId, (proxyId) => {
  const proxy = proxyId == null ? null : proxies.value.find((p) => p.id === proxyId)
  const proxyInfo = proxy ? { id: proxy.id, name: proxy.name, protocol: proxy.protocol, host: proxy.host, port: proxy.port, ip_address: proxy.ip_address, country: proxy.country, country_code: proxy.country_code } : null
  const outbound = payloads.outbound as Record<string, unknown>
  if (outbound) outbound.proxy = proxyInfo
  const upstream = payloads['upstream-request'] as Record<string, unknown>
  if (upstream) { upstream.proxy = proxyInfo; upstream.proxy_url = proxy ? `${proxy.protocol}://${proxy.host}:${proxy.port}` : null }
  void loadDefaults()
})
const currentPayload = computed(() => JSON.stringify(payloads[activeTab.value], null, 2)); const imagePreviewUrl = computed(() => { const body = (payloads['upstream-response'] as any)?.body; return imageMode.value && body?.data?.[0]?.url ? String(body.data[0].url) : '' }); const tabDescription = computed(() => tabs.find(t => t.key === activeTab.value)?.label)
const upstreamUrl = computed(() => String((payloads['upstream-request'] as any)?.url || `https://api.openai.com/v1/${form.endpoint}`))
async function loadAccountModels() {
  const accountId = form.account
  const requestSeq = ++modelsRequestSeq
  modelsError.value = ''
  if (!/^\d+$/.test(accountId)) {
    models.value = fallbackModels.map((model) => ({ ...model }))
    modelsLoading.value = false
    return
  }
  modelsLoading.value = true
  try {
    const available = await getAvailableModels(Number(accountId))
    const options = (available || [])
      .map((m: any) => typeof m === 'string' ? { id: m, display_name: m } : { id: m?.id, display_name: m?.display_name || m?.id, type: m?.type, created_at: m?.created_at })
      .filter((m: DebugModel): m is DebugModel => typeof m.id === 'string' && m.id.length > 0)
      .filter((model, index, list) => list.findIndex((item) => item.id === model.id) === index)
    if (requestSeq !== modelsRequestSeq) return
    if (options.length) {
      models.value = options
      if (!models.value.some((model) => model.id === form.model)) form.model = options[0].id
    } else {
      models.value = fallbackModels.map((model) => ({ ...model }))
      modelsError.value = '该账号暂无可用模型，当前显示内置模型'
    }
  } catch {
    if (requestSeq === modelsRequestSeq) {
      models.value = fallbackModels.map((model) => ({ ...model }))
      modelsError.value = '该账号模型列表加载失败，当前显示内置模型'
    }
  } finally {
    if (requestSeq === modelsRequestSeq) modelsLoading.value = false
  }
}

async function loadDefaults() {
  const requestSeq = ++defaultsRequestSeq
  defaultsLoading.value = true
  try {
    const defaults = await getUpstreamTestDefaults(form.endpoint, form.account, imageMode.value ? imageForm.prompt : undefined, form.proxyId)
    if (requestSeq !== defaultsRequestSeq) return
    form.apiParams = JSON.stringify(defaults.body, null, 2)
    form.headers = JSON.stringify(defaults.headers, null, 2)
    upstreamHeaders.value = { ...defaults.headers }
    defaultUpstreamUrl.value = defaults.url || `https://api.openai.com/v1/${form.endpoint}`
    if (typeof defaults.body.model === 'string' && defaults.body.model) {
      if (!models.value.some((model) => model.id === defaults.body.model)) models.value.unshift({ id: defaults.body.model, display_name: defaults.body.model })
      form.model = defaults.body.model
    }
    if (Array.isArray(defaults.body.messages)) {
      const system = defaults.body.messages.find((item: any) => item?.role === 'system')
      form.system = typeof system?.content === 'string' ? system.content : ''
      const user = [...defaults.body.messages].reverse().find((item: any) => item?.role === 'user')
      if (typeof user?.content === 'string') form.prompt = user.content
    } else if (Array.isArray(defaults.body.input)) {
      const user = [...defaults.body.input].reverse().find((item: any) => item?.role === 'user')
      const text = user?.content?.find?.((part: any) => part?.type === 'input_text')?.text
      if (typeof text === 'string') form.prompt = text
    }
    if (typeof defaults.body.instructions === 'string' && form.endpoint === 'responses') form.system = defaults.body.instructions
    if (typeof defaults.body.prompt === 'string') imageForm.prompt = defaults.body.prompt
    if (typeof defaults.body.n === 'number') imageForm.n = defaults.body.n
    if (typeof defaults.body.response_format === 'string') imageForm.responseFormat = defaults.body.response_format
    if (typeof defaults.body.stream === 'boolean') form.stream = defaults.body.stream
    const body = JSON.parse(JSON.stringify(defaults.body))
    payloads.inbound = { method: 'POST', path: `/v1/${form.endpoint}`, headers: defaults.headers, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, body }
    payloads.outbound = { status: 'ready', transformed_model: defaults.body.model, route: `account://${form.account}`, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, body: JSON.parse(JSON.stringify(defaults.body)) }
    payloads['upstream-request'] = { url: defaultUpstreamUrl.value, method: 'POST', headers: { ...upstreamHeaders.value }, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, proxy_url: selectedProxy.value ? `${selectedProxy.value.protocol}://${selectedProxy.value.host}:${selectedProxy.value.port}` : null, body: JSON.parse(JSON.stringify(defaults.upstream_body || defaults.body)) }
    defaultsSource.value = `后端默认 · ${defaults.account_type}`
  } catch {
    if (requestSeq !== defaultsRequestSeq) return
    defaultsSource.value = '本地样本默认值（后端未连接）'
  } finally {
    if (requestSeq === defaultsRequestSeq) defaultsLoading.value = false
  }
}
onMounted(async () => {
  accountsLoading.value = true
  accountsError.value = ''
  try {
    const result = await listAccounts(1, 50, { platform: 'openai', status: 'active' })
    const accountTypeLabel = (type: string) => {
      const normalized = type.trim().toLowerCase()
      return ({ oauth: 'OAuth', 'setup-token': 'Setup Token', apikey: 'API Key', 'api-key': 'API Key', upstream: 'Upstream' } as Record<string, string>)[normalized] || type || 'Account'
    }
    const items: DebugAccount[] = (result.items ?? []).map((item: any) => ({
      id: String(item.id),
      name: item.name || `GPT 账号 ${item.id}`,
      email: item.email || item.account || '已配置账号',
      status: item.status || 'active',
      typeLabel: accountTypeLabel(String(item.type || 'oauth')),
      platform: item.platform || 'openai'
    }))
    if (items.length) {
      accounts.value = items
      form.account = items[0].id
    }
  } catch {
    accountsError.value = '账号列表加载失败，当前显示本地样本'
  } finally { accountsLoading.value = false }
  try {
    const proxyItems = await proxiesAPI.getAll()
    proxies.value = (proxyItems || []).filter((proxy) => proxy.status === 'active')
  } catch {
    proxies.value = []
  }
  await loadAccountModels()
  await loadDefaults()
})
function applyPreset(p: { prompt: string }) {
  form.prompt = p.prompt
  try {
    const body = JSON.parse(form.apiParams)
    if (form.endpoint === 'images/generations' || imageMode.value) body.prompt = p.prompt
    else if (Array.isArray(body.messages)) {
      const user = [...body.messages].reverse().find((item: any) => item?.role === 'user')
      if (user) user.content = p.prompt
      else body.messages.push({ role: 'user', content: p.prompt })
    } else if (Array.isArray(body.input)) {
      const item = [...body.input].reverse().find((entry: any) => entry?.role === 'user')
      if (item?.content?.[0]) item.content[0].text = p.prompt
    }
    form.apiParams = JSON.stringify(body, null, 2)
  } catch { /* validation is reported when the request is sent */ }
}
async function resetAll() {
  responseStatus.value = ''
  payloads['upstream-response'] = { status: '—', headers: {}, body: null }
  await loadDefaults()
}
async function runRequest() {
  running.value = true; responseStatus.value = ''
  let customHeaders: Record<string, string> = {}
  try {
    const parsedHeaders = JSON.parse(form.headers || '{}')
    if (!parsedHeaders || typeof parsedHeaders !== 'object' || Array.isArray(parsedHeaders)) throw new Error('请求头必须是 JSON 对象')
    for (const [key, value] of Object.entries(parsedHeaders)) {
      if (typeof value !== 'string') throw new Error('请求头值必须是字符串')
      customHeaders[key] = value
    }
  } catch {
    running.value = false
    responseStatus.value = '请求头错误'
    return
  }
  let apiBody: Record<string, unknown>
  try {
    const parsed = JSON.parse(form.apiParams)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('参数必须是 JSON 对象')
    apiBody = parsed as Record<string, unknown>
  } catch {
    running.value = false
    responseStatus.value = '参数错误'
    return
  }
  // The JSON editor is the source of truth; controls below only provide a quick way to update it.
  if (typeof apiBody.model === 'string' && apiBody.model.trim()) form.model = apiBody.model
  else apiBody.model = form.model
  if (imageMode.value) Object.assign(apiBody, { model: form.model, prompt: imageForm.prompt, n: imageForm.n, response_format: imageForm.responseFormat })
  const requestBody = JSON.parse(JSON.stringify(apiBody)) as Record<string, unknown>
  payloads.inbound = { method: 'POST', path: `/v1/${form.endpoint}`, headers: { authorization: 'Bearer ••••••••', 'content-type': 'application/json', ...customHeaders }, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, body: requestBody }
  const upstreamBody = JSON.parse(JSON.stringify(requestBody)) as Record<string, unknown>
  payloads.outbound = { status: 'routing', transformed_model: form.model, route: `account://${form.account}`, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, body: upstreamBody }
  payloads['upstream-request'] = { url: defaultUpstreamUrl.value, method: 'POST', headers: { ...upstreamHeaders.value, ...customHeaders }, proxy: selectedProxy.value ? { id: selectedProxy.value.id, name: selectedProxy.value.name, protocol: selectedProxy.value.protocol, host: selectedProxy.value.host, port: selectedProxy.value.port, ip_address: selectedProxy.value.ip_address, country: selectedProxy.value.country, country_code: selectedProxy.value.country_code } : null, proxy_url: selectedProxy.value ? `${selectedProxy.value.protocol}://${selectedProxy.value.host}:${selectedProxy.value.port}` : null, body: upstreamBody }
  // Use the real SSE connectivity test when a persisted account is selected;
  // fixture accounts continue to use the local preview response.
  if (/^\d+$/.test(form.account)) {
    try {
      const raw = await runAccountConnectivityTest(form.account, { model_id: String(apiBody.model || form.model), prompt: imageMode.value ? imageForm.prompt : form.prompt, mode: form.endpoint === 'images/generations' ? 'image' : '', ...(form.proxyId != null ? { proxy_id: form.proxyId } : {}) })
      const events = raw.split(/\n\s*\n/).map((chunk) => chunk.match(/^data:\s*(.+)$/m)?.[1]).filter(Boolean).map((data) => { try { return JSON.parse(data as string) } catch { return { raw: data } } })
      const completed = [...events].reverse().find((event: any) => event?.type === 'test_complete')
      responseStatus.value = completed?.success === false ? '500' : '200'
      payloads['upstream-response'] = { status: Number(responseStatus.value), headers: { 'content-type': 'text/event-stream' }, body: { events } }
    } catch (error: any) {
      responseStatus.value = 'error'
      payloads['upstream-response'] = { status: 502, headers: {}, body: { error: error?.message || '上游请求失败' } }
    } finally {
      running.value = false
    }
    return
  }
  await new Promise(r => setTimeout(r, 700)); responseStatus.value = '200'; payloads.outbound = { ...(payloads.outbound as Record<string, unknown>), status: 'completed' }
  payloads['upstream-response'] = imageMode.value ? { status: 200, headers: { 'content-type': 'application/json' }, body: { created: Math.floor(Date.now() / 1000), data: [{ url: 'https://images.example.com/debug-preview.png', revised_prompt: imageForm.prompt }] } } : { status: 200, headers: { 'content-type': 'application/json', 'x-request-id': 'dbg_' + Date.now() }, body: { id: 'chatcmpl_debug', model: form.model, choices: [{ index: 0, message: { role: 'assistant', content: '调试请求已成功返回。' }, finish_reason: 'stop' }], usage: { prompt_tokens: 24, completion_tokens: 8, total_tokens: 32 } } }; running.value = false
}
function copyCurrent() { navigator.clipboard?.writeText(currentPayload.value) }
</script>

<style scoped>
.field-label { @apply text-xs font-medium text-gray-600 dark:text-dark-200; }
.section-title { @apply flex items-center gap-2 text-sm font-semibold text-gray-800 dark:text-white; }
.badge-muted { @apply rounded bg-gray-100 px-2 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700 dark:text-dark-300; }
.tab-button { @apply -mb-px border-b-2 border-transparent px-3 py-3 text-xs font-medium text-gray-500 transition-colors hover:text-gray-800 dark:text-dark-300 dark:hover:text-white; }
.tab-button-active { @apply border-primary-500 text-primary-600 dark:text-primary-400; }
</style>
