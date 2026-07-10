import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { update: mocks.update },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess, showError: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    props: ['show', 'title', 'width'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import UserWeeklyThresholdModal from '../UserWeeklyThresholdModal.vue'

async function mountAndOpen(threshold: number | null) {
  const wrapper = mount(UserWeeklyThresholdModal, {
    props: {
      show: false,
      user: { id: 9, email: 'user@example.com', weekly_cost_threshold: threshold } as any,
    },
  })
  await wrapper.setProps({ show: true })
  return wrapper
}

describe('UserWeeklyThresholdModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.update.mockResolvedValue({})
  })

  it('updates a positive weekly threshold', async () => {
    const wrapper = await mountAndOpen(35)
    const input = wrapper.get('[data-test="weekly-threshold-input"]')
    expect((input.element as HTMLInputElement).value).toBe('35')
    await input.setValue('75')
    await wrapper.get('[data-test="weekly-threshold-save"]').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(9, { weekly_cost_threshold: 75 })
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('uses zero to clear the weekly threshold', async () => {
    const wrapper = await mountAndOpen(35)
    await wrapper.get('[data-test="weekly-threshold-input"]').setValue('0')
    await wrapper.get('[data-test="weekly-threshold-save"]').trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(9, { weekly_cost_threshold: 0 })
  })
})
