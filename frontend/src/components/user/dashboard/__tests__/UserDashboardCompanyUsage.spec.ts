import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

import UserDashboardCompanyUsage from '../UserDashboardCompanyUsage.vue'

describe('UserDashboardCompanyUsage', () => {
  it('renders global models and ranking without admin drill-down', () => {
    const wrapper = mount(UserDashboardCompanyUsage, {
      props: {
        loading: false,
        models: [{ model: 'gpt-5.4', requests: 1, total_tokens: 10, cost: 1, actual_cost: 1 } as any],
        rankingResponse: {
          ranking: [{ user_id: 7, email: 'u@example.com', requests: 1, tokens: 10, actual_cost: 1 }],
          total_actual_cost: 1,
          total_requests: 1,
          total_tokens: 10,
          start_date: '2026-07-01',
          end_date: '2026-07-07',
        },
      },
      global: {
        stubs: {
          ModelDistributionChart: {
            name: 'ModelDistributionChart',
            props: [
              'modelStats',
              'rankingItems',
              'enableRankingView',
              'enableBreakdown',
              'showAccountCost',
            ],
            template: '<div data-test="company-chart" />',
          },
        },
      },
    })

    const chart = wrapper.getComponent({ name: 'ModelDistributionChart' })
    expect(chart.props('modelStats')).toHaveLength(1)
    expect(chart.props('rankingItems')).toHaveLength(1)
    expect(chart.props('enableRankingView')).toBe(true)
    expect(chart.props('enableBreakdown')).toBe(false)
    expect(chart.props('showAccountCost')).toBe(false)
  })
})
