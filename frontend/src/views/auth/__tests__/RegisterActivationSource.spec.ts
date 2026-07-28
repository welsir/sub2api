import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import RegisterView from '@/views/auth/RegisterView.vue'

const {
  routeQuery,
  pushMock,
  registerMock,
  showSuccessMock,
  showErrorMock,
  showWarningMock,
  getPublicSettingsMock,
} = vi.hoisted(() => ({
  routeQuery: {} as Record<string, string | undefined>,
  pushMock: vi.fn(),
  registerMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showErrorMock: vi.fn(),
  showWarningMock: vi.fn(),
  getPublicSettingsMock: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({
    push: pushMock,
  }),
  useRoute: () => ({
    query: routeQuery,
  }),
}))

vi.mock('vue-i18n', () => ({
  createI18n: () => ({
    global: {
      t: (key: string) => key,
    },
  }),
  useI18n: () => ({
    t: (key: string) => key,
    locale: { value: 'en' },
  }),
}))

vi.mock('@/stores', () => ({
  useAuthStore: () => ({
    register: (...args: unknown[]) => registerMock(...args),
  }),
  useAppStore: () => ({
    showSuccess: (...args: unknown[]) => showSuccessMock(...args),
    showError: (...args: unknown[]) => showErrorMock(...args),
    showWarning: (...args: unknown[]) => showWarningMock(...args),
  }),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: (...args: unknown[]) => getPublicSettingsMock(...args),
  isWeChatWebOAuthEnabled: () => false,
  validatePromoCode: vi.fn(),
  validateInvitationCode: vi.fn(),
}))

function publicSettings(emailVerifyEnabled: boolean) {
  return {
    registration_enabled: true,
    email_verify_enabled: emailVerifyEnabled,
    promo_code_enabled: false,
    invitation_code_enabled: false,
    turnstile_enabled: false,
    turnstile_site_key: '',
    site_name: 'Sub2API',
    linuxdo_oauth_enabled: false,
    wechat_oauth_enabled: false,
    oidc_oauth_enabled: false,
    oidc_oauth_provider_name: 'OIDC',
    github_oauth_enabled: false,
    google_oauth_enabled: false,
    registration_email_suffix_whitelist: [],
    login_agreement_enabled: false,
    login_agreement_documents: [],
  }
}

async function mountAndSubmitRegister() {
  const wrapper = mount(RegisterView, {
    global: {
      stubs: {
        AuthLayout: { template: '<div><slot /><slot name="footer" /></div>' },
        LinuxDoOAuthSection: true,
        OidcOAuthSection: true,
        WechatOAuthSection: true,
        EmailOAuthButtons: true,
        LoginAgreementPrompt: true,
        Icon: true,
        TurnstileWidget: true,
        RouterLink: true,
        transition: false,
      },
    },
  })
  await flushPromises()
  await wrapper.get('#email').setValue('new-user@example.com')
  await wrapper.get('#password').setValue('secret-123')
  await wrapper.get('form').trigger('submit.prevent')
  await flushPromises()
  return wrapper
}

describe('RegisterView activation source', () => {
  beforeEach(() => {
    for (const key of Object.keys(routeQuery)) {
      delete routeQuery[key]
    }
    pushMock.mockReset()
    registerMock.mockReset()
    registerMock.mockResolvedValue({})
    showSuccessMock.mockReset()
    showErrorMock.mockReset()
    showWarningMock.mockReset()
    getPublicSettingsMock.mockReset()
    sessionStorage.clear()
    localStorage.clear()
  })

  it('persists a safe HVOY context for email verification', async () => {
    routeQuery.source = 'hvoy_partner'
    routeQuery.redirect = '/activation'
    getPublicSettingsMock.mockResolvedValue(publicSettings(true))

    await mountAndSubmitRegister()

    expect(JSON.parse(sessionStorage.getItem('register_data') || '{}')).toMatchObject({
      email: 'new-user@example.com',
      campaign_source: 'hvoy_partner',
      redirect: '/activation',
    })
    expect(pushMock).toHaveBeenCalledWith('/email-verify')
    expect(registerMock).not.toHaveBeenCalled()
  })

  it('drops an unknown source and its redirect from email verification context', async () => {
    routeQuery.source = 'unknown_partner'
    routeQuery.redirect = '/activation'
    getPublicSettingsMock.mockResolvedValue(publicSettings(true))

    await mountAndSubmitRegister()

    const stored = JSON.parse(sessionStorage.getItem('register_data') || '{}')
    expect(stored).not.toHaveProperty('campaign_source')
    expect(stored).not.toHaveProperty('redirect')
  })

  it.each([
    'https://evil.example/phish',
    '//evil.example/phish',
    String.raw`\evil.example\phish`,
    '/activation\u0000https://evil.example',
  ])('drops unsafe redirect %s', async (redirect) => {
    routeQuery.source = 'hvoy_partner'
    routeQuery.redirect = redirect
    getPublicSettingsMock.mockResolvedValue(publicSettings(true))

    await mountAndSubmitRegister()

    const stored = JSON.parse(sessionStorage.getItem('register_data') || '{}')
    expect(stored.campaign_source).toBe('hvoy_partner')
    expect(stored).not.toHaveProperty('redirect')
  })

  it('drops HVOY activation context when email verification is disabled', async () => {
    routeQuery.source = 'hvoy_partner'
    routeQuery.redirect = '/activation'
    getPublicSettingsMock.mockResolvedValue(publicSettings(false))

    await mountAndSubmitRegister()

    expect(registerMock.mock.calls[0]?.[0]).not.toHaveProperty('campaign_source')
    expect(pushMock).toHaveBeenCalledWith('/dashboard')
  })

  it.each([
    ['unknown_partner', '/activation'],
    ['hvoy_partner', 'https://evil.example/phish'],
    ['hvoy_partner', '//evil.example/phish'],
  ])('falls back to the dashboard for source=%s redirect=%s', async (source, redirect) => {
    routeQuery.source = source
    routeQuery.redirect = redirect
    getPublicSettingsMock.mockResolvedValue(publicSettings(false))

    await mountAndSubmitRegister()

    if (source === 'unknown_partner') {
      expect(registerMock.mock.calls[0]?.[0]).not.toHaveProperty('campaign_source')
    }
    expect(pushMock).toHaveBeenCalledWith('/dashboard')
  })
})
