<template>
  <fieldset class="space-y-2.5 rounded-xl border border-primary-100 bg-primary-50/30 p-3 dark:border-primary-900/50 dark:bg-primary-900/10" :disabled="disabled" :aria-label="t('custom.keyRouting.keyRulesTitle')" data-test="key-route-failure-rules">
    <div class="flex items-center justify-between gap-3" data-test="key-route-failure-rules-toggle">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('custom.keyRouting.keyRulesTitle') }}</h3>
      <div class="flex flex-shrink-0 items-center gap-2">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ statusLabel }}</span>
        <button
          type="button"
          role="switch"
          :aria-label="t('custom.keyRouting.keyRulesOverride')"
          :aria-checked="overrideEnabled"
          class="relative inline-flex h-5 w-9 flex-shrink-0 rounded-full transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 dark:focus:ring-offset-dark-900"
          :class="overrideEnabled ? 'bg-primary-600' : 'bg-gray-300 dark:bg-dark-600'"
          :disabled="disabled"
          data-test="key-route-failure-rules-switch"
          @mousedown.prevent
          @click="toggleOverride"
        >
          <span class="pointer-events-none inline-block h-4 w-4 translate-y-0.5 rounded-full bg-white shadow transition" :class="overrideEnabled ? 'translate-x-[18px]' : 'translate-x-0.5'" />
        </button>
      </div>
    </div>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t(accountScoped ? 'custom.keyRouting.keyRulesAccountDescription' : 'custom.keyRouting.keyRulesDescription') }}</p>

    <button
      type="button"
      class="flex w-full items-center justify-between gap-3 py-1 text-left text-xs text-gray-500 transition-colors hover:text-primary-700 dark:text-gray-400 dark:hover:text-primary-300"
      :aria-expanded="expanded"
      data-test="key-route-rules-toggle-details"
      @mousedown.prevent
      @click="toggleExpanded"
    >
      <span>{{ t('custom.keyRouting.keyRulesSection') }}</span>
      <span>{{ expanded ? t('custom.keyRouting.keyRulesCollapse') : t('custom.keyRouting.keyRulesEdit') }}</span>
    </button>

    <template v-if="expanded && mode === 'inherit'">
      <p class="rounded-lg bg-gray-50 p-3 text-xs leading-5 text-gray-500 dark:bg-dark-800 dark:text-gray-400" data-test="key-route-rules-inherited-hint">
        {{ t(accountScoped ? 'custom.keyRouting.keyRulesAccountInheritedHint' : 'custom.keyRouting.keyRulesInheritedHint') }}
      </p>
    </template>

    <template v-if="expanded && mode !== 'inherit' && mode !== 'disabled'">
      <div v-for="(rule, index) in draftRules" :key="rule.id" class="space-y-2 rounded-lg border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800/70" :data-rule="index">
        <div class="flex items-center justify-between gap-2">
          <span class="text-xs text-gray-700 dark:text-gray-200">{{ t(accountScoped ? 'custom.keyRouting.keyRulesAccountRule' : 'custom.keyRouting.keyRulesRule', { value: index + 1 }) }}</span>
          <button type="button" class="flex-shrink-0 text-xs text-gray-400 transition-colors hover:text-red-600 dark:hover:text-red-400" :disabled="disabled" :data-test="`key-route-rule-remove-${index}`" :data-rule-id="rule.id" @mousedown.prevent @click="removeRule(rule.id)">{{ t('custom.keyRouting.keyRulesRemove') }}</button>
        </div>
        <div class="flex flex-wrap items-center gap-x-2 gap-y-2 text-xs text-gray-600 dark:text-gray-300">
          <span>{{ t('custom.keyRouting.keyRulesType') }}</span>
          <label>
            <span class="sr-only">{{ t('custom.keyRouting.keyRulesType') }}</span>
            <select :value="rule.type" class="input h-8 w-44 px-2 py-1 text-xs" :disabled="disabled" :data-test="`key-route-rule-${index}-type`" @change="changeRuleType(rule, ($event.target as HTMLSelectElement).value)">
              <option v-for="type in KEY_ROUTE_RULE_TYPES" :key="type" :value="type" :disabled="isTypeUsed(type, rule)">{{ t(accountScoped && type === 'rate_limit' ? 'custom.keyRouting.keyRulesAccountRateLimit' : `custom.keyRouting.keyRulesType_${type}`) }}</option>
            </select>
          </label>
          <button v-if="rule.type !== 'status_code'" type="button" class="inline-flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border border-gray-300 text-[10px] text-gray-400 dark:border-dark-500" :title="ruleHelp(rule.type)" :aria-label="ruleHelp(rule.type)">?</button>
          <template v-if="rule.type === 'ttft'">
            <span>{{ t('custom.keyRouting.keyRulesGreaterThan') }}</span>
            <input v-model="rule.first_content_seconds" type="text" inputmode="numeric" class="input h-8 w-14 px-2 py-1 text-xs" :aria-label="t('custom.keyRouting.keyRulesFirstContentSeconds')" :data-test="`key-route-rule-${index}-first_content_seconds`" @blur="restoreEmptyField(rule, 'first_content_seconds')" />
            <span>{{ t('custom.keyRouting.keyRulesSeconds') }}</span>
          </template>
          <template v-if="isSampleRule(rule)">
            <span>{{ t('custom.keyRouting.keyRulesRecent') }}</span>
            <label>
              <span class="sr-only">{{ t('custom.keyRouting.keyRulesSample') }}</span>
              <input v-model="rule.sample_size" type="text" inputmode="numeric" class="input h-8 w-14 px-2 py-1 text-xs" :data-test="`key-route-rule-${index}-sample_size`" @blur="restoreEmptyField(rule, 'sample_size')" />
            </label>
            <span>{{ t(rule.type === 'sla_success_rate' ? 'custom.keyRouting.keyRulesSuccessBelow' : 'custom.keyRouting.keyRulesRequestsWithin') }}</span>
          </template>
          <template v-if="rule.type === 'status_code'">
            <label>
              <span class="sr-only">{{ t('custom.keyRouting.keyRulesStatusCodes') }}</span>
              <input v-model="rule.status_codes_input" type="text" inputmode="numeric" class="input h-8 w-40 px-2 py-1 text-xs" :placeholder="t('custom.keyRouting.keyRulesStatusCodesPlaceholder')" :title="t('custom.keyRouting.keyRulesStatusCodesHint')" :data-test="`key-route-rule-${index}-status-codes`" @blur="normalizeStatusInput(rule)" />
            </label>
            <button type="button" class="inline-flex h-4 w-4 flex-shrink-0 items-center justify-center rounded-full border border-gray-300 text-[10px] text-gray-400 dark:border-dark-500" :title="t('custom.keyRouting.keyRulesStatusCodeHelp')" :aria-label="t('custom.keyRouting.keyRulesStatusCodeHelp')" data-test="key-route-status-code-help">?</button>
            <span>{{ t('custom.keyRouting.keyRulesWithin') }}</span>
            <label>
              <span class="sr-only">{{ t('custom.keyRouting.keyRulesWindow') }}</span>
              <input v-model="rule.window_seconds" type="text" inputmode="numeric" class="input h-8 w-16 px-2 py-1 text-xs" :data-test="`key-route-rule-${index}-window_seconds`" @blur="restoreEmptyField(rule, 'window_seconds')" />
            </label>
            <span>{{ t('custom.keyRouting.keyRulesSecondsWithin') }}</span>
          </template>
          <span v-if="rule.type !== 'sla_success_rate'">{{ t('custom.keyRouting.keyRulesReach') }}</span>
          <label>
            <span class="sr-only">{{ t(rule.type === 'sla_success_rate' ? 'custom.keyRouting.keyRulesSuccessPercent' : rule.type === 'consecutive_failures' ? 'custom.keyRouting.keyRulesConsecutiveThreshold' : 'custom.keyRouting.keyRulesThreshold') }}</span>
            <input v-model="rule.threshold" type="text" inputmode="numeric" class="input h-8 w-14 px-2 py-1 text-xs" :data-test="`key-route-rule-${index}-threshold`" @blur="restoreEmptyField(rule, 'threshold')" />
          </label>
          <span>{{ rule.type === 'sla_success_rate' ? '%' : t('custom.keyRouting.keyRulesTimes') }}</span>
        </div>
        <p v-if="ruleError(rule)" role="alert" class="text-xs text-red-600 dark:text-red-400">{{ ruleError(rule) }}</p>
      </div>
      <p v-if="draftRules.length === 0" class="rounded-lg border border-dashed border-gray-300 p-3 text-center text-xs text-gray-500 dark:border-dark-600">{{ t('custom.keyRouting.keyRulesDisabled') }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <button type="button" class="rounded-lg border border-gray-200 bg-white px-3 py-1.5 text-xs text-gray-600 transition-colors hover:border-primary-300 hover:text-primary-700 disabled:opacity-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:text-primary-300" :disabled="disabled || draftRules.length >= KEY_ROUTE_RULE_LIMITS.rules.max" data-test="key-route-rule-add" @mousedown.prevent @click="addRule">+ {{ t('custom.keyRouting.keyRulesAdd') }}</button>
      </div>
      <div class="flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
        <label :for="cooldownId">{{ t('custom.keyRouting.keyRulesCooldownLabel') }}</label>
        <input :id="cooldownId" v-model="globalCooldown" type="text" inputmode="numeric" class="input h-8 w-20 px-2 py-1 text-xs" :class="mixedCooldown && 'w-48'" :aria-label="t('custom.keyRouting.keyRulesCooldown')" :placeholder="mixedCooldown ? t('custom.keyRouting.keyRulesCooldownMixed') : ''" data-test="key-route-global-cooldown" @blur="restoreGlobalCooldown" />
        <span>{{ t('custom.keyRouting.keyRulesSeconds') }}</span>
      </div>
      <p v-if="mixedCooldown" class="text-xs text-gray-500 dark:text-gray-400" data-test="key-route-cooldown-mixed">{{ t('custom.keyRouting.keyRulesCooldownMixedHint') }}</p>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(accountScoped ? 'custom.keyRouting.keyRulesAccountScopeHint' : 'custom.keyRouting.keyRulesScopeHint') }}</p>
      <p v-if="draftRules.some(rule => rule.type === 'ttft')" class="text-xs leading-5 text-gray-500 dark:text-gray-400" data-test="key-route-ttft-safety">{{ t(accountScoped ? 'custom.keyRouting.keyRulesAccountTTFTSafety' : 'custom.keyRouting.keyRulesTTFTSafety') }}</p>
    </template>

    <template v-if="expanded && mode === 'disabled'">
      <p class="rounded-lg bg-gray-50 p-3 text-xs leading-5 text-gray-500 dark:bg-dark-800 dark:text-gray-400" data-test="key-route-rules-disabled-hint">
        {{ t('custom.keyRouting.keyRulesDisableHint') }}
      </p>
    </template>

    <button
      v-if="expanded && mode !== 'inherit'"
      type="button"
      class="text-xs text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
      :disabled="disabled"
      data-test="key-route-rules-inherit"
      @mousedown.prevent
      @click="inheritGroupRules"
    >
      {{ t('custom.keyRouting.keyRulesInheritHint') }}
    </button>
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { KeyRouteFailurePolicy, KeyRouteFailureRule, KeyRouteFailureRuleType, KeyRouteScope } from '@/types'
import { KEY_ROUTE_RULE_DEFAULTS, KEY_ROUTE_RULE_LIMITS, KEY_ROUTE_RULE_TYPES, KEY_ROUTE_USER_RULE_DEFAULTS, parseKeyRouteStatusCodes, validateKeyRouteFailurePolicy } from './keyRouting'

