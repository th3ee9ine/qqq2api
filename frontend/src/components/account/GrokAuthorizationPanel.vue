<template>
  <div class="space-y-4" data-testid="grok-authorization-panel">
    <label class="input-label" for="grok-auth-method">{{ t('admin.accounts.inputMethod') }}</label>
    <select id="grok-auth-method" v-model="method" class="input" :disabled="loading || busy">
      <option value="manual">{{ t('admin.accounts.oauth.grok.generateAuthUrl') }}</option>
      <option value="refresh_token">{{ t('admin.accounts.oauth.grok.refreshTokenAuth') }}</option>
      <option value="sso">{{ t('admin.accounts.oauth.grok.ssoCookieAuth') }}</option>
      <option v-if="passwordEnabled" value="password">{{ t('admin.accounts.oauth.grok.emailPasswordAuth') }}</option>
    </select>
    <template v-if="method === 'manual'">
      <button type="button" class="btn btn-secondary" :disabled="loading || busy" @click="oauth.generateAuthUrl(proxyId)">
        {{ t('admin.accounts.oauth.grok.generateAuthUrl') }}
      </button>
      <a v-if="authUrl" :href="authUrl" target="_blank" rel="noopener noreferrer" class="block break-all text-primary-600">{{ authUrl }}</a>
      <p class="input-hint">{{ t('admin.accounts.oauth.grok.importantNotice') }}</p>
      <textarea v-model="input" class="input" rows="3" :disabled="loading || busy" :placeholder="t('admin.accounts.oauth.grok.authCodePlaceholder')" />
    </template>
    <template v-else>
      <label class="input-label" for="grok-auth-input">{{ inputLabel }}</label>
      <input id="grok-auth-input" v-model="input" type="password" autocomplete="off" class="input" :disabled="loading || busy" />
      <p v-if="method === 'password'" class="input-hint">{{ t('admin.accounts.oauth.grok.emailPasswordDesc') }}</p>
    </template>
    <p v-if="error" class="whitespace-pre-wrap text-sm text-red-600" role="alert">{{ error }}</p>
    <button type="button" class="btn btn-primary" :disabled="!input.trim() || loading || busy || (method === 'manual' && !sessionId)" @click="authorize">
      {{ loading || busy ? t('admin.accounts.oauth.verifying') : t('admin.accounts.oauth.completeAuth') }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { GrokTokenInfo } from '@/api/admin/grok'
import { useGrokOAuth } from '@/composables/useGrokOAuth'

const props = defineProps<{ proxyId?: number | null; busy?: boolean }>()
const emit = defineEmits<{ authorized: [tokenInfo: GrokTokenInfo] }>()
const { t } = useI18n()
const oauth = useGrokOAuth()
const { authUrl, sessionId, loading, error } = oauth
const method = ref<'manual' | 'refresh_token' | 'sso' | 'password'>('manual')
const input = ref('')
const passwordEnabled = ref(false)
const inputLabel = computed(() => t(`admin.accounts.oauth.grok.${method.value === 'sso' ? 'ssoCookieLabel' : method.value === 'password' ? 'emailPasswordInputLabel' : 'refreshTokenAuth'}`))

onMounted(async () => {
  try { passwordEnabled.value = (await adminAPI.grok.getCapabilities()).password_auth_enabled } catch { /* Optional server capability. */ }
})
watch(method, () => { input.value = ''; oauth.resetState() })
watch(() => props.proxyId, () => { input.value = ''; oauth.resetState() })

async function authorize() {
  if (loading.value || props.busy) return
  let tokenInfo: GrokTokenInfo | null = null
  if (method.value === 'manual') {
    const raw = input.value.trim()
    let code = raw
    let state = oauth.state.value
    if (raw.includes('code=')) {
      const params = new URLSearchParams(raw.includes('?') ? raw.slice(raw.indexOf('?') + 1).split('#')[0] : raw.replace(/^\?/, ''))
      code = params.get('code') || ''
      state = params.get('state') || state
    }
    tokenInfo = await oauth.exchangeAuthCode({ code, state, sessionId: sessionId.value, proxyId: props.proxyId })
  } else if (method.value === 'refresh_token') {
    tokenInfo = await oauth.validateRefreshToken(input.value, props.proxyId)
  } else if (method.value === 'sso') {
    tokenInfo = await oauth.validateSSOToken(input.value, props.proxyId)
  } else if (passwordEnabled.value) {
    tokenInfo = await oauth.authorizePassword(input.value, props.proxyId)
  }
  if (tokenInfo) {
    input.value = ''
    emit('authorized', tokenInfo)
  }
}
</script>
