<template>
  <BaseDialog :show="show" :title="t('custom.gatewayResilience.title')" width="wide" @close="close">
    <div class="space-y-4" data-testid="group-failure-policy">
      <p v-if="group" class="font-medium text-gray-900 dark:text-white">{{ group.name }}</p>
      <p v-if="!canRead" role="alert" class="text-sm text-red-600">{{ t('custom.gatewayResilience.noAccess') }}</p>
      <template v-else>
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('custom.gatewayResilience.description') }}</p>
        <div class="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
          <p>{{ t('custom.gatewayResilience.accountScope') }}</p>
          <p>{{ t('custom.gatewayResilience.safety') }}</p>
        </div>
        <p v-if="!canWrite" data-testid="failure-policy-readonly" class="text-sm text-gray-500">{{ t('custom.gatewayResilience.readOnly') }}</p>
        <p v-if="loading" role="status">{{ t('custom.gatewayResilience.loading') }}</p>
        <div v-else-if="loadError" role="alert" class="space-y-2 text-sm text-red-600">
          <p>{{ t('custom.gatewayResilience.loadError') }}</p>
          <button type="button" class="btn btn-secondary" data-testid="failure-policy-reload" @click="loadPolicy">{{ t('custom.gatewayResilience.retry') }}</button>
        </div>
        <template v-else-if="loaded">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.gatewayResilience.draftHint') }}</p>
          <fieldset
            v-for="kind in failureKinds"
            :key="kind"
            :disabled="!canWrite || saving"
            :data-rule="kind"
            class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
          >
            <legend class="px-1 text-sm font-medium text-gray-900 dark:text-white">{{ t(`custom.gatewayResilience.${kind}`) }}</legend>
            <div class="grid gap-3 sm:grid-cols-3">
              <label v-for="field in fields" :key="field.key" class="block text-sm text-gray-700 dark:text-gray-300">
                <span class="mb-1 block">{{ t(`custom.gatewayResilience.${field.label}`) }}</span>
                <input
                  v-model="draft[kind][field.key]"
                  type="text"
                  inputmode="numeric"
                  :data-testid="`failure-${kind}-${field.key}`"
                  :aria-invalid="!validField(fieldValue(kind, field.key), ruleLimits[field.key])"
                  class="input w-full"
                  @blur="restoreEmptyField(kind, field.key)"
                />
                <span v-if="field.key !== 'threshold' && validField(fieldValue(kind, field.key), ruleLimits[field.key])" class="mt-1 block text-xs text-gray-500">{{ duration(fieldValue(kind, field.key)) }}</span>
              </label>
            </div>
            <p v-if="!validDraftRule(kind)" role="alert" class="text-xs text-red-600">{{ t('custom.gatewayResilience.validation') }}</p>
          </fieldset>
        </template>
        <p v-if="saveError" role="alert" class="text-sm text-red-600">{{ t('custom.gatewayResilience.saveError') }}</p>
      </template>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" data-testid="failure-policy-close" @click="close">{{ t('custom.gatewayResilience.close') }}</button>
      <button v-if="canRead && canWrite" type="button" class="btn btn-primary" data-testid="failure-policy-save" :disabled="!canSave" @click="save">
        {{ t(saving ? 'custom.gatewayResilience.saving' : 'custom.gatewayResilience.save') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
import type { AdminGroup } from '@/types'
import {
  failureKinds, getGroupFailurePolicy, putGroupFailurePolicy, ruleDefaults, ruleLimits,
  type FailureKind, type GroupFailurePolicy, type GroupFailureRule
} from './groupFailurePolicy'

const props = defineProps<{ show: boolean; group: Pick<AdminGroup, 'id' | 'name'> | null }>()
const emit = defineEmits<{ close: []; success: [] }>()
const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const canRead = computed(() => auth.canOperator('groups.read'))
const canWrite = computed(() => auth.canOperator('groups.write'))

interface DraftRule {
  threshold: string | number
  window_seconds: string | number
  cooldown_seconds: string | number
}

const fields = [
  { key: 'threshold', label: 'threshold' },
  { key: 'window_seconds', label: 'window' },
  { key: 'cooldown_seconds', label: 'cooldown' }
] as const

function createDraft(policy: GroupFailurePolicy = { rules: {} }): Record<FailureKind, DraftRule> {
  return Object.fromEntries(failureKinds.map(kind => [kind, {
    ...(policy.rules[kind] ?? ruleDefaults[kind])
  }])) as Record<FailureKind, DraftRule>
}

const draft = ref(createDraft())
const initial = ref('')
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const loadError = ref(false)
const saveError = ref(false)
let version = 0
let controller: AbortController | null = null

function invalidate() {
  version++
  controller?.abort()
  controller = null
}

function validField(value: string | number, max: number): boolean {
  return String(value).trim() !== '' && Number.isInteger(Number(value)) && Number(value) >= 1 && Number(value) <= max
}

function fieldValue(kind: FailureKind, field: keyof GroupFailureRule): string | number {
  const value = draft.value[kind][field]
  return String(value).trim() === '' ? ruleDefaults[kind][field] : value
}

function restoreEmptyField(kind: FailureKind, field: keyof GroupFailureRule) {
  draft.value[kind][field] = fieldValue(kind, field)
}

function validDraftRule(kind: FailureKind): boolean {
  return fields.every(field => validField(fieldValue(kind, field.key), ruleLimits[field.key]))
}

function policySignature(policy: GroupFailurePolicy): string {
  // A server may return a different key order. Compare only explicit saved
  // rules so missing defaults are a real, savable change rather than a no-op.
  return JSON.stringify({ rules: Object.fromEntries(failureKinds
    .filter(kind => policy.rules[kind])
    .map(kind => [kind, policy.rules[kind]])) })
}

function payload(): GroupFailurePolicy {
  const rules: GroupFailurePolicy['rules'] = {}
  for (const kind of failureKinds) {
    rules[kind] = {
      threshold: Number(fieldValue(kind, 'threshold')),
      window_seconds: Number(fieldValue(kind, 'window_seconds')),
      cooldown_seconds: Number(fieldValue(kind, 'cooldown_seconds'))
    }
  }
  return { rules }
}

const canSave = computed(() => props.show && !!props.group && canRead.value && canWrite.value && loaded.value && !loading.value && !loadError.value && !saving.value
  && failureKinds.every(validDraftRule)
  && JSON.stringify(payload()) !== initial.value)

function duration(value: string | number): string {
  const seconds = Number(value)
  return seconds % 60 === 0
    ? t('custom.gatewayResilience.minutes', { value: seconds / 60 })
    : t('custom.gatewayResilience.seconds', { value: seconds })
}

async function loadPolicy() {
  invalidate()
  loaded.value = false
  loading.value = false
  saving.value = false
  loadError.value = false
  saveError.value = false
  draft.value = createDraft()
  initial.value = ''
  if (!props.show || !props.group || !canRead.value) return
  const requestVersion = version
  const groupId = props.group.id
  controller = new AbortController()
  loading.value = true
  try {
    const policy = await getGroupFailurePolicy(groupId, controller.signal)
    if (requestVersion !== version) return
    draft.value = createDraft(policy)
    initial.value = policySignature(policy)
    loaded.value = true
  } catch {
    if (requestVersion === version) loadError.value = true
  } finally {
    if (requestVersion === version) loading.value = false
  }
}

async function save() {
  if (!canSave.value || !props.group) return
  const requestVersion = version
  const groupId = props.group.id
  const policy = payload()
  controller = new AbortController()
  saving.value = true
  saveError.value = false
  try {
    await putGroupFailurePolicy(groupId, policy, controller.signal)
    if (requestVersion !== version) return
    initial.value = JSON.stringify(policy)
    app.showSuccess(t('custom.gatewayResilience.saved'))
    emit('success')
    close()
  } catch {
    if (requestVersion === version) saveError.value = true
  } finally {
    if (requestVersion === version) saving.value = false
  }
}

function close() {
  invalidate()
  loaded.value = false
  loading.value = false
  saving.value = false
  emit('close')
}

watch([() => props.show, () => props.group?.id, canRead, canWrite], loadPolicy, { immediate: true })
onUnmounted(invalidate)
</script>
