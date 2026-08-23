<template>
  <div class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-700 dark:bg-blue-900/30">
    <div class="flex items-start gap-4">
      <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-blue-500">
        <Icon name="link" size="md" class="text-white" />
      </div>
      <div class="min-w-0 flex-1">
        <h4 class="mb-3 font-semibold text-blue-900 dark:text-blue-200">{{ oauthTitle }}</h4>

        <fieldset v-if="methodOptions.length > 1" class="mb-4 border-0 p-0">
          <legend class="mb-2 text-sm font-medium text-blue-800 dark:text-blue-300">
            {{ methodLabel }}
          </legend>
          <div class="flex flex-wrap gap-4">
            <label
              v-for="option in methodOptions"
              :key="option.value"
              class="flex cursor-pointer items-center gap-2"
            >
              <input
                v-model="inputMethod"
                type="radio"
                :value="option.value"
                class="text-blue-600 focus:ring-blue-500"
              />
              <span class="text-sm text-blue-900 dark:text-blue-200">{{ option.label }}</span>
            </label>
          </div>
        </fieldset>

        <div
          v-if="inputMethod === 'refresh_token' || inputMethod === 'mobile_refresh_token'"
          class="space-y-3"
        >
          <label class="input-label">Refresh Token</label>
          <textarea
            v-model="refreshTokenInput"
            rows="4"
            class="input w-full resize-y font-mono text-sm"
            :placeholder="t(oauthKey('refreshTokenPlaceholder'))"
          />
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !refreshTokenInput.trim()"
            @click="handleValidateRefreshToken"
          >
            {{ loading ? t(oauthKey('validating')) : t(oauthKey('validateAndCreate')) }}
          </button>
        </div>

        <div v-else-if="inputMethod === 'cookie'" class="space-y-3">
          <label class="input-label">{{ t('admin.accounts.oauth.sessionKey') }}</label>
          <textarea
            v-model="sessionKeyInput"
            rows="4"
            class="input w-full resize-y font-mono text-sm"
            :placeholder="
              allowMultiple
                ? t('admin.accounts.oauth.sessionKeyPlaceholder')
                : t('admin.accounts.oauth.sessionKeyPlaceholderSingle')
            "
          />
          <p v-if="showHelp" class="text-xs text-blue-700 dark:text-blue-300">
            {{ t('admin.accounts.oauth.cookieAutoAuthDesc') }}
          </p>
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !sessionKeyInput.trim()"
            @click="handleCookieAuth"
          >
            {{ loading ? t('admin.accounts.oauth.authorizing') : t('admin.accounts.oauth.startAutoAuth') }}
          </button>
        </div>

        <div v-else-if="inputMethod === 'session_token'" class="space-y-3">
          <label class="input-label">Session Token</label>
          <textarea v-model="sessionTokenInput" rows="4" class="input w-full resize-y font-mono text-sm" />
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !sessionTokenInput.trim()"
            @click="emit('validate-session-token', sessionTokenInput.trim())"
          >
            {{ t(oauthKey('validateAndCreate')) }}
          </button>
        </div>

        <div v-else-if="inputMethod === 'access_token'" class="space-y-3">
          <label class="input-label">Access Token</label>
          <textarea v-model="accessTokenInput" rows="4" class="input w-full resize-y font-mono text-sm" />
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !accessTokenInput.trim()"
            @click="emit('import-access-token', accessTokenInput.trim())"
          >
            {{ t(oauthKey('validateAndCreate')) }}
          </button>
        </div>

        <div
          v-else-if="inputMethod === 'codex_session' || inputMethod === 'agent_identity'"
          class="space-y-3"
        >
          <label class="input-label">
            {{
              t(
                inputMethod === 'agent_identity'
                  ? 'admin.accounts.oauth.openai.agentIdentityInputLabel'
                  : 'admin.accounts.oauth.openai.codexSessionInputLabel'
              )
            }}
          </label>
          <textarea
            v-model="codexSessionInput"
            rows="8"
            class="input w-full resize-y font-mono text-sm"
            :placeholder="
              t(
                inputMethod === 'agent_identity'
                  ? 'admin.accounts.oauth.openai.agentIdentityPlaceholder'
                  : 'admin.accounts.oauth.openai.codexSessionPlaceholder'
              )
            "
          />
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !codexSessionInput.trim()"
            @click="emit('import-codex-session', codexSessionInput.trim())"
          >
            {{ t('admin.accounts.oauth.openai.codexSessionImportAndCreate') }}
          </button>
        </div>

        <div v-else-if="inputMethod === 'codex_pat'" class="space-y-3">
          <label class="input-label">{{ t('admin.accounts.oauth.openai.codexPatInputLabel') }}</label>
          <textarea
            v-model="codexPATInput"
            rows="4"
            class="input w-full resize-y font-mono text-sm"
            :placeholder="t('admin.accounts.oauth.openai.codexPatPlaceholder')"
          />
          <button
            type="button"
            class="btn btn-primary w-full"
            :disabled="loading || !codexPATInput.trim()"
            @click="emit('import-codex-pat', codexPATInput.trim())"
          >
            {{ t('admin.accounts.oauth.openai.codexPatImportAndCreate') }}
          </button>
        </div>

        <div v-else class="space-y-4">
          <p class="text-sm text-blue-800 dark:text-blue-300">{{ t(oauthKey('followSteps')) }}</p>
          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-2 font-medium text-blue-900 dark:text-blue-200">
              1. {{ t(oauthKey('step1GenerateUrl')) }}
            </p>
            <button
              v-if="!authUrl"
              type="button"
              class="btn btn-primary"
              :disabled="loading"
              @click="emit('generate-url')"
            >
              {{ loading ? t('admin.accounts.oauth.generating') : t(oauthKey('generateAuthUrl')) }}
            </button>
            <div v-else class="flex items-center gap-2">
              <input :value="authUrl" readonly class="input min-w-0 flex-1 font-mono text-xs" />
              <button type="button" class="btn btn-secondary" @click="copyAuthorizationUrl">
                {{ copied ? '✓' : t('common.copy') }}
              </button>
            </div>
          </div>

          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <p class="mb-2 font-medium text-blue-900 dark:text-blue-200">
              2. {{ t(oauthKey('step2OpenUrl')) }}
            </p>
            <p class="text-sm text-blue-700 dark:text-blue-300">{{ t(oauthKey('openUrlDesc')) }}</p>
            <p
              v-if="platform === 'openai'"
              class="mt-2 rounded border border-amber-300 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
            >
              {{ t('admin.accounts.oauth.openai.importantNotice') }}
            </p>
            <p
              v-else-if="showProxyWarning"
              class="mt-2 rounded border border-yellow-300 bg-yellow-50 p-3 text-xs text-yellow-800 dark:border-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300"
            >
              {{ t('admin.accounts.oauth.proxyWarning') }}
            </p>
          </div>

          <div class="rounded-lg border border-blue-300 bg-white/80 p-4 dark:border-blue-600 dark:bg-gray-800/80">
            <label class="input-label">3. {{ t(oauthKey('authCode')) }}</label>
            <textarea
              v-model="authCodeInput"
              rows="3"
              class="input w-full resize-y font-mono text-sm"
              :placeholder="t(oauthKey('authCodePlaceholder'))"
            />
          </div>
        </div>

        <p
          v-if="error"
          class="mt-3 whitespace-pre-line rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-600 dark:border-red-700 dark:bg-red-900/30 dark:text-red-400"
        >
          {{ error }}
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { AddMethod, AuthInputMethod } from '@/composables/useAccountOAuth'
import Icon from '@/components/icons/Icon.vue'

