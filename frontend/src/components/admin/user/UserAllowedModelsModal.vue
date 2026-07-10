<template>
  <BaseDialog :show="show" :title="t('admin.users.modelWhitelist')" width="wide" @close="$emit('close')">
    <div v-if="user" class="space-y-6">
      <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800">
        <p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
        <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
          {{ t('admin.users.modelWhitelistHint') }}
        </p>
      </div>

      <div>
        <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('admin.users.addModel') }}
        </label>
        <div class="flex gap-2">
          <input
            v-model="draft"
            type="text"
            :placeholder="t('admin.users.modelPlaceholder')"
            class="flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/20 dark:border-dark-500 dark:bg-dark-700"
            @keydown.enter.prevent="addModel"
          />
          <button type="button" class="btn btn-secondary px-4" :disabled="!draft.trim()" @click="addModel">
            {{ t('common.add') }}
          </button>
        </div>
        <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.users.modelWhitelistWildcardHint') }}
        </p>
      </div>

      <div>
        <div class="mb-3 flex items-center gap-2">
          <h4 class="text-sm font-semibold text-gray-700 dark:text-gray-300">
            {{ t('admin.users.allowedModelsList') }}
          </h4>
          <span class="text-xs text-gray-400">({{ models.length }})</span>
        </div>

        <div v-if="models.length" class="flex flex-wrap gap-2">
          <span
            v-for="(model, index) in models"
            :key="model"
            class="inline-flex items-center gap-1.5 rounded-full bg-primary-100 py-1 pl-3 pr-1.5 text-sm font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
          >
            {{ model }}
            <button
              type="button"
              class="flex h-5 w-5 items-center justify-center rounded-full text-primary-500 hover:bg-primary-200 hover:text-primary-700 dark:hover:bg-primary-800/60"
              :aria-label="t('common.delete')"
              @click="removeModel(index)"
            >
              ×
            </button>
          </span>
        </div>
        <div
          v-else
          class="rounded-xl border-2 border-dashed border-gray-200 bg-gray-50/50 px-4 py-6 text-center dark:border-dark-600 dark:bg-dark-800/50"
        >
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.users.modelWhitelistEmpty') }}
          </p>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary px-5" @click="$emit('close')">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary px-6" :disabled="submitting" @click="handleSave">
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
const models = ref<string[]>([])
const draft = ref('')
const submitting = ref(false)

watch(
  () => props.show,
  (show) => {
    if (show && props.user) {
      models.value = [...(props.user.allowed_models ?? [])]
      draft.value = ''
    }
  }
)

const addModel = () => {
  const model = draft.value.trim()
  if (model && !models.value.includes(model)) {
    models.value.push(model)
  }
  draft.value = ''
}

const removeModel = (index: number) => {
  models.value.splice(index, 1)
}

const handleSave = async () => {
  if (!props.user) return
  submitting.value = true
  try {
    await adminAPI.users.update(props.user.id, { allowed_models: models.value })
    appStore.showSuccess(t('admin.users.modelWhitelistUpdated'))
    emit('success')
    emit('close')
  } finally {
    submitting.value = false
  }
}
</script>
