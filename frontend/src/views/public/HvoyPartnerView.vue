<!--
[INPUT]: The public HVOY activation offer, landing-page i18n messages, and shared public UI primitives.
[OUTPUT]: A responsive trust handoff with truthful offer disclosure and activation entry points.
[POS]: Public conversion page between the HVOY partner listing and Sub2API registration or login.
-->

<template>
  <div
    data-testid="partner-page"
    class="overflow-x-hidden bg-gray-50 text-gray-950 dark:bg-dark-950 dark:text-white"
  >
    <header class="border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
      <div class="mx-auto flex max-w-6xl items-center justify-between gap-4 px-4 py-3 sm:px-6">
        <RouterLink to="/home" class="flex min-w-0 items-center gap-3">
          <span
            class="flex h-9 w-9 flex-shrink-0 items-center justify-center overflow-hidden rounded-md border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-800"
          >
            <img src="/logo.png" alt="Omni API" class="h-full w-full object-contain" />
          </span>
          <span class="min-w-0">
            <span class="block truncate text-sm font-semibold">{{ t('hvoyPartner.brand') }}</span>
            <span class="block truncate text-xs text-gray-500 dark:text-dark-400">
              {{ t('hvoyPartner.channel') }}
            </span>
          </span>
        </RouterLink>

        <div class="flex flex-shrink-0 items-center gap-2 sm:gap-3">
          <LocaleSwitcher />
          <RouterLink
            to="/login?redirect=/activation"
            class="inline-flex items-center gap-2 rounded-md border border-gray-300 px-3 py-2 text-sm font-semibold text-gray-700 transition-colors hover:border-gray-400 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-200 dark:hover:bg-dark-800"
          >
            <Icon name="login" size="sm" />
            <span class="hidden sm:inline">{{ t('hvoyPartner.navLogin') }}</span>
          </RouterLink>
        </div>
      </div>
    </header>

    <main>
      <section class="border-b border-gray-200 bg-white dark:border-dark-800 dark:bg-dark-900">
        <div
          class="mx-auto grid max-w-6xl min-w-0 gap-3 px-4 pb-4 pt-3 sm:gap-7 sm:px-6 sm:pb-10 sm:pt-11 lg:grid-cols-[minmax(0,1.15fr)_minmax(280px,0.85fr)] lg:gap-12"
        >
          <div class="min-w-0">
            <div
              class="inline-flex max-w-full items-center gap-2 rounded-md border border-gray-200 bg-gray-50 px-3 py-1.5 text-xs font-semibold text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200"
            >
              <span
                class="h-2 w-2 flex-shrink-0 rounded-full"
                :class="isEnabled ? 'bg-emerald-500' : 'bg-gray-400'"
              ></span>
              <span class="break-words">{{ availabilityLabel }}</span>
            </div>

            <h1
              class="mt-5 max-w-3xl break-words text-3xl font-bold leading-tight tracking-normal text-gray-950 dark:text-white sm:text-4xl"
            >
              {{ heroTitle }}
            </h1>
            <p
              class="mt-4 max-w-2xl break-words text-sm leading-6 text-gray-600 dark:text-dark-300 sm:text-base sm:leading-7"
            >
              {{ heroDescription }}
            </p>

            <div class="mt-6 flex min-w-0 flex-col gap-3 sm:flex-row">
              <RouterLink
                data-testid="primary-cta"
                :to="primaryCta.href"
                class="inline-flex min-h-11 min-w-0 items-center justify-center gap-2 rounded-md bg-primary-600 px-5 py-2.5 text-center text-sm font-semibold text-white shadow-sm transition-colors hover:bg-primary-700"
              >
                <Icon :name="isEnabled ? 'gift' : 'login'" size="sm" />
                <span class="break-words">{{ primaryCta.label }}</span>
                <Icon name="arrowRight" size="sm" />
              </RouterLink>
              <RouterLink
                data-testid="secondary-cta"
                :to="secondaryCta.href"
                class="inline-flex min-h-11 min-w-0 items-center justify-center rounded-md border border-gray-300 bg-white px-5 py-2.5 text-center text-sm font-semibold text-gray-700 transition-colors hover:border-gray-400 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 dark:hover:bg-dark-800"
              >
                <span class="break-words">{{ secondaryCta.label }}</span>
              </RouterLink>
            </div>
          </div>

          <aside
            class="min-w-0 border-t border-gray-200 pt-6 dark:border-dark-700 lg:border-l lg:border-t-0 lg:pl-10 lg:pt-1"
          >
            <template v-if="isEnabled">
              <p class="text-xs font-semibold uppercase text-gray-500 dark:text-dark-400">
                {{ t('hvoyPartner.offer.label') }}
              </p>
              <p
                v-if="starterSummary"
                class="mt-2 break-words text-4xl font-bold tracking-normal text-gray-950 dark:text-white"
              >
                {{ starterSummary }}
              </p>
              <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
                {{ t('hvoyPartner.offer.starterNote') }}
              </p>
            </template>
            <template v-else>
              <p class="text-xs font-semibold uppercase text-gray-500 dark:text-dark-400">
                {{ t('hvoyPartner.offer.unavailableLabel') }}
              </p>
              <p class="mt-3 text-lg font-semibold text-gray-950 dark:text-white">
                {{ t('hvoyPartner.offer.unavailableTitle') }}
              </p>
              <p class="mt-2 text-sm leading-6 text-gray-600 dark:text-dark-300">
                {{ t('hvoyPartner.offer.unavailableNote') }}
              </p>
            </template>

            <dl class="mt-5 divide-y divide-gray-200 border-y border-gray-200 dark:divide-dark-700 dark:border-dark-700">
              <div v-if="rechargeSummary" class="flex min-w-0 items-center justify-between gap-4 py-3">
                <dt class="flex min-w-0 items-center gap-2 text-sm text-gray-500 dark:text-dark-400">
                  <Icon name="calculator" size="sm" class="flex-shrink-0" />
                  <span class="break-words">{{ t('hvoyPartner.pricing.rechargeLabel') }}</span>
                </dt>
                <dd class="flex-shrink-0 text-sm font-semibold">{{ rechargeSummary }}</dd>
              </div>
              <div v-if="proRateSummary" class="flex min-w-0 items-center justify-between gap-4 py-3">
                <dt class="flex min-w-0 items-center gap-2 text-sm text-gray-500 dark:text-dark-400">
                  <Icon name="creditCard" size="sm" class="flex-shrink-0" />
                  <span class="break-words">{{ t('hvoyPartner.pricing.proLabel') }}</span>
                </dt>
                <dd class="flex-shrink-0 text-sm font-semibold">{{ proRateSummary }}</dd>
              </div>
              <div class="flex min-w-0 items-center justify-between gap-4 py-3">
                <dt class="flex min-w-0 items-center gap-2 text-sm text-gray-500 dark:text-dark-400">
                  <Icon name="shield" size="sm" class="flex-shrink-0" />
                  <span class="break-words">{{ t('hvoyPartner.pricing.balanceLabel') }}</span>
                </dt>
                <dd class="text-right text-sm font-semibold">{{ t('hvoyPartner.pricing.paidNoExpiry') }}</dd>
              </div>
            </dl>

            <p v-if="minimumRechargeSummary" class="mt-3 text-xs text-gray-500 dark:text-dark-400">
              {{ minimumRechargeSummary }}
            </p>
            <p class="mt-3 flex min-w-0 items-start gap-2 text-xs leading-5 text-gray-500 dark:text-dark-400">
              <Icon name="chat" size="sm" class="mt-0.5 flex-shrink-0" />
              <span class="break-words">{{ supportSummary }}</span>
            </p>
          </aside>
        </div>
      </section>

      <section
        data-testid="next-section"
        class="border-b border-gray-200 bg-gray-50 dark:border-dark-800 dark:bg-dark-950"
      >
        <div class="mx-auto grid max-w-6xl min-w-0 sm:grid-cols-2 lg:grid-cols-4">
          <div class="min-w-0 border-b border-gray-200 px-4 py-5 dark:border-dark-800 sm:border-r sm:px-6 lg:border-b-0">
            <Icon :name="isEnabled ? 'clock' : 'login'" size="sm" class="text-primary-600 dark:text-primary-400" />
            <p class="mt-3 text-sm font-semibold">{{ firstFactTitle }}</p>
            <p class="mt-1 break-words text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ firstFactDescription }}
            </p>
          </div>
          <div class="min-w-0 border-b border-gray-200 px-4 py-5 dark:border-dark-800 sm:px-6 lg:border-b-0 lg:border-r">
            <Icon name="calculator" size="sm" class="text-primary-600 dark:text-primary-400" />
            <p class="mt-3 text-sm font-semibold">{{ t('hvoyPartner.facts.rechargeTitle') }}</p>
            <p class="mt-1 break-words text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ t('hvoyPartner.facts.rechargeDescription') }}
            </p>
          </div>
          <div class="min-w-0 border-b border-gray-200 px-4 py-5 dark:border-dark-800 sm:border-r sm:px-6 lg:border-b-0">
            <Icon name="shield" size="sm" class="text-primary-600 dark:text-primary-400" />
            <p class="mt-3 text-sm font-semibold">{{ t('hvoyPartner.facts.balanceTitle') }}</p>
            <p class="mt-1 break-words text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ t('hvoyPartner.facts.balanceDescription') }}
            </p>
          </div>
          <div class="min-w-0 px-4 py-5 sm:px-6">
            <Icon name="chat" size="sm" class="text-primary-600 dark:text-primary-400" />
            <p class="mt-3 text-sm font-semibold">{{ t('hvoyPartner.facts.supportTitle') }}</p>
            <p class="mt-1 break-words text-xs leading-5 text-gray-500 dark:text-dark-400">
              {{ t('hvoyPartner.facts.supportDescription') }}
            </p>
          </div>
        </div>
      </section>

      <section class="bg-white dark:bg-dark-900">
        <div class="mx-auto max-w-6xl px-4 py-10 sm:px-6 sm:py-12">
          <p class="text-xs font-semibold text-primary-700 dark:text-primary-300">
            {{ t('hvoyPartner.flow.eyebrow') }}
          </p>
          <h2 class="mt-2 break-words text-xl font-bold tracking-normal sm:text-2xl">
            {{ t('hvoyPartner.flow.title') }}
          </h2>
          <ol class="mt-7 grid min-w-0 gap-6 md:grid-cols-3">
            <li v-for="(step, index) in flowSteps" :key="step.title" class="min-w-0 border-t border-gray-200 pt-4 dark:border-dark-700">
              <span class="text-xs font-semibold text-gray-400">0{{ index + 1 }}</span>
              <p class="mt-2 break-words text-sm font-semibold">{{ step.title }}</p>
              <p class="mt-1 break-words text-sm leading-6 text-gray-500 dark:text-dark-400">
                {{ step.description }}
              </p>
            </li>
          </ol>
        </div>
      </section>
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  getHvoyActivationOffer,
  type HvoyActivationOffer,
} from '@/api/activation'