type SupportedOAuthPlatform = 'anthropic' | 'openai'

const props = withDefaults(defineProps<{
  addMethod: AddMethod
  authUrl?: string
  sessionId?: string
  loading?: boolean
  error?: string
  showHelp?: boolean
  showProxyWarning?: boolean
  allowMultiple?: boolean
  methodLabel?: string
  showCookieOption?: boolean
  showRefreshTokenOption?: boolean
  showMobileRefreshTokenOption?: boolean
  showSessionTokenOption?: boolean
  showAccessTokenOption?: boolean
  showCodexSessionImportOption?: boolean
  showAgentIdentityOption?: boolean
  showCodexPatOption?: boolean
  showManualOption?: boolean
  initialInputMethod?: AuthInputMethod
  platform?: SupportedOAuthPlatform
}>(), {
  authUrl: '',
  sessionId: '',
  loading: false,
  error: '',
  showHelp: true,
  showProxyWarning: true,
  allowMultiple: false,
  methodLabel: 'Authorization Method',
  showCookieOption: true,
  showRefreshTokenOption: false,
  showMobileRefreshTokenOption: false,
  showSessionTokenOption: false,
  showAccessTokenOption: false,
  showCodexSessionImportOption: false,
  showAgentIdentityOption: false,
  showCodexPatOption: false,
  showManualOption: true,
  initialInputMethod: 'manual',
  platform: 'anthropic'
})

