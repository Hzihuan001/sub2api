<template>
  <div
    v-if="supportsCustomMenuAuthContext(url)"
    class="flex items-start justify-between gap-4 rounded-lg bg-gray-50 px-3 py-2.5 sm:col-span-2 dark:bg-dark-700/50"
  >
    <div>
      <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.customMenu.passAuthContext') }}
      </p>
      <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.customMenu.passAuthContextHint') }}
      </p>
    </div>
    <Toggle
      data-testid="custom-menu-pass-auth-context"
      :model-value="normalizeCustomMenuAuthContext(modelValue)"
      :disabled="readOnly"
      @update:model-value="updateValue"
    />
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Toggle from '@/components/common/Toggle.vue'
import { normalizeCustomMenuAuthContext, supportsCustomMenuAuthContext } from './customMenuAuthContext'

const props = withDefaults(defineProps<{
  modelValue?: boolean
  url?: string
  readOnly?: boolean
}>(), { modelValue: undefined, url: undefined, readOnly: true })
const emit = defineEmits<{ (event: 'update:modelValue', value: boolean): void }>()
const { t } = useI18n()

function updateValue(value: boolean): void {
  if (!props.readOnly) emit('update:modelValue', value)
}
</script>
