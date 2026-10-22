import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AdminUserApiKeyEditor from '../AdminUserApiKeyEditor.vue'
import type { AdminUser, ApiKey, Group, KeyRouteFailurePolicy } from '@/types'
import type { ApiKeyEditorForm } from '@/features/key-editor/apiKeyEditor'

const { updateKey, getModels, showError, showSuccess } = vi.hoisted(() => ({
  updateKey: vi.fn(), getModels: vi.fn(), showError: vi.fn(), showSuccess: vi.fn()
}))
vi.mock('@/api/admin', () => ({ adminAPI: { apiKeys: { updateApiKey: updateKey }, groups: { getEffectiveModels: getModels } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))
vi.mock('@/utils/format', () => ({ formatDateTime: (value: string) => value }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const Editor = defineComponent({
  name: 'ApiKeyEditorDialog', props: ['form', 'show', 'editing', 'selectedKey', 'groups', 'userGroupRates', 'submitting', 'formId'],
  emits: ['update:form', 'submit', 'close', 'resetQuota', 'resetRateLimit'], template: '<div />'
})
const Confirmation = defineComponent({
  name: 'ConfirmDialog', props: ['show', 'title', 'message'], emits: ['confirm', 'cancel'], template: '<div />'
})
const groups = [1, 2, 3].map((id) => ({ id, name: `group-${id}`, status: 'active', subscription_type: 'standard', claude_code_only: false })) as Group[]
const owner = { id: 42, email: 'owner@example.invalid', group_rates: { 2: 0.2 } } as unknown as AdminUser
function fixture(overrides: Partial<ApiKey> = {}): ApiKey {
  return {
    id: 11, user_id: 42, name: 'original', group_id: 1, backup_group_ids: [2], status: 'active',
    key_route_scope: 'group_v1', key_route_failure_policy: null, key_model_policy: null,
    ip_whitelist: [], ip_blacklist: [], quota: 12, quota_used: 4, rate_limit_5h: 1, rate_limit_1d: 2, rate_limit_7d: 3,
    expires_at: '2030-05-01T12:00:00.000Z', ...overrides
  } as ApiKey
}
function open(key = fixture()) {
  return mount(AdminUserApiKeyEditor, { props: { apiKey: key, user: owner, groups }, global: { stubs: { ApiKeyEditorDialog: Editor, ConfirmDialog: Confirmation } } })
}
function draft(wrapper: ReturnType<typeof open>) { return wrapper.findComponent(Editor).props('form') as ApiKeyEditorForm }
async function edit(wrapper: ReturnType<typeof open>, updates: Partial<ApiKeyEditorForm>) {
  wrapper.findComponent(Editor).vm.$emit('update:form', { ...draft(wrapper), ...updates })
  await flushPromises()
}
async function submit(wrapper: ReturnType<typeof open>) {
  wrapper.findComponent(Editor).vm.$emit('submit'); await flushPromises()
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => { resolve = res })
  return { promise, resolve }
}
const policy: KeyRouteFailurePolicy = { rules: [{ status_codes: [502, 503], threshold: 5, window_seconds: 60, cooldown_seconds: 600 }] }

enableAutoUnmount(afterEach)
beforeEach(() => {
  updateKey.mockReset().mockResolvedValue({ api_key: fixture({ name: 'saved' }), auto_granted_group_access: false })
  getModels.mockReset().mockImplementation((id: number) => Promise.resolve([`model-${id}`]))
  showError.mockReset(); showSuccess.mockReset()
})

describe('admin full API key editor', () => {
  it('loads the owner draft and uses the same complete key editor', async () => {
    const key = fixture({ key_route_failure_policy: policy, key_model_policy: { enabled: true, models: ['model-1'] } })
    const wrapper = open(key); await flushPromises()
    const editor = wrapper.findComponent(Editor)
    expect(editor.props()).toMatchObject({ show: true, editing: true, selectedKey: key, userGroupRates: { 2: 0.2 }, formId: 'admin-user-key-form' })
    expect(editor.props('groups').map((group: Group) => group.model_candidates)).toEqual([['model-1'], ['model-2'], undefined])
    expect(draft(wrapper)).toMatchObject({ name: 'original', group_id: 1, backup_group_ids: [2], key_route_scope: 'group_v1', quota: 12, key_route_failure_policy: policy })
    expect(draft(wrapper).backup_group_ids).not.toBe(key.backup_group_ids)
    expect(draft(wrapper).key_route_failure_policy).not.toBe(key.key_route_failure_policy)
    expect(draft(wrapper).key_model_policy).not.toBe(key.key_model_policy)
    expect(updateKey).not.toHaveBeenCalled()
  })

  it('saves ordered groups, failure rules, model restriction and all editable limits for the key owner', async () => {
    const wrapper = open(); await flushPromises()
    await edit(wrapper, { name: '  renamed  ', group_id: 3, backup_group_ids: [1, 2], key_route_scope: 'account_v2', key_route_failure_policy: policy,
      key_model_policy: { enabled: true, models: [' model-3 ', 'model-3', 'model-1'] }, status: 'inactive', enable_ip_restriction: true,
      ip_whitelist: ' 192.0.2.1 \n\n198.51.100.0/24', ip_blacklist: ' 203.0.113.2 ', quota: 30,
      enable_rate_limit: true, rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20,
      enable_expiration: true, expiration_date: '2031-03-05T10:15' })
    await submit(wrapper)
    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(updateKey).toHaveBeenCalledWith(11, {
      name: 'renamed', group_id: 3, backup_group_ids: [1, 2], key_route_scope: 'account_v2', key_route_failure_policy: policy,
      key_model_policy: { enabled: true, models: ['model-3', 'model-1'] }, status: 'inactive',
      ip_whitelist: ['192.0.2.1', '198.51.100.0/24'], ip_blacklist: ['203.0.113.2'], quota: 30,
      expires_at: new Date('2031-03-05T10:15').toISOString(), rate_limit_5h: 5, rate_limit_1d: 10, rate_limit_7d: 20
    })
    expect(wrapper.emitted('saved')?.[0]?.[0]).toMatchObject({ id: 11, name: 'saved' })
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(updateKey.mock.calls[0][1]).not.toHaveProperty('reset_quota')
    expect(updateKey.mock.calls[0][1]).not.toHaveProperty('reset_rate_limit_usage')
  })

  it('preserves legacy scope on ordinary save instead of implicitly upgrading it', async () => {
    const wrapper = open(fixture({ key_route_scope: undefined })); await submit(wrapper)
    expect(updateKey.mock.calls[0][1]).not.toHaveProperty('key_route_scope')
  })

  it('clears hidden routing rules when changed to a single group', async () => {
    const wrapper = open(fixture({ key_route_failure_policy: policy })); await flushPromises()
    await edit(wrapper, { group_id: 2, backup_group_ids: [] }); await submit(wrapper)
    expect(updateKey.mock.calls[0][1]).toMatchObject({ group_id: 2, backup_group_ids: [], key_route_failure_policy: null })
  })

  it('can clear model, IP, expiry and rate restrictions without resetting usage', async () => {
    const wrapper = open(fixture({ key_model_policy: { enabled: true, models: ['model-1'] }, ip_whitelist: ['192.0.2.1'] })); await flushPromises()
    await edit(wrapper, { key_model_policy: null, enable_ip_restriction: false, enable_expiration: false, enable_rate_limit: false, quota: null }); await submit(wrapper)
    expect(updateKey.mock.calls[0][1]).toMatchObject({ key_model_policy: null, ip_whitelist: [], ip_blacklist: [], expires_at: '', quota: 0, rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 })
    expect(updateKey.mock.calls[0][1]).not.toHaveProperty('reset_quota')
  })

  it('blocks duplicate status codes across rules', async () => {
    const wrapper = open(); await flushPromises()
    await edit(wrapper, { key_route_failure_policy: { rules: [policy.rules[0], { ...policy.rules[0], status_codes: [503] }] } }); await submit(wrapper)
    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('custom.keyRouting.keyRulesDuplicate')
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('blocks duplicate groups and preserves the draft', async () => {
    const wrapper = open(); await flushPromises()
    await edit(wrapper, { backup_group_ids: [1] }); await submit(wrapper)
    expect(updateKey).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('custom.keyRouting.duplicate')
    expect(draft(wrapper).backup_group_ids).toEqual([1])
  })

  it('does not rewrite a derived quota-exhausted status on an unrelated edit', async () => {
    const wrapper = open(fixture({ status: 'quota_exhausted' })); await submit(wrapper)
    expect(updateKey.mock.calls[0][1]).not.toHaveProperty('status')
  })

  it('allows explicit reactivation of a derived status through native validation', async () => {
    const wrapper = open(fixture({ status: 'expired' })); await flushPromises()
    await edit(wrapper, { status: 'active', enable_expiration: false }); await submit(wrapper)
    expect(updateKey.mock.calls[0][1]).toMatchObject({ status: 'active', expires_at: '' })
  })

  it('keeps the edited form open when the server rejects a save', async () => {
    updateKey.mockRejectedValueOnce(new Error('target group denied'))
    const wrapper = open(); await flushPromises()
    await edit(wrapper, { name: 'unsaved name' }); await submit(wrapper)
    expect(showError).toHaveBeenCalledWith('target group denied')
    expect(draft(wrapper).name).toBe('unsaved name')
    expect(wrapper.findComponent(Editor).props('submitting')).toBe(false)
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('cancels editing without sending any mutation', async () => {
    const wrapper = open(); await flushPromises()
    await edit(wrapper, { group_id: 3, backup_group_ids: [2, 1] })
    wrapper.findComponent(Editor).vm.$emit('close'); await flushPromises()
    expect(updateKey).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it.each(['quota', 'rate'] as const)('resets %s usage only after explicit confirmation', async (kind) => {
    const wrapper = open(); await flushPromises()
    wrapper.findComponent(Editor).vm.$emit(kind === 'quota' ? 'resetQuota' : 'resetRateLimit'); await flushPromises()
    expect(wrapper.findComponent(Confirmation).props('show')).toBe(true)
    expect(updateKey).not.toHaveBeenCalled()
    wrapper.findComponent(Confirmation).vm.$emit('cancel'); await flushPromises()
    expect(updateKey).not.toHaveBeenCalled()
    wrapper.findComponent(Editor).vm.$emit(kind === 'quota' ? 'resetQuota' : 'resetRateLimit'); await flushPromises()
    wrapper.findComponent(Confirmation).vm.$emit('confirm'); await flushPromises()
    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(updateKey).toHaveBeenCalledWith(11, kind === 'quota' ? { reset_quota: true } : { reset_rate_limit_usage: true })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('prevents duplicate submits and ignores a late response after switching users', async () => {
    const pending = deferred<{ api_key: ApiKey }>()
    updateKey.mockReturnValueOnce(pending.promise)
    const wrapper = open(); await flushPromises()
    wrapper.findComponent(Editor).vm.$emit('submit'); await flushPromises()
    expect(wrapper.findComponent(Editor).props('submitting')).toBe(true)
    wrapper.findComponent(Editor).vm.$emit('submit')
    wrapper.findComponent(Editor).vm.$emit('close'); await flushPromises()
    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
    pending.resolve({ api_key: fixture({ name: 'late response' }) }); await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('keeps the group model snapshot if fetching the live catalog fails', async () => {
    getModels.mockRejectedValue(new Error('catalog unavailable'))
    const snapshotGroups = groups.map((group) => ({ ...group, model_candidates: [`snapshot-${group.id}`] }))
    const wrapper = open(fixture({ key_model_policy: { enabled: true, models: ['manual-model'] } }))
    await wrapper.setProps({ groups: snapshotGroups }); await flushPromises()
    expect(wrapper.findComponent(Editor).props('groups')[0].model_candidates).toEqual(['snapshot-1'])
    expect(draft(wrapper).key_model_policy?.models).toEqual(['manual-model'])
  })
})
