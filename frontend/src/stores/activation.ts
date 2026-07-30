/**
 * [INPUT]: Authenticated activation status and recall-claim API operations.
 * [OUTPUT]: Account-isolated activation status, split status/claim errors, reset, and idempotent recall action.
 * [POS]: Server-state owner for the verified-user activation workbench.
 *
 * [PROTOCOL]:
 * 1. Gate claims only with explicit segment and recall.claimable fields.
 * 2. Reject superseded requests without allowing them to mutate a newer account generation.
 * 3. Reuse a failed claim's idempotency key until a claim response succeeds.
 * 4. Update this header and the containing folder documentation when state ownership changes.
 */

import { defineStore } from 'pinia'
import { ref } from 'vue'

import activationAPI from '@/api/activation'
import type { UserActivationStatus } from '@/types'

function createClaimIdempotencyKey(): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') {
    return globalThis.crypto.randomUUID()
  }

  return `activation-recall-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  if (error && typeof error === 'object' && 'message' in error) {
    return String(error.message)
  }
  return 'Activation request failed'
}

function supersededRequestError(): Error {
  return new Error('Activation request superseded')
}

export const useActivationStore = defineStore('activation', () => {
  const status = ref<UserActivationStatus | null>(null)
  const loading = ref(false)
  const claiming = ref(false)
  const statusError = ref<string | null>(null)
  const claimError = ref<string | null>(null)

  let generation = 0
  let claimIdempotencyKey: string | null = null
  let claimPromise: Promise<UserActivationStatus> | null = null

  async function loadStatus(): Promise<UserActivationStatus> {
    const requestGeneration = generation
    loading.value = true
    statusError.value = null
    try {
      const nextStatus = await activationAPI.getStatus()
      if (requestGeneration !== generation) {
        throw supersededRequestError()
      }
      status.value = nextStatus
      return nextStatus
    } catch (requestError) {
      if (requestGeneration !== generation) {
        throw supersededRequestError()
      }
      statusError.value = errorMessage(requestError)
      throw requestError
    } finally {
      if (requestGeneration === generation) {
        loading.value = false
      }
    }
  }

  function refreshStatus(): Promise<UserActivationStatus> {
    return loadStatus()
  }

  function claimRecall(): Promise<UserActivationStatus> {
    if (claimPromise) {
      return claimPromise
    }

    const currentStatus = status.value
    if (
      !currentStatus?.enabled ||
      currentStatus.segment === 'PAID_ZERO_SUCCESS' ||
      currentStatus.segment === 'SUCCESS' ||
      !currentStatus.recall.claimable
    ) {
      return Promise.reject(new Error('Recall is not claimable'))
    }

    claimIdempotencyKey ||= createClaimIdempotencyKey()
    const requestKey = claimIdempotencyKey
    const requestGeneration = generation
    claiming.value = true
    claimError.value = null

    const request = (async () => {
      try {
        let claimedStatus: UserActivationStatus
        try {
          claimedStatus = await activationAPI.claimRecall(requestKey)
          if (requestGeneration !== generation) {
            throw supersededRequestError()
          }
        } catch (requestError) {
          if (requestGeneration !== generation) {
            throw supersededRequestError()
          }
          claimError.value = errorMessage(requestError)
          throw requestError
        }

        status.value = claimedStatus
        claimIdempotencyKey = null
        try {
          return await loadStatus()
        } catch {
          if (requestGeneration !== generation) {
            throw supersededRequestError()
          }
          return claimedStatus
        }
      } finally {
        if (requestGeneration === generation) {
          claimPromise = null
          claiming.value = false
        }
      }
    })()

    claimPromise = request
    return request
  }

  function reset(): void {
    generation += 1
    status.value = null
    loading.value = false
    claiming.value = false
    statusError.value = null
    claimError.value = null
    claimIdempotencyKey = null
    claimPromise = null
  }

  return {
    status,
    loading,
    claiming,
    statusError,
    claimError,
    loadStatus,
    refreshStatus,
    claimRecall,
    reset
  }
})