const props = withDefaults(defineProps<{ modelValue: KeyRouteFailurePolicy | null; disabled?: boolean; scope?: KeyRouteScope }>(), { disabled: false, scope: 'group_v1' })
const emit = defineEmits<{ 'update:modelValue': [value: KeyRouteFailurePolicy | null] }>()
const { t } = useI18n()
const accountScoped = computed(() => props.scope === 'account_v2')
const ruleHelp = (type: KeyRouteFailureRuleType) => t(`custom.keyRouting.${accountScoped.value ? 'keyRulesAccountHint' : 'keyRulesHint'}_${type}`)

interface DraftRule {
  id: number
  type: KeyRouteFailureRuleType
  legacyStatusCode: boolean
  status_codes_input: string
  threshold: string | number
  window_seconds: string | number
  sample_size: string | number
  first_content_seconds: string | number
  cooldown_seconds: string | number
}

let nextId = 1
const draftRules = ref<DraftRule[]>([])
const mode = ref<'inherit' | 'override' | 'disabled'>('inherit')
const expanded = ref(false)
const cooldownId = useId()
const mixedCooldown = computed(() => new Set(draftRules.value.map((rule) => String(rule.cooldown_seconds))).size > 1)
// A single input matches the editing UX without changing the wire protocol.
// Legacy policies with per-rule values stay untouched until explicitly edited.
const globalCooldown = computed<string | number>({
  get: () => mixedCooldown.value ? '' : (draftRules.value[0]?.cooldown_seconds ?? KEY_ROUTE_USER_RULE_DEFAULTS.cooldown_seconds),
  set: (value) => { for (const rule of draftRules.value) rule.cooldown_seconds = value }
})
const overrideEnabled = computed(() => mode.value === 'override')
const statusLabel = computed(() => {
  if (mode.value === 'override') return t('custom.keyRouting.keyRulesEnabled')
  if (mode.value === 'disabled') return t('custom.keyRouting.keyRulesDisabled')
  return t('custom.keyRouting.keyRulesInherited')
})
let syncing = false

