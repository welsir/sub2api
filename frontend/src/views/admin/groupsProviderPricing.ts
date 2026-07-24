export interface ProviderPricingFormState {
  enabled: boolean
  groupName: string
  modelsText: string
}

export const normalizeProviderPricingModels = (value: string): string[] => {
  const seen = new Set<string>()
  return value
    .split(/[\s,]+/)
    .map(model => model.trim())
    .filter(model => {
      if (!model || seen.has(model)) {
        return false
      }
      seen.add(model)
      return true
    })
}

export const isValidProviderPricingGroupName = (value: string): boolean =>
  /^gpt\d{2,}$/.test(value.trim())

export const buildProviderPricingPayload = (state: ProviderPricingFormState) => ({
  provider_pricing_enabled: state.enabled,
  provider_pricing_group_name: state.groupName.trim(),
  provider_pricing_models: normalizeProviderPricingModels(state.modelsText),
})
