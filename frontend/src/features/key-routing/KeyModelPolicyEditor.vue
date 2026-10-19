<template>
  <section class="space-y-3" data-test="key-model-policy">
    <div class="flex items-start justify-between gap-3">
      <div>
        <label class="input-label mb-1">{{ t('custom.keyRouting.modelPolicyTitle') }}</label>
        <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.modelPolicyHint') }}</p>
      </div>
      <button type="button" role="switch" :aria-checked="enabled" :aria-label="t('custom.keyRouting.modelPolicyTitle')" :disabled="disabled" class="relative mt-1 h-5 w-9 shrink-0 rounded-full transition-colors disabled:opacity-50" :class="enabled ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'" data-test="model-policy-toggle" @click="toggleEnabled">
        <span class="absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white transition-transform" :class="enabled ? 'translate-x-4' : 'translate-x-0'" />
      </button>
    </div>
    <div v-if="enabled" class="space-y-3 rounded-xl border border-gray-200 p-3 dark:border-dark-600">
      <div class="flex items-center justify-between gap-2 text-xs">
        <span class="text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.modelPolicySelected', { count: selected.length }) }}</span>
        <button type="button" class="text-primary-600 disabled:opacity-40 dark:text-primary-400" :disabled="disabled || selected.length === 0" data-test="model-policy-clear" @click="publish([])">{{ t('custom.keyRouting.modelPolicyClear') }}</button>
      </div>
      <div v-if="selected.length" class="flex flex-wrap gap-2">
        <span v-for="model in selected" :key="model" class="inline-flex max-w-full items-center gap-1 rounded-md bg-primary-50 px-2 py-1 text-xs text-primary-700 dark:bg-primary-900/20 dark:text-primary-300" data-test="model-policy-chip">
          <span class="min-w-0 break-all">{{ model }}</span>
          <button type="button" class="shrink-0 rounded px-1 hover:bg-primary-100 disabled:opacity-40 dark:hover:bg-primary-900/40" :aria-label="t('custom.keyRouting.modelPolicyRemove', { model })" :disabled="disabled" :data-model="model" data-test="model-policy-remove" @click="publish(selected.filter((item) => item !== model))">×</button>
        </span>
      </div>
      <input v-model="search" class="input" type="search" :placeholder="t('custom.keyRouting.modelPolicySearch')" :aria-label="t('custom.keyRouting.modelPolicySearch')" :disabled="disabled" data-test="model-policy-search" />
      <div class="flex justify-end">
        <button type="button" class="text-xs text-primary-600 disabled:opacity-40 dark:text-primary-400" :disabled="disabled || filtered.length === 0" data-test="model-policy-select-results" @click="selectResults">{{ t('custom.keyRouting.modelPolicySelectResults') }}</button>
      </div>
      <div v-if="filtered.length" class="max-h-40 space-y-1 overflow-y-auto rounded-lg border border-gray-100 p-2 dark:border-dark-700">
        <label v-for="model in filtered" :key="model" class="flex cursor-pointer items-start gap-2 rounded px-1 py-1 text-sm text-gray-700 hover:bg-gray-50 dark:text-gray-200 dark:hover:bg-dark-700">
          <input type="checkbox" class="mt-0.5 shrink-0" :checked="selected.includes(model)" :disabled="disabled" :value="model" data-test="model-policy-candidate" @change="toggleModel(model)" />
          <span class="min-w-0 break-all">{{ model }}</span>
        </label>
      </div>
      <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.modelPolicyNoCandidates') }}</p>
      <div class="flex gap-2">
        <input v-model="manual" type="text" class="input min-w-0 flex-1" :placeholder="t('custom.keyRouting.modelPolicyManual')" :aria-label="t('custom.keyRouting.modelPolicyManual')" :disabled="disabled" data-test="model-policy-manual" @keydown.enter.prevent="addManual" />
        <button type="button" class="btn btn-secondary shrink-0" :disabled="disabled || !manual.trim()" data-test="model-policy-add" @click="addManual">{{ t('custom.keyRouting.modelPolicyAdd') }}</button>
      </div>
      <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ t(`custom.keyRouting.${error}`) }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KeyModelPolicy } from '@/types'
import { normalizeKeyModelPolicy, validateKeyModelPolicy } from './keyModelPolicy'

const props = withDefaults(defineProps<{ modelValue: KeyModelPolicy | null; models?: string[]; disabled?: boolean }>(), { models: () => [], disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: KeyModelPolicy | null] }>()
const { t } = useI18n()
const selected = ref<string[]>([...(props.modelValue?.models ?? [])])
const search = ref('')
const manual = ref('')
const inputError = ref('')
const enabled = computed(() => props.modelValue?.enabled === true)
watch(() => props.modelValue, (value) => {
  // A disabled parent's null echo must not discard this dialog's draft.
  if (value?.enabled) selected.value = [...value.models]
}, { deep: true })
const validExactID = (model: string) => validateKeyModelPolicy({ enabled: true, models: [model] })
const filtered = computed(() => [...new Set(props.models.map((model) => model.trim()).filter(validExactID))]
  .filter((model) => model.toLocaleLowerCase().includes(search.value.trim().toLocaleLowerCase())))
const error = computed(() => inputError.value || (enabled.value && !validateKeyModelPolicy({ enabled: true, models: selected.value }) ? 'modelPolicyValidation' : ''))

function publish(models: string[]) {
  if (props.disabled) return
  const normalized = normalizeKeyModelPolicy({ enabled: true, models })!
  if (normalized.models.length > 200) { inputError.value = 'modelPolicyValidation'; return }
  inputError.value = ''
  selected.value = [...normalized.models]
  emit('update:modelValue', normalized)
}
function toggleEnabled() {
  if (props.disabled) return
  inputError.value = ''
  emit('update:modelValue', enabled.value ? null : { enabled: true, models: [...selected.value] })
}
function toggleModel(model: string) {
  publish(selected.value.includes(model) ? selected.value.filter((item) => item !== model) : [...selected.value, model])
}
function selectResults() { publish([...selected.value, ...filtered.value]) }
function addManual() {
  if (props.disabled) return
  const model = manual.value.trim()
  if (!validExactID(model)) { inputError.value = 'modelPolicyValidation'; return }
  if (selected.value.includes(model)) { inputError.value = 'modelPolicyDuplicate'; return }
  if (selected.value.length >= 200) { inputError.value = 'modelPolicyValidation'; return }
  publish([...selected.value, model])
  manual.value = ''
}
</script>
