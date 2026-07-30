/**
 * [INPUT]: Explicit activation statuses plus mocked store, key, settings, and router boundaries.
 * [OUTPUT]: DOM and interaction coverage for guided key creation, runnable first-call commands, and every verified-user activation state.
 * [POS]: Page integration tests for ActivationView and its real UseKeyModal handoff.
 *
 * [PROTOCOL]:
 * 1. Assert user-visible behavior and real modal props rather than stub existence.
 * 2. Update this header and the containing folder documentation when page behavior changes.
 */

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { ApiKey, UserActivationStatus } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import UseKeyModal from '@/components/keys/UseKeyModal.vue'
import enDashboard from '@/i18n/locales/en/dashboard'
import zhDashboard from '@/i18n/locales/zh/dashboard'

const {
  activationStore,
  loadStatus,
  refreshStatus,
  claimRecall,
  listKeys,
  createKey,
  getPublicSettings,
  push,
  showError,
  copyToClipboard
} = vi.hoisted(() => {
  const store = {
    status: null as UserActivationStatus | null,
    loading: false,
    claiming: false,
    statusError: null as string | null,
    claimError: null as string | null,
    loadStatus: vi.fn(),
    refreshStatus: vi.fn(),
    claimRecall: vi.fn()
  }
  return {
    activationStore: store,
    loadStatus: store.loadStatus,
    refreshStatus: store.refreshStatus,
    claimRecall: store.claimRecall,
    listKeys: vi.fn(),
    createKey: vi.fn(),
    getPublicSettings: vi.fn(),
    push: vi.fn(),
    showError: vi.fn(),
    copyToClipboard: vi.fn()
  }
})

vi.mock('@/stores/activation', () => ({
  useActivationStore: () => activationStore
}))

vi.mock('@/api/keys', () => ({
  keysAPI: {
    list: listKeys,
    create: createKey
  }
}))

vi.mock('@/api/auth', () => ({
  authAPI: {
    getPublicSettings
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRouter: () => ({ push })
  }
})

const messages: Record<string, string> = {
  'activation.title': 'First successful call',
  'activation.description': 'Configure, send, verify',
  'activation.loading': 'Loading activation status',
  'activation.unavailable': 'Activation unavailable',
  'activation.initialLoadFailed': 'Activation status failed to load',
  'activation.retryLoad': 'Retry status load',
  'activation.starterActive': 'Trial active',
  'activation.recallActive': 'Recall active',
  'activation.expiresAt': 'Trial access expires {time} and then ends automatically',
  'activation.useExistingKey': 'Use existing key',
  'activation.createTrialKey': 'Create trial key',
  'activation.creatingKey': 'Creating key',
  'activation.keyLoadFailed': 'Failed to load trial keys',
  'activation.retryKeyLoad': 'Retry key load',
  'activation.steps.label': 'Activation progress',
  'activation.steps.credit': 'Trial credit',
  'activation.steps.key': 'Create key',
  'activation.steps.call': 'First API call',
  'activation.quickStart.title': 'Run your first request',
  'activation.quickStart.description': 'Follow these three steps. No extra tools are required.',
  'activation.quickStart.macos': 'macOS / Linux',
  'activation.quickStart.windows': 'Windows',
  'activation.quickStart.guide.macosOpen': 'Open Terminal with Command + Space on Mac.',
  'activation.quickStart.guide.windowsOpen': 'Press the Windows key and open PowerShell.',
  'activation.quickStart.guide.paste': 'Copy the command, paste it, and press Enter.',
  'activation.quickStart.guide.success': 'After connection successful appears, refresh the status.',
  'activation.quickStart.copy': 'Copy command',
  'activation.quickStart.copied': 'Copied',
  'activation.quickStart.configure': 'Configure Codex',
  'activation.quickStart.refresh': 'I ran it, refresh status',
  'activation.troubleshooting.title': 'Troubleshoot the call',
  'activation.troubleshooting.baseUrl': 'Base URL',
  'activation.troubleshooting.model': 'Model availability',
  'activation.troubleshooting.modelHelp': 'Copy a model returned by /v1/models.',
  'activation.troubleshooting.key': 'API key',
  'activation.troubleshooting.keyHelp': 'Use the key assigned to the active group.',
  'activation.troubleshooting.request': 'Request format',
  'activation.troubleshooting.requestHelp': 'Check the endpoint and JSON request format.',
  'activation.refresh': 'Refresh call status',
  'activation.refreshing': 'Refreshing',
  'activation.paidSupportTitle': 'Manual configuration support',
  'activation.paidSupportDescription': 'Payment succeeded but the first call still needs setup.',
  'activation.supportWechat': 'WeChat support: {wechat}',
  'activation.copyWechat': 'Copy WeChat',
  'activation.recallTitle': 'One recall trial is available',
  'activation.recallDescription': 'Confirm before claiming this one-time trial.',
  'activation.claimRecall': 'Claim recall trial',
  'activation.claimConfirmTitle': 'Confirm recall claim',
  'activation.claimConfirmDescription': 'Claim the one-time recall trial now?',
  'activation.confirmClaim': 'Confirm claim',
  'activation.cancel': 'Cancel',
  'activation.claiming': 'Claiming',
  'activation.claimFailed': 'Failed to claim recall trial',
  'activation.successTitle': 'First call verified',
  'activation.successDescription': 'Activation prompts are complete.',
  'activation.purchase': 'Recharge',
  'activation.expiredTitle': 'Trial subsidy ended',
  'activation.expiredDescription': 'Continue with troubleshooting and a manual support interview.',
  'activation.noActiveGroup': 'No active trial group',
  'activation.loadFailed': 'Failed to load activation status',
  'activation.keyCreateFailed': 'Failed to create trial key',
  'activation.refreshWarning': 'Latest status refresh failed',
  'activation.retryRefresh': 'Retry status refresh'
}

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        const template = messages[key] ?? key
        return Object.entries(params ?? {}).reduce(
          (result, [name, value]) => result.replace(`{${name}}`, String(value)),
          template
        )
      },
      locale: { value: 'en' }
    })
  }
})

