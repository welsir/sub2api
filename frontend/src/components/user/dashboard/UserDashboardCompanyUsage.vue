<template>
  <div class="space-y-3">
    <h2 class="text-base font-semibold text-gray-900 dark:text-white">
      {{ t('dashboard.companyUsageTitle') }}
    </h2>
    <ModelDistributionChart
      :model-stats="models"
      :enable-ranking-view="true"
      :disable-breakdown="true"
      :show-metric-toggle="true"
      v-model:metric="metric"
      :ranking-items="rankingResponse?.ranking ?? []"
      :ranking-total-actual-cost="rankingResponse?.total_actual_cost ?? 0"
      :ranking-total-requests="rankingResponse?.total_requests ?? 0"
      :ranking-total-tokens="rankingResponse?.total_tokens ?? 0"
      :loading="loading"
      :ranking-loading="loading"
    />
  </div>
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
