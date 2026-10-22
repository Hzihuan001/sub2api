<template>
  <ApiKeyEditorDialog
    v-model:form="form"
    :show="true"
    :editing="true"
    :selected-key="currentKey"
    :groups="editorGroups"
    :user-group-rates="user.group_rates || {}"
    :submitting="submitting"
    form-id="admin-user-key-form"
    @submit="save"
    @close="close"
    @reset-quota="resetKind = 'quota'"
    @reset-rate-limit="resetKind = 'rate'"
  />
  <ConfirmDialog
    :show="resetKind !== null && !submitting"
    :title="t(resetKind === 'quota' ? 'keys.resetQuotaTitle' : 'keys.resetRateLimitTitle')"
    :message="t(resetKind === 'quota' ? 'keys.resetQuotaConfirmMessage' : 'keys.resetRateLimitConfirmMessage', { name: currentKey.name, used: (currentKey.quota_used || 0).toFixed(4) })"
    @confirm="resetUsage"
    @cancel="resetKind = null"
  />
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AdminUser, ApiKey, Group, UpdateApiKeyRequest } from '@/types'
import type { ApiKeyEditorForm } from '@/features/key-editor/apiKeyEditor'
import ApiKeyEditorDialog from '@/components/keys/ApiKeyEditorDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { cloneKeyModelPolicy, normalizeKeyModelPolicy, validateKeyModelPolicy } from '@/features/key-routing/keyModelPolicy'
import { keyRouteFromAPI, validateKeyRouteFailurePolicy, validateOrderedKeyRouting } from '@/features/key-routing/keyRouting'

const props = defineProps<{ apiKey: ApiKey; user: AdminUser; groups: Group[] }>()
const emit = defineEmits<{ close: []; saved: [key: ApiKey] }>()
const { t } = useI18n()
const app = useAppStore()
const currentKey = ref(props.apiKey)
const submitting = ref(false)
const resetKind = ref<'quota' | 'rate' | null>(null)
let active = true
onBeforeUnmount(() => { active = false })

function localDate(value: string) {
  const date = new Date(value)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}
const key = props.apiKey
const form = ref<ApiKeyEditorForm>({
  name: key.name, group_id: key.group_id, backup_group_ids: [...(key.backup_group_ids ?? [])],
  key_route_scope: key.key_route_scope ?? 'group_v1',
  key_route_failure_policy: key.key_route_failure_policy ? JSON.parse(JSON.stringify(key.key_route_failure_policy)) : null,
  key_model_policy: cloneKeyModelPolicy(key.key_model_policy),
  status: key.status === 'active' ? 'active' : 'inactive',
  use_custom_key: false, custom_key: '',
  enable_ip_restriction: !!(key.ip_whitelist?.length || key.ip_blacklist?.length),
  ip_whitelist: (key.ip_whitelist ?? []).join('\n'), ip_blacklist: (key.ip_blacklist ?? []).join('\n'),
  enable_quota: key.quota > 0, quota: key.quota || null,
  enable_rate_limit: key.rate_limit_5h > 0 || key.rate_limit_1d > 0 || key.rate_limit_7d > 0,
  rate_limit_5h: key.rate_limit_5h || null, rate_limit_1d: key.rate_limit_1d || null, rate_limit_7d: key.rate_limit_7d || null,
  enable_expiration: !!key.expires_at, expiration_preset: 'custom', expiration_date: key.expires_at ? localDate(key.expires_at) : ''
})
const modelCandidates = ref<Record<number, string[]>>({})
const editorGroups = computed(() => props.groups.map((group) => ({ ...group, model_candidates: modelCandidates.value[group.id] ?? group.model_candidates })))
watch(() => keyRouteFromAPI(form.value.group_id, form.value.backup_group_ids), async (ids) => {
  await Promise.all(ids.filter((id) => modelCandidates.value[id] === undefined).map(async (id) => {
    // Catalog failure must not erase an existing model policy; manual IDs remain available.
    try {
      const models = await adminAPI.groups.getEffectiveModels(id)
      if (active) modelCandidates.value[id] = models
    } catch { /* Keep the group snapshot when the catalog is temporarily unavailable. */ }
  }))
}, { immediate: true })

