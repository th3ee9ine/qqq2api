<template>
  <BaseDialog
    :show="show && supported"
    :title="t('admin.accounts.testAccountConnection')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="account" class="space-y-4">
      <div
        class="flex items-center justify-between rounded-xl border border-gray-200 bg-gradient-to-r from-gray-50 to-gray-100 p-3 dark:border-dark-500 dark:from-dark-700 dark:to-dark-600"
      >
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-primary-500">
            <Icon name="play" size="md" class="text-white" />
          </div>
          <div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</div>
            <div class="text-xs text-gray-500 dark:text-gray-400">
              {{ account.platform }} · {{ account.type }}
            </div>
          </div>
        </div>
        <span
          :class="[
            'rounded-full px-2.5 py-1 text-xs font-semibold',
            account.status === 'active'
              ? 'bg-green-100 text-green-700 dark:bg-green-500/20 dark:text-green-400'
              : 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'
          ]"
        >
          {{ account.status }}
        </span>
      </div>

      <div class="space-y-1.5">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.selectTestModel') }}
        </label>
        <Select
          v-model="selectedModelId"
          :options="availableModels"
          :disabled="loadingModels || status === 'connecting'"
          value-key="id"
          label-key="display_name"
          :placeholder="loadingModels ? t('common.loading') + '...' : t('admin.accounts.selectTestModel')"
        />
      </div>

      <div v-if="isOpenAI" class="space-y-1.5">
        <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.accounts.openai.testMode') }}
        </label>
        <Select
          v-model="testMode"
          :options="openAITestModeOptions"
          :disabled="status === 'connecting'"
        />
      </div>

      <div class="space-y-1.5">
        <TextArea
          v-model="testPrompt"
          :label="t('admin.accounts.testPrompt')"
          :placeholder="t('admin.accounts.testPromptPlaceholder')"
          :disabled="status === 'connecting'"
          rows="3"
        />
      </div>

      <div class="group relative">
        <div
          ref="terminalRef"
          class="max-h-[260px] min-h-[140px] overflow-y-auto rounded-xl border border-gray-700 bg-gray-900 p-4 font-mono text-sm dark:border-gray-800 dark:bg-black"
        >
          <div v-if="status === 'idle'" class="flex items-center gap-2 text-gray-500">
            <Icon name="play" size="sm" />
            <span>{{ t('admin.accounts.readyToTest') }}</span>
          </div>
          <div v-else-if="status === 'connecting'" class="flex items-center gap-2 text-yellow-400">
            <Icon name="refresh" size="sm" class="animate-spin" />
            <span>{{ t('admin.accounts.connectingToApi') }}</span>
          </div>
          <div v-for="(line, index) in outputLines" :key="index" :class="line.class">
            {{ line.text }}
          </div>
          <div v-if="streamingContent" class="whitespace-pre-wrap text-green-400">
            {{ streamingContent }}<span v-if="status === 'connecting'" class="animate-pulse">_</span>
          </div>
          <div v-if="status === 'success'" class="mt-3 flex items-center gap-2 text-green-400">
            <Icon name="check" size="sm" />
            <span>{{ t('admin.accounts.testSuccessful') }}</span>
          </div>
          <div v-if="status === 'error'" class="mt-3 text-red-400">
            {{ errorMessage }}
          </div>
        </div>
        <button
          v-if="outputText"
          type="button"
          class="absolute right-2 top-2 rounded bg-gray-700 px-2 py-1 text-xs text-gray-200 hover:bg-gray-600"
          @click="copyOutput"
        >
          {{ t('common.copy') }}
        </button>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">
          {{ t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!canStartTest"
          @click="startTest"
        >
          <Icon
            :name="status === 'connecting' ? 'refresh' : status === 'idle' ? 'play' : 'refresh'"
            size="sm"
            :class="{ 'animate-spin': status === 'connecting' }"
          />
          {{
            status === 'connecting'
              ? t('admin.accounts.testing')
              : status === 'idle'
                ? t('admin.accounts.startTest')
                : t('admin.accounts.retry')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { buildApiUrl } from '@/api/client'
import { ADMIN_UI_REQUEST_HEADER } from '@/api/adminUIRequest'
import { useClipboard } from '@/composables/useClipboard'
import type { Account, ClaudeModel } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import { Icon } from '@/components/icons'

interface OutputLine {
  text: string
  class: string
}

interface TestEvent {
  type?: string
  text?: string
  message?: string
  success?: boolean
  model?: string
}

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const terminalRef = ref<HTMLElement | null>(null)
const status = ref<'idle' | 'connecting' | 'success' | 'error'>('idle')
const outputLines = ref<OutputLine[]>([])
const streamingContent = ref('')
const errorMessage = ref('')
const availableModels = ref<ClaudeModel[]>([])
const selectedModelId = ref('')
const testPrompt = ref('')
const testMode = ref<'default' | 'compact'>('default')
const loadingModels = ref(false)
let abortController: AbortController | null = null

const supported = computed(() =>
  props.account?.platform === 'anthropic' || props.account?.platform === 'openai'
)
const isOpenAI = computed(() => props.account?.platform === 'openai')
const openAITestModeOptions = computed(() => [
  { value: 'default', label: t('admin.accounts.openai.testModeDefault') },
  { value: 'compact', label: t('admin.accounts.openai.testModeCompact') }
])
const canStartTest = computed(() =>
  supported.value && status.value !== 'connecting' && Boolean(selectedModelId.value)
)
const outputText = computed(() => [
  ...outputLines.value.map(line => line.text),
  streamingContent.value,
  errorMessage.value
].filter(Boolean).join('\n'))

watch(
  () => [props.show, props.account] as const,
  async ([visible]) => {
    if (!visible) {
      abortStream()
      return
    }
    if (!supported.value) {
      emit('close')
      return
    }
    resetState()
    testMode.value = 'default'
    testPrompt.value = ''
    await loadAvailableModels()
  }
)

async function loadAvailableModels() {
  if (!props.account || !supported.value) return
  loadingModels.value = true
  selectedModelId.value = ''
  try {
    availableModels.value = await adminAPI.accounts.getAvailableModels(props.account.id)
    const preferred = isOpenAI.value
      ? availableModels.value.find(model => model.id.startsWith('gpt-'))
      : availableModels.value.find(model => model.id.includes('sonnet'))
    selectedModelId.value = preferred?.id || availableModels.value[0]?.id || ''
  } catch {
    availableModels.value = []
  } finally {
    loadingModels.value = false
  }
}

function resetState() {
  status.value = 'idle'
  outputLines.value = []
  streamingContent.value = ''
  errorMessage.value = ''
}

function abortStream() {
  abortController?.abort()
  abortController = null
}

function handleClose() {
  abortStream()
  emit('close')
}

async function addLine(text: string, className = 'text-gray-300') {
  outputLines.value.push({ text, class: className })
  await nextTick()
  if (terminalRef.value) terminalRef.value.scrollTop = terminalRef.value.scrollHeight
}

function copyOutput() {
  copyToClipboard(outputText.value, 'Output copied to clipboard')
}

function handleEvent(event: TestEvent) {
  if (event.type === 'content' && event.text) {
    streamingContent.value += event.text
    return
  }
  if (event.type === 'test_start') {
    if (event.model) void addLine(event.model, 'text-blue-300')
    return
  }
  if (event.type === 'error') {
    status.value = 'error'
    errorMessage.value = event.message || t('admin.accounts.testFailed')
    return
  }
  if (event.type === 'test_complete') {
    status.value = event.success === false ? 'error' : 'success'
    if (event.success === false && event.message) errorMessage.value = event.message
  }
}

async function startTest() {
  if (!props.account || !canStartTest.value) return
  resetState()
  status.value = 'connecting'
  await addLine(
    t('admin.accounts.startingTestForAccount', { name: props.account.name }),
    'text-blue-400'
  )
  abortStream()
  abortController = new AbortController()
  try {
    const body: { model_id: string; prompt: string; mode?: string } = {
      model_id: selectedModelId.value,
      prompt: testPrompt.value.trim()
    }
    if (isOpenAI.value) body.mode = testMode.value
    const response = await fetch(buildApiUrl('/admin/accounts/' + props.account.id + '/test'), {
      method: 'POST',
      headers: {
        Authorization: 'Bearer ' + localStorage.getItem('auth_token'),
        'Content-Type': 'application/json',
        [ADMIN_UI_REQUEST_HEADER]: '1'
      },
      body: JSON.stringify(body),
      signal: abortController.signal
    })
    if (!response.ok) throw new Error('HTTP error! status: ' + response.status)
    const reader = response.body?.getReader()
    if (!reader) throw new Error(t('admin.accounts.testFailed'))
    const decoder = new TextDecoder()
    let buffer = ''
    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() || ''
      for (const line of lines) {
        if (!line.startsWith('data:')) continue
        const data = line.slice(5).trim()
        if (!data || data === '[DONE]') continue
        try {
          handleEvent(JSON.parse(data) as TestEvent)
        } catch {
          // Ignore malformed keep-alive events.
        }
      }
    }
    if (status.value === 'connecting') status.value = 'success'
  } catch (error: any) {
    if (error?.name === 'AbortError') return
    status.value = 'error'
    errorMessage.value = error?.message || t('admin.accounts.testFailed')
  } finally {
    abortController = null
  }
}
</script>
