/**
 * [INPUT]: Mocked authenticated activation APIs and explicit activation status fixtures.
 * [OUTPUT]: Pinia behavior coverage for status refresh, claim gating, and stable retry identity.
 * [POS]: Server-state ownership tests for the verified-user activation lifecycle.
 *
 * [PROTOCOL]:
 * 1. Keep fixtures complete for the documented UserActivationStatus contract.
 * 2. Update this header and the containing folder documentation when store behavior changes.
 */

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import type { UserActivationStatus } from '@/types'

const { getUserActivationStatus, claimActivationRecall } = vi.hoisted(() => ({
  getUserActivationStatus: vi.fn(),
  claimActivationRecall: vi.fn()
}))

vi.mock('@/api/activation', () => ({
  default: {
    getStatus: getUserActivationStatus,
    claimRecall: claimActivationRecall
  }
}))

import { useActivationStore } from '@/stores/activation'

const claimableStatus: UserActivationStatus = {
  enabled: true,
  segment: 'ATTEMPTED_ZERO_SUCCESS',
  first_success_at: undefined,
  starter: {
    state: 'expired',
    subscription_id: 70,
    granted_at: '2026-07-28T08:00:00Z',
    expires_at: '2026-07-29T08:00:00Z'
  },
  recall: {
    state: 'claimable',
    subscription_id: undefined,
    claimed_at: undefined,
    expires_at: undefined,
    claimable: true
  },
  active_group: undefined,
  support_wechat: 'welsir02',
  next_action: 'claim_recall'
}

const refreshedClaimedStatus: UserActivationStatus = {
  enabled: true,
  segment: 'ATTEMPTED_ZERO_SUCCESS',
  first_success_at: undefined,
  starter: {
    state: 'expired',
    subscription_id: 70,
    granted_at: '2026-07-28T08:00:00Z',
    expires_at: '2026-07-29T08:00:00Z'
  },
  recall: {
    state: 'claimed',
    subscription_id: 91,
    claimed_at: '2026-07-30T08:00:00Z',
    expires_at: '2026-07-31T08:00:00Z',
    claimable: false
  },
  active_group: {
    group_id: 44,
    subscription_id: 91,
    starts_at: '2026-07-30T08:00:00Z',
    expires_at: '2026-07-31T08:00:00Z'
  },
  support_wechat: 'welsir02',
  next_action: 'use_trial'
}