function close() { if (!submitting.value) emit('close') }
function fail(message: string) { app.showError(t(message)) }
async function save() {
  if (submitting.value || !active) return
  const draft = form.value
  if (!draft.name.trim()) return fail('keys.failedToSave')
  const ids = keyRouteFromAPI(draft.group_id, draft.backup_group_ids)
  const routeError = validateOrderedKeyRouting(ids, props.groups)
  if (routeError) return fail(`custom.keyRouting.${routeError}`)
  const policy = ids.length > 1 ? draft.key_route_failure_policy : null
  const ruleError = validateKeyRouteFailurePolicy(policy)
  if (ruleError) return fail(ruleError === 'duplicate' ? 'custom.keyRouting.keyRulesDuplicate' : 'custom.keyRouting.keyRulesValidation')
  if (!validateKeyModelPolicy(draft.key_model_policy)) return fail('custom.keyRouting.modelPolicyValidation')
  const amount = (value: number | null) => Number(value ?? 0)
  const amounts = [draft.quota, ...(draft.enable_rate_limit ? [draft.rate_limit_5h, draft.rate_limit_1d, draft.rate_limit_7d] : [])]
  if (amounts.some((value) => !Number.isFinite(amount(value)) || amount(value) < 0)) return fail('keys.failedToSave')
  const expiresAt = draft.enable_expiration && draft.expiration_date ? new Date(draft.expiration_date) : null
  if (expiresAt && !Number.isFinite(expiresAt.getTime())) return fail('keys.failedToSave')
  const parseIPs = (value: string) => value.split('\n').map((ip) => ip.trim()).filter(Boolean)
  const updates: UpdateApiKeyRequest = {
    name: draft.name.trim(), group_id: draft.group_id ?? 0, backup_group_ids: [...draft.backup_group_ids],
    key_route_failure_policy: policy, key_model_policy: normalizeKeyModelPolicy(draft.key_model_policy),
    ip_whitelist: draft.enable_ip_restriction ? parseIPs(draft.ip_whitelist) : [],
    ip_blacklist: draft.enable_ip_restriction ? parseIPs(draft.ip_blacklist) : [],
    quota: amount(draft.quota),
    expires_at: expiresAt
      ? (key.expires_at && draft.expiration_date === localDate(key.expires_at) ? key.expires_at : expiresAt.toISOString())
      : '',
    rate_limit_5h: draft.enable_rate_limit ? amount(draft.rate_limit_5h) : 0,
    rate_limit_1d: draft.enable_rate_limit ? amount(draft.rate_limit_1d) : 0,
    rate_limit_7d: draft.enable_rate_limit ? amount(draft.rate_limit_7d) : 0
  }
  // Derived expired/exhausted states remain owned by native quota/expiry logic.
  if (!['expired', 'quota_exhausted'].includes(currentKey.value.status) || draft.status === 'active') updates.status = draft.status
  if (draft.key_route_scope !== (currentKey.value.key_route_scope ?? 'group_v1')) updates.key_route_scope = draft.key_route_scope
  submitting.value = true
  try {
    const result = await adminAPI.apiKeys.updateApiKey(key.id, updates)
    if (!active) return
    app.showSuccess(t('keys.keyUpdatedSuccess'))
    emit('saved', result.api_key)
    emit('close')
  } catch (error: any) {
    if (active) app.showError(error?.message || t('keys.failedToSave'))
  } finally { if (active) submitting.value = false }
}
async function resetUsage() {
  if (submitting.value || resetKind.value === null || !active) return
  const kind = resetKind.value
  resetKind.value = null
  submitting.value = true
  try {
    const result = await adminAPI.apiKeys.updateApiKey(key.id, kind === 'quota' ? { reset_quota: true } : { reset_rate_limit_usage: true })
    if (!active) return
    currentKey.value = result.api_key
    if (result.api_key.status === 'active' && ['expired', 'quota_exhausted'].includes(key.status)) form.value.status = 'active'
    emit('saved', result.api_key)
    app.showSuccess(t(kind === 'quota' ? 'keys.quotaResetSuccess' : 'keys.rateLimitResetSuccess'))
  } catch (error: any) { if (active) app.showError(error?.message || t('keys.failedToSave')) }
  finally { if (active) submitting.value = false }
}
</script>