const emit = defineEmits<{
  'generate-url': []
  'cookie-auth': [sessionKey: string]
  'validate-refresh-token': [refreshToken: string]
  'validate-mobile-refresh-token': [refreshToken: string]
  'validate-session-token': [sessionToken: string]
  'import-access-token': [accessToken: string]
  'import-codex-session': [content: string]
  'import-codex-pat': [accessToken: string]
  'update:inputMethod': [method: AuthInputMethod]
}>()

const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const inputMethod = ref<AuthInputMethod>(props.initialInputMethod)
const authCodeInput = ref('')
const oauthState = ref('')
const sessionKeyInput = ref('')
const refreshTokenInput = ref('')
const sessionTokenInput = ref('')
const accessTokenInput = ref('')
const codexSessionInput = ref('')
const codexPATInput = ref('')

const oauthKey = (key: string) =>
  props.platform === 'openai' ? 'admin.accounts.oauth.openai.' + key : 'admin.accounts.oauth.' + key
const oauthTitle = computed(() => t(oauthKey('title')))
const methodOptions = computed<Array<{ value: AuthInputMethod; label: string }>>(() => {
  const options: Array<{ value: AuthInputMethod; label: string }> = []
  if (props.showManualOption) options.push({ value: 'manual', label: t('admin.accounts.oauth.manualAuth') })
  if (props.showCookieOption) options.push({ value: 'cookie', label: t('admin.accounts.oauth.cookieAutoAuth') })
  if (props.showRefreshTokenOption) options.push({ value: 'refresh_token', label: t(oauthKey('refreshTokenAuth')) })
  if (props.showMobileRefreshTokenOption) {
    options.push({ value: 'mobile_refresh_token', label: t('admin.accounts.oauth.openai.mobileRefreshTokenAuth') })
  }
  if (props.showSessionTokenOption) options.push({ value: 'session_token', label: 'Session Token' })
  if (props.showAccessTokenOption) options.push({ value: 'access_token', label: t('admin.accounts.oauth.openai.accessTokenAuth') })
  if (props.showCodexSessionImportOption) {
    options.push({ value: 'codex_session', label: t('admin.accounts.oauth.openai.codexSessionAuth') })
  }
  if (props.showAgentIdentityOption) {
    options.push({ value: 'agent_identity', label: t('admin.accounts.oauth.openai.agentIdentityAuth') })
  }
  if (props.showCodexPatOption) {
    options.push({ value: 'codex_pat', label: t('admin.accounts.oauth.openai.codexPatAuth') })
  }
  return options
})

watch(() => props.initialInputMethod, value => { inputMethod.value = value })
watch(inputMethod, value => emit('update:inputMethod', value))
watch(authCodeInput, value => {
  if (props.platform !== 'openai' || !value.includes('code=')) return
  try {
    const parsed = new URL(value.includes('?') ? value : 'http://localhost/callback?' + value.replace(/^\?/, ''))
    const code = parsed.searchParams.get('code')
    oauthState.value = parsed.searchParams.get('state') || oauthState.value
    if (code) authCodeInput.value = code
  } catch {
    const code = value.match(/[?&]code=([^&]+)/)?.[1]
    const state = value.match(/[?&]state=([^&]+)/)?.[1]
    if (state) oauthState.value = state
    if (code) authCodeInput.value = code
  }
})

function handleValidateRefreshToken() {
  const token = refreshTokenInput.value.trim()
  if (!token) return
  if (inputMethod.value === 'mobile_refresh_token') {
    emit('validate-mobile-refresh-token', token)
  } else {
    emit('validate-refresh-token', token)
  }
}

function handleCookieAuth() {
  const value = sessionKeyInput.value.trim()
  if (value) emit('cookie-auth', value)
}

function copyAuthorizationUrl() {
  if (props.authUrl) copyToClipboard(props.authUrl, 'URL copied to clipboard')
}

function reset() {
  authCodeInput.value = ''
  oauthState.value = ''
  sessionKeyInput.value = ''
  refreshTokenInput.value = ''
  sessionTokenInput.value = ''
  accessTokenInput.value = ''
  codexSessionInput.value = ''
  codexPATInput.value = ''
  inputMethod.value = props.initialInputMethod
}

defineExpose({
  authCode: authCodeInput,
  oauthState,
  sessionKey: sessionKeyInput,
  refreshToken: refreshTokenInput,
  sessionToken: sessionTokenInput,
  accessToken: accessTokenInput,
  codexSession: codexSessionInput,
  codexPAT: codexPATInput,
  inputMethod,
  reset
})
</script>