const { t } = useI18n()

const loading = ref(true)
const offer = ref<HvoyActivationOffer | null>(null)

const isEnabled = computed(() => !loading.value && offer.value?.enabled === true)

function isPositiveNumber(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: 2,
  }).format(value)
}

const starterSummary = computed(() => {
  if (
    !isEnabled.value
    || !isPositiveNumber(offer.value?.starter_credit_usd)
    || !isPositiveNumber(offer.value?.starter_valid_hours)
  ) {
    return ''
  }
  return `$${formatNumber(offer.value.starter_credit_usd)} / ${formatNumber(offer.value.starter_valid_hours)}h`
})

const rechargeSummary = computed(() => {
  if (!isEnabled.value || !isPositiveNumber(offer.value?.recharge_credit_rate)) {
    return ''
  }
  return `¥1 = $${formatNumber(offer.value.recharge_credit_rate)}`
})

const proRateSummary = computed(() => {
  if (!isEnabled.value || !isPositiveNumber(offer.value?.pro_rate_multiplier)) {
    return ''
  }
  return `${formatNumber(offer.value.pro_rate_multiplier)}x`
})

const minimumRechargeSummary = computed(() => {
  if (!isEnabled.value || !isPositiveNumber(offer.value?.minimum_recharge_cny)) {
    return ''
  }
  return t('hvoyPartner.pricing.minimumRecharge', {
    amount: formatNumber(offer.value.minimum_recharge_cny),
  })
})

