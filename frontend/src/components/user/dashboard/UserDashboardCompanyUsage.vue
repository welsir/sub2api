<template>
  <section class="space-y-3">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">
      {{ t('dashboard.companyUsageTitle') }}
    </h2>
    <ModelDistributionChart
      v-model:metric="metric"
      :model-stats="models"
      :enable-ranking-view="true"
      :enable-breakdown="false"
      :show-account-cost="false"
      :show-metric-toggle="true"
      :ranking-items="rankingResponse?.ranking ?? []"
      :ranking-total-actual-cost="rankingResponse?.total_actual_cost ?? 0"
      :ranking-total-requests="rankingResponse?.total_requests ?? 0"
      :ranking-total-tokens="rankingResponse?.total_tokens ?? 0"
      :loading="loading"
      :ranking-loading="loading"
    />
  </section>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'
import type { ModelStat, UserSpendingRankingResponse } from '@/types'

defineProps<{
  loading: boolean
  models: ModelStat[]
  rankingResponse: UserSpendingRankingResponse | null
}>()

const { t } = useI18n()
const metric = ref<'tokens' | 'actual_cost'>('tokens')
</script>
