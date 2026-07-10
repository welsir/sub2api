<template>
  <section class="card p-4" data-test="working-dir-spending">
    <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.dashboard.workingDirectories.title') }}
        </h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.dashboard.workingDirectories.subtitle') }}
        </p>
      </div>
      <div class="rounded-lg bg-emerald-50 px-3 py-2 text-right dark:bg-emerald-900/20">
        <p class="text-[11px] text-emerald-700 dark:text-emerald-300">
          {{ t('admin.dashboard.workingDirectories.total') }}
        </p>
        <p class="text-sm font-semibold text-emerald-700 dark:text-emerald-300">
          ${{ formatCost(totalActualCost) }}
        </p>
      </div>
    </div>

    <div v-if="loading" class="flex justify-center py-10">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="error" class="py-8 text-center text-sm text-red-500">
      {{ t('admin.dashboard.workingDirectories.failed') }}
    </div>
    <div v-else-if="items.length === 0" class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.dashboard.workingDirectories.empty') }}
    </div>
    <div v-else class="overflow-x-auto">
      <table class="min-w-full text-sm">
        <thead>
          <tr class="border-b border-gray-200 text-left text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
            <th class="px-3 py-2 font-medium">{{ t('admin.dashboard.workingDirectories.user') }}</th>
            <th class="px-3 py-2 font-medium">{{ t('admin.dashboard.workingDirectories.directory') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('admin.dashboard.workingDirectories.requests') }}</th>
            <th class="px-3 py-2 text-right font-medium">{{ t('admin.dashboard.workingDirectories.spend') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in items"
            :key="`${item.user_id}:${item.working_directory}`"
            class="border-b border-gray-100 last:border-0 dark:border-dark-800"
          >
            <td class="px-3 py-2 text-gray-900 dark:text-white">
              {{ item.email || `#${item.user_id}` }}
            </td>
            <td class="max-w-[32rem] px-3 py-2 font-mono text-xs text-gray-600 dark:text-gray-300">
              <span class="block truncate" :title="item.working_directory || undefined">
                {{ item.working_directory || t('admin.dashboard.workingDirectories.unknown') }}
              </span>
            </td>
            <td class="px-3 py-2 text-right text-gray-600 dark:text-gray-300">
              {{ item.requests.toLocaleString() }}
            </td>
            <td class="px-3 py-2 text-right font-medium text-emerald-600 dark:text-emerald-400">
              ${{ formatCost(item.actual_cost) }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { WorkingDirSpendingItem } from '@/api/admin/dashboard'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const props = defineProps<{
  startDate: string
  endDate: string
}>()

const { t } = useI18n()
const items = ref<WorkingDirSpendingItem[]>([])
const totalActualCost = ref(0)
const loading = ref(false)
const error = ref(false)
let loadSequence = 0

const formatCost = (value: number) => Number(value || 0).toFixed(2)

const load = async () => {
  const sequence = ++loadSequence
  loading.value = true
  error.value = false
  try {
    const response = await adminAPI.dashboard.getWorkingDirSpending({
      start_date: props.startDate,
      end_date: props.endDate,
      limit: 100,
    })
    if (sequence !== loadSequence) return
    items.value = response.items ?? []
    totalActualCost.value = response.total_actual_cost ?? 0
  } catch (cause) {
    if (sequence !== loadSequence) return
    console.error('Error loading working-directory spending:', cause)
    items.value = []
    totalActualCost.value = 0
    error.value = true
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

watch(() => [props.startDate, props.endDate], load, { immediate: true })
</script>
