import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, shallowMount } from '@vue/test-utils'

vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

import ScheduledTestsPanel from '../ScheduledTestsPanel.vue'
const { listByAccount, listResults, create, update, showError, showSuccess } = vi.hoisted(() => ({
  listByAccount: vi.fn(), listResults: vi.fn(), create: vi.fn(), update: vi.fn(),
  showError: vi.fn(), showSuccess: vi.fn(),
}))
vi.mock('@/api/admin', () => ({ adminAPI: { scheduledTests: { listByAccount, listResults, create, update } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError, showSuccess }) }))

async function openPanel() {
  const wrapper = shallowMount(ScheduledTestsPanel, {
    props: { show: false, accountId: 1, modelOptions: [], groupOptions: [] },
    global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}
const result = (id: number) => ({ id, latency_ms: id, status: 'success', started_at: '2026-10-03T00:00:00Z' })

describe('scheduled test result selection', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    listByAccount.mockResolvedValue([1, 2].map(id => ({ id, model_id: `model-${id}`, enabled: true })))
  })

  it.each(['resolve', 'reject'] as const)('ignores an old plan request that later %ss', async outcome => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const headers = wrapper.findAll('.cursor-pointer')
    await headers[0].trigger('click')
    await headers[1].trigger('click')
    await flushPromises()
    const current = wrapper.html()
    if (outcome === 'resolve') old.resolve([result(11)])
    else old.reject(new Error('obsolete error'))
    await flushPromises()
    expect(wrapper.html()).toBe(current)
    expect(showError).not.toHaveBeenCalled()
  })

  it('keeps loading the current plan when the previous request completes', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    const current = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise)
    const wrapper = await openPanel()
    await wrapper.findAll('.cursor-pointer')[0].trigger('click')
    await wrapper.findAll('.cursor-pointer')[1].trigger('click')
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.text()).toContain('common.loading')
    current.resolve([result(22)])
    await flushPromises()
    expect(wrapper.text()).not.toContain('common.loading')
  })

  it('invalidates a request when the same plan is collapsed and reopened', async () => {
    const old = deferred<ReturnType<typeof result>[]>()
    listResults.mockReturnValueOnce(old.promise).mockResolvedValueOnce([result(22)])
    const wrapper = await openPanel()
    const header = wrapper.findAll('.cursor-pointer')[0]
    await header.trigger('click')
    await header.trigger('click')
    await header.trigger('click')
    await flushPromises()
    const current = wrapper.html()
    old.resolve([result(11)])
    await flushPromises()
    expect(wrapper.html()).toBe(current)
  })

  it('offers xhigh for channel showcase test plans, but not connection tests', async () => {
    const wrapper = await openPanel()
    await wrapper.find('button.btn-primary').trigger('click')
    await flushPromises()

    const selects = wrapper.findAll('select')
    const testKind = selects.find(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'pelican')
    )
    expect(testKind).toBeDefined()
    expect(wrapper.findAll('select').some(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'xhigh')
    )).toBe(false)

    await testKind!.setValue('pelican')
    const effort = wrapper.findAll('select').find(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'xhigh')
    )
    expect(effort).toBeDefined()
    expect(Array.from((effort!.element as HTMLSelectElement).options).map(option => option.value)).toContain('xhigh')
    await effort!.setValue('xhigh')
    expect((effort!.element as HTMLSelectElement).value).toBe('xhigh')
  })

  it('includes the selected reasoning effort when creating a showcase plan', async () => {
    create.mockResolvedValueOnce({ id: 3 })
    const wrapper = await openPanel()
    await wrapper.get('button.btn-primary').trigger('click')
    const testKind = wrapper.findAll('select').find(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'pelican')
    )
    await testKind!.setValue('pelican')
    const effort = wrapper.findAll('select').find(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'xhigh')
    )
    await effort!.setValue('xhigh')
    const form = (wrapper.vm as unknown as { newPlan: { model_id: string; group_id: number; cron_expression: string } }).newPlan
    form.model_id = 'gpt-6.1-sol'
    form.group_id = 7
    form.cron_expression = '0 * * * *'
    await wrapper.vm.$nextTick()
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    expect(save).toBeDefined()
    await save!.trigger('click')
    await flushPromises()

    expect(create).toHaveBeenCalledWith(expect.objectContaining({
      account_id: 1, group_id: 7, model_id: 'gpt-6.1-sol',
      test_kind: 'pelican', thinking_effort: 'xhigh',
    }))
  })

  it('saves xhigh when editing a showcase plan and shows the saved setting', async () => {
    const plan = {
      id: 1, model_id: 'gpt-6.1-sol', test_kind: 'pelican', group_id: 7,
      thinking_effort: 'medium', cron_expression: '0 * * * *', max_results: 100,
      enabled: true, auto_recover: false,
    }
    listByAccount.mockResolvedValueOnce([plan])
    update.mockResolvedValueOnce({ ...plan, thinking_effort: 'xhigh' })
    const wrapper = await openPanel()

    await wrapper.get('button[title="admin.scheduledTests.editPlan"]').trigger('click')
    const effort = wrapper.findAll('select').find(select =>
      Array.from((select.element as HTMLSelectElement).options).some(option => option.value === 'xhigh')
    )
    expect(effort).toBeDefined()
    await effort!.setValue('xhigh')
    const save = wrapper.findAll('button').find(button => button.text() === 'common.save')
    expect(save).toBeDefined()
    await save!.trigger('click')
    await flushPromises()

    expect(update).toHaveBeenCalledWith(1, expect.objectContaining({ thinking_effort: 'xhigh' }))
    expect(wrapper.text()).toContain('admin.scheduledTests.thinkingEffortXHigh')
  })

  it('shows failed test reasons immediately and keeps full details expandable', async () => {
    listByAccount.mockResolvedValueOnce([{
      id: 1, model_id: 'gpt-6.1-sol', test_kind: 'pelican', group_id: 7,
      thinking_effort: 'xhigh', cron_expression: '0 * * * *', max_results: 100,
      enabled: true, auto_recover: false,
    }])
    listResults.mockResolvedValueOnce([
      { ...result(1), status: 'failed', error_message: 'upstream timed out\nrequest details', response_text: '' },
      { ...result(2), status: 'failed', error_message: '', response_text: '' },
    ])
    const wrapper = await openPanel()
    await wrapper.get('.cursor-pointer').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('upstream timed out')
    expect(wrapper.text()).not.toContain('request details')
    expect(wrapper.text()).toContain('admin.scheduledTests.errorUnavailable')
    await wrapper.get('button[aria-expanded="false"]').trigger('click')
    expect(wrapper.text()).toContain('request details')
  })
})
