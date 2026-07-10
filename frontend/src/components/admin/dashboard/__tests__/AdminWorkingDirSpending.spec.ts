import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  getWorkingDirSpending: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    dashboard: {
      getWorkingDirSpending: mocks.getWorkingDirSpending,
    },
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

import AdminWorkingDirSpending from '../AdminWorkingDirSpending.vue'

describe('AdminWorkingDirSpending', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.getWorkingDirSpending.mockResolvedValue({
      items: [
        {
          user_id: 9,
          email: 'u@example.com',
          working_directory: '/work/company',
          actual_cost: 12.5,
          requests: 4,
        },
        {
          user_id: 10,
          email: 'no-cwd@example.com',
          working_directory: '',
          actual_cost: 2,
          requests: 1,
        },
      ],
      total_actual_cost: 14.5,
    })
  })

  it('loads the selected range and renders directory attribution', async () => {
    const wrapper = mount(AdminWorkingDirSpending, {
      props: { startDate: '2026-07-01', endDate: '2026-07-07' },
      global: { stubs: { LoadingSpinner: true } },
    })
    await flushPromises()

    expect(mocks.getWorkingDirSpending).toHaveBeenCalledWith({
      start_date: '2026-07-01',
      end_date: '2026-07-07',
      limit: 100,
    })
    expect(wrapper.text()).toContain('u@example.com')
    expect(wrapper.text()).toContain('/work/company')
    expect(wrapper.text()).toContain('admin.dashboard.workingDirectories.unknown')
    expect(wrapper.text()).toContain('$14.50')
  })
})
