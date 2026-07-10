import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { getDashboardUsersRanking, getGlobalModels } from '@/api/usage'

describe('company usage API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests company-wide models for the selected date range', async () => {
    const response = { models: [], start_date: '2026-07-01', end_date: '2026-07-07' }
    get.mockResolvedValue({ data: response })

    await expect(getGlobalModels({ start_date: '2026-07-01', end_date: '2026-07-07' })).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/usage/dashboard/global-models', {
      params: { start_date: '2026-07-01', end_date: '2026-07-07' },
    })
  })

  it('requests company-wide user ranking with a limit', async () => {
    const response = {
      ranking: [],
      total_actual_cost: 0,
      total_requests: 0,
      total_tokens: 0,
      start_date: '2026-07-01',
      end_date: '2026-07-07',
    }
    get.mockResolvedValue({ data: response })

    await expect(getDashboardUsersRanking({ limit: 20 })).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/usage/dashboard/users-ranking', { params: { limit: 20 } })
  })
})
