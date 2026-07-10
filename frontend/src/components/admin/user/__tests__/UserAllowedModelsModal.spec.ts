import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  update: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      update: mocks.update,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: mocks.showSuccess }),
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

import UserAllowedModelsModal from '../UserAllowedModelsModal.vue'

describe('UserAllowedModelsModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.update.mockResolvedValue({})
  })

  it('loads the existing whitelist, adds a wildcard, and persists it', async () => {
    const wrapper = mount(UserAllowedModelsModal, {
      props: {
        show: false,
        user: {
          id: 9,
          email: 'user@example.com',
          allowed_models: ['gpt-5.4'],
        } as any,
      },
    })

    await wrapper.setProps({ show: true })
    const input = wrapper.get('input')
    await input.setValue('claude-*')
    await input.trigger('keydown.enter')

    const save = wrapper.findAll('button').find((button) => button.text() === 'common.save')
    expect(save).toBeTruthy()
    await save!.trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(9, {
      allowed_models: ['gpt-5.4', 'claude-*'],
    })
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('persists an empty list as unrestricted access', async () => {
    const wrapper = mount(UserAllowedModelsModal, {
      props: {
        show: false,
        user: {
          id: 10,
          email: 'empty@example.com',
          allowed_models: [],
        } as any,
      },
    })

    await wrapper.setProps({ show: true })
    const save = wrapper.findAll('button').find((button) => button.text() === 'common.save')
    await save!.trigger('click')
    await flushPromises()

    expect(mocks.update).toHaveBeenCalledWith(10, { allowed_models: [] })
  })
})
