/**
 * [INPUT]: Mocked apiClient responses and the keys/activation API modules.
 * [OUTPUT]: Regression coverage for compatible key creation and activation endpoint contracts.
 * [POS]: Focused frontend API contract tests for the verified-user activation workbench.
 *
 * [PROTOCOL]:
 * 1. Keep request payload and header assertions aligned with the authenticated backend routes.
 * 2. Update this header and the containing folder documentation when this contract changes.
 */

import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post
  }
}))

import { claimActivationRecall, getUserActivationStatus } from '@/api/activation'
import { keysAPI } from '@/api/keys'

describe('activation workbench APIs', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('preserves the existing keysAPI.create payload and call shape when no options are passed', async () => {
    await keysAPI.create(
      'legacy-key',
      7,
      'custom-key-value',
      ['203.0.113.10'],
      ['198.51.100.0/24'],
      12,
      30,
      {
        rate_limit_5h: 1,
        rate_limit_1d: 2,
        rate_limit_7d: 3
      },
      [7, 8]
    )

    expect(post).toHaveBeenCalledWith('/keys', {
      name: 'legacy-key',
      group_id: 7,
      group_ids: [7, 8],
      custom_key: 'custom-key-value',
      ip_whitelist: ['203.0.113.10'],
      ip_blacklist: ['198.51.100.0/24'],
      quota: 12,
      expires_in_days: 30,
      rate_limit_5h: 1,
      rate_limit_1d: 2,
      rate_limit_7d: 3
    })
  })

  it('adds Idempotency-Key only when the compatible final create option is provided', async () => {
    await keysAPI.create(
      'Omni Trial',
      31,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      { idempotencyKey: 'key-create-attempt-1' }
    )

    expect(post).toHaveBeenCalledWith(
      '/keys',
      {
        name: 'Omni Trial',
        group_id: 31
      },
      {
        headers: {
          'Idempotency-Key': 'key-create-attempt-1'
        }
      }
    )
  })

  it('loads authenticated activation status from the user endpoint', async () => {
    const status = {
      enabled: true,
      segment: 'REGISTERED_NO_ATTEMPT',
      starter: { state: 'granted' },
      recall: { state: 'locked', claimable: false },
      next_action: 'use_trial'
    }
    get.mockResolvedValueOnce({ data: status })

    await expect(getUserActivationStatus()).resolves.toEqual(status)
    expect(get).toHaveBeenCalledWith('/user/activation')
  })

  it('claims recall with the required Idempotency-Key header and an explicit empty body', async () => {
    const claimed = {
      enabled: true,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      starter: { state: 'expired' },
      recall: { state: 'claimed', claimable: false },
      active_group: {
        group_id: 44,
        subscription_id: 91,
        starts_at: '2026-07-30T08:00:00Z',
        expires_at: '2026-07-31T08:00:00Z'
      },
      next_action: 'use_trial'
    }
    post.mockResolvedValueOnce({ data: claimed })

    await expect(claimActivationRecall('recall-attempt-1')).resolves.toEqual(claimed)
    expect(post).toHaveBeenCalledWith(
      '/user/activation/recall/claim',
      {},
      {
        headers: {
          'Idempotency-Key': 'recall-attempt-1'
        }
      }
    )
  })

  it('rejects an empty recall idempotency key before making a request', async () => {
    await expect(claimActivationRecall('   ')).rejects.toThrow('Idempotency-Key is required')
    expect(post).not.toHaveBeenCalled()
  })
})
