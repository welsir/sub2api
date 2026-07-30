/**
 * [INPUT]: HvoyPartnerView, the public HVOY offer API, router records, and landing-page i18n messages.
 * [OUTPUT]: Regression coverage for offer disclosure, fallback CTAs, homepage routing, and responsive layout intent.
 * [POS]: Public-homepage acceptance test for the new-user activation funnel.
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
  hasPendingAuthSession: false,
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

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: () => 'Omni API',
}))

vi.stubGlobal('scrollTo', vi.fn())

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
    authStore.checkAuth.mockReset()
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    authStore.hasPendingAuthSession = false
    appStore.backendModeEnabled = false
  })

  it('promotes the trial and payment safeguards without exposing internal pricing fields', async () => {
    getHvoyActivationOfferMock.mockResolvedValue(enabledOffer)

    const wrapper = mountPage()
    await flushPromises()

    expect(getHvoyActivationOfferMock).toHaveBeenCalledOnce()
    expect(wrapper.text()).toContain('免费体验，满意再充值')
    expect(wrapper.text()).toContain('注册后体验额度自动到账，创建密钥就能直接用。')
    expect(wrapper.text()).toContain('注册即送免费体验额度')
    expect(wrapper.text()).toContain('完成邮箱验证后自动到账，不用手动领取。')
    expect(wrapper.text()).toContain('支持多种支付渠道')
    expect(wrapper.text()).toContain('使用不满意可按实际使用比例退款')
    expect(wrapper.text()).toContain('正常充值不会过期')
    expect(wrapper.text()).toContain('充值后的额度可以一直使用。')
    expect(wrapper.text()).toContain('不会配置？直接问')
    expect(wrapper.text()).toContain('配置或调用遇到问题，可以加页面上的微信找人排查。')
    expect(wrapper.text()).toContain('微信 omni-support')
    expect(wrapper.text()).not.toContain('$1')
    expect(wrapper.text()).not.toContain('¥1')
    expect(wrapper.text()).not.toContain('0.2x')
    expect(wrapper.text()).not.toContain('最低充值')
    expect(wrapper.text()).not.toContain('计价倍率')
    expect(wrapper.text()).not.toContain('HVOY 合作入口')
    expect(wrapper.text()).not.toContain('Codex / GPT 按量 API 服务')
    expect(wrapper.text()).not.toContain('先用真实调用，验证这项服务是否适合你')
    expect(wrapper.text()).not.toContain('体验有明确时效')
    expect(wrapper.text()).not.toContain('人工支持有边界')
    expect(wrapper.text()).not.toContain('短期体验额度会过期')
    expect(pageSource).not.toContain("t('hvoyPartner.channel')")
    expect(wrapper.get('[data-testid="primary-cta"]').attributes('data-to')).toBe(
      '/register?redirect=/activation',
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
      '/',
      '/login?redirect=/activation',
      '/register?redirect=/activation',
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
    expect(wrapper.get('[data-testid="secondary-cta"]').attributes('data-to')).toBe('/')
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
    expect(wrapper.get('[data-testid="secondary-cta"]').attributes('data-to')).toBe('/')
  })

  it('keeps the layout fluid and leaves the next information band in normal document flow', () => {
    expect(pageSource).not.toMatch(/\b(?:h-screen|min-h-screen|w-screen|w-\[\d+px\])\b/)
    expect(pageSource).toContain('overflow-x-hidden')
    expect(pageSource).toContain('flex-col')
    expect(pageSource).toContain('sm:flex-row')
    expect(pageSource).toContain(
      'gap-3 px-4 pb-2 pt-3 sm:gap-7 sm:px-6 sm:pb-10 sm:pt-11',
    )
    expect(pageSource).toContain('data-testid="next-section"')
  })

  it('registers the activation landing as the public homepage and redirects legacy paths', async () => {
    const { default: router } = await import('@/router')
    const homepage = router.getRoutes().find((record) => record.name === 'Home')
    const legacyHome = router.getRoutes().find((record) => record.path === '/home')
    const legacyHvoy = router.getRoutes().find((record) => record.path === '/partner/hvoy')

    expect(homepage?.path).toBe('/')
    expect(homepage?.meta.requiresAuth).toBe(false)
    expect(legacyHome?.redirect).toBe('/')
    expect(legacyHvoy?.redirect).toBe('/')
  })

  it('keeps the homepage public for unauthenticated visitors in backend mode', async () => {
    const { default: router } = await import('@/router')
    appStore.backendModeEnabled = true

    try {
      await router.push('/')
      await router.isReady()

      expect(router.currentRoute.value.path).toBe('/')
    } finally {
      appStore.backendModeEnabled = false
      await router.replace('/login')
    }
  })
})
