import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAnnouncementStore } from '@/stores/announcements'
import type { UserAnnouncement } from '@/types'

const mockList = vi.fn()
const mockMarkRead = vi.fn()

vi.mock('@/api', () => ({
  announcementsAPI: {
    list: (...args: unknown[]) => mockList(...args),
    markRead: (...args: unknown[]) => mockMarkRead(...args)
  }
}))

function announcement(overrides: Partial<UserAnnouncement> = {}): UserAnnouncement {
  return {
    id: 1,
    title: '网站迁移公告',
    content: '公告内容',
    notify_mode: 'popup',
    created_at: '2026-07-26T00:00:00Z',
    updated_at: '2026-07-26T00:00:00Z',
    ...overrides
  }
}

describe('useAnnouncementStore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.clearAllMocks()
    mockMarkRead.mockResolvedValue({ message: 'ok' })
  })

  it('shows a read popup_every_visit announcement once per visit', async () => {
    mockList.mockResolvedValue([
      announcement({
        notify_mode: 'popup_every_visit' as UserAnnouncement['notify_mode'],
        read_at: '2026-07-26T01:00:00Z'
      })
    ])
    const store = useAnnouncementStore()

    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)

    await store.dismissPopup()
    await store.fetchAnnouncements(true)
    expect(store.currentPopup).toBeNull()

    store.reset()
    await store.fetchAnnouncements()
    expect(store.currentPopup?.id).toBe(1)
  })

  it('does not show an ordinary popup after it has been read', async () => {
    mockList.mockResolvedValue([
      announcement({ read_at: '2026-07-26T01:00:00Z' })
    ])
    const store = useAnnouncementStore()

    await store.fetchAnnouncements()

    expect(store.currentPopup).toBeNull()
  })
})
