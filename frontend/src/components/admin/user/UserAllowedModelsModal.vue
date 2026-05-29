<template>
  <BaseDialog :show="show" :title="t('admin.users.modelWhitelist')" width="wide" @close="$emit('close')">
    <div v-if="user" class="space-y-6">
      <!-- 用户信息头部 -->
      <div class="flex items-center gap-4 rounded-2xl bg-gradient-to-r from-primary-50 to-primary-100 p-5 dark:from-primary-900/30 dark:to-primary-800/20">
        <div class="flex h-14 w-14 items-center justify-center rounded-full bg-white shadow-sm dark:bg-dark-700">
          <span class="text-2xl font-semibold text-primary-600 dark:text-primary-400">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div class="flex-1">
          <p class="text-lg font-semibold text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">{{ t('admin.users.modelWhitelistHint') }}</p>
        </div>
      </div>

      <!-- 输入区域 -->
      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.users.addModel') }}</label>
        <div class="flex gap-2">
          <input
            v-model="draft"
            type="text"
            :placeholder="t('admin.users.modelPlaceholder')"
            @keydown.enter.prevent="addModel"
            class="flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm transition-colors focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700 dark:focus:border-primary-500"
          />
          <button type="button" @click="addModel" :disabled="!draft.trim()" class="btn btn-secondary px-4">
            {{ t('common.add') }}
          </button>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.users.modelWhitelistWildcardHint') }}</p>
      </div>

      <!-- 已添加的模型列表 -->
      <div>
        <div class="mb-3 flex items-center gap-2">
          <div class="h-1.5 w-1.5 rounded-full bg-primary-500"></div>
          <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">{{ t('admin.users.allowedModelsList') }}</h4>
          <span class="text-xs text-gray-400">({{ models.length }})</span>
        </div>

        <div v-if="models.length > 0" class="flex flex-wrap gap-2">
          <span
            v-for="(m, idx) in models"
            :key="m"
            class="inline-flex items-center gap-1.5 rounded-full bg-primary-100 py-1 pl-3 pr-1.5 text-sm font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
          >
            {{ m }}
            <button
              type="button"
              @click="removeModel(idx)"
              class="flex h-5 w-5 items-center justify-center rounded-full text-primary-500 transition-colors hover:bg-primary-200 hover:text-primary-700 dark:hover:bg-primary-800/60"
              :aria-label="t('common.delete')"
            >
              <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </span>
        </div>

        <!-- 空状态：表示不限制 -->
        <div v-else class="rounded-xl border-2 border-dashed border-gray-200 bg-gray-50/50 px-4 py-6 text-center dark:border-dark-600 dark:bg-dark-800/50">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.users.modelWhitelistEmpty') }}</p>
        </div>
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
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close', 'success'])
const { t } = useI18n()
const appStore = useAppStore()

const models = ref<string[]>([])
const draft = ref('')
const submitting = ref(false)

watch(
  () => props.show,
  (v) => {
    if (v && props.user) {
      models.value = [...(props.user.allowed_models ?? [])]
      draft.value = ''
    }
  }
)

const addModel = () => {
  const value = draft.value.trim()
  if (!value) return
  if (!models.value.includes(value)) {
    models.value.push(value)
  }
  draft.value = ''
}

const removeModel = (idx: number) => {
  models.value.splice(idx, 1)
}

const handleSave = async () => {
  if (!props.user) return
  submitting.value = true
  try {
    await adminAPI.users.update(props.user.id, {
      allowed_models: models.value,
    })
    appStore.showSuccess(t('admin.users.modelWhitelistUpdated'))
    emit('success')
    emit('close')
  } catch (error) {
    console.error('Failed to update user model whitelist:', error)
  } finally {
    submitting.value = false
  }
}
</script>
