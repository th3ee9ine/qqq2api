import { describe, expect, it } from 'vitest'
import { reactive } from 'vue'
import { grokMediaPricingFromGroup, grokMediaPricingToAPI } from '../groupsGrokMedia'

describe('Grok group media prices', () => {
  it('preserves zero prices and per-model overrides while clearing optional empty fields', () => {
    const state = grokMediaPricingFromGroup(reactive({ video_price_720p: 0, video_model_prices: { 'grok-imagine-video-1.5': { '720p': 0.14 } } }))
    state.audio_stt_price_per_hour = ''
    state.video_rate_multiplier = ''
    expect(grokMediaPricingToAPI('grok', state)).toMatchObject({
      video_price_720p: 0, audio_stt_price_per_hour: null, video_rate_multiplier: 1,
      video_model_prices: { 'grok-imagine-video-1.5': { '720p': 0.14 } }
    })
  })
  it('omits Grok-only prices when switching to another platform', () => {
    const state = grokMediaPricingFromGroup({ video_price_720p: 0.07 })
    expect(grokMediaPricingToAPI('openai', state)).toEqual({})
  })
})
