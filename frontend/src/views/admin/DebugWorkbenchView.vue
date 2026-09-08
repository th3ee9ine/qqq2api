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
        <div class="grid grid-cols-1 gap-4 lg:grid-cols-[1.2fr_1fr_1fr_auto] lg:items-end">
          <label class="block"><span class="field-label">GPT 账号</span><select v-model="form.account" class="input mt-1.5"><option v-for="a in accounts" :key="a.id" :value="a.id">{{ a.name }} · {{ a.email }}</option></select></label>
          <label class="block"><span class="field-label">模型</span><select v-model="form.model" class="input mt-1.5"><option v-for="m in models" :key="m" :value="m">{{ m }}</option></select></label>
          <label class="block"><span class="field-label">请求方式</span><select v-model="form.endpoint" class="input mt-1.5"><option value="chat/completions">Chat Completions</option><option value="responses">Responses API</option><option value="images/generations">Images Generation</option></select></label>
          <button type="button" class="btn btn-primary h-10 justify-center px-6" :disabled="running" @click="runRequest"><Icon :name="running ? 'refresh' : 'play'" size="sm" :class="running && 'animate-spin'" /> {{ running ? '请求中…' : '发送请求' }}</button>
        </div>
        <div class="mt-4 flex flex-wrap items-center gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
          <span class="text-xs text-gray-500 dark:text-dark-300">快捷场景</span>
          <button v-for="preset in presets" :key="preset.label" type="button" class="rounded-md border border-gray-200 px-2.5 py-1 text-xs text-gray-600 hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:text-dark-200" @click="applyPreset(preset)">{{ preset.label }}</button>
          <label class="ml-auto inline-flex cursor-pointer items-center gap-2 text-xs text-gray-500 dark:text-dark-300"><input v-model="form.stream" type="checkbox" class="rounded border-gray-300 text-primary-600" /> 流式响应</label>
        </div>
      </section>

      <div class="grid grid-cols-1 gap-5 xl:grid-cols-2">
        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"><h2 class="section-title"><Icon name="upload" size="sm" class="text-primary-500" /> 请求参数</h2><span class="badge-muted">JSON</span></div>
          <div class="space-y-4 p-4">
            <label class="block"><span class="field-label">系统提示词</span><textarea v-model="form.system" rows="2" class="input mt-1.5 resize-y" placeholder="You are a helpful assistant…"></textarea></label>
            <label class="block"><span class="field-label">用户消息</span><textarea v-model="form.prompt" rows="4" class="input mt-1.5 resize-y" placeholder="输入要调试的内容…"></textarea></label>
            <div class="grid grid-cols-2 gap-3"><label><span class="field-label">Temperature</span><input v-model.number="form.temperature" type="number" min="0" max="2" step="0.1" class="input mt-1.5" /></label><label><span class="field-label">最大 Tokens</span><input v-model.number="form.maxTokens" type="number" min="1" class="input mt-1.5" /></label></div>
            <label class="block"><span class="field-label">自定义请求头</span><textarea v-model="form.headers" rows="3" class="input mt-1.5 font-mono text-xs" spellcheck="false"></textarea></label>
          </div>
        </section>

        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-4 py-3 dark:border-dark-700"><h2 class="section-title"><Icon name="beaker" size="sm" class="text-fuchsia-500" /> 生图测试</h2><label class="inline-flex items-center gap-2 text-xs text-gray-500"><input v-model="imageMode" type="checkbox" class="rounded border-gray-300 text-fuchsia-600" /> 启用</label></div>
          <div class="space-y-4 p-4" :class="!imageMode && 'opacity-50 pointer-events-none'"><label class="block"><span class="field-label">图像提示词</span><textarea v-model="imageForm.prompt" rows="3" class="input mt-1.5 resize-y" placeholder="描述你想生成的图像…"></textarea></label><div class="grid grid-cols-2 gap-3"><label><span class="field-label">尺寸</span><select v-model="imageForm.size" class="input mt-1.5"><option>1024x1024</option><option>1536x1024</option><option>1024x1536</option></select></label><label><span class="field-label">质量</span><select v-model="imageForm.quality" class="input mt-1.5"><option>standard</option><option>hd</option></select></label></div><div class="flex min-h-24 items-center justify-center overflow-hidden rounded-lg border border-dashed border-gray-300 bg-gray-50 text-xs text-gray-400 dark:border-dark-600 dark:bg-dark-800/50"><img v-if="imagePreviewUrl" :src="imagePreviewUrl" alt="生成结果预览" class="max-h-48 max-w-full rounded object-contain" /><span v-else>生成结果将在此处预览</span></div></div>
        </section>
      </div>

      <section class="card overflow-hidden">
        <div class="flex flex-wrap items-center gap-1 border-b border-gray-100 px-2 dark:border-dark-700"><button v-for="tab in tabs" :key="tab.key" type="button" class="tab-button" :class="activeTab === tab.key && 'tab-button-active'" @click="activeTab = tab.key">{{ tab.label }}<span v-if="tab.key === 'upstream-response' && responseStatus" class="ml-1.5 rounded-full bg-emerald-100 px-1.5 py-0.5 text-[10px] text-emerald-600">{{ responseStatus }}</span></button></div>
        <div class="p-4"><div class="mb-3 flex items-center justify-between"><div class="text-xs text-gray-500 dark:text-dark-300">{{ tabDescription }}</div><button type="button" class="btn btn-ghost px-2 py-1 text-xs" @click="copyCurrent"><Icon name="copy" size="sm" /> 复制 JSON</button></div><pre class="max-h-[380px] overflow-auto rounded-lg bg-gray-900 p-4 font-mono text-xs leading-relaxed text-gray-100">{{ currentPayload }}</pre></div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const accounts = [{ id: 'acc-01', name: 'GPT Team · 主账号', email: 'team@example.com' }, { id: 'acc-02', name: 'GPT Plus · 备用', email: 'backup@example.com' }]
