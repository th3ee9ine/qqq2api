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
          <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br from-primary-500 to-primary-600">
            <Icon name="play" size="md" class="text-white" :stroke-width="2" />
          </div>
          <div>
            <div class="font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</div>
            <div class="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
              <span class="rounded bg-gray-200 px-1.5 py-0.5 text-[10px] font-medium uppercase dark:bg-dark-500">
                {{ account.type }}
              </span>
              <span>{{ t('admin.accounts.account') }}</span>
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

      <div v-if="supportsPromptInput" class="space-y-1.5">
        <TextArea
          v-model="testPrompt"
          :label="t('admin.accounts.imagePromptLabel')"
          :placeholder="t('admin.accounts.imagePromptPlaceholder')"
          :hint="t('admin.accounts.imageTestHint')"
          :disabled="status === 'connecting'"
          rows="3"
        />
      </div>

      <div class="group relative">
        <div
          ref="terminalRef"
          class="max-h-[240px] min-h-[120px] overflow-y-auto rounded-xl border border-gray-700 bg-gray-900 p-4 font-mono text-sm dark:border-gray-800 dark:bg-black"
        >
          <div v-if="status === 'idle'" class="flex items-center gap-2 text-gray-500">
            <Icon name="play" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.readyToTest') }}</span>
          </div>
          <div v-else-if="status === 'connecting'" class="flex items-center gap-2 text-yellow-400">
            <Icon name="refresh" size="sm" class="animate-spin" :stroke-width="2" />
            <span>{{ t('admin.accounts.connectingToApi') }}</span>
          </div>
          <div v-for="(line, index) in outputLines" :key="index" :class="line.class">
            {{ line.text }}
          </div>
          <div v-if="streamingContent" class="text-green-400">
            {{ streamingContent }}<span class="animate-pulse">_</span>
          </div>
          <div v-if="status === 'success'" class="mt-3 flex items-center gap-2 border-t border-gray-700 pt-3 text-green-400">
            <Icon name="check" size="sm" :stroke-width="2" />
            <span>{{ t('admin.accounts.testCompleted') }}</span>
          </div>
          <div v-else-if="status === 'error'" class="mt-3 flex items-center gap-2 border-t border-gray-700 pt-3 text-red-400">
            <Icon name="x" size="sm" :stroke-width="2" />
            <span>{{ errorMessage }}</span>
          </div>
        </div>
        <button
          v-if="outputLines.length > 0"
          class="absolute right-2 top-2 rounded-lg bg-gray-800/80 p-1.5 text-gray-400 opacity-0 transition-all hover:bg-gray-700 hover:text-white group-hover:opacity-100"
          :title="t('admin.accounts.copyOutput')"
          @click="copyOutput"
        >
          <Icon name="link" size="sm" :stroke-width="2" />
        </button>
      </div>

      <!-- OpenAI image models return image events over the same test stream. -->
      <div v-if="generatedImages.length > 0" class="space-y-2">
        <div class="text-xs font-medium text-gray-600 dark:text-gray-300">
          {{ t('admin.accounts.imagePreview') }}
        </div>
        <div class="flex flex-wrap justify-center gap-3">
          <div
            v-for="(image, index) in generatedImages"
            :key="`${image.url}-${index}`"
            class="group/img relative cursor-pointer overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm transition hover:border-primary-300 hover:shadow-md dark:border-dark-500 dark:bg-dark-700"
            @click="previewImageUrl = image.url"
          >
            <img
              :src="image.url"
              :alt="t('admin.accounts.imagePreviewAlt', { index: index + 1 })"
              class="max-h-[360px] w-full object-contain"
            />
            <div class="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover/img:bg-black/20">
              <Icon name="eye" size="lg" class="text-white opacity-0 drop-shadow-lg transition-opacity group-hover/img:opacity-100" :stroke-width="2" />
            </div>
            <div class="border-t border-gray-100 px-3 py-1.5 text-xs text-gray-500 dark:border-dark-500 dark:text-gray-300">
              {{ image.mimeType || 'image/*' }}
            </div>
          </div>
        </div>
      </div>

      <Teleport to="body">
        <Transition name="fade">
          <div
            v-if="previewImageUrl"
            class="fixed inset-0 z-[100] flex items-center justify-center bg-black/80 p-4"
            @click.self="previewImageUrl = ''"
          >
            <button
              class="absolute right-4 top-4 rounded-full bg-black/50 p-2 text-white transition-colors hover:bg-black/70"
              @click="previewImageUrl = ''"
            >
              <Icon name="x" size="lg" :stroke-width="2" />
            </button>
            <img
              :src="previewImageUrl"
              :alt="t('admin.accounts.imageLightboxAlt')"
              class="max-h-[90vh] max-w-[90vw] rounded-lg object-contain shadow-2xl"
            />
          </div>
        </Transition>
      </Teleport>

      <div class="flex items-center justify-between px-1 text-xs text-gray-500 dark:text-gray-400">
        <div class="flex items-center gap-3">
          <span class="flex items-center gap-1">
            <Icon name="grid" size="sm" :stroke-width="2" />
            {{ t('admin.accounts.testModel') }}
          </span>
        </div>
        <span class="flex items-center gap-1">
          <Icon name="chat" size="sm" :stroke-width="2" />
          {{ testModeSummary }}
        </span>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button
          class="rounded-lg bg-gray-100 px-4 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-200 dark:bg-dark-600 dark:text-gray-300 dark:hover:bg-dark-500"
          @click="handleClose"
        >
          {{ t('common.close') }}
        </button>
        <button
          :class="[
            'flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-all',
            !canStartTest
              ? 'cursor-not-allowed bg-primary-400 text-white'
              : status === 'success'
                ? 'bg-green-500 text-white hover:bg-green-600'
                : status === 'error'
                  ? 'bg-orange-500 text-white hover:bg-orange-600'
                  : 'bg-primary-500 text-white hover:bg-primary-600'
          ]"
          :disabled="!canStartTest"
          @click="startTest"
        >
          <Icon v-if="status === 'connecting'" name="refresh" size="sm" class="animate-spin" :stroke-width="2" />
          <Icon v-else-if="status === 'idle'" name="play" size="sm" :stroke-width="2" />
          <Icon v-else name="refresh" size="sm" :stroke-width="2" />
          <span>
            {{
              status === 'connecting'
                ? t('admin.accounts.testing')
                : status === 'idle'
                  ? t('admin.accounts.startTest')
                  : t('admin.accounts.retry')
            }}
          </span>
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
  error?: string
  success?: boolean
  model?: string
  image_url?: string
  mime_type?: string
}