import router from '@/router'
import ActivationView from '../ActivationView.vue'

const activeStarterStatus: UserActivationStatus = {
  enabled: true,
  segment: 'REGISTERED_NO_ATTEMPT',
  first_success_at: undefined,
  starter: {
    state: 'granted',
    subscription_id: 81,
    granted_at: '2026-07-30T08:00:00Z',
    expires_at: '2026-07-31T08:00:00Z'
  },
  recall: {
    state: 'locked',
    subscription_id: undefined,
    claimed_at: undefined,
    expires_at: undefined,
    claimable: false
  },
  active_group: {
    group_id: 31,
    subscription_id: 81,
    starts_at: '2026-07-30T08:00:00Z',
    expires_at: '2026-07-31T08:00:00Z'
  },
  support_wechat: 'welsir02',
  next_action: 'use_trial'
}

const apiKey: ApiKey = {
  id: 15,
  user_id: 9,
  key: 'sk-real-returned-key',
  name: 'Omni Trial',
  group_id: 31,
  group_ids: [31],
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-07-30T08:00:00Z',
  updated_at: '2026-07-30T08:00:00Z',
  current_concurrency: 0,
  group: {
    id: 31,
    name: 'Omni Trial',
    description: 'Verified-user trial group',
    platform: 'openai',
    rate_multiplier: 1,
    is_exclusive: true,
    status: 'active',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    allow_image_generation: false,
    allow_batch_image_generation: false,
    image_rate_independent: false,
    image_rate_multiplier: 1,
    batch_image_discount_multiplier: 1,
    batch_image_hold_multiplier: 1,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    video_rate_independent: false,
    video_rate_multiplier: 1,
    video_price_480p: null,
    video_price_720p: null,
    video_price_1080p: null,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: '2026-07-30T08:00:00Z',
    updated_at: '2026-07-30T08:00:00Z'
  },
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null
}

function setStatus(status: UserActivationStatus) {
  activationStore.status = status
  loadStatus.mockResolvedValue(status)
  refreshStatus.mockResolvedValue(status)
}

function mountView(attachToBody: boolean = false) {
  return mount(ActivationView, {
    ...(attachToBody ? { attachTo: document.body } : {}),
    global: {
      stubs: {
        AppLayout: {
          template: '<main><slot /></main>'
        },
        Icon: true
      }
    }
  })
}

function findRecallDialog(): HTMLElement | undefined {
  return Array.from(document.body.querySelectorAll<HTMLElement>('[role="dialog"]'))
    .filter((dialog) => dialog.textContent?.includes('Confirm recall claim'))
    .at(-1)
}

