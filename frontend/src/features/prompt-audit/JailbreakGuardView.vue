<template>
  <AppLayout>
    <div class="mx-auto max-w-[1200px] space-y-6">
      <header>
        <p class="text-xs font-semibold uppercase tracking-[0.16em] text-primary-600 dark:text-primary-400">{{ t('nav.securityAudit') }}</p>
        <h1 class="mt-1 text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('admin.promptAudit.localGuard.title') }}</h1>
        <p class="mt-2 max-w-3xl text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.localGuard.pageDescription') }}</p>
      </header>

      <LocalJailbreakGuardPanel
        :enabled="guardEnabled"
        :policy-id="policyId"
        :editable="Boolean(serverConfig)"
        :saving="saving"
        @toggle="guardEnabled = !guardEnabled"
      />

      <section v-if="serverConfig" data-test="local-jailbreak-guard-save-bar" class="card flex flex-wrap items-center justify-between gap-4 p-4 sm:p-5">
        <div>
          <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ dirty ? t('admin.promptAudit.saveBar.dirty') : t('admin.promptAudit.saveBar.synced') }}</p>
          <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.localGuard.switchHint') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <button type="button" class="btn btn-secondary" data-test="reset-local-jailbreak-guard" :disabled="!dirty || saving" @click="resetGuard">{{ t('common.reset') }}</button>
          <button type="button" class="btn btn-primary" data-test="save-local-jailbreak-guard" :disabled="!dirty || saving" @click="saveGuard">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </section>

      <p v-if="error" data-test="local-jailbreak-guard-error" role="alert" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/70 dark:bg-red-950/30 dark:text-red-300">{{ error }}</p>

      <section class="card p-5 sm:p-6">
        <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.localGuard.behaviorTitle') }}</h2>
        <ul class="mt-4 grid grid-cols-1 gap-3 text-sm leading-6 text-gray-600 dark:text-dark-300 md:grid-cols-2">
          <li v-for="item in behaviorItems" :key="item" class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-800/70">{{ t(item) }}</li>
        </ul>
        <p class="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-900 dark:border-amber-900/70 dark:bg-amber-950/30 dark:text-amber-200">
          {{ t('admin.promptAudit.localGuard.remoteIndependent') }}
        </p>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LocalJailbreakGuardPanel from './components/LocalJailbreakGuardPanel.vue'
import promptAuditAPI from './api'
import type { PromptAuditDraft } from './types'
import { buildUpdateRequest, cloneData, configToDraft } from './viewModel'

const { t } = useI18n()
const guardEnabled = ref(true)
const policyId = ref('local-jailbreak-v1')
const serverConfig = ref<PromptAuditDraft | null>(null)
const saving = ref(false)
const error = ref('')
const dirty = computed(() => {
  if (!serverConfig.value) return false
  return guardEnabled.value !== (serverConfig.value.local_jailbreak_guard_enabled !== false)
})
const behaviorItems = [
  'admin.promptAudit.localGuard.behaviors.instructionOverride',
  'admin.promptAudit.localGuard.behaviors.roleOverride',
  'admin.promptAudit.localGuard.behaviors.safetyBypass',
  'admin.promptAudit.localGuard.behaviors.promptExfiltration',
]

onMounted(async () => {
  try {
    const config = await promptAuditAPI.getConfig()
    serverConfig.value = configToDraft(config)
    guardEnabled.value = config.local_jailbreak_guard_enabled !== false
    policyId.value = config.local_jailbreak_policy_id || policyId.value
  } catch {
    // The local guard remains enabled even if the optional configuration view
    // is unavailable; the backend is the source of truth for enforcement.
  }
})

function resetGuard() {
  if (!serverConfig.value) return
  guardEnabled.value = serverConfig.value.local_jailbreak_guard_enabled !== false
}

async function saveGuard() {
  if (!serverConfig.value || !dirty.value || saving.value) return
  saving.value = true
  error.value = ''
  try {
    const next = cloneData(serverConfig.value)
    next.local_jailbreak_guard_enabled = guardEnabled.value
    const saved = await promptAuditAPI.updateConfig(buildUpdateRequest(next))
    serverConfig.value = configToDraft(saved)
    guardEnabled.value = saved.local_jailbreak_guard_enabled !== false
    policyId.value = saved.local_jailbreak_policy_id || policyId.value
  } catch {
    error.value = t('admin.promptAudit.localGuard.saveError')
  } finally {
    saving.value = false
  }
}
</script>
