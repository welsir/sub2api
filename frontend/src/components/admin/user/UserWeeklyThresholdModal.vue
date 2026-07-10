<template>
  <BaseDialog
    :show="show"
    :title="t('admin.users.weeklyThreshold.title')"
    @close="$emit('close')"
  >
    <div v-if="user" class="space-y-5">
      <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800">
        <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
          {{ t('admin.users.weeklyThreshold.hint') }}
        </p>
      </div>

      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.users.weeklyThreshold.label') }}
        </label>
        <div class="relative">
          <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center text-gray-400">$</span>
          <input
            v-model.number="threshold"
            data-test="weekly-threshold-input"
            type="number"
            min="0"
            step="0.01"
            class="input w-full pl-7"
          />
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.users.weeklyThreshold.disableHint') }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="$emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          data-test="weekly-threshold-save"
          :disabled="submitting"
          @click="handleSave"
        >
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits<{
  close: []
  success: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const threshold = ref(0)
const submitting = ref(false)

watch(
  () => props.show,
  (show) => {
    if (show && props.user) {
      threshold.value = props.user.weekly_cost_threshold ?? 0
    }
  }
)

const handleSave = async () => {
  if (!props.user) return
  const value = Number(threshold.value)
  if (!Number.isFinite(value) || value < 0) {
    appStore.showError(t('admin.users.weeklyThreshold.invalid'))
    return
  }

  submitting.value = true
  try {
    await adminAPI.users.update(props.user.id, { weekly_cost_threshold: value })
    appStore.showSuccess(t('admin.users.weeklyThreshold.updated'))
    emit('success')
    emit('close')
  } catch (error: any) {
    appStore.showError(error?.response?.data?.detail || t('admin.users.weeklyThreshold.failed'))
  } finally {
    submitting.value = false
  }
}
</script>
