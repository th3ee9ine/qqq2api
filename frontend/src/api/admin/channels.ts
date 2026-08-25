/**
 * Shared group-pricing DTOs.
 *
 * Channel management endpoints were retired. These types and the read-only
 * default-price lookup remain because group pricing uses the same schema.
 */

import { apiClient } from '../client'
import type { BillingMode } from '@/constants/channel'

export type { BillingMode } from '@/constants/channel'

export interface PricingInterval {
  id?: number
  min_tokens: number
  max_tokens: number | null
  tier_label: string
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  input_multiplier: number | null
  output_multiplier: number | null
  cache_write_multiplier: number | null
  cache_read_multiplier: number | null
  per_request_price: number | null
  sort_order: number
}

export interface ChannelTimePricingPeriod {
  start_time: string
  end_time: string
  multiplier: number
}

export interface ChannelTimePricing {
  timezone: string
  weekdays_only?: boolean
  periods: ChannelTimePricingPeriod[]
}

export interface ChannelModelPricing {
  id?: number
  platform: string
  models: string[]
  billing_mode: BillingMode
  input_price: number | null
  output_price: number | null
  cache_write_price: number | null
  cache_read_price: number | null
  fast_multiplier?: number | null
  flex_multiplier?: number | null
  image_input_price: number | null
  image_output_price: number | null
  per_request_price: number | null
  intervals: PricingInterval[]
  time_pricing: ChannelTimePricing | null
}

export type ModelDefaultPricingPlatform = 'anthropic' | 'openai'

export interface ModelDefaultPricing {
  found: boolean
  input_price?: number
  output_price?: number
  cache_write_price?: number
  cache_read_price?: number
  image_input_price?: number
  image_output_price?: number
}

/** Read-only default pricing lookup used by the group pricing editor. */
export async function getModelDefaultPricing(
  model: string,
  platform: ModelDefaultPricingPlatform
): Promise<ModelDefaultPricing> {
  const { data } = await apiClient.get<ModelDefaultPricing>('/admin/channels/model-pricing', {
    params: { model, platform }
  })
  return data
}

const channelsAPI = { getModelDefaultPricing }
export default channelsAPI
