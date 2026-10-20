<template>
  <div class="rounded-lg border border-primary-100 bg-primary-50/30 p-3 text-xs leading-5 dark:border-primary-900/50 dark:bg-primary-900/10" data-test="key-route-scope">
    <template v-if="modelValue === 'account_v2'">
      <p class="font-medium text-gray-800 dark:text-gray-200">{{ t('custom.keyRouting.accountScopeTitle') }}</p>
      <p class="text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.accountScopeDescription') }}</p>
      <p v-if="newKey" class="text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.accountScopeNewDefault') }}</p>
    </template>
    <template v-else>
      <p class="text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.legacyScopeDescription') }}</p>
      <button type="button" class="mt-1 font-medium text-primary-600 disabled:opacity-50 dark:text-primary-400" :disabled="disabled" data-test="key-route-scope-upgrade" @click="emit('update:modelValue', 'account_v2')">
        {{ t('custom.keyRouting.accountScopeUpgrade') }}
      </button>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { KeyRouteScope } from '@/types'

withDefaults(defineProps<{ modelValue: KeyRouteScope; disabled?: boolean; newKey?: boolean }>(), { disabled: false, newKey: false })
const emit = defineEmits<{ 'update:modelValue': [value: KeyRouteScope] }>()
const { t } = useI18n()
</script>