function fromRule(rule: KeyRouteFailureRule): DraftRule {
  return { id: nextId++, type: rule.type ?? 'status_code', legacyStatusCode: rule.type === undefined, status_codes_input: rule.status_codes?.join(', ') ?? '', threshold: rule.threshold, window_seconds: rule.window_seconds ?? KEY_ROUTE_RULE_DEFAULTS.window_seconds, sample_size: rule.sample_size ?? KEY_ROUTE_USER_RULE_DEFAULTS.sample_size, first_content_seconds: rule.first_content_seconds ?? 15, cooldown_seconds: rule.cooldown_seconds }
}

function defaultRule(type: KeyRouteFailureRuleType = 'consecutive_failures'): DraftRule {
  return fromRule(type === 'status_code'
    ? { type, status_codes: [502, 503, 504, 529], ...KEY_ROUTE_RULE_DEFAULTS }
    : { type, ...KEY_ROUTE_USER_RULE_DEFAULTS, ...(type === 'sla_success_rate' ? { threshold: 90 } : {}), ...(type === 'ttft' ? { first_content_seconds: 15 } : {}) })
}

function toRule(rule: DraftRule): KeyRouteFailureRule {
  const common = { ...(rule.legacyStatusCode && rule.type === 'status_code' ? {} : { type: rule.type }), threshold: Number(rule.threshold), cooldown_seconds: Number(rule.cooldown_seconds) }
  if (rule.type === 'status_code') return { ...common, status_codes: parseKeyRouteStatusCodes(rule.status_codes_input) ?? [], window_seconds: Number(rule.window_seconds) }
  if (isSampleRule(rule)) return { ...common, sample_size: Number(rule.sample_size), ...(rule.type === 'ttft' ? { first_content_seconds: Number(rule.first_content_seconds) } : {}) }
  return common
}