interface PreviewMedia {
  url: string
  mimeType?: string
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
const generatedImages = ref<PreviewMedia[]>([])
const previewImageUrl = ref('')
const loadingModels = ref(false)
let abortController: AbortController | null = null

const supported = computed(() =>
  props.account?.platform === 'anthropic' || props.account?.platform === 'openai'
)
const isOpenAI = computed(() => props.account?.platform === 'openai')
const supportsOpenAIImageTest = computed(() =>
  isOpenAI.value && selectedModelId.value.toLowerCase().startsWith('gpt-image-')
)
const supportsPromptInput = computed(() => supportsOpenAIImageTest.value)
const openAITestModeOptions = computed(() => [
  { value: 'default', label: t('admin.accounts.openai.testModeDefault') },
  { value: 'compact', label: t('admin.accounts.openai.testModeCompact') }
])
const canStartTest = computed(() =>
  supported.value && status.value !== 'connecting' && Boolean(selectedModelId.value)
)
const testModeSummary = computed(() =>
  supportsOpenAIImageTest.value
    ? t('admin.accounts.imageTestMode')
    : t('admin.accounts.testPrompt')
)

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
      ? availableModels.value.find(model => {
        const id = model.id.toLowerCase()
        return id.startsWith('gpt-') && !id.startsWith('gpt-image-')
      }) || availableModels.value.find(model => model.id.toLowerCase().startsWith('gpt-image-'))
      : availableModels.value.find(model => model.id.includes('sonnet'))
    selectedModelId.value = preferred?.id || availableModels.value[0]?.id || ''
  } catch {
    availableModels.value = []
  } finally {
    loadingModels.value = false
  }
}

watch(supportsOpenAIImageTest, enabled => {
  if (enabled && !testPrompt.value.trim()) {
    testPrompt.value = t('admin.accounts.imagePromptDefault')
  }
})

function resetState() {
  status.value = 'idle'
  outputLines.value = []
  streamingContent.value = ''
  errorMessage.value = ''
  generatedImages.value = []
  previewImageUrl.value = ''
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
  const text = outputLines.value.map(line => line.text).join('\n')
  copyToClipboard(text, t('admin.accounts.outputCopied'))
}

function handleEvent(event: TestEvent) {
  if (event.type === 'content' && event.text) {
    streamingContent.value += event.text
    return
  }
  if (event.type === 'test_start') {
    void addLine(t('admin.accounts.connectedToApi'), 'text-green-400')
    if (event.model) {
      void addLine(t('admin.accounts.usingModel', { model: event.model }), 'text-cyan-400')
    }
    void addLine(
      supportsOpenAIImageTest.value
        ? t('admin.accounts.sendingImageRequest')
        : t('admin.accounts.sendingTestMessage'),
      'text-gray-400'
    )
    void addLine('', 'text-gray-300')
    void addLine(t('admin.accounts.response'), 'text-yellow-400')
    return
  }
  if (event.type === 'error') {
    status.value = 'error'
    errorMessage.value = event.error || event.message || t('common.unknownError')
    if (streamingContent.value) {
      void addLine(streamingContent.value, 'text-green-300')
      streamingContent.value = ''
    }
    return
  }
  if (event.type === 'image' && event.image_url) {
    generatedImages.value.push({ url: event.image_url, mimeType: event.mime_type })
    void addLine(
      t('admin.accounts.imageReceived', { count: generatedImages.value.length }),
      'text-purple-300'
    )
    return
  }
  if (event.type === 'status' && event.text) {
    void addLine(event.text, 'text-cyan-300')
    return
  }
  if (event.type === 'test_complete') {
    if (streamingContent.value) {
      void addLine(streamingContent.value, 'text-green-300')
      streamingContent.value = ''
    }
    status.value = event.success === false ? 'error' : 'success'
    if (event.success === false) {
      errorMessage.value = event.error || event.message || t('admin.accounts.testFailed')
    }
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
  await addLine(
    t('admin.accounts.testAccountTypeLabel', { type: props.account.type }),
    'text-gray-400'
  )
  await addLine('', 'text-gray-300')
  abortStream()
  abortController = new AbortController()
  try {
    const body: { model_id: string; prompt: string; mode?: string } = {
      model_id: selectedModelId.value,
      prompt: supportsPromptInput.value ? testPrompt.value.trim() : ''
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
  } catch (error: unknown) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      status.value = 'idle'
      return
    }
    status.value = 'error'
    const message = error instanceof Error ? error.message : t('common.unknownError')
    errorMessage.value = message
    await addLine(t('admin.accounts.errorPrefix', { message }), 'text-red-400')
  } finally {
    abortController = null
  }
}
</script>

<style>
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
</style>