const availabilityLabel = computed(() => {
  if (loading.value) {
    return t('hvoyPartner.loading')
  }
  return isEnabled.value
    ? t('hvoyPartner.available')
    : t('hvoyPartner.unavailable')
})

const heroTitle = computed(() =>
  isEnabled.value
    ? t('hvoyPartner.hero.enabledTitle')
    : t('hvoyPartner.hero.unavailableTitle')
)

const heroDescription = computed(() =>
  isEnabled.value
    ? t('hvoyPartner.hero.enabledDescription')
    : t('hvoyPartner.hero.unavailableDescription')
)

const primaryCta = computed(() => (
  isEnabled.value
    ? {
        href: '/register?source=hvoy_partner&redirect=/activation',
        label: t('hvoyPartner.cta.register'),
      }
    : {
        href: '/login?redirect=/activation',
        label: t('hvoyPartner.cta.login'),
      }
))

const secondaryCta = computed(() => (
  isEnabled.value
    ? {
        href: '/login?redirect=/activation',
        label: t('hvoyPartner.cta.existingUser'),
      }
    : {
        href: '/home',
        label: t('hvoyPartner.cta.viewService'),
      }
))

const supportSummary = computed(() => {
  const wechat = offer.value?.support_wechat?.trim()
  if (isEnabled.value && wechat) {
    return t('hvoyPartner.support.withWechat', { wechat })
  }
  return t('hvoyPartner.support.generic')
})

