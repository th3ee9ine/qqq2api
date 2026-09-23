<template>
  <div class="space-y-1" data-testid="grok-usage-summary">
    <UsageProgressBar v-if="billing?.usage_percent != null" :label="billing.period_type === 'monthly' ? '30d' : '7d'" :utilization="billing.usage_percent" :resets-at="billing.period_end" color="indigo" />
    <UsageProgressBar v-if="billing?.used_percent != null" label="30d" :utilization="billing.used_percent" :resets-at="billing.billing_period_end" color="emerald" />
    <div v-if="billing?.prepaid_balance != null" class="text-xs">{{ t('admin.accounts.usageWindow.grokPrepaid') }} ${{ billing.prepaid_balance.toFixed(2) }}</div>
    <template v-for="window in quotaWindows" :key="window.label">
      <UsageProgressBar v-if="window.quota.limit && window.quota.remaining != null" :label="window.label" :utilization="Math.max(0, Math.min(100, (1 - window.quota.remaining / window.quota.limit) * 100))" :resets-at="window.quota.reset_at || undefined" color="purple" />
    </template>
    <p v-if="!billing && quotaWindows.length === 0" class="max-w-56 text-xs text-gray-400">{{ t('admin.accounts.usageWindow.grokNoHeaders') }}</p>
    <p v-if="usage?.grok_retry_after_seconds" class="text-xs text-amber-600">{{ t('admin.accounts.usageWindow.grokRetryAfter', { time: `${usage.grok_retry_after_seconds}s` }) }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountUsageInfo, GrokQuotaWindow } from '@/types'
import UsageProgressBar from './UsageProgressBar.vue'
const props = defineProps<{ usage?: AccountUsageInfo | null }>()
const { t } = useI18n()
const billing = computed(() => props.usage?.grok_billing)
const quotaWindows = computed(() => [
  { label: t('admin.accounts.usageWindow.grokRequests'), quota: props.usage?.grok_request_quota },
  { label: t('admin.accounts.usageWindow.grokTokens'), quota: props.usage?.grok_token_quota }
].filter((entry): entry is { label: string; quota: GrokQuotaWindow } => entry.quota != null))
</script>
