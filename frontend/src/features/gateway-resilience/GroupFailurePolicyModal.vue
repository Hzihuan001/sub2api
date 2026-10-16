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
          <div class="space-y-3">
            <fieldset
              v-for="(rule, index) in draftRules"
              :key="rule.id"
              :disabled="!canWrite || saving"
              :data-rule="index"
              class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600"
            >
              <legend class="flex items-center gap-2 px-1 text-sm font-medium text-gray-900 dark:text-white">
                {{ t('custom.gatewayResilience.rule', { value: index + 1 }) }}
                <button
                  type="button"
                  class="inline-flex h-5 w-5 items-center justify-center rounded-full border border-gray-400 text-xs text-gray-500 hover:text-gray-900 dark:hover:text-white"
                  :aria-label="t('custom.gatewayResilience.statusCodeHelp')"
                  :title="t('custom.gatewayResilience.statusCodeHelp')"
                  data-testid="failure-status-help"
                >?</button>
                <button v-if="canWrite" type="button" class="ml-auto text-xs text-gray-500 hover:text-red-600" :data-testid="`failure-rule-remove-${index}`" @click="removeRule(index)">
                  {{ t('custom.gatewayResilience.remove') }}
                </button>
              </legend>
              <label class="block text-sm text-gray-700 dark:text-gray-300">
                <span class="mb-1 flex items-center gap-1">
                  {{ t('custom.gatewayResilience.statusCodes') }}
                  <span class="text-xs text-gray-500">{{ t('custom.gatewayResilience.statusCodesHint') }}</span>
                </span>
                <input
                  v-model="rule.status_codes_input"
                  type="text"
                  inputmode="numeric"
                  :data-testid="`failure-rule-${index}-status-codes`"
                  :aria-invalid="!validStatusInput(rule)"
                  class="input w-full"
                  :placeholder="t('custom.gatewayResilience.statusCodesPlaceholder')"
                  @blur="normalizeStatusInput(rule)"
                />
              </label>
              <p v-if="statusError(rule)" role="alert" class="text-xs text-red-600">{{ statusError(rule) }}</p>
              <div class="grid gap-3 sm:grid-cols-3">
                <label v-for="field in fields" :key="field.key" class="block text-sm text-gray-700 dark:text-gray-300">
                  <span class="mb-1 block">{{ t(`custom.gatewayResilience.${field.label}`) }}</span>
                  <input
                    v-model="rule[field.key]"
                    type="text"
                    inputmode="numeric"
                    :data-testid="`failure-rule-${index}-${field.key}`"
                    :aria-invalid="!validField(rule[field.key], field.key)"
                    class="input w-full"
                    @blur="restoreEmptyField(rule, field.key)"
                  />
                  <span v-if="field.key !== 'threshold' && validField(rule[field.key], field.key)" class="mt-1 block text-xs text-gray-500">{{ duration(rule[field.key]) }}</span>
                </label>
              </div>
              <p v-if="!validDraftRule(rule) && !statusError(rule)" role="alert" class="text-xs text-red-600">{{ t('custom.gatewayResilience.validation') }}</p>
            </fieldset>
          </div>
          <button v-if="canWrite && !saving" type="button" class="btn btn-secondary" data-testid="failure-policy-add" @click="addRule">
            + {{ t('custom.gatewayResilience.addRule') }}
          </button>
          <p v-if="draftRules.length === 0" class="rounded-lg border border-dashed border-gray-300 p-4 text-center text-sm text-gray-500">{{ t('custom.gatewayResilience.noRules') }}</p>
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
  getGroupFailurePolicy, isValidStatusCode, putGroupFailurePolicy, ruleDefaults, ruleLimits,
  type GroupFailurePolicy, type GroupFailureRule
} from './groupFailurePolicy'

const props = defineProps<{ show: boolean; group: Pick<AdminGroup, 'id' | 'name'> | null }>()
const emit = defineEmits<{ close: []; success: [] }>()
const { t } = useI18n()
const auth = useAuthStore()
const app = useAppStore()
const canRead = computed(() => auth.canOperator('groups.read'))
const canWrite = computed(() => auth.canOperator('groups.write'))

interface DraftRule {
  id: number
  status_codes_input: string
  threshold: string | number
  window_seconds: string | number
  cooldown_seconds: string | number
}

const fields = [
  { key: 'threshold', label: 'threshold' },
  { key: 'window_seconds', label: 'window' },
  { key: 'cooldown_seconds', label: 'cooldown' }
] as const

let nextRuleId = 1
function fromRule(rule: GroupFailureRule): DraftRule {
  return { id: nextRuleId++, status_codes_input: rule.status_codes.join(', '), threshold: rule.threshold, window_seconds: rule.window_seconds, cooldown_seconds: rule.cooldown_seconds }
}
function emptyRule(): DraftRule {
  return fromRule({ status_codes: [], ...ruleDefaults })
}
function createDraft(policy: GroupFailurePolicy = { rules: [] }): DraftRule[] {
  return policy.rules.map(fromRule)
}

