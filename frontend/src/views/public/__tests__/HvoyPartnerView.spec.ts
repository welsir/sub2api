/**
 * [INPUT]: HvoyPartnerView, the public HVOY offer API, router records, and landing-page i18n messages.
 * [OUTPUT]: Regression coverage for offer disclosure, fallback CTAs, public routing, and responsive layout intent.
 * [POS]: Public-partner handoff acceptance test for the HVOY activation funnel.
 */

import { createI18n } from 'vue-i18n'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import zhLanding from '@/i18n/locales/zh/landing'
import HvoyPartnerView from '@/views/public/HvoyPartnerView.vue'
import pageSource from '@/views/public/HvoyPartnerView.vue?raw'

const { getHvoyActivationOfferMock } = vi.hoisted(() => ({
  getHvoyActivationOfferMock: vi.fn(),
}))

vi.mock('@/api/activation', () => ({
  getHvoyActivationOffer: (...args: unknown[]) => getHvoyActivationOfferMock(...args),
}))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    customMenuItems: [],
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

const enabledOffer = {
  enabled: true,
  starter_credit_usd: 1,
  starter_valid_hours: 24,
  recall_credit_usd: 2,
  recall_valid_hours: 24,
  recall_window_days: 14,
  minimum_recharge_cny: 5,
  recharge_credit_rate: 1,
  pro_rate_multiplier: 0.2,
  support_wechat: 'omni-support',
}

function compileMessages(value: unknown): unknown {
  if (typeof value === 'string') {
    return (context: { named: (key: string) => unknown }) =>
      value.replace(/\{(\w+)\}/g, (_match, key: string) => String(context.named(key)))
  }
  if (Array.isArray(value)) {
    return value.map(compileMessages)
  }
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value).map(([key, nestedValue]) => [key, compileMessages(nestedValue)]),
    )
  }
  return value
}

function mountPage() {
  const i18n = createI18n({
    legacy: false,
    locale: 'zh',
    messages: {
      zh: compileMessages(zhLanding) as typeof zhLanding,
    },
  })

  return mount(HvoyPartnerView, {
    global: {
      plugins: [i18n],
      stubs: {
        Icon: {
          props: ['name'],
          template: '<span :data-icon="name"></span>',
        },
        LocaleSwitcher: true,
        RouterLink: {
          props: ['to'],
          template: '<a data-router-link :data-to="to"><slot /></a>',
        },
      },
    },
  })
}

describe('HvoyPartnerView', () => {
  beforeEach(() => {
    getHvoyActivationOfferMock.mockReset()
  })

  it('shows the live starter offer and exact activation CTAs when the offer is enabled', async () => {
    getHvoyActivationOfferMock.mockResolvedValue(enabledOffer)

    const wrapper = mountPage()
    await flushPromises()

    expect(getHvoyActivationOfferMock).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('$1 / 24h')
    expect(wrapper.text()).toContain('¥1 = $1')
    expect(wrapper.text()).toContain('0.2x')
    expect(wrapper.text()).toContain('最低充值 ¥5')
    expect(wrapper.text()).toContain('付费额度不过期')
    expect(wrapper.text()).toContain('微信 omni-support')
    expect(wrapper.get('[data-testid="primary-cta"]').attributes('data-to')).toBe(
      '/register?source=hvoy_partner&redirect=/activation',
    )
    expect(wrapper.get('[data-testid="secondary-cta"]').attributes('data-to')).toBe(
      '/login?redirect=/activation',
    )
    expect(wrapper.text()).not.toContain('加微信领钱')
  })

  it('delegates every internal destination to RouterLink', async () => {
    getHvoyActivationOfferMock.mockResolvedValue(enabledOffer)

    const wrapper = mountPage()
    await flushPromises()

    expect(
      wrapper.findAll('[data-router-link]').map((link) => link.attributes('data-to')),
    ).toEqual([
      '/home',
      '/login?redirect=/activation',
      '/register?source=hvoy_partner&redirect=/activation',
      '/login?redirect=/activation',
    ])
    expect(wrapper.findAll('a[href^="/"]')).toHaveLength(0)
  })

  it('removes the starter-credit promise and offers service entry when the offer is disabled', async () => {
    getHvoyActivationOfferMock.mockResolvedValue({ enabled: false })

    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).not.toContain('$1 / 24h')
    expect(wrapper.text()).not.toContain('注册送')
    expect(wrapper.text()).not.toContain('免费 $1')
    expect(wrapper.get('[data-testid="primary-cta"]').attributes('data-to')).toBe(
      '/login?redirect=/activation',
    )
    expect(wrapper.get('[data-testid="secondary-cta"]').attributes('data-to')).toBe('/home')
  })

  it('fails closed when the public offer request is rejected', async () => {
    getHvoyActivationOfferMock.mockRejectedValue(new Error('network unavailable'))

    const wrapper = mountPage()
    await flushPromises()

    expect(wrapper.text()).not.toContain('$1 / 24h')
    expect(wrapper.text()).not.toContain('当前可领取')
    expect(wrapper.get('[data-testid="primary-cta"]').attributes('data-to')).toBe(
      '/login?redirect=/activation',
    )
    expect(wrapper.get('[data-testid="secondary-cta"]').attributes('data-to')).toBe('/home')
  })

  it('keeps the layout fluid and leaves the next information band in normal document flow', () => {
    expect(pageSource).not.toMatch(/\b(?:h-screen|min-h-screen|w-screen|w-\[\d+px\])\b/)
    expect(pageSource).toContain('overflow-x-hidden')
    expect(pageSource).toContain('flex-col')
    expect(pageSource).toContain('sm:flex-row')
    expect(pageSource).toContain('data-testid="next-section"')
  })

  it('registers the HVOY handoff as a public route', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'HvoyPartner')

    expect(route?.path).toBe('/partner/hvoy')
    expect(route?.meta.requiresAuth).toBe(false)
  })
})
