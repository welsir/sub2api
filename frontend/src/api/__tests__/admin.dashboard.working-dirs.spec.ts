import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: { get },
}))

import { getWorkingDirSpending } from '@/api/admin/dashboard'

describe('admin working-directory spending API', () => {
  beforeEach(() => {
    get.mockReset()
  })

  it('requests the selected date range and limit', async () => {
    const response = { items: [], total_actual_cost: 0 }
    get.mockResolvedValue({ data: response })

    await expect(getWorkingDirSpending({
      start_date: '2026-07-01',
      end_date: '2026-07-07',
      limit: 50,
    })).resolves.toEqual(response)

    expect(get).toHaveBeenCalledWith('/admin/dashboard/working-dirs', {
      params: {
        start_date: '2026-07-01',
        end_date: '2026-07-07',
        limit: 50,
      },
    })
  })
})
