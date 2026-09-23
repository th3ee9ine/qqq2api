import type { Group, VideoModelPrices } from '@/types'

const priceKeys = [
  'video_price_480p', 'video_price_720p', 'video_price_1080p',
  'search_price_per_1k', 'audio_realtime_price_per_min',
  'audio_tts_price_per_million_chars', 'audio_stt_price_per_hour'
] as const
export type GrokMediaPriceKey = typeof priceKeys[number]
export type GrokMediaPricingState = Record<GrokMediaPriceKey, number | string | null> & {
  video_rate_independent: boolean
  video_rate_multiplier: number | string
  video_model_prices: VideoModelPrices
}
export function grokMediaPricingFromGroup(group?: Partial<Group>): GrokMediaPricingState {
  return {
    ...Object.fromEntries(priceKeys.map(key => [key, group?.[key] ?? null])) as Record<GrokMediaPriceKey, number | null>,
    video_rate_independent: group?.video_rate_independent ?? false,
    video_rate_multiplier: group?.video_rate_multiplier ?? 1,
    video_model_prices: Object.fromEntries(Object.entries(group?.video_model_prices ?? {}).map(([model, prices]) => [model, { ...prices }]))
  }
}
export function grokMediaPricingToAPI(platform: string, state: GrokMediaPricingState) {
  if (platform !== 'grok') return {}
  return {
    ...Object.fromEntries(priceKeys.map(key => [key, state[key] === '' || state[key] == null ? null : Number(state[key])])),
    video_rate_independent: state.video_rate_independent,
    video_rate_multiplier: state.video_rate_multiplier === '' ? 1 : Number(state.video_rate_multiplier),
    video_model_prices: state.video_model_prices
  }
}