function currentPolicy(): KeyRouteFailurePolicy {
  return { rules: draftRules.value.map(toRule) }
}

function syncFromModel(policy: KeyRouteFailurePolicy | null) {
  syncing = true
  mode.value = policy === null ? 'inherit' : policy.rules.length ? 'override' : 'disabled'
  draftRules.value = policy?.rules.map(fromRule) ?? []
  syncing = false
}

watch(() => props.modelValue, (policy) => {
  // Parent v-model echoes must not reset active inputs or discard partial text.
  // Turning the switch off emits an empty policy, but its echo must leave the
  // editor draft intact so switching back on restores what the user entered.
  if (policy !== null && policy.rules.length === 0 && mode.value === 'disabled') return
  if (policy !== null && mode.value !== 'inherit' && JSON.stringify(policy) === JSON.stringify(currentPolicy())) return
  syncFromModel(policy)
}, { immediate: true, deep: true })
watch(draftRules, () => {
  if (!overrideEnabled.value || syncing) return
  const next = currentPolicy()
  if (JSON.stringify(next) !== JSON.stringify(props.modelValue)) emit('update:modelValue', next)
}, { deep: true })

function toggleOverride() {
  if (props.disabled) return
  if (mode.value === 'override') {
    mode.value = 'disabled'
    emit('update:modelValue', { rules: [] })
    return
  }
  mode.value = 'override'
  // Seed only an empty draft. Persisted policies and a temporarily disabled
  // local draft must never be replaced by the reference UI's starting values.
  if (!draftRules.value.length) draftRules.value = [
    { type: 'ttft', first_content_seconds: 15, threshold: 3, sample_size: 20, cooldown_seconds: 120 },
    { type: 'sla_success_rate', threshold: 90, sample_size: 20, cooldown_seconds: 120 },
    { type: 'consecutive_failures', threshold: 5, cooldown_seconds: 120 },
    { type: 'timeout', threshold: 10, sample_size: 20, cooldown_seconds: 120 },
    { type: 'rate_limit', threshold: 10, sample_size: 20, cooldown_seconds: 120 }
  ].map((rule) => fromRule(rule as KeyRouteFailureRule))
  emitCurrent()
}

