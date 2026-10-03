import { shallowMount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import type { PricingFormEntry } from '../types'

vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

function createEntry(billingMode: PricingFormEntry['billing_mode'] = 'token'): PricingFormEntry {
  return {
    models: [],
    billing_mode: billingMode,
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
    time_pricing: {
      timezone: 'Asia/Shanghai',
      periods: [{ start_time: '09:00', end_time: '12:00', multiplier: '2.00' }],
    },
  }
}

describe('PricingEntryCard time pricing visibility', () => {
  it('is hidden by default', () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: createEntry() },
    })

    expect(wrapper.findComponent({ name: 'TimePricingSection' }).exists()).toBe(false)
  })

  it('is shown for token pricing when explicitly enabled', () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), enableTimePricing: true },
    })

    expect(wrapper.findComponent({ name: 'TimePricingSection' }).exists()).toBe(true)
  })

  it('is hidden for non-token pricing even when explicitly enabled', () => {
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry: createEntry('per_request'), enableTimePricing: true },
    })

    expect(wrapper.findComponent({ name: 'TimePricingSection' }).exists()).toBe(false)
  })

  it('clears time periods when changing billing mode', () => {
    const entry = createEntry()
    const wrapper = shallowMount(PricingEntryCard, {
      props: { entry, enableTimePricing: true },
    })

    wrapper.findComponent({ name: 'Select' }).vm.$emit('update:modelValue', 'image')

    expect(wrapper.emitted('update')?.[0]?.[0]).toEqual({
      ...entry,
      billing_mode: 'image',
      intervals: [],
      time_pricing: { timezone: 'Asia/Shanghai', periods: [] },
    })
    expect(entry.time_pricing.periods).toHaveLength(1)
  })
})

describe('PricingEntryCard service tier multipliers', () => {
  it('shows Fast and Flex controls only when explicitly enabled', () => {
    const hidden = shallowMount(PricingEntryCard, { props: { entry: createEntry() } })
    expect(hidden.text()).not.toContain('admin.channels.form.fastMultiplier')

    const shown = shallowMount(PricingEntryCard, {
      props: { entry: createEntry(), enableTierMultipliers: true },
    })
    expect(shown.text()).toContain('admin.channels.form.fastMultiplier')
    expect(shown.text()).toContain('admin.channels.form.flexMultiplier')
  })
})

vi.mock('@/api/admin/channels', () => ({
  default: {
    getModelDefaultPricing: vi.fn(),
  },
}))

describe('PricingEntryCard official price follow', () => {
  it('does not freeze official prices when a model is added', async () => {
    const channelsAPI = (await import('@/api/admin/channels')).default
    const entry = createEntry()
    const wrapper = shallowMount(PricingEntryCard, { props: { entry } })

    wrapper.findComponent({ name: 'ModelTagInput' }).vm.$emit('update:models', ['kimi-k3'])
    await wrapper.vm.$nextTick()

    expect(channelsAPI.getModelDefaultPricing).not.toHaveBeenCalled()
    expect(wrapper.emitted('update')?.[0]?.[0]).toEqual({
      ...entry,
      models: ['kimi-k3'],
    })
  })

  it('fills current official prices only when requested', async () => {
    const channelsAPI = (await import('@/api/admin/channels')).default
    vi.mocked(channelsAPI.getModelDefaultPricing).mockResolvedValue({
      found: true,
      input_price: 3e-6,
      output_price: 15e-6,
      cache_write_price: 0,
      cache_read_price: 0.30e-6,
    })
    const entry = { ...createEntry(), models: ['kimi-k3'] }
    const wrapper = shallowMount(PricingEntryCard, { props: { entry } })

    await wrapper.get('[data-testid="fill-official-prices"]').trigger('click')
    await wrapper.vm.$nextTick()

    expect(channelsAPI.getModelDefaultPricing).toHaveBeenCalledWith('kimi-k3')
    const updated = wrapper.emitted('update')?.at(-1)?.[0] as PricingFormEntry
    expect(updated.input_price).toBe(3)
    expect(updated.output_price).toBe(15)
    expect(updated.cache_read_price).toBe(0.3)
  })

  it('clears overrides so billing can follow the official catalog', async () => {
    const entry = {
      ...createEntry(),
      models: ['kimi-k3'],
      input_price: 20,
      output_price: 100,
      cache_write_price: 2,
      cache_read_price: 2,
    }
    const wrapper = shallowMount(PricingEntryCard, { props: { entry } })

    await wrapper.get('[data-testid="clear-price-overrides"]').trigger('click')

    expect(wrapper.emitted('update')?.[0]?.[0]).toMatchObject({
      models: ['kimi-k3'],
      input_price: null,
      output_price: null,
      cache_write_price: null,
      cache_read_price: null,
      image_input_price: null,
      image_output_price: null,
    })
  })
})
