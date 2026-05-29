<template>
  <BaseDialog :show="show" :title="t('admin.users.weeklyThreshold')" width="normal" @close="$emit('close')">
    <div v-if="user" class="space-y-6">
      <!-- 用户信息头部 -->
      <div class="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-primary-50 to-primary-100 p-5 dark:from-primary-900/30 dark:to-primary-800/20">
        <div class="flex h-14 w-14 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
          <span class="text-2xl font-semibold text-primary-600 dark:text-primary-400">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="flex-1">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('admin.users.weeklyThresholdHint') }}</p>
        </div>
      </div>

      <!-- 阈值输入 -->
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.users.weeklyThresholdLabel') }}</label>
        <div class="relative">
          <span class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-gray-400">$</span>
          <input
            v-model="amount"
            type="number"
            min="0"
            step="0.01"
            :placeholder="t('admin.users.weeklyThresholdPlaceholder')"
            class="w-full rounded-lg border border-gray-300 bg-white py-2 pl-7 pr-3 text-sm transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
          />
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.weeklyThresholdNoLimit') }}</p>
      </div>

      <!-- 当前状态提示 -->
      <div class="rounded-xl border border-gray-200 bg-gray-50/50 px-4 py-3 text-sm dark:border-dark-600 dark:bg-dark-800/50">
        <span class="text-gray-500 dark:text-gray-400">{{ t('admin.users.weeklyThresholdCurrent') }}：</span>
        <span class="font-medium text-gray-900 dark:text-white">
          {{ effectiveThreshold > 0 ? `$${effectiveThreshold.toFixed(2)}` : t('admin.users.weeklyThresholdUnlimited') }}
        </span>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button @click="$emit('close')" class="btn btn-secondary px-5">{{ t('common.cancel') }}</button>
        <button @click="handleSave" :disabled="submitting" class="btn btn-primary px-6">
          <svg v-if="submitting" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ submitting ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const amount = ref<string>('')
const submitting = ref(false)

// effectiveThreshold reflects what is currently persisted (for the status line).
const effectiveThreshold = computed(() => {
  const v = props.user?.weekly_cost_threshold
  return typeof v === 'number' && v > 0 ? v : 0
})

watch(
  () => props.show,
  (v) => {
    if (v && props.user) {
      const t0 = props.user.weekly_cost_threshold
      amount.value = typeof t0 === 'number' && t0 > 0 ? String(t0) : ''
    }
  }
)

const handleSave = async () => {
  if (!props.user) return
  // 空 / 非数字 / <=0 → 0（清除，不限制）；否则按数值设置。
  const parsed = parseFloat(amount.value)
  const value = Number.isFinite(parsed) && parsed > 0 ? parsed : 0
  submitting.value = true
  try {
    await adminAPI.users.update(props.user.id, { weekly_cost_threshold: value })
    appStore.showSuccess(t('admin.users.weeklyThresholdUpdated'))
    emit('success')
    emit('close')
  } catch (error) {
    console.error('Failed to update user weekly threshold:', error)
  } finally {
    submitting.value = false
  }
}
</script>
