import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelTagInput from '../ModelTagInput.vue'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'

const { getModelDefaultPricing } = vi.hoisted(() => ({
  getModelDefaultPricing: vi.fn()
}))

vi.mock('@/api/admin/channels', () => ({
  default: { getModelDefaultPricing }
}))

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))

function createEntry(overrides: Partial<PricingFormEntry> = {}): PricingFormEntry {
  return {
    models: [],
    billing_mode: 'token',
    input_price: null,
    output_price: null,
    cache_write_price: null,
    cache_read_price: null,
    fast_multiplier: null,
    flex_multiplier: null,
    image_input_price: null,
    image_output_price: null,
    per_request_price: null,
    intervals: [],
    time_pricing: { timezone: 'Asia/Shanghai', periods: [] },
    ...overrides
  }
}

describe('PricingEntryCard default pricing', () => {
  beforeEach(() => {
    getModelDefaultPricing.mockReset()
  })

  it.each([
    ['anthropic', 'claude-sonnet-4'],
    ['openai', 'gpt-5.4']
  ] as const)('auto-fills a newly added %s model in $/MTok', async (platform, model) => {
    getModelDefaultPricing.mockResolvedValue({
      found: true,
      input_price: 3e-6,
      output_price: 15e-6,
      cache_write_price: 3.75e-6,
      cache_read_price: 0.3e-6,
      image_input_price: 1e-6,
      image_output_price: 2e-6
    })
    const entry = createEntry()
    const wrapper = shallowMount(PricingEntryCard, { props: { entry, platform } })

    wrapper.findComponent(ModelTagInput).vm.$emit('update:models', [model])
    await flushPromises()

    expect(getModelDefaultPricing).toHaveBeenCalledWith(model, platform)
    expect(wrapper.emitted('update')).toEqual([
      [{ ...entry, models: [model] }],
      [{
        ...entry,
        models: [model],
        input_price: 3,
        output_price: 15,
        cache_write_price: 3.75,
        cache_write_1h_price: null,
        cache_read_price: 0.3,
        image_input_price: 1,
        image_output_price: 2
      }]
    ])
  })

  it('does not overwrite a manually entered price', async () => {
    const entry = createEntry({ input_price: 9 })
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, platform: 'anthropic' }
    })

    wrapper.findComponent(ModelTagInput).vm.$emit('update:models', ['claude-sonnet-4'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(wrapper.emitted('update')).toEqual([
      [{ ...entry, models: ['claude-sonnet-4'] }]
    ])
  })

  it('does not query defaults for a retired platform', async () => {
    const entry = createEntry()
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, platform: 'gemini' }
    })

    wrapper.findComponent(ModelTagInput).vm.$emit('update:models', ['gemini-3-pro'])
    await flushPromises()

    expect(getModelDefaultPricing).not.toHaveBeenCalled()
    expect(wrapper.emitted('update')).toEqual([
      [{ ...entry, models: ['gemini-3-pro'] }]
    ])
  })

  it('keeps the model update when the lookup fails', async () => {
    getModelDefaultPricing.mockRejectedValue(new Error('network error'))
    const entry = createEntry()
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, platform: 'openai' }
    })

    wrapper.findComponent(ModelTagInput).vm.$emit('update:models', ['gpt-5.4'])
    await flushPromises()

    expect(wrapper.emitted('update')).toEqual([
      [{ ...entry, models: ['gpt-5.4'] }]
    ])
  })
})
