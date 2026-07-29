/**
 * [INPUT]: Public activation-offer responses from the Sub2API backend.
 * [OUTPUT]: Typed access to the HVOY activation offer without requiring authentication.
 * [POS]: Frontend API boundary for partner activation and trust-handoff pages.
 */

import { apiClient } from './client'

export interface HvoyActivationOffer {
  enabled: boolean
  starter_credit_usd?: number
  starter_valid_hours?: number
  recall_credit_usd?: number
  recall_valid_hours?: number
  recall_window_days?: number
  minimum_recharge_cny?: number
  recharge_credit_rate?: number
  pro_rate_multiplier?: number
  support_wechat?: string
}

export async function getHvoyActivationOffer(): Promise<HvoyActivationOffer> {
  const response = await apiClient.get<HvoyActivationOffer>('/public/activation/hvoy')
  return response.data
}
