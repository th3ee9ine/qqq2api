<template>
  <div class="space-y-4 border-t pt-4" data-testid="grok-media-pricing">
    <h4 class="font-medium">{{ t('admin.groups.videoPricing.title') }}</h4>
    <p class="input-hint">{{ t('admin.groups.videoPricing.description') }}</p>
    <label class="flex items-center gap-2 text-sm"><input v-model="modelValue.video_rate_independent" type="checkbox" />{{ t('admin.groups.videoPricing.independentMultiplier') }}</label>
    <label v-if="modelValue.video_rate_independent" class="block input-label">{{ t('admin.groups.videoPricing.videoMultiplier') }}
      <input v-model.number="modelValue.video_rate_multiplier" type="number" min="0" step="0.0001" class="input" />
    </label>
    <div class="grid grid-cols-3 gap-3">
      <label v-for="tier in videoTiers" :key="tier.key" class="input-label">{{ tier.label }} ($/s)
        <input v-model.number="modelValue[tier.key]" type="number" min="0" step="0.001" class="input" :placeholder="tier.price" />
      </label>
    </div>
    <h4 class="font-medium">{{ t('admin.groups.explicitPricing.title') }}</h4>
    <p class="input-hint">{{ t('admin.groups.explicitPricing.description') }}</p>
    <div class="grid grid-cols-2 gap-3">
      <label v-for="field in voiceFields" :key="field.key" class="input-label">{{ t(field.label) }}
        <input v-model.number="modelValue[field.key]" type="number" min="0" step="0.001" class="input" :placeholder="t('admin.groups.explicitPricing.pricePlaceholder')" />
      </label>
    </div>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { GrokMediaPricingState } from '@/views/admin/groupsGrokMedia'
const modelValue = defineModel<GrokMediaPricingState>({ required: true })
const { t } = useI18n()
const videoTiers = [
  { key: 'video_price_480p', label: '480p', price: '0.05' },
  { key: 'video_price_720p', label: '720p', price: '0.07' },
  { key: 'video_price_1080p', label: '1080p', price: '0.25' }
] as const
const voiceFields = [
  { key: 'search_price_per_1k', label: 'admin.groups.explicitPricing.searchPricePer1k' },
  { key: 'audio_realtime_price_per_min', label: 'admin.groups.voicePricing.audioRealtimePerMin' },
  { key: 'audio_tts_price_per_million_chars', label: 'admin.groups.voicePricing.audioTtsPerMillionChars' },
  { key: 'audio_stt_price_per_hour', label: 'admin.groups.voicePricing.audioSttPerHour' }
] as const
</script>
