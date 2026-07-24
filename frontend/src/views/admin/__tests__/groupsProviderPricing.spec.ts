import { describe, expect, it } from 'vitest'

import {
  buildProviderPricingPayload,
  isValidProviderPricingGroupName,
  normalizeProviderPricingModels,
} from '../groupsProviderPricing'

describe('groupsProviderPricing', () => {
  it('normalizes comma, whitespace, and newline separated model ids', () => {
    expect(normalizeProviderPricingModels(' gpt-5.6, gpt-5.4\ngpt-5.6  gpt-5.2 ')).toEqual([
      'gpt-5.6',
      'gpt-5.4',
      'gpt-5.2',
    ])
  })

  it('accepts only stable gptNN external group names', () => {
    expect(isValidProviderPricingGroupName('gpt01')).toBe(true)
    expect(isValidProviderPricingGroupName('gpt123')).toBe(true)
    expect(isValidProviderPricingGroupName('gpt1')).toBe(false)
    expect(isValidProviderPricingGroupName('pro号池')).toBe(false)
  })

  it('builds the dynamic admin payload', () => {
    expect(buildProviderPricingPayload({
      enabled: true,
      groupName: ' gpt01 ',
      modelsText: 'gpt-5.6\ngpt-5.4',
    })).toEqual({
      provider_pricing_enabled: true,
      provider_pricing_group_name: 'gpt01',
      provider_pricing_models: ['gpt-5.6', 'gpt-5.4'],
    })
  })
})