describe('useActivationStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    getUserActivationStatus.mockReset()
    claimActivationRecall.mockReset()
  })

  it('loads and force-refreshes the explicit server status', async () => {
    getUserActivationStatus
      .mockResolvedValueOnce(claimableStatus)
      .mockResolvedValueOnce(refreshedClaimedStatus)
    const store = useActivationStore()

    await expect(store.loadStatus()).resolves.toEqual(claimableStatus)
    expect(store.status).toEqual(claimableStatus)

    await expect(store.refreshStatus()).resolves.toEqual(refreshedClaimedStatus)
    expect(store.status).toEqual(refreshedClaimedStatus)
    expect(getUserActivationStatus).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['paid users', 'PAID_ZERO_SUCCESS', true],
    ['successful users', 'SUCCESS', true],
    ['non-claimable users', 'ATTEMPTED_ZERO_SUCCESS', false]
  ] as const)('does not expose a recall mutation for %s', async (_label, segment, claimable) => {
    getUserActivationStatus.mockResolvedValueOnce({
      ...claimableStatus,
      segment,
      recall: {
        ...claimableStatus.recall,
        claimable
      }
    })
    const store = useActivationStore()
    await store.loadStatus()

    await expect(store.claimRecall()).rejects.toThrow('Recall is not claimable')
    expect(claimActivationRecall).not.toHaveBeenCalled()
  })

  it('reuses one non-empty claim key after failure and refreshes active group after success', async () => {
    getUserActivationStatus
      .mockResolvedValueOnce(claimableStatus)
      .mockResolvedValueOnce(refreshedClaimedStatus)
    claimActivationRecall
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce({
        ...refreshedClaimedStatus,
        active_group: undefined
      })
    const store = useActivationStore()
    await store.loadStatus()

    await expect(store.claimRecall()).rejects.toThrow('network unavailable')
    expect(store.claimError).toBe('network unavailable')
    expect(store.statusError).toBeNull()
    await expect(store.claimRecall()).resolves.toEqual(refreshedClaimedStatus)

    expect(claimActivationRecall).toHaveBeenCalledTimes(2)
    const firstKey = claimActivationRecall.mock.calls[0][0]
    const retryKey = claimActivationRecall.mock.calls[1][0]
    expect(firstKey).toEqual(expect.any(String))
    expect(firstKey).not.toBe('')
    expect(retryKey).toBe(firstKey)
    expect(getUserActivationStatus).toHaveBeenCalledTimes(2)
    expect(store.status?.active_group).toEqual(refreshedClaimedStatus.active_group)
    expect(store.claimError).toBeNull()
    expect(store.claiming).toBe(false)
  })

  it('keeps a successful claim when the follow-up refresh fails and resets claim identity', async () => {
    const claimedWithoutRefresh: UserActivationStatus = {
      ...refreshedClaimedStatus,
      active_group: undefined
    }
    getUserActivationStatus
      .mockResolvedValueOnce(claimableStatus)
      .mockRejectedValueOnce(new Error('refresh unavailable'))
    claimActivationRecall
      .mockResolvedValueOnce(claimedWithoutRefresh)
      .mockRejectedValueOnce(new Error('second claim probe'))
    const store = useActivationStore()
    await store.loadStatus()

    await expect(store.claimRecall()).resolves.toEqual(claimedWithoutRefresh)
    expect(store.status).toEqual(claimedWithoutRefresh)
    expect(store.statusError).toBe('refresh unavailable')
    expect(store.claimError).toBeNull()

    const successfulClaimKey = claimActivationRecall.mock.calls[0][0]
    store.status = claimableStatus
    await expect(store.claimRecall()).rejects.toThrow('second claim probe')
    const nextClaimKey = claimActivationRecall.mock.calls[1][0]
    expect(nextClaimKey).not.toBe(successfulClaimKey)
  })

  it('isolates an in-flight account A status request after reset when account B fails', async () => {
    let resolveAccountA!: (status: UserActivationStatus) => void
    getUserActivationStatus
      .mockImplementationOnce(
        () =>
          new Promise<UserActivationStatus>((resolve) => {
            resolveAccountA = resolve
          })
      )
      .mockRejectedValueOnce(new Error('account B unavailable'))
    const store = useActivationStore()

    const accountARequest = store.loadStatus()
    store.reset()
    const accountBRequest = store.loadStatus()

    await expect(accountBRequest).rejects.toThrow('account B unavailable')
    resolveAccountA(claimableStatus)
    await expect(accountARequest).rejects.toThrow('superseded')

    expect(store.status).toBeNull()
    expect(store.statusError).toBe('account B unavailable')
    expect(store.claimError).toBeNull()
    expect(store.loading).toBe(false)
  })

  it('deduplicates concurrent claim clicks into one in-flight operation', async () => {
    getUserActivationStatus
      .mockResolvedValueOnce(claimableStatus)
      .mockResolvedValueOnce(refreshedClaimedStatus)
    let resolveClaim!: (status: UserActivationStatus) => void
    claimActivationRecall.mockImplementationOnce(
      () => new Promise<UserActivationStatus>((resolve) => {
        resolveClaim = resolve
      })
    )
    const store = useActivationStore()
    await store.loadStatus()

    const first = store.claimRecall()
    const second = store.claimRecall()
    expect(claimActivationRecall).toHaveBeenCalledTimes(1)

    resolveClaim(refreshedClaimedStatus)
    await expect(Promise.all([first, second])).resolves.toEqual([
      refreshedClaimedStatus,
      refreshedClaimedStatus
    ])
    expect(claimActivationRecall).toHaveBeenCalledTimes(1)
  })
})