describe('ActivationView', () => {
  beforeEach(() => {
    vi.useRealTimers()
    activationStore.status = null
    activationStore.loading = false
    activationStore.claiming = false
    activationStore.statusError = null
    activationStore.claimError = null
    loadStatus.mockReset()
    refreshStatus.mockReset()
    claimRecall.mockReset()
    listKeys.mockReset()
    createKey.mockReset()
    getPublicSettings.mockReset()
    push.mockReset()
    showError.mockReset()
    copyToClipboard.mockReset()
    copyToClipboard.mockResolvedValue(true)
    getPublicSettings.mockResolvedValue({
      api_base_url: 'https://api.example.test'
    })
    listKeys.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 1,
      pages: 0
    })
  })

  it('registers /activation as an authenticated user route', () => {
    const route = router.getRoutes().find((candidate) => candidate.path === '/activation')

    expect(route?.name).toBe('Activation')
    expect(route?.meta.requiresAuth).toBe(true)
    expect(route?.meta.requiresAdmin).toBe(false)
  })

  it('shows completed credit and key steps plus a directly runnable first-call command', async () => {
    setStatus(activeStarterStatus)
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Trial active')
    expect(wrapper.text()).toContain('Trial access expires')
    expect(wrapper.text()).not.toContain('hours remaining')
    expect(wrapper.text()).not.toContain('activation.remaining')
    expect(listKeys).toHaveBeenCalledWith(1, 1, {
      group_id: 31,
      status: 'active'
    })
    expect(wrapper.get('[data-testid="activation-progress"]').attributes('aria-label')).toBe(
      'Activation progress'
    )
    expect(wrapper.get('[data-testid="activation-step-credit"]').attributes('data-state')).toBe(
      'complete'
    )
    expect(wrapper.get('[data-testid="activation-step-key"]').attributes('data-state')).toBe(
      'complete'
    )
    expect(wrapper.get('[data-testid="activation-step-call"]').attributes('data-state')).toBe(
      'current'
    )

    const command = wrapper.get('[data-testid="quick-start-command"]').text()
    expect(command).toContain('curl "https://api.example.test/v1/responses"')
    expect(command).toContain('Authorization: Bearer sk-real-returned-key')
    expect(command).toContain('"model":"gpt-5.6"')
    expect(command).toContain('"input":"Reply exactly: connection successful"')
    expect(command).not.toContain('jq')
  })

  it('removes the repeated page intro and names the optional setup action Codex', async () => {
    setStatus(activeStarterStatus)
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.find('main > div > header').exists()).toBe(false)
    expect(wrapper.get('[data-testid="configure-client"]').text()).toContain('Configure Codex')
  })

  it('renders the backend expiry and explains that trial access ends automatically', async () => {
    setStatus({
      ...activeStarterStatus,
      active_group: {
        ...activeStarterStatus.active_group!,
        expires_at: '2026-08-02T10:30:00Z'
      }
    })
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Aug 2, 2026')
    expect(wrapper.text()).toContain('and then ends automatically')
    expect(wrapper.text()).not.toContain('Jul 31, 2026')
    expect(zhDashboard.activation.expiresAt).toBe('体验有效期至 {time}，到期后自动失效')
  })

  it('tells beginners how to open the correct terminal and finish the request', async () => {
    setStatus(activeStarterStatus)
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Open Terminal with Command + Space on Mac.')
    expect(wrapper.text()).toContain('Copy the command, paste it, and press Enter.')
    expect(wrapper.text()).toContain('After connection successful appears, refresh the status.')
    expect(wrapper.text()).not.toContain('Press the Windows key and open PowerShell.')

    await wrapper.get('[data-testid="quick-start-windows"]').trigger('click')

    expect(wrapper.text()).toContain('Press the Windows key and open PowerShell.')
    expect(wrapper.text()).not.toContain('Open Terminal with Command + Space on Mac.')
  })

  it('copies a directly runnable Windows curl command', async () => {
    setStatus(activeStarterStatus)
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="quick-start-windows"]').trigger('click')

    const command = wrapper.get('[data-testid="quick-start-command"]').text()
    expect(command).toContain('curl.exe "https://api.example.test/v1/responses"')
    expect(command).toContain('Authorization: Bearer sk-real-returned-key')
    expect(command).not.toContain('\n')

    await wrapper.get('[data-testid="copy-quick-start-command"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith(command)
  })

  it('keeps client configuration optional and refreshes real activation status after the command', async () => {
    setStatus(activeStarterStatus)
    listKeys.mockResolvedValueOnce({
      items: [apiKey],
      total: 1,
      page: 1,
      page_size: 1,
      pages: 1
    })

    const wrapper = mountView()
    await flushPromises()

    const modal = wrapper.findComponent(UseKeyModal)
    expect(modal.props('show')).toBe(false)

    await wrapper.get('[data-testid="configure-client"]').trigger('click')

    expect(modal.props()).toMatchObject({
      show: true,
      apiKey: 'sk-real-returned-key',
      baseUrl: 'https://api.example.test',
      platform: 'openai'
    })

    await wrapper.get('[data-testid="refresh-after-command"]').trigger('click')
    expect(refreshStatus).toHaveBeenCalledTimes(1)
  })

  it('uses explicit automatic-grant copy for the starter trial in both locales', () => {
    expect(zhDashboard.activation.starterActive).toBe('免费体验额度已自动发放')
    expect(enDashboard.activation.starterActive).toBe('Free trial credit was granted automatically')
  })

  it('does not expose the internal group concept in activation copy', () => {
    const zhCopy = [
      zhDashboard.activation.keySetupDescription,
      zhDashboard.activation.troubleshooting.keyHelp,
      zhDashboard.activation.noActiveGroup
    ].join(' ')
    const enCopy = [
      enDashboard.activation.keySetupDescription,
      enDashboard.activation.troubleshooting.keyHelp,
      enDashboard.activation.noActiveGroup
    ].join(' ')

    expect(zhCopy).not.toContain('分组')
    expect(enCopy.toLowerCase()).not.toContain('group')
  })

  it('never offers recall while a starter trial group is still active', async () => {
    setStatus({
      ...activeStarterStatus,
      recall: {
        state: 'claimable',
        claimable: true
      },
      next_action: 'claim_recall'
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Trial active')
    expect(wrapper.find('[data-testid="claim-recall"]').exists()).toBe(false)
  })

  it('does not offer key creation when active-group key loading fails and recovers existing key on retry', async () => {
    setStatus(activeStarterStatus)
    listKeys
      .mockRejectedValueOnce(new Error('keys unavailable'))
      .mockResolvedValueOnce({
        items: [apiKey],
        total: 1,
        page: 1,
        page_size: 1,
        pages: 1
      })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Failed to load trial keys')
    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(false)

    await wrapper.get('[data-testid="retry-key-load"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="configure-client"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="quick-start-command"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(false)
  })

  it('offers key creation only after a failed key load retries successfully with no key', async () => {
    setStatus(activeStarterStatus)
    listKeys
      .mockRejectedValueOnce(new Error('keys unavailable'))
      .mockResolvedValueOnce({
        items: [],
        total: 0,
        page: 1,
        page_size: 1,
        pages: 0
      })

    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="retry-key-load"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="configure-client"]').exists()).toBe(false)
  })

  it('reuses one create key after failure and reveals the inline first-call command on success', async () => {
    setStatus(activeStarterStatus)
    createKey
      .mockRejectedValueOnce(new Error('connection reset'))
      .mockResolvedValueOnce(apiKey)
    const wrapper = mountView()
    await flushPromises()

    const createButton = wrapper.get('[data-testid="create-trial-key"]')
    await createButton.trigger('click')
    await flushPromises()
    await createButton.trigger('click')
    await flushPromises()

    expect(createKey).toHaveBeenCalledTimes(2)
    for (const call of createKey.mock.calls) {
      expect(call[0]).toBe('Omni Trial')
      expect(call[1]).toBe(31)
      expect(call[9].idempotencyKey).toEqual(expect.any(String))
      expect(call[9].idempotencyKey).not.toBe('')
    }
    expect(createKey.mock.calls[1][9].idempotencyKey).toBe(
      createKey.mock.calls[0][9].idempotencyKey
    )

    expect(wrapper.findComponent(UseKeyModal).props('show')).toBe(false)
    expect(wrapper.get('[data-testid="quick-start-command"]').text()).toContain(
      'Authorization: Bearer sk-real-returned-key'
    )
  })

  it('shows call-format troubleshooting and a real refresh action after a failed attempt', async () => {
    setStatus({
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      starter: {
        ...activeStarterStatus.starter,
        state: 'expired'
      },
      active_group: undefined,
      recall: {
        ...activeStarterStatus.recall,
        state: 'locked'
      },
      next_action: 'troubleshoot'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('https://api.example.test')
    expect(wrapper.text()).toContain('Model availability')
    expect(wrapper.text()).toContain('API key')
    expect(wrapper.text()).toContain('Request format')

    await wrapper.get('[data-testid="refresh-activation"]').trigger('click')
    expect(refreshStatus).toHaveBeenCalledTimes(1)
  })

  it('makes manual WeChat support primary for paid zero-success and hides recall', async () => {
    setStatus({
      ...activeStarterStatus,
      segment: 'PAID_ZERO_SUCCESS',
      active_group: undefined,
      recall: {
        ...activeStarterStatus.recall,
        state: 'claimable',
        claimable: true
      },
      next_action: 'support'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Manual configuration support')
    expect(wrapper.get('[data-testid="support-wechat"]').text()).toContain('welsir02')
    expect(wrapper.find('[data-testid="claim-recall"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(false)
  })

  it('requires explicit confirmation before claiming recall', async () => {
    const claimable: UserActivationStatus = {
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      active_group: undefined,
      recall: {
        state: 'claimable',
        subscription_id: undefined,
        claimed_at: undefined,
        expires_at: undefined,
        claimable: true
      },
      next_action: 'claim_recall'
    }
    setStatus(claimable)
    claimRecall.mockResolvedValue({
      ...claimable,
      recall: {
        ...claimable.recall,
        state: 'claimed',
        claimable: false
      }
    })
    const wrapper = mountView(true)
    await flushPromises()

    await wrapper.get('[data-testid="claim-recall"]').trigger('click')
    expect(claimRecall).not.toHaveBeenCalled()
    expect(document.body.textContent).toContain('Confirm recall claim')

    ;(
      findRecallDialog()!.querySelector(
        '[data-testid="confirm-claim-recall"]'
      ) as HTMLButtonElement
    ).click()
    await flushPromises()
    expect(claimRecall).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('closes recall confirmation and shows a non-blocking refresh warning after a successful claim', async () => {
    const claimable: UserActivationStatus = {
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      active_group: undefined,
      recall: {
        state: 'claimable',
        claimable: true
      },
      next_action: 'claim_recall'
    }
    const claimed: UserActivationStatus = {
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      recall: {
        state: 'claimed',
        subscription_id: 92,
        claimed_at: '2026-07-30T09:00:00Z',
        expires_at: '2026-07-31T09:00:00Z',
        claimable: false
      },
      next_action: 'use_trial'
    }
    setStatus(claimable)
    claimRecall.mockImplementationOnce(async () => {
      activationStore.status = claimed
      activationStore.statusError = 'refresh unavailable'
      return claimed
    })

    const wrapper = mountView(true)
    await flushPromises()
    await wrapper.get('[data-testid="claim-recall"]').trigger('click')
    const recallDialog = wrapper
      .findAllComponents(BaseDialog)
      .find((dialog) => dialog.props('title') === 'Confirm recall claim')
    ;(
      findRecallDialog()!.querySelector(
        '[data-testid="confirm-claim-recall"]'
      ) as HTMLButtonElement
    ).click()
    await flushPromises()

    expect(recallDialog?.props('show')).toBe(false)
    expect(wrapper.get('[data-testid="activation-refresh-warning"]').text()).toContain(
      'Latest status refresh failed'
    )
    expect(showError).not.toHaveBeenCalledWith('Failed to claim recall trial')
    wrapper.unmount()
  })

  it('keeps claim failure separate from status refresh warnings', async () => {
    const claimable: UserActivationStatus = {
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      active_group: undefined,
      recall: {
        state: 'claimable',
        claimable: true
      },
      next_action: 'claim_recall'
    }
    setStatus(claimable)
    claimRecall.mockImplementationOnce(async () => {
      activationStore.claimError = 'claim unavailable'
      throw new Error('claim unavailable')
    })

    const wrapper = mountView(true)
    await flushPromises()
    await wrapper.get('[data-testid="claim-recall"]').trigger('click')
    ;(
      findRecallDialog()!.querySelector(
        '[data-testid="confirm-claim-recall"]'
      ) as HTMLButtonElement
    ).click()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('Failed to claim recall trial')
    expect(wrapper.find('[data-testid="activation-refresh-warning"]').exists()).toBe(false)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    wrapper.unmount()
  })

  it('uses the accessible dialog focus and Escape lifecycle for recall confirmation', async () => {
    const claimable: UserActivationStatus = {
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      active_group: undefined,
      recall: {
        state: 'claimable',
        claimable: true
      },
      next_action: 'claim_recall'
    }
    setStatus(claimable)
    const wrapper = mountView(true)
    await flushPromises()

    const trigger = wrapper.get('[data-testid="claim-recall"]').element as HTMLButtonElement
    trigger.focus()
    await wrapper.get('[data-testid="claim-recall"]').trigger('click')
    await flushPromises()

    expect(wrapper.findAllComponents(BaseDialog)).toHaveLength(2)
    const recallDialogComponent = wrapper
      .findAllComponents(BaseDialog)
      .find((candidate) => candidate.props('title') === 'Confirm recall claim')
    const dialog = findRecallDialog()
    expect(dialog).toBeTruthy()
    const titleId = dialog!.getAttribute('aria-labelledby')
    expect(titleId).toBeTruthy()
    expect(dialog!.querySelector(`#${titleId}`)?.textContent).toContain('Confirm recall claim')
    expect(document.activeElement).toBe(
      dialog!.querySelector('[data-testid="cancel-claim-recall"]')
    )

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(recallDialogComponent?.props('show')).toBe(false)
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('distinguishes initial status failure from an explicitly disabled activation feature', async () => {
    loadStatus.mockImplementationOnce(async () => {
      activationStore.statusError = 'status unavailable'
      throw new Error('status unavailable')
    })

    const failedWrapper = mountView()
    await flushPromises()

    expect(failedWrapper.text()).toContain('Activation status failed to load')
    expect(failedWrapper.find('[data-testid="retry-initial-status"]').exists()).toBe(true)
    expect(failedWrapper.text()).not.toContain('Activation unavailable')
    failedWrapper.unmount()

    setStatus({ enabled: false })
    const disabledWrapper = mountView()
    await flushPromises()

    expect(disabledWrapper.text()).toContain('Activation unavailable')
    expect(disabledWrapper.find('[data-testid="retry-initial-status"]').exists()).toBe(false)
  })

  it('shows a non-blocking refresh warning when explicit status remains available', async () => {
    setStatus(activeStarterStatus)
    loadStatus.mockImplementationOnce(async () => {
      activationStore.statusError = 'refresh unavailable'
      throw new Error('refresh unavailable')
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-testid="activation-refresh-warning"]').text()).toContain(
      'Latest status refresh failed'
    )
    await wrapper.get('[data-testid="retry-activation-refresh"]').trigger('click')
    expect(refreshStatus).toHaveBeenCalledTimes(1)
  })

  it('stops activation prompts after success and routes the primary action to purchase', async () => {
    setStatus({
      ...activeStarterStatus,
      segment: 'SUCCESS',
      first_success_at: '2026-07-30T09:00:00Z',
      recall: {
        ...activeStarterStatus.recall,
        state: 'closed_success',
        claimable: false
      },
      next_action: 'purchase'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('First call verified')
    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="claim-recall"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="refresh-activation"]').exists()).toBe(false)

    await wrapper.get('[data-testid="purchase"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/purchase')
  })

  it('offers only troubleshooting and manual support after recall expires without success', async () => {
    setStatus({
      ...activeStarterStatus,
      segment: 'ATTEMPTED_ZERO_SUCCESS',
      starter: {
        ...activeStarterStatus.starter,
        state: 'expired'
      },
      active_group: undefined,
      recall: {
        state: 'expired',
        subscription_id: undefined,
        claimed_at: undefined,
        expires_at: '2026-07-30T08:00:00Z',
        claimable: false
      },
      next_action: 'troubleshoot'
    })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('Trial subsidy ended')
    expect(wrapper.text()).toContain('Troubleshoot the call')
    expect(wrapper.get('[data-testid="support-wechat"]').text()).toContain('welsir02')
    expect(wrapper.find('[data-testid="claim-recall"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="create-trial-key"]').exists()).toBe(false)
  })
})
