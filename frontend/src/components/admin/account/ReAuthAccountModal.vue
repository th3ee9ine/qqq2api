<template>
  <BaseDialog
    :show="show && supported"
    :title="t('admin.accounts.reAuthorizeAccount')"
    width="normal"
    @close="handleClose"
  >
    <div v-if="account" class="space-y-4">
      <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-700">
        <div class="flex items-center gap-3">
          <div
            :class="[
              'flex h-10 w-10 items-center justify-center rounded-lg bg-gradient-to-br',
              isOpenAI ? 'from-green-500 to-green-600' : 'from-orange-500 to-orange-600'
            ]"
          >
            <Icon name="sparkles" size="md" class="text-white" />
          </div>
          <div>
            <span class="block font-semibold text-gray-900 dark:text-white">{{ account.name }}</span>
            <span class="text-sm text-gray-500 dark:text-gray-400">
              {{ isOpenAI ? t('admin.accounts.openaiAccount') : t('admin.accounts.claudeCodeAccount') }}
            </span>
          </div>
        </div>
      </div>

      <fieldset v-if="isAnthropic" class="border-0 p-0">
        <legend class="input-label">{{ t('admin.accounts.oauth.authMethod') }}</legend>
        <div class="mt-2 flex gap-4">
          <label class="flex cursor-pointer items-center">
            <input v-model="addMethod" type="radio" value="oauth" class="mr-2 text-primary-600 focus:ring-primary-500" />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.types.oauth') }}</span>
          </label>
          <label class="flex cursor-pointer items-center">
            <input v-model="addMethod" type="radio" value="setup-token" class="mr-2 text-primary-600 focus:ring-primary-500" />
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.accounts.setupTokenLongLived') }}</span>
          </label>
        </div>
      </fieldset>

      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        :add-method="addMethod"
        :auth-url="currentAuthUrl"
        :session-id="currentSessionId"
        :loading="currentLoading"
        :error="currentError"
        :show-help="isAnthropic"
        :show-proxy-warning="isAnthropic"
        :show-cookie-option="isAnthropic"
        :show-refresh-token-option="isOpenAI"
        :show-mobile-refresh-token-option="isOpenAI"
        :allow-multiple="false"
        :method-label="t('admin.accounts.inputMethod')"
        :platform="isOpenAI ? 'openai' : 'anthropic'"
        @generate-url="handleGenerateUrl"
        @cookie-auth="handleCookieAuth"
        @validate-refresh-token="handleOpenAIRefreshToken"
        @validate-mobile-refresh-token="handleOpenAIMobileRefreshToken"
      />
    </div>

    <template #footer>
      <div v-if="account" class="flex justify-between gap-3">
        <button type="button" class="btn btn-secondary" @click="handleClose">{{ t('common.cancel') }}</button>
        <button
          v-if="isManualInputMethod"
          type="button"
          :disabled="!canExchangeCode"
          class="btn btn-primary"
          @click="handleExchangeCode"
        >
          <svg
            v-if="currentLoading"
            class="-ml-1 mr-2 h-4 w-4 animate-spin"
            fill="none"
            viewBox="0 0 24 24"
          >
            <circle
              class="opacity-25"
              cx="12"
              cy="12"
              r="10"
              stroke="currentColor"
              stroke-width="4"
            />
            <path
              class="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
          {{ currentLoading ? t('admin.accounts.oauth.verifying') : t('admin.accounts.oauth.completeAuth') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { useAccountOAuth, type AddMethod, type AuthInputMethod } from '@/composables/useAccountOAuth'
import { useOpenAIOAuth, type OpenAITokenInfo } from '@/composables/useOpenAIOAuth'
import type { Account } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'

interface OAuthFlowExposed {
  authCode: string
  oauthState: string
  sessionKey: string
  inputMethod: AuthInputMethod
  reset: () => void
}

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: []; reauthorized: [account: Account] }>()
const { t } = useI18n()
const appStore = useAppStore()
const claudeOAuth = useAccountOAuth()
const openaiOAuth = useOpenAIOAuth()
const oauthFlowRef = ref<OAuthFlowExposed | null>(null)
const addMethod = ref<AddMethod>('oauth')

const isOpenAI = computed(() => props.account?.platform === 'openai')
const isAnthropic = computed(() => props.account?.platform === 'anthropic')
const supported = computed(() => isOpenAI.value || isAnthropic.value)
const currentAuthUrl = computed(() => isOpenAI.value ? openaiOAuth.authUrl.value : claudeOAuth.authUrl.value)
const currentSessionId = computed(() => isOpenAI.value ? openaiOAuth.sessionId.value : claudeOAuth.sessionId.value)
const currentLoading = computed(() => isOpenAI.value ? openaiOAuth.loading.value : claudeOAuth.loading.value)
const currentError = computed(() => isOpenAI.value ? openaiOAuth.error.value : claudeOAuth.error.value)
const isManualInputMethod = computed(() => oauthFlowRef.value?.inputMethod === 'manual')
const canExchangeCode = computed(() => Boolean(
  oauthFlowRef.value?.authCode.trim() && currentSessionId.value && !currentLoading.value
))

watch(() => props.show, visible => {
  if (visible && props.account && supported.value) {
    addMethod.value = props.account.type === 'setup-token' ? 'setup-token' : 'oauth'
  } else if (visible && !supported.value) {
    emit('close')
  } else if (!visible) {
    resetState()
  }
})

function resetState() {
  addMethod.value = 'oauth'
  claudeOAuth.resetState()
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}
function handleClose() { emit('close') }

async function handleGenerateUrl() {
  if (!props.account || !supported.value) return
  if (isOpenAI.value) await openaiOAuth.generateAuthUrl(props.account.proxy_id)
  else await claudeOAuth.generateAuthUrl(addMethod.value, props.account.proxy_id)
}

async function applyOpenAITokens(tokenInfo: OpenAITokenInfo) {
  if (!props.account || !isOpenAI.value) return
  try {
    const updated = await adminAPI.accounts.applyOAuthCredentials(props.account.id, {
      type: 'oauth',
      credentials: openaiOAuth.buildCredentials(tokenInfo),
      extra: openaiOAuth.buildExtraInfo(tokenInfo)
    })
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized', updated)
    handleClose()
  } catch (error: any) {
    openaiOAuth.error.value = error?.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(openaiOAuth.error.value)
  }
}

const OPENAI_MOBILE_RT_CLIENT_ID = 'app_LlGpXReQgckcGGUo2JrYvtJK'

async function handleOpenAIRefreshToken(value: string, clientId?: string) {
  if (!props.account || !isOpenAI.value) return
  const refreshToken = value.split('\n').map(item => item.trim()).find(Boolean)
  if (!refreshToken) return
  const tokenInfo = await openaiOAuth.validateRefreshToken(refreshToken, props.account.proxy_id, clientId)
  if (!tokenInfo) return
  if (clientId && !tokenInfo.client_id) tokenInfo.client_id = clientId
  await applyOpenAITokens(tokenInfo)
}

const handleOpenAIMobileRefreshToken = (value: string) =>
  handleOpenAIRefreshToken(value, OPENAI_MOBILE_RT_CLIENT_ID)

async function handleExchangeCode() {
  if (!props.account || !supported.value) return
  const code = oauthFlowRef.value?.authCode.trim()
  if (!code) return
  if (isOpenAI.value) {
    const state = (oauthFlowRef.value?.oauthState || openaiOAuth.oauthState.value).trim()
    const tokenInfo = await openaiOAuth.exchangeAuthCode(code, openaiOAuth.sessionId.value, state, props.account.proxy_id)
    if (tokenInfo) await applyOpenAITokens(tokenInfo)
    return
  }
  claudeOAuth.loading.value = true
  claudeOAuth.error.value = ''
  try {
    const endpoint = addMethod.value === 'oauth'
      ? '/admin/accounts/exchange-code'
      : '/admin/accounts/exchange-setup-token-code'
    const tokenInfo = await adminAPI.accounts.exchangeCode(endpoint, {
      session_id: claudeOAuth.sessionId.value,
      code,
      ...(props.account.proxy_id ? { proxy_id: props.account.proxy_id } : {})
    })
    const updated = await adminAPI.accounts.applyOAuthCredentials(props.account.id, {
      type: addMethod.value,
      credentials: tokenInfo as Record<string, unknown>,
      extra: claudeOAuth.buildExtraInfo(tokenInfo)
    })
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized', updated)
    handleClose()
  } catch (error: any) {
    claudeOAuth.error.value = error?.response?.data?.detail || t('admin.accounts.oauth.authFailed')
    appStore.showError(claudeOAuth.error.value)
  } finally {
    claudeOAuth.loading.value = false
  }
}

async function handleCookieAuth(sessionKey: string) {
  if (!props.account || !isAnthropic.value || !sessionKey.trim()) return
  try {
    const tokenInfo = await claudeOAuth.cookieAuth(addMethod.value, sessionKey, props.account.proxy_id)
    if (!tokenInfo) return
    const updated = await adminAPI.accounts.applyOAuthCredentials(props.account.id, {
      type: addMethod.value,
      credentials: tokenInfo as Record<string, unknown>,
      extra: claudeOAuth.buildExtraInfo(tokenInfo)
    })
    appStore.showSuccess(t('admin.accounts.reAuthorizedSuccess'))
    emit('reauthorized', updated)
    handleClose()
  } catch (error: any) {
    claudeOAuth.error.value = error?.response?.data?.detail || t('admin.accounts.oauth.cookieAuthFailed')
    appStore.showError(claudeOAuth.error.value)
  }
}
</script>
