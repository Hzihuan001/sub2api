import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import GroupFailurePolicyModal from '../GroupFailurePolicyModal.vue'
import { failureKinds } from '../groupFailurePolicy'
import { zh } from '../messages'

const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), success: vi.fn(), can: vi.fn((_permission: string) => true) }))
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
  vi.resetAllMocks()
  mocks.can.mockReturnValue(true)
  mocks.get.mockResolvedValue({ data: { rules: {} } })
  mocks.put.mockResolvedValue({ data: { rules: {} } })
})

function deferred() {
  let resolve!: (value: unknown) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

const rule = { threshold: 4, window_seconds: 300, cooldown_seconds: 600 }
const group = (id: number) => ({ id, name: `Group ${id}` })
function open() {
  return mount(GroupFailurePolicyModal, {
    props: { show: true, group: group(1) },
    global: { stubs: { teleport: true, Icon: true } }
  })
}

describe('group failover editor', () => {
  it('loads explicit rules, leaves missing kinds inherited, and explains account-wide safety boundaries', async () => {
    mocks.get.mockResolvedValue({ data: { rules: { overload: rule } } })
    const wrapper = open()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('/admin/groups/1/failure-policy', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.get<HTMLSelectElement>('[data-testid="failure-mode-overload"]').element.value).toBe('custom')
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-overload-threshold"]').element.value).toBe('4')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="failure-mode-timeout"]').element.value).toBe('inherit')
    expect(wrapper.find('[data-testid="failure-timeout-threshold"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('5 分钟')
    expect(wrapper.text()).toContain('10 分钟')
    expect(wrapper.text()).toContain('所有分组')
    expect(wrapper.text()).toContain('账号管理 → 恢复状态')
    expect(wrapper.text()).toContain('已经输出部分内容的流不会自动重放')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
  })

  it('offers examples only after opting in and saves exactly the selected custom kinds', async () => {
    const wrapper = open()
    await flushPromises()
    expect(mocks.put).not.toHaveBeenCalled()
    for (const kind of failureKinds) await wrapper.get(`[data-testid="failure-mode-${kind}"]`).setValue('custom')
    await wrapper.get('[data-testid="failure-server_error-threshold"]').setValue('5')
    await wrapper.get('[data-testid="failure-rate_limit-window_seconds"]').setValue('61')
    expect(wrapper.text()).toContain('61 秒')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/groups/1/failure-policy', { rules: {
      overload: { threshold: 3, window_seconds: 300, cooldown_seconds: 600 },
      server_error: { threshold: 5, window_seconds: 60, cooldown_seconds: 60 },
      timeout: { threshold: 2, window_seconds: 60, cooldown_seconds: 60 },
      rate_limit: { threshold: 3, window_seconds: 61, cooldown_seconds: 60 }
    } }, expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('removes an override by saving an absent rule, without replacing it with an example', async () => {
    mocks.get.mockResolvedValue({ data: { rules: { overload: rule } } })
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-overload"]').setValue('inherit')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/groups/1/failure-policy', { rules: {} }, expect.anything())
  })

  it.each([
    ['threshold', '0'], ['threshold', '101'], ['threshold', '1.5'], ['threshold', ''],
    ['window_seconds', '0'], ['window_seconds', '3601'], ['window_seconds', '1.5'],
    ['cooldown_seconds', '0'], ['cooldown_seconds', '7201'], ['cooldown_seconds', '1.5']
  ])('rejects invalid %s value %s without sending a request', async (field, value) => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-overload"]').setValue('custom')
    await wrapper.get(`[data-testid="failure-overload-${field}"]`).setValue(value)
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toContain('请输入整数')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    expect(mocks.put).not.toHaveBeenCalled()
  })

  it('accepts inclusive lower and upper bounds', async () => {
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-overload"]').setValue('custom')
    for (const field of ['threshold', 'window_seconds', 'cooldown_seconds']) {
      await wrapper.get(`[data-testid="failure-overload-${field}"]`).setValue('1')
    }
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(false)
    await wrapper.get('[data-testid="failure-overload-threshold"]').setValue('100')
    await wrapper.get('[data-testid="failure-overload-window_seconds"]').setValue('3600')
    await wrapper.get('[data-testid="failure-overload-cooldown_seconds"]').setValue('7200')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    expect(mocks.put).toHaveBeenCalledWith('/admin/groups/1/failure-policy', {
      rules: { overload: { threshold: 100, window_seconds: 3600, cooldown_seconds: 7200 } }
    }, expect.anything())
  })

  it('supports groups.read without a save control or enabled editing fields', async () => {
    mocks.can.mockImplementation(permission => permission === 'groups.read')
    mocks.get.mockResolvedValue({ data: { rules: { overload: rule } } })
    const wrapper = open()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledTimes(1)
    expect(wrapper.find('[data-testid="failure-policy-save"]').exists()).toBe(false)
    expect(wrapper.findAll('fieldset').every(field => field.attributes('disabled') !== undefined)).toBe(true)
    expect(wrapper.find('[data-testid="failure-policy-readonly"]').exists()).toBe(true)
    expect(mocks.put).not.toHaveBeenCalled()
  })

  it('does not load or edit with write permission alone', async () => {
    mocks.can.mockImplementation(permission => permission === 'groups.write')
    const wrapper = open()
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="failure-policy-save"]').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toContain('没有查看')
  })

  it('disables save during loading and after a load error, then permits a successful retry', async () => {
    const request = deferred()
    mocks.get.mockReturnValueOnce(request.promise)
    const wrapper = open()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    request.reject(new Error('offline'))
    await flushPromises()
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
    expect(wrapper.find('fieldset').exists()).toBe(false)
    await wrapper.get('[data-testid="failure-policy-reload"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-overload"]').setValue('custom')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(false)
  })

  it.each([{ rules: null }, { rules: { overload: { threshold: 3 } } }, { rules: { unknown_kind: rule } }])('refuses malformed or unsupported loaded policy %j', async policy => {
    mocks.get.mockResolvedValue({ data: policy })
    const wrapper = open()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('无法加载')
    expect(wrapper.get<HTMLButtonElement>('[data-testid="failure-policy-save"]').element.disabled).toBe(true)
  })

  it('preserves the draft on save failure and suppresses duplicate saves', async () => {
    const pending = deferred()
    mocks.put.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-timeout"]').setValue('custom')
    const save = wrapper.get('[data-testid="failure-policy-save"]')
    await save.trigger('click')
    await save.trigger('click')
    expect(mocks.put).toHaveBeenCalledTimes(1)
    pending.reject(new Error('save failed'))
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('草稿已保留')
    expect(wrapper.get<HTMLInputElement>('[data-testid="failure-timeout-threshold"]').element.value).toBe('2')
    expect(wrapper.emitted('close')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it('aborts a previous load and ignores its result after changing groups', async () => {
    const old = deferred()
    mocks.get.mockReturnValueOnce(old.promise).mockResolvedValueOnce({ data: { rules: { timeout: rule } } })
    const wrapper = open()
    const oldSignal = mocks.get.mock.calls[0][1].signal as AbortSignal
    await wrapper.setProps({ group: group(2) })
    await flushPromises()
    expect(oldSignal.aborted).toBe(true)
    old.resolve({ data: { rules: { overload: rule } } })
    await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('[data-testid="failure-mode-timeout"]').element.value).toBe('custom')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="failure-mode-overload"]').element.value).toBe('inherit')
    await wrapper.get('[data-testid="failure-timeout-threshold"]').setValue('5')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    expect(mocks.put.mock.calls[0][0]).toBe('/admin/groups/2/failure-policy')
  })

  it('keeps the new load pending when an older request fails', async () => {
    const old = deferred()
    const current = deferred()
    mocks.get.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = open()
    await wrapper.setProps({ group: group(2) })
    old.reject(new Error('old load'))
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[role="status"]').text()).toContain('正在加载')
    current.resolve({ data: { rules: {} } })
    await flushPromises()
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
  })

  it('discards drafts on close/reopen and ignores stale save completion', async () => {
    const saveRequest = deferred()
    mocks.put.mockReturnValueOnce(saveRequest.promise)
    const wrapper = open()
    await flushPromises()
    await wrapper.get('[data-testid="failure-mode-overload"]').setValue('custom')
    await wrapper.get('[data-testid="failure-policy-save"]').trigger('click')
    const saveSignal = mocks.put.mock.calls[0][2].signal as AbortSignal
    await wrapper.get('[data-testid="failure-policy-close"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true, group: group(2) })
    await flushPromises()
    expect(saveSignal.aborted).toBe(true)
    expect(wrapper.get<HTMLSelectElement>('[data-testid="failure-mode-overload"]').element.value).toBe('inherit')
    saveRequest.resolve({ data: { rules: {} } })
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(mocks.success).not.toHaveBeenCalled()
  })

  it('ignores pending failures after unmount', async () => {
    const pending = deferred()
    mocks.get.mockReturnValueOnce(pending.promise)
    const wrapper = open()
    const signal = mocks.get.mock.calls[0][1].signal as AbortSignal
    wrapper.unmount()
    pending.reject(new Error('late failure'))
    await flushPromises()
    expect(signal.aborted).toBe(true)
    expect(wrapper.emitted('success')).toBeUndefined()
  })
})
