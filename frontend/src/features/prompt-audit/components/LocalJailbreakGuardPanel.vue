<template>
  <section data-test="local-jailbreak-guard" class="rounded-xl border p-5" :class="enabled ? 'border-emerald-200 bg-emerald-50/70 dark:border-emerald-900/70 dark:bg-emerald-950/20' : 'border-red-200 bg-red-50/70 dark:border-red-900/70 dark:bg-red-950/20'">
    <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <div class="flex items-center gap-2">
          <span class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-sm font-bold text-white" :class="enabled ? 'bg-emerald-600 dark:bg-emerald-500' : 'bg-red-600'">{{ enabled ? '✓' : '!' }}</span>
          <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.localGuard.title') }}</h2>
        </div>
        <p class="mt-2 max-w-3xl text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('admin.promptAudit.localGuard.description') }}</p>
      </div>
      <span
        data-test="local-jailbreak-guard-status"
        class="inline-flex w-fit shrink-0 items-center rounded-full px-3 py-1 text-xs font-semibold"
        :class="enabled ? 'bg-emerald-600 text-white dark:bg-emerald-500' : 'bg-red-600 text-white'"
      >
        {{ enabled ? t('admin.promptAudit.localGuard.enabled') : t('admin.promptAudit.localGuard.disabled') }}
      </span>
    </div>

    <div v-if="editable" class="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border border-gray-200 bg-white/70 px-4 py-3 dark:border-dark-700 dark:bg-dark-900/40">
      <div>
        <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.promptAudit.localGuard.toggle') }}</p>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.localGuard.switchHint') }}</p>
      </div>
      <button
        type="button"
        role="switch"
        data-test="local-jailbreak-guard-toggle"
        :aria-checked="enabled"
        :aria-label="t('admin.promptAudit.localGuard.toggle')"
        :disabled="saving"
        class="relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2"
        :class="[enabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-600', saving ? 'cursor-not-allowed opacity-60' : 'cursor-pointer']"
        @click="$emit('toggle')"
      >
        <span class="pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow transition-transform duration-200 ease-in-out" :class="enabled ? 'translate-x-5' : 'translate-x-0'" />
      </button>
    </div>

    <div class="mt-5 grid grid-cols-1 gap-3 sm:grid-cols-3">
      <div class="rounded-lg bg-white/80 p-4 dark:bg-dark-800/70">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.localGuard.execution') }}</p>
        <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ enabled ? t('admin.promptAudit.localGuard.alwaysOn') : t('admin.promptAudit.localGuard.disabledByAdmin') }}</p>
      </div>
      <div class="rounded-lg bg-white/80 p-4 dark:bg-dark-800/70">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.localGuard.policy') }}</p>
        <code class="mt-1 block truncate text-sm font-semibold text-gray-900 dark:text-white">{{ policyId }}</code>
      </div>
      <div class="rounded-lg bg-white/80 p-4 dark:bg-dark-800/70">
        <p class="text-xs font-medium text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.localGuard.action') }}</p>
        <p class="mt-1 text-sm font-semibold text-gray-900 dark:text-white">{{ enabled ? t('admin.promptAudit.localGuard.blockBeforeUpstream') : t('admin.promptAudit.localGuard.disabledAction') }}</p>
      </div>
    </div>

    <div class="mt-5 rounded-lg border border-emerald-200/80 bg-white/60 px-4 py-3 dark:border-emerald-900/60 dark:bg-dark-900/30">
      <p class="text-sm font-medium text-gray-800 dark:text-dark-100">{{ t('admin.promptAudit.localGuard.coverageTitle') }}</p>
      <p class="mt-1 text-sm leading-6 text-gray-600 dark:text-dark-300">{{ t('admin.promptAudit.localGuard.coverage') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  enabled: boolean
  policyId: string
  editable?: boolean
  saving?: boolean
}>()

defineEmits<{ toggle: [] }>()

const { t } = useI18n()
</script>