function inheritGroupRules() {
  if (props.disabled) return
  mode.value = 'inherit'
  draftRules.value = []
  emit('update:modelValue', null)
}

function toggleExpanded() {
  expanded.value = !expanded.value
}

function emitCurrent() {
  emit('update:modelValue', currentPolicy())
}

function addRule() {
  if (props.disabled || draftRules.value.length >= KEY_ROUTE_RULE_LIMITS.rules.max) return
  const type = KEY_ROUTE_RULE_TYPES.find((candidate) => !isTypeUsed(candidate)) ?? 'status_code'
  const next = defaultRule(type)
  if (!mixedCooldown.value) next.cooldown_seconds = globalCooldown.value
  if (type === 'status_code') {
    const used = new Set(draftRules.value.filter((rule) => rule.type === 'status_code')
      .flatMap((rule) => parseKeyRouteStatusCodes(rule.status_codes_input) ?? []))
    // Do not insert duplicate codes just by clicking Add. A further status
    // rule starts empty when all suggested codes are already in use.
    next.status_codes_input = (parseKeyRouteStatusCodes(next.status_codes_input) ?? [])
      .filter((code) => !used.has(code)).join(', ')
  }
  draftRules.value.push(next)
}

function removeRule(id: number) {
  if (props.disabled) return
  // UI positions are presentation only; remove the selected draft identity.
  // The button also keeps focus on mousedown so blur validation cannot move
  // a different remove button under the pointer before mouseup/click.
  const index = draftRules.value.findIndex((rule) => rule.id === id)
  if (index < 0) return
  draftRules.value.splice(index, 1)
  if (!draftRules.value.length) {
    mode.value = 'disabled'
    emit('update:modelValue', { rules: [] })
  }
}

function normalizeStatusInput(rule: DraftRule) {
  const parsed = parseKeyRouteStatusCodes(rule.status_codes_input)
  if (parsed) rule.status_codes_input = parsed.join(', ')
}

function isSampleRule(rule: DraftRule): boolean {
  return rule.type === 'timeout' || rule.type === 'rate_limit' || rule.type === 'sla_success_rate' || rule.type === 'ttft'
}

function isTypeUsed(type: KeyRouteFailureRuleType, current?: DraftRule): boolean {
  return type !== 'status_code' && draftRules.value.some((rule) => rule !== current && rule.type === type)
}

function changeRuleType(rule: DraftRule, value: string) {
  if (props.disabled || !KEY_ROUTE_RULE_TYPES.includes(value as KeyRouteFailureRuleType) || isTypeUsed(value as KeyRouteFailureRuleType, rule)) return
  const next = defaultRule(value as KeyRouteFailureRuleType)
  Object.assign(rule, next, { id: rule.id, cooldown_seconds: rule.cooldown_seconds })
}

function restoreGlobalCooldown() {
  if (!mixedCooldown.value && String(globalCooldown.value).trim() === '') globalCooldown.value = KEY_ROUTE_USER_RULE_DEFAULTS.cooldown_seconds
}

function restoreEmptyField(rule: DraftRule, field: 'threshold' | 'window_seconds' | 'sample_size' | 'cooldown_seconds' | 'first_content_seconds') {
  if (String(rule[field]).trim() !== '') return
  const defaults = defaultRule(rule.type)
  rule[field] = defaults[field]
}

function ruleError(rule: DraftRule): string {
  // Keep validation beside the affected card. A partial input in one rule
  // must not add/remove error rows in every other rule while it is edited.
  if (rule.type === 'status_code' && !parseKeyRouteStatusCodes(rule.status_codes_input)) return t('custom.keyRouting.keyRulesValidation')
  if (isSampleRule(rule) && rule.type !== 'sla_success_rate' && Number(rule.threshold) > Number(rule.sample_size)) return t('custom.keyRouting.keyRulesSampleValidation')
  if (validateKeyRouteFailurePolicy({ rules: [toRule(rule)] })) return t('custom.keyRouting.keyRulesValidation')
  if (isTypeUsed(rule.type, rule)) return t('custom.keyRouting.keyRulesDuplicate')
  if (rule.type === 'status_code') {
    const codes = new Set(parseKeyRouteStatusCodes(rule.status_codes_input) ?? [])
    const duplicate = draftRules.value.some((other) => other !== rule && other.type === 'status_code'
      && (parseKeyRouteStatusCodes(other.status_codes_input) ?? []).some((code) => codes.has(code)))
    if (duplicate) return t('custom.keyRouting.keyRulesDuplicate')
  }
  return ''
}
</script>
