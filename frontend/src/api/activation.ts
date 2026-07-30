/**
 * [INPUT]: Public activation offers and authenticated verified-user activation responses.
 * [OUTPUT]: Typed HVOY offer, status, and idempotent recall-claim endpoint access.
 * [POS]: Frontend API boundary for partner handoff and the activation workbench.
 *
 * [PROTOCOL]:
 * 1. Keep authenticated status meaning aligned with UserActivationStatus.
 * 2. Update this header and the containing folder documentation when endpoint contracts change.
 */

import { apiClient } from './client'
import type { UserActivationStatus } from '@/types'

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

export async function getUserActivationStatus(): Promise<UserActivationStatus> {
  const response = await apiClient.get<UserActivationStatus>('/user/activation')
  return response.data
}

export async function claimActivationRecall(
  idempotencyKey: string
): Promise<UserActivationStatus> {
  const normalizedKey = idempotencyKey.trim()
  if (!normalizedKey) {
    throw new Error('Idempotency-Key is required')
  }

  const response = await apiClient.post<UserActivationStatus>(
    '/user/activation/recall/claim',
    {},
    {
      headers: {
        'Idempotency-Key': normalizedKey
      }
    }
  )
  return response.data
}

export const activationAPI = {
  getHvoyActivationOffer,
  getStatus: getUserActivationStatus,
  claimRecall: claimActivationRecall
}

export default activationAPI
