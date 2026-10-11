<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/components/common/HelpTooltip.vue'
import { formatDateTime } from '@/utils/format'
import type { OpenAIOAuthTTFTHistory, OpenAIOAuthTTFTRequest } from '@/types'

const props = defineProps<{ history?: OpenAIOAuthTTFTHistory | null }>()
const { t } = useI18n()

// The API orders observations newest first; keep that order even when two
// requests have the same displayed timestamp.
const requests = computed(() => (props.history?.recent_requests ?? []).slice(0, 5))

const seconds = (milliseconds: number) => String(Number((milliseconds / 1000).toFixed(3)))
const elapsedLabel = (request: OpenAIOAuthTTFTRequest) => request.first_token_ms == null
  ? t('admin.accounts.ttftHistory.unmeasured')
  : t('admin.accounts.ttftHistory.seconds', { value: seconds(request.first_token_ms) })
const isSlow = (request: OpenAIOAuthTTFTRequest) => request.first_token_ms != null &&
  request.first_token_ms > (props.history?.threshold_ms ?? Infinity)
</script>

<template>
  <div v-if="history" class="max-w-[220px] text-[11px] leading-4" data-testid="account-ttft-history">
    <div class="mb-0.5 text-gray-500 dark:text-gray-400">{{ t('admin.accounts.ttftHistory.label') }}</div>
    <span v-if="requests.length === 0" class="text-gray-400 dark:text-gray-500" :title="t('admin.accounts.ttftHistory.newestFirst')" data-testid="account-ttft-empty">
      {{ t('admin.accounts.ttftHistory.empty') }}
    </span>
    <HelpTooltip v-else trigger="click" width-class="w-72 max-w-[calc(100vw-2rem)]" class="!ml-0">
      <template #trigger>
        <button
          type="button"
          class="flex flex-wrap gap-1 rounded text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500"
          :aria-label="t('admin.accounts.ttftHistory.details')"
          data-testid="account-ttft-details"
        >
          <span
            v-for="(request, index) in requests"
            :key="index"
            data-testid="account-ttft-sample"
            :data-slow="isSlow(request)"
            class="rounded px-1 py-0.5 font-mono tabular-nums"
            :class="isSlow(request)
              ? 'bg-red-50 font-semibold text-red-600 dark:bg-red-950/40 dark:text-red-400'
              : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
          >{{ elapsedLabel(request) }}</span>
        </button>
      </template>
      <p class="font-medium">{{ t('admin.accounts.ttftHistory.details') }}</p>
      <p class="mt-1 text-gray-300">{{ t('admin.accounts.ttftHistory.newestFirst') }}</p>
      <p class="text-gray-300">{{ t('admin.accounts.ttftHistory.threshold', { value: seconds(history.threshold_ms) }) }}</p>
      <ol class="mt-2 space-y-2">
        <li v-for="(request, index) in requests" :key="index" class="border-t border-white/10 pt-1" data-testid="account-ttft-observation">
          <div class="flex items-center justify-between gap-2">
            <time :datetime="request.observed_at">{{ formatDateTime(request.observed_at) }}</time>
            <span class="shrink-0 font-mono tabular-nums" :class="{ 'font-semibold text-red-300': isSlow(request) }">{{ elapsedLabel(request) }}</span>
          </div>
          <div class="break-all text-gray-300">{{ request.model }}</div>
        </li>
      </ol>
    </HelpTooltip>
  </div>
</template>