const models = ['gpt-4o', 'gpt-4.1', 'o3-mini', 'gpt-Image-1']
const form = reactive({ account: 'acc-01', model: 'gpt-4o', endpoint: 'chat/completions', system: 'You are a helpful assistant.', prompt: '请用一句话介绍这个调试工作台。', temperature: 0.7, maxTokens: 512, stream: false, headers: '{\n  "X-Debug-Trace": "true"\n}' })
const imageForm = reactive({ prompt: '一张简洁的科技感蓝色渐变背景', size: '1024x1024', quality: 'standard' })
const imageMode = ref(false); const running = ref(false); const responseStatus = ref(''); const activeTab = ref('inbound')
watch(() => form.endpoint, (endpoint) => {
  if (endpoint === 'images/generations') imageMode.value = true
})
const tabs = [{ key: 'inbound', label: '入站完整参数' }, { key: 'outbound', label: '出站完整参数' }, { key: 'upstream-request', label: '请求上游完整参数' }, { key: 'upstream-response', label: '上游完整响应参数' }]
const presets = [{ label: '健康检查', prompt: '返回 OK' }, { label: '长文本', prompt: '请总结这段文本：' }, { label: 'JSON 输出', prompt: '仅输出合法 JSON，对象包含 message 字段。' }]
const payloads = reactive<Record<string, unknown>>({ inbound: { method: 'POST', path: '/v1/chat/completions', headers: { 'content-type': 'application/json', authorization: 'Bearer ••••••••' }, body: { model: form.model, messages: [{ role: 'user', content: form.prompt }], stream: form.stream } }, outbound: { status: 'pending', transformed_model: form.model, route: 'account://acc-01', body: { temperature: form.temperature, max_tokens: form.maxTokens } }, 'upstream-request': { url: 'https://api.openai.com/v1/chat/completions', method: 'POST', headers: { authorization: 'Bearer ••••••••', 'content-type': 'application/json' }, body: { model: form.model, messages: [{ role: 'system', content: form.system }, { role: 'user', content: form.prompt }] } }, 'upstream-response': { status: '—', headers: {}, body: null } })
const currentPayload = computed(() => JSON.stringify(payloads[activeTab.value], null, 2)); const imagePreviewUrl = computed(() => { const body = (payloads['upstream-response'] as any)?.body; return imageMode.value && body?.data?.[0]?.url ? String(body.data[0].url) : '' }); const tabDescription = computed(() => tabs.find(t => t.key === activeTab.value)?.label)
function applyPreset(p: { prompt: string }) { form.prompt = p.prompt }
function resetAll() { form.prompt = ''; responseStatus.value = ''; payloads['upstream-response'] = { status: '—', headers: {}, body: null } }
async function runRequest() {
  running.value = true; responseStatus.value = ''
  let customHeaders: Record<string, string> = {}
  try { customHeaders = JSON.parse(form.headers || '{}') } catch { customHeaders = { 'X-Debug-Trace': 'true' } }
  payloads.inbound = { method: 'POST', path: `/v1/${form.endpoint}`, headers: { authorization: 'Bearer ••••••••', 'content-type': 'application/json', ...customHeaders }, body: { model: form.model, messages: [{ role: 'user', content: form.prompt }], stream: form.stream } }
  const upstreamBody = imageMode.value ? { model: form.model, prompt: imageForm.prompt, size: imageForm.size, quality: imageForm.quality } : { model: form.model, messages: [{ role: 'system', content: form.system }, { role: 'user', content: form.prompt }], temperature: form.temperature, max_tokens: form.maxTokens, stream: form.stream }
  payloads.outbound = { status: 'routing', transformed_model: form.model, route: `account://${form.account}`, body: upstreamBody }
  payloads['upstream-request'] = { url: `https://api.openai.com/v1/${form.endpoint}`, method: 'POST', headers: { authorization: 'Bearer ••••••••', 'content-type': 'application/json' }, body: upstreamBody }
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
