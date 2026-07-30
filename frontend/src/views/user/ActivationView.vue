<!--
[INPUT]: Explicit activation status, active-group API keys, public API settings, and router navigation.
[OUTPUT]: Three-step activation guidance, runnable first-call commands, optional client setup, recall confirmation, and support actions.
[POS]: Authenticated workbench between verified registration and the first successful API call.

[PROTOCOL]:
1. Never render a recall claim while an active trial group is available.
2. Render backend-provided activation fields directly; do not infer business segments.
3. Reuse one idempotency key across retries until trial-key creation succeeds.
4. Update this header and the containing folder documentation when page behavior changes.
-->

<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <div v-if="initializing" class="card flex min-h-48 items-center justify-center p-8">
        <span class="text-sm text-gray-600 dark:text-gray-400">{{ t('activation.loading') }}</span>
      </div>

      <div
        v-else-if="initialLoadFailed"
        class="card border-red-200 bg-red-50 p-6 dark:border-red-900/60 dark:bg-red-950/20"
      >
        <p class="font-medium text-red-900 dark:text-red-200">
          {{ t('activation.initialLoadFailed') }}
        </p>
        <button
          data-testid="retry-initial-status"
          type="button"
          class="btn btn-secondary mt-4"
          @click="retryInitialStatus"
        >
          <Icon name="refresh" size="sm" class="mr-2" />
          {{ t('activation.retryLoad') }}
        </button>
      </div>

      <div v-else-if="status?.enabled === false" class="card p-6">
        <p class="font-medium text-gray-900 dark:text-white">{{ t('activation.unavailable') }}</p>
      </div>

      <template v-else-if="enabledStatus">
        <section
          v-if="activationStore.statusError"
          data-testid="activation-refresh-warning"
          class="card border-amber-200 bg-amber-50 p-4 dark:border-amber-900/60 dark:bg-amber-950/20"
        >
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <p class="text-sm text-amber-900 dark:text-amber-200">
              {{ t('activation.refreshWarning') }}
            </p>
            <button
              data-testid="retry-activation-refresh"
              type="button"
              class="btn btn-secondary"
              :disabled="refreshing"
              @click="refreshActivation"
            >
              <Icon name="refresh" size="sm" class="mr-2" />
              {{ t('activation.retryRefresh') }}
            </button>
          </div>
        </section>

        <section
          v-if="isSuccess"
          class="card border-emerald-200 bg-emerald-50 p-6 dark:border-emerald-900/60 dark:bg-emerald-950/20"
        >
          <div class="flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between">
            <div class="flex items-start gap-4">
              <Icon name="checkCircle" size="lg" class="text-emerald-600 dark:text-emerald-400" />
              <div>
                <h2 class="text-lg font-semibold text-emerald-900 dark:text-emerald-200">
                  {{ t('activation.successTitle') }}
                </h2>
                <p class="mt-1 text-sm text-emerald-800 dark:text-emerald-300">
                  {{ t('activation.successDescription') }}
                </p>
              </div>
            </div>
            <button
              data-testid="purchase"
              type="button"
              class="btn btn-primary"
              @click="router.push('/purchase')"
            >
              <Icon name="creditCard" size="sm" class="mr-2" />
              {{ t('activation.purchase') }}
            </button>
          </div>
        </section>

        <template v-else>
          <section
            v-if="hasActiveGroup"
            class="card overflow-hidden border-primary-200 dark:border-primary-900/60"
          >
            <div class="bg-primary-50 p-6 dark:bg-primary-950/20">
              <div class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                    {{
                      enabledStatus.recall.state === 'claimed'
                        ? t('activation.recallActive')
                        : t('activation.starterActive')
                    }}
                  </h2>
                  <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
                    {{ t('activation.expiresAt', { time: formattedExpiry }) }}
                  </p>
                </div>
              </div>
            </div>

            <ol
              data-testid="activation-progress"
              :aria-label="t('activation.steps.label')"
              class="grid grid-cols-1 gap-4 border-t border-primary-100 p-6 dark:border-primary-900/50 sm:grid-cols-3"
            >
              <li
                data-testid="activation-step-credit"
                data-state="complete"
                class="flex min-w-0 items-center gap-3"
              >
                <span class="flex size-9 shrink-0 items-center justify-center rounded-full bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300">
                  <Icon name="check" size="sm" :stroke-width="2" />
                </span>
                <span class="min-w-0 text-sm font-medium text-gray-900 dark:text-white">
                  {{ t('activation.steps.credit') }}
                </span>
              </li>
              <li
                data-testid="activation-step-key"
                :data-state="activeKey ? 'complete' : 'current'"
                class="flex min-w-0 items-center gap-3"
              >
                <span
                  class="flex size-9 shrink-0 items-center justify-center rounded-full"
                  :class="
                    activeKey
                      ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
                      : 'bg-primary-100 text-primary-700 ring-2 ring-primary-500 dark:bg-primary-900/40 dark:text-primary-300'
                  "
                >
                  <Icon v-if="activeKey" name="check" size="sm" :stroke-width="2" />
                  <Icon v-else name="key" size="sm" />
                </span>
                <span class="min-w-0 text-sm font-medium text-gray-900 dark:text-white">
                  {{ t('activation.steps.key') }}
                </span>
              </li>
              <li
                data-testid="activation-step-call"
                :data-state="activeKey ? 'current' : 'pending'"
                class="flex min-w-0 items-center gap-3"
              >
                <span
                  class="flex size-9 shrink-0 items-center justify-center rounded-full"
                  :class="
                    activeKey
                      ? 'bg-primary-100 text-primary-700 ring-2 ring-primary-500 dark:bg-primary-900/40 dark:text-primary-300'
                      : 'bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-gray-500'
                  "
                >
                  <Icon name="terminal" size="sm" />
                </span>
                <span
                  class="min-w-0 text-sm font-medium"
                  :class="
                    activeKey
                      ? 'text-gray-900 dark:text-white'
                      : 'text-gray-500 dark:text-gray-400'
                  "
                >
                  {{ t('activation.steps.call') }}
                </span>
              </li>
            </ol>

            <div
              v-if="keyLoadError"
              class="flex flex-col gap-4 border-t border-red-100 bg-red-50 p-6 dark:border-red-900/50 dark:bg-red-950/20 sm:flex-row sm:items-center sm:justify-between"
            >
              <p class="text-sm text-red-700 dark:text-red-300">
                {{ t('activation.keyLoadFailed') }}
              </p>
              <button
                data-testid="retry-key-load"
                type="button"
                class="btn btn-secondary"
                :disabled="keyLoading"
                @click="retryKeyLoad"
              >
                <Icon name="refresh" size="sm" class="mr-2" />
                {{ t('activation.retryKeyLoad') }}
              </button>
            </div>

            <div
              v-else-if="keyLoading"
              class="border-t border-primary-100 p-6 text-sm text-gray-500 dark:border-primary-900/50 dark:text-gray-400"
            >
              <span>
                {{ t('activation.loadingKeys') }}
              </span>
            </div>

            <div
              v-else-if="!activeKey"
              class="flex flex-col gap-4 border-t border-primary-100 p-6 dark:border-primary-900/50 sm:flex-row sm:items-center sm:justify-between"
            >
              <p class="text-sm text-gray-600 dark:text-gray-400">
                {{ t('activation.keySetupDescription') }}
              </p>
              <button
                data-testid="create-trial-key"
                type="button"
                class="btn btn-primary"
                :disabled="creatingKey"
                @click="createTrialKey"
              >
                <Icon name="key" size="sm" class="mr-2" />
                {{ creatingKey ? t('activation.creatingKey') : t('activation.createTrialKey') }}
              </button>
            </div>

            <div
              v-else
              class="space-y-5 border-t border-primary-100 p-6 dark:border-primary-900/50"
            >
              <div>
                <h3 class="text-base font-semibold text-gray-900 dark:text-white">
                  {{ t('activation.quickStart.title') }}
                </h3>
                <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
                  {{ t('activation.quickStart.description') }}
                </p>
              </div>

              <div
                class="inline-flex max-w-full rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-700 dark:bg-dark-900"
                role="group"
                :aria-label="t('activation.quickStart.shellLabel')"
              >
                <button
                  data-testid="quick-start-macos"
                  type="button"
                  class="min-h-9 rounded-md px-3 text-sm font-medium transition-colors"
                  :class="
                    activeQuickStartShell === 'macos'
                      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                      : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                  "
                  @click="selectQuickStartShell('macos')"
                >
                  {{ t('activation.quickStart.macos') }}
                </button>
                <button
                  data-testid="quick-start-windows"
                  type="button"
                  class="min-h-9 rounded-md px-3 text-sm font-medium transition-colors"
                  :class="
                    activeQuickStartShell === 'windows'
                      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                      : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'
                  "
                  @click="selectQuickStartShell('windows')"
                >
                  {{ t('activation.quickStart.windows') }}
                </button>
              </div>

              <ol class="space-y-3" :aria-label="t('activation.quickStart.guideLabel')">
                <li class="flex items-start gap-3">
                  <span class="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                    1
                  </span>
                  <p class="pt-1 text-sm text-gray-700 dark:text-gray-300">
                    {{
                      t(
                        activeQuickStartShell === 'windows'
                          ? 'activation.quickStart.guide.windowsOpen'
                          : 'activation.quickStart.guide.macosOpen'
                      )
                    }}
                  </p>
                </li>
                <li class="flex items-start gap-3">
                  <span class="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                    2
                  </span>
                  <p class="pt-1 text-sm text-gray-700 dark:text-gray-300">
                    {{ t('activation.quickStart.guide.paste') }}
                  </p>
                </li>
                <li class="flex items-start gap-3">
                  <span class="flex size-7 shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/40 dark:text-primary-300">
                    3
                  </span>
                  <p class="pt-1 text-sm text-gray-700 dark:text-gray-300">
                    {{ t('activation.quickStart.guide.success') }}
                  </p>
                </li>
              </ol>

              <div class="overflow-hidden rounded-lg border border-gray-700 bg-gray-950">
                <div class="flex items-center justify-between gap-3 border-b border-gray-800 px-4 py-2">
                  <span class="truncate font-mono text-xs text-gray-400">
                    {{ activeQuickStartShell === 'windows' ? 'PowerShell' : 'Terminal' }}
                  </span>
                  <button
                    data-testid="copy-quick-start-command"
                    type="button"
                    class="inline-flex min-h-8 shrink-0 items-center gap-1.5 rounded-md px-2.5 text-xs font-medium text-gray-300 transition-colors hover:bg-gray-800 hover:text-white"
                    @click="copyQuickStartCommand"
                  >
                    <Icon :name="quickStartCopied ? 'check' : 'copy'" size="xs" />
                    {{
                      quickStartCopied
                        ? t('activation.quickStart.copied')
                        : t('activation.quickStart.copy')
                    }}
                  </button>
                </div>
                <pre class="max-h-72 overflow-auto p-4 text-sm leading-6 text-gray-100"><code data-testid="quick-start-command" v-text="quickStartCommand"></code></pre>
              </div>

              <div class="flex flex-col gap-3 sm:flex-row sm:items-center">
                <button
                  data-testid="refresh-after-command"
                  type="button"
                  class="btn btn-primary"
                  :disabled="refreshing"
                  @click="refreshActivation"
                >
                  <Icon name="refresh" size="sm" class="mr-2" />
                  {{
                    refreshing
                      ? t('activation.refreshing')
                      : t('activation.quickStart.refresh')
                  }}
                </button>
                <button
                  data-testid="configure-client"
                  type="button"
                  class="btn btn-secondary"
                  @click="openKey(activeKey)"
                >
                  <Icon name="terminal" size="sm" class="mr-2" />
                  {{ t('activation.quickStart.configure') }}
                </button>
              </div>
            </div>
          </section>

          <section
            v-if="isRecallExpired"
            class="card border-amber-200 bg-amber-50 p-6 dark:border-amber-900/60 dark:bg-amber-950/20"
          >
            <h2 class="font-semibold text-amber-900 dark:text-amber-200">
              {{ t('activation.expiredTitle') }}
            </h2>
            <p class="mt-1 text-sm text-amber-800 dark:text-amber-300">
              {{ t('activation.expiredDescription') }}
            </p>
          </section>

          <section
            v-if="isPaidZeroSuccess"
            class="card border-emerald-200 bg-emerald-50 p-6 dark:border-emerald-900/60 dark:bg-emerald-950/20"
          >
            <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 class="text-lg font-semibold text-emerald-900 dark:text-emerald-200">
                  {{ t('activation.paidSupportTitle') }}
                </h2>
                <p class="mt-1 text-sm text-emerald-800 dark:text-emerald-300">
                  {{ t('activation.paidSupportDescription') }}
                </p>
                <p
                  v-if="enabledStatus.support_wechat"
                  data-testid="support-wechat"
                  class="mt-3 font-mono text-sm text-emerald-900 dark:text-emerald-200"
                >
                  {{ t('activation.supportWechat', { wechat: enabledStatus.support_wechat }) }}
                </p>
              </div>
              <button
                v-if="enabledStatus.support_wechat"
                type="button"
                class="btn btn-secondary"
                @click="copySupportWechat"
              >
                <Icon name="copy" size="sm" class="mr-2" />
                {{ t('activation.copyWechat') }}
              </button>
            </div>
          </section>

          <section v-if="canClaimRecall" class="card border-primary-200 p-6 dark:border-primary-900/60">
            <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                  {{ t('activation.recallTitle') }}
                </h2>
                <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
                  {{ t('activation.recallDescription') }}
                </p>
              </div>
              <button
                data-testid="claim-recall"
                type="button"
                class="btn btn-primary"
                @click="showRecallConfirm = true"
              >
                {{ t('activation.claimRecall') }}
              </button>
            </div>
          </section>

          <section v-if="showTroubleshooting" class="card overflow-hidden">
            <div class="border-b border-gray-100 px-6 py-5 dark:border-dark-700">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('activation.troubleshooting.title') }}
              </h2>
            </div>
            <div class="grid gap-px bg-gray-100 dark:bg-dark-700 md:grid-cols-2">
              <article class="bg-white p-5 dark:bg-dark-800">
                <p class="text-xs font-medium text-gray-500">{{ t('activation.troubleshooting.baseUrl') }}</p>
                <code class="mt-2 block break-all text-sm text-gray-900 dark:text-gray-100">{{ apiBaseUrl }}</code>
              </article>
              <article class="bg-white p-5 dark:bg-dark-800">
                <p class="text-xs font-medium text-gray-500">{{ t('activation.troubleshooting.model') }}</p>
                <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ t('activation.troubleshooting.modelHelp') }}</p>
              </article>
              <article class="bg-white p-5 dark:bg-dark-800">
                <p class="text-xs font-medium text-gray-500">{{ t('activation.troubleshooting.key') }}</p>
                <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ t('activation.troubleshooting.keyHelp') }}</p>
              </article>
              <article class="bg-white p-5 dark:bg-dark-800">
                <p class="text-xs font-medium text-gray-500">{{ t('activation.troubleshooting.request') }}</p>
                <p class="mt-2 text-sm text-gray-600 dark:text-gray-400">{{ t('activation.troubleshooting.requestHelp') }}</p>
              </article>
            </div>
            <div class="flex justify-end border-t border-gray-100 p-4 dark:border-dark-700">
              <button
                data-testid="refresh-activation"
                type="button"
                class="btn btn-secondary"
                :disabled="refreshing"
                @click="refreshActivation"
              >
                <Icon name="refresh" size="sm" class="mr-2" />
                {{ refreshing ? t('activation.refreshing') : t('activation.refresh') }}
              </button>
            </div>
          </section>

          <section v-if="isRecallExpired && enabledStatus.support_wechat" class="card p-6">
            <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <p data-testid="support-wechat" class="font-mono text-sm text-gray-700 dark:text-gray-300">
                {{ t('activation.supportWechat', { wechat: enabledStatus.support_wechat }) }}
              </p>
              <button type="button" class="btn btn-secondary" @click="copySupportWechat">
                <Icon name="copy" size="sm" class="mr-2" />
                {{ t('activation.copyWechat') }}
              </button>
            </div>
          </section>

          <section
            v-if="!hasActiveGroup && !showTroubleshooting && !canClaimRecall && !isPaidZeroSuccess"
            class="card p-6"
          >
            <p class="text-sm text-gray-600 dark:text-gray-400">
              {{ t('activation.noActiveGroup') }}
            </p>
          </section>
        </template>
      </template>
    </div>

    <BaseDialog
      :show="showRecallConfirm"
      :title="t('activation.claimConfirmTitle')"
      width="narrow"
      :show-close-button="false"
      @close="showRecallConfirm = false"
    >
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('activation.claimConfirmDescription') }}
      </p>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button
            data-testid="cancel-claim-recall"
            type="button"
            class="btn btn-secondary"
            @click="showRecallConfirm = false"
          >
            {{ t('activation.cancel') }}
          </button>
          <button
            data-testid="confirm-claim-recall"
            type="button"
            class="btn btn-primary"
            :disabled="activationStore.claiming"
            @click="confirmRecallClaim"
          >
            {{ activationStore.claiming ? t('activation.claiming') : t('activation.confirmClaim') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <UseKeyModal
      :show="showUseKeyModal"
      :api-key="selectedKey?.key || ''"
      :base-url="apiBaseUrl"
      :platform="selectedKey?.group?.platform || null"
      :allow-messages-dispatch="selectedKey?.group?.allow_messages_dispatch || false"
      @close="closeUseKeyModal"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { authAPI } from '@/api/auth'
import { keysAPI } from '@/api/keys'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import UseKeyModal from '@/components/keys/UseKeyModal.vue'
import AppLayout from '@/components/layout/AppLayout.vue'
import { useClipboard } from '@/composables/useClipboard'
import { useActivationStore } from '@/stores/activation'
import { useAppStore } from '@/stores/app'
import type { ApiKey, EnabledUserActivationStatus, UserActivationStatus } from '@/types'

const { t, locale } = useI18n()
const router = useRouter()
const activationStore = useActivationStore()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const initializing = ref(true)
const initialLoadFailed = ref(false)
const refreshing = ref(false)
const creatingKey = ref(false)
const keyLoading = ref(false)
const keyLoadError = ref(false)
const activeKey = ref<ApiKey | null>(null)
const selectedKey = ref<ApiKey | null>(null)
const showUseKeyModal = ref(false)
const showRecallConfirm = ref(false)
const apiBaseUrl = ref('')
const activeQuickStartShell = ref<'macos' | 'windows'>('macos')
const quickStartCopied = ref(false)
let createKeyIdempotencyKey: string | null = null

const status = computed<UserActivationStatus | null>(() => activationStore.status)
const enabledStatus = computed<EnabledUserActivationStatus | null>(() =>
  status.value?.enabled ? status.value : null
)
const isSuccess = computed(() => enabledStatus.value?.segment === 'SUCCESS')
const isPaidZeroSuccess = computed(() => enabledStatus.value?.segment === 'PAID_ZERO_SUCCESS')
const isRecallExpired = computed(() => enabledStatus.value?.recall.state === 'expired')
const canClaimRecall = computed(
  () =>
    enabledStatus.value?.segment !== 'SUCCESS' &&
    enabledStatus.value?.segment !== 'PAID_ZERO_SUCCESS' &&
    !enabledStatus.value?.active_group &&
    enabledStatus.value?.recall.claimable === true
)
const hasActiveGroup = computed(
  () => Boolean(enabledStatus.value?.active_group) && !isPaidZeroSuccess.value
)
const showTroubleshooting = computed(
  () =>
    enabledStatus.value?.segment === 'ATTEMPTED_ZERO_SUCCESS' ||
    enabledStatus.value?.segment === 'PAID_ZERO_SUCCESS' ||
    isRecallExpired.value
)
const formattedExpiry = computed(() => {
  const expiry = enabledStatus.value?.active_group?.expires_at
  return expiry
    ? new Intl.DateTimeFormat(locale.value, { dateStyle: 'medium', timeStyle: 'short' }).format(
        new Date(expiry)
      )
    : ''
})
const quickStartCommand = computed(() => {
  if (!activeKey.value) return ''

  const baseUrl = (apiBaseUrl.value || window.location.origin).replace(/\/+$/, '')
  const apiV1Base = baseUrl.endsWith('/v1') ? baseUrl : `${baseUrl}/v1`
  const endpoint = `${apiV1Base}/responses`
  const authorization = `Authorization: Bearer ${activeKey.value.key}`
  const payload = '{"model":"gpt-5.6","input":"Reply exactly: connection successful"}'

  if (activeQuickStartShell.value === 'windows') {
    return `curl.exe "${endpoint}" -H "${authorization}" -H "Content-Type: application/json" -d '${payload}'`
  }

  return `curl "${endpoint}" \\
  -H "${authorization}" \\
  -H "Content-Type: application/json" \\
  -d '${payload}'`
})

function newIdempotencyKey(): string {
  return typeof globalThis.crypto?.randomUUID === 'function'
    ? globalThis.crypto.randomUUID()
    : `activation-key-${Math.random().toString(36).slice(2)}-${Math.random().toString(36).slice(2)}`
}

function openKey(key: ApiKey): void {
  selectedKey.value = key
  showUseKeyModal.value = true
}

function closeUseKeyModal(): void {
  showUseKeyModal.value = false
  selectedKey.value = null
}

function selectQuickStartShell(shell: 'macos' | 'windows'): void {
  activeQuickStartShell.value = shell
  quickStartCopied.value = false
}

async function copyQuickStartCommand(): Promise<void> {
  quickStartCopied.value = await copyToClipboard(quickStartCommand.value)
}

async function loadActiveGroupKey(nextStatus: UserActivationStatus | null): Promise<void> {
  if (
    !nextStatus?.enabled ||
    !nextStatus.active_group ||
    nextStatus.segment === 'SUCCESS' ||
    nextStatus.segment === 'PAID_ZERO_SUCCESS'
  ) {
    activeKey.value = null
    keyLoadError.value = false
    return
  }
  keyLoading.value = true
  keyLoadError.value = false
  try {
    const response = await keysAPI.list(1, 1, {
      group_id: nextStatus.active_group.group_id,
      status: 'active'
    })
    activeKey.value = response.items[0] ?? null
  } catch {
    activeKey.value = null
    keyLoadError.value = true
  } finally {
    keyLoading.value = false
  }
}

function retryKeyLoad(): Promise<void> {
  return loadActiveGroupKey(status.value)
}

async function createTrialKey(): Promise<void> {
  const groupId = enabledStatus.value?.active_group?.group_id
  if (!groupId || creatingKey.value) return

  createKeyIdempotencyKey ||= newIdempotencyKey()
  creatingKey.value = true
  try {
    const key = await keysAPI.create(
      'Omni Trial',
      groupId,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      undefined,
      { idempotencyKey: createKeyIdempotencyKey }
    )
    createKeyIdempotencyKey = null
    activeKey.value = key
  } catch {
    appStore.showError(t('activation.keyCreateFailed'))
  } finally {
    creatingKey.value = false
  }
}

async function refreshActivation(): Promise<void> {
  refreshing.value = true
  try {
    await loadActiveGroupKey(await activationStore.refreshStatus())
  } catch {
    appStore.showError(t('activation.loadFailed'))
  } finally {
    refreshing.value = false
  }
}

async function retryInitialStatus(): Promise<void> {
  initializing.value = true
  initialLoadFailed.value = false
  try {
    const nextStatus = await activationStore.loadStatus()
    await loadActiveGroupKey(nextStatus)
  } catch {
    initialLoadFailed.value = status.value === null
  } finally {
    initializing.value = false
  }
}

async function confirmRecallClaim(): Promise<void> {
  try {
    await loadActiveGroupKey(await activationStore.claimRecall())
    showRecallConfirm.value = false
  } catch {
    appStore.showError(t('activation.claimFailed'))
  }
}

function copySupportWechat(): void {
  const wechat = enabledStatus.value?.support_wechat
  if (wechat) void copyToClipboard(wechat)
}

onMounted(async () => {
  const settingsRequest = authAPI
    .getPublicSettings()
    .then((settings) => {
      apiBaseUrl.value = settings.api_base_url || window.location.origin
    })
    .catch(() => {
      apiBaseUrl.value = window.location.origin
    })

  try {
    const nextStatus = await activationStore.loadStatus()
    await loadActiveGroupKey(nextStatus)
  } catch {
    initialLoadFailed.value = status.value === null
  } finally {
    await settingsRequest
    initializing.value = false
  }
})
</script>
