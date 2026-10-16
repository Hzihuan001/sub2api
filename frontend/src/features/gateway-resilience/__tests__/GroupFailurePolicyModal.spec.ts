import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import GroupFailurePolicyModal from '../GroupFailurePolicyModal.vue'
import { ruleDefaults } from '../groupFailurePolicy'
import { zh } from '../messages'

const mocks = vi.hoisted(() => ({
  get: vi.fn(), put: vi.fn(), success: vi.fn(), can: vi.fn((_permission: string) => true)
}))
vi.mock('@/api/client', () => ({ apiClient: { get: mocks.get, put: mocks.put } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ canOperator: mocks.can }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: mocks.success }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string, params: Record<string, string | number> = {}) => {
    const message = zh[key.replace('custom.gatewayResilience.', '') as keyof typeof zh] ?? key
    return message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? name))
  }
}) }))

enableAutoUnmount(afterEach)
beforeEach(() => {
  vi.resetAllMocks(); mocks.can.mockReturnValue(true)
  mocks.get.mockResolvedValue({ data: { rules: [] } }); mocks.put.mockResolvedValue({ data: { rules: [] } })
})

function deferred() {
  let resolve!: (value: unknown) => void; let reject!: (reason: unknown) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const group = (id = 1) => ({ id, name: `Group ${id}` })
const existingRule = { status_codes: [502, 503], threshold: 5, window_seconds: 60, cooldown_seconds: 600 }
function open() {
  return mount(GroupFailurePolicyModal, { props: { show: true, group: group() }, global: { stubs: { teleport: true, Icon: true } } })
}

describe('group failover status-code rule editor', () => {
  it('loads an empty rules array without creating or saving defaults', async () => {
    const wrapper = open(); await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('/admin/groups/1/failure-policy', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.text()).toContain('尚未添加规则')
    expect(wrapper.text()).toContain('暂停对该账号关联的所有分组生效')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    expect(mocks.put).not.toHaveBeenCalled()
  })

  it('adds a rule with documented defaults and saves comma-separated codes', async () => {
    const wrapper = open(); await flushPromises()
    await wrapper.get('[data-testid="failure-policy-add"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-threshold"]').element.value).toBe(String(ruleDefaults.threshold))
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-window_seconds"]').element.value).toBe(String(ruleDefaults.window_seconds))
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-cooldown_seconds"]').element.value).toBe(String(ruleDefaults.cooldown_seconds))
    await wrapper.get('[data-testid="failure-rule-0-status-codes"]').setValue('502, 503')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click'); await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/groups/1/failure-policy', { rules: [{ status_codes: [502, 503], ...ruleDefaults }] }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.emitted('success')).toHaveLength(1); expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('accepts full-width commas and normalizes the displayed status code list', async () => {
    const wrapper = open(); await flushPromises(); await wrapper.get('[data-testid="failure-policy-add"]').trigger('click')
    const input = wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-status-codes"]')
    await input.setValue('502，503'); await input.trigger('blur')
    expect(input.element.value).toBe('502, 503'); expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(false)
  })

  it('rejects a status code repeated in a later rule before sending a request', async () => {
    const wrapper = open(); await flushPromises()
    await wrapper.get('[data-testid="failure-policy-add"]').trigger('click'); await wrapper.get('[data-testid="failure-rule-0-status-codes"]').setValue('502,503')
    await wrapper.get('[data-testid="failure-policy-add"]').trigger('click'); await wrapper.get('[data-testid="failure-rule-1-status-codes"]').setValue('504, 502')
    expect(wrapper.text()).toContain('该状态码已在其他规则中配置'); expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click'); expect(mocks.put).not.toHaveBeenCalled()
  })

  it('rejects malformed status codes and accepts the inclusive 100–599 range', async () => {
    const wrapper = open(); await flushPromises(); await wrapper.get('[data-testid="failure-policy-add"]').trigger('click')
    const input = wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-status-codes"]')
    await input.setValue('99, 600'); expect(wrapper.text()).toContain('请输入 100–599'); expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    await input.setValue('100, 599'); expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(false)
  })

  it('loads, edits, removes and replaces existing rules', async () => {
    mocks.get.mockResolvedValue({ data: { rules: [existingRule, { status_codes: [429], ...ruleDefaults }] } })
    const wrapper = open(); await flushPromises()
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-status-codes"]').element.value).toBe('502, 503')
    await wrapper.get('[data-testid="failure-rule-0-threshold"]').setValue('6'); await wrapper.get('[data-testid="failure-rule-remove-1"]').trigger('click')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click'); await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/groups/1/failure-policy', { rules: [{ status_codes: [502, 503], threshold: 6, window_seconds: 60, cooldown_seconds: 600 }] }, expect.anything())
  })

  it('supports read-only groups and requires groups.read before loading', async () => {
    mocks.can.mockImplementation(permission => permission === 'groups.read'); const reader = open(); await flushPromises()
    expect(reader.find('[data-testid="failure-policy-save"]').exists()).toBe(false); expect(reader.find('[data-testid="failure-policy-readonly"]').exists()).toBe(true)
    expect(reader.findAll('fieldset').every(field => field.attributes('disabled') !== undefined)).toBe(true)
    mocks.can.mockImplementation(permission => permission === 'groups.write'); const writerWithoutRead = open(); await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(1); expect(writerWithoutRead.text()).toContain('没有查看分组切换规则的权限')
  })

  it('keeps the draft on save failure and prevents duplicate submissions', async () => {
    const pending = deferred(); mocks.put.mockReturnValueOnce(pending.promise); const wrapper = open(); await flushPromises()
    await wrapper.get('[data-testid="failure-policy-add"]').trigger('click'); await wrapper.get('[data-testid="failure-rule-0-status-codes"]').setValue('502')
    const save = wrapper.get('[data-testid="failure-policy-save"]'); await save.trigger('click'); await save.trigger('click'); expect(mocks.put).toHaveBeenCalledTimes(1)
    pending.reject(new Error('save failed')); await flushPromises(); expect(wrapper.text()).toContain('草稿已保留')
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-rule-0-status-codes"]').element.value).toBe('502'); expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('does not enable save while loading and permits reload after a failure', async () => {
    const request = deferred(); mocks.get.mockReturnValueOnce(request.promise); const wrapper = open()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true); request.reject(new Error('offline')); await flushPromises()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true); mocks.get.mockResolvedValueOnce({ data: { rules: [] } })
    await wrapper.get('[data-testid="failure-policy-reload"]').trigger('click'); await flushPromises(); expect(wrapper.get('[data-testid="failure-policy-add"]').exists()).toBe(true)
  })
})