const draftRules = ref<DraftRule[]>([])
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

function parseStatusCodes(value: string): number[] | null {
  const tokens = value.split(/[\s,，]+/).map(token => token.trim()).filter(Boolean)
  if (!tokens.length || tokens.length > ruleLimits.status_codes.max) return null
  const codes = tokens.map(Number)
  if (codes.some(code => !Number.isInteger(code) || !isValidStatusCode(code))) return null
  return new Set(codes).size === codes.length ? codes : null
}

function validField(value: string | number, field: keyof typeof ruleLimits): boolean {
  const limit = ruleLimits[field]
  if (!('min' in limit)) return false
  return String(value).trim() !== '' && Number.isInteger(Number(value)) && Number(value) >= limit.min && Number(value) <= limit.max
}

function validStatusInput(rule: DraftRule): boolean {
  return parseStatusCodes(rule.status_codes_input) !== null
}

const codeCounts = computed(() => {
  const counts = new Map<number, number>()
  for (const rule of draftRules.value) {
    for (const code of parseStatusCodes(rule.status_codes_input) ?? []) counts.set(code, (counts.get(code) ?? 0) + 1)
  }
  return counts
})

function statusError(rule: DraftRule): string {
  const parsed = parseStatusCodes(rule.status_codes_input)
  if (!parsed) return t('custom.gatewayResilience.statusCodesValidation')
  if (parsed.some(code => (codeCounts.value.get(code) ?? 0) > 1)) return t('custom.gatewayResilience.statusCodesDuplicate')
  return ''
}

function validDraftRule(rule: DraftRule): boolean {
  return !statusError(rule) && validField(rule.threshold, 'threshold') && validField(rule.window_seconds, 'window_seconds') && validField(rule.cooldown_seconds, 'cooldown_seconds')
}

function normalizeStatusInput(rule: DraftRule) {
  const parsed = parseStatusCodes(rule.status_codes_input)
  if (parsed) rule.status_codes_input = parsed.join(', ')
}

function restoreEmptyField(rule: DraftRule, field: keyof Pick<DraftRule, 'threshold' | 'window_seconds' | 'cooldown_seconds'>) {
  if (String(rule[field]).trim() === '') rule[field] = ruleDefaults[field]
}

function payload(): GroupFailurePolicy {
  return { rules: draftRules.value.map(rule => ({
    status_codes: parseStatusCodes(rule.status_codes_input) ?? [],
    threshold: Number(rule.threshold),
    window_seconds: Number(rule.window_seconds),
    cooldown_seconds: Number(rule.cooldown_seconds)
  })) }
}

const canSave = computed(() => props.show && !!props.group && canRead.value && canWrite.value && loaded.value && !loading.value && !loadError.value && !saving.value && draftRules.value.every(validDraftRule)
  && JSON.stringify(payload()) !== initial.value)

function duration(value: string | number): string {
  const seconds = Number(value)
  return seconds % 60 === 0 ? t('custom.gatewayResilience.minutes', { value: seconds / 60 }) : t('custom.gatewayResilience.seconds', { value: seconds })
}

function addRule() { draftRules.value.push(emptyRule()) }
function removeRule(index: number) { draftRules.value.splice(index, 1) }

async function loadPolicy() {
  invalidate(); loaded.value = false; loading.value = false; saving.value = false; loadError.value = false; saveError.value = false
  draftRules.value = []; initial.value = ''
  if (!props.show || !props.group || !canRead.value) return
  const requestVersion = version; const groupId = props.group.id; controller = new AbortController(); loading.value = true
  try {
    const policy = await getGroupFailurePolicy(groupId, controller.signal)
    if (requestVersion !== version) return
    draftRules.value = createDraft(policy); initial.value = JSON.stringify(policy); loaded.value = true
  } catch { if (requestVersion === version) loadError.value = true } finally { if (requestVersion === version) loading.value = false }
}

async function save() {
  if (!canSave.value || !props.group) return
  const requestVersion = version; const groupId = props.group.id; const policy = payload(); controller = new AbortController(); saving.value = true; saveError.value = false
  try {
    await putGroupFailurePolicy(groupId, policy, controller.signal)
    if (requestVersion !== version) return
    initial.value = JSON.stringify(policy); app.showSuccess(t('custom.gatewayResilience.saved')); emit('success'); close()
  } catch { if (requestVersion === version) saveError.value = true } finally { if (requestVersion === version) saving.value = false }
}

function close() { invalidate(); loaded.value = false; loading.value = false; saving.value = false; emit('close') }
watch([() => props.show, () => props.group?.id, canRead, canWrite], loadPolicy, { immediate: true })
onUnmounted(invalidate)
</script>