const firstFactTitle = computed(() =>
  isEnabled.value
    ? t('hvoyPartner.facts.trialTitle')
    : t('hvoyPartner.facts.entryTitle')
)

const firstFactDescription = computed(() =>
  isEnabled.value
    ? t('hvoyPartner.facts.trialDescription')
    : t('hvoyPartner.facts.entryDescription')
)

const flowSteps = computed(() => {
  if (isEnabled.value) {
    return [
      {
        title: t('hvoyPartner.flow.enabled.verifyTitle'),
        description: t('hvoyPartner.flow.enabled.verifyDescription'),
      },
      {
        title: t('hvoyPartner.flow.enabled.creditTitle'),
        description: t('hvoyPartner.flow.enabled.creditDescription'),
      },
      {
        title: t('hvoyPartner.flow.enabled.useTitle'),
        description: t('hvoyPartner.flow.enabled.useDescription'),
      },
    ]
  }
  return [
    {
      title: t('hvoyPartner.flow.unavailable.loginTitle'),
      description: t('hvoyPartner.flow.unavailable.loginDescription'),
    },
    {
      title: t('hvoyPartner.flow.unavailable.serviceTitle'),
      description: t('hvoyPartner.flow.unavailable.serviceDescription'),
    },
    {
      title: t('hvoyPartner.flow.unavailable.supportTitle'),
      description: t('hvoyPartner.flow.unavailable.supportDescription'),
    },
  ]
})

onMounted(async () => {
  try {
    offer.value = await getHvoyActivationOffer()
  } catch {
    offer.value = { enabled: false }
  } finally {
    loading.value = false
  }
})
</script>
