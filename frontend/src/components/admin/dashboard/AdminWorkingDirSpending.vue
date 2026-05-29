<template>
  <div class="card p-4">
    <div class="mb-1 flex items-center justify-between">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.dashboard.workingDirTitle') }}</h3>
      <span v-if="!loading && !error" class="text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.dashboard.workingDirTotal') }}: ${{ totalActualCost.toFixed(2) }}
      </span>
    </div>
    <p class="mb-3 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.dashboard.workingDirHint') }}</p>

    <div v-if="loading" class="flex h-32 items-center justify-center">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="error" class="flex h-32 items-center justify-center text-sm text-red-500">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
    <div v-else-if="items.length === 0" class="flex h-32 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.noDataAvailable') }}
    </div>
    <div v-else class="max-h-96 overflow-auto">
      <table class="w-full text-left text-sm">
        <thead class="sticky top-0 bg-white text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">
          <tr class="border-b border-gray-100 dark:border-gray-700">
            <th class="py-2 pr-2 font-medium">{{ t('admin.dashboard.workingDirUser') }}</th>
            <th class="py-2 pr-2 font-medium">{{ t('admin.dashboard.workingDirColumn') }}</th>
            <th class="py-2 pr-2 text-right font-medium">{{ t('admin.dashboard.workingDirCost') }}</th>
            <th class="py-2 text-right font-medium">{{ t('admin.dashboard.workingDirRequests') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(row, idx) in items"
            :key="idx"
            class="border-b border-gray-50 transition-colors hover:bg-gray-50 dark:border-gray-800 dark:hover:bg-dark-700/40"
          >
            <td class="max-w-[160px] truncate py-1.5 pr-2 text-gray-700 dark:text-gray-300" :title="row.email">{{ row.email || `#${row.user_id}` }}</td>
            <td class="max-w-[360px] truncate py-1.5 pr-2 font-mono text-xs" :title="row.working_directory">
              <span v-if="row.working_directory" class="text-gray-900 dark:text-white">{{ row.working_directory }}</span>
              <span v-else class="italic text-gray-400">{{ t('admin.dashboard.workingDirUnknown') }}</span>
            </td>
            <td class="py-1.5 pr-2 text-right font-medium text-gray-900 dark:text-white">${{ row.actual_cost.toFixed(2) }}</td>
            <td class="py-1.5 text-right text-gray-600 dark:text-gray-400">{{ row.requests }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { WorkingDirSpendingItem } from '@/api/admin/dashboard'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = defineProps<{ startDate: string; endDate: string }>()
const { t } = useI18n()

const items = ref<WorkingDirSpendingItem[]>([])
const totalActualCost = ref(0)
const loading = ref(false)
const error = ref(false)

const load = async () => {
  loading.value = true
  error.value = false
  try {
    const resp = await adminAPI.dashboard.getWorkingDirSpending({
      start_date: props.startDate,
      end_date: props.endDate,
      limit: 100
    })
    items.value = resp.items || []
    totalActualCost.value = resp.total_actual_cost || 0
  } catch (e) {
    console.error('Failed to load working directory spending:', e)
    error.value = true
  } finally {
    loading.value = false
  }
}

watch(() => [props.startDate, props.endDate], load)
onMounted(load)
</script>
