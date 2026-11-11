import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useImageStudioTasks } from '../useImageStudioTasks'
import type { StoredStudioTask } from '../tasks'
import type { ImageStudioTask } from '@/api/imageStudio'

const mocks = vi.hoisted(() => ({ submit: vi.fn(), get: vi.fn(), sync: vi.fn(), list: vi.fn(), save: vi.fn(), remove: vi.fn() }))
vi.mock('@/api/imageStudio', async (original) => ({
  ...await original<typeof import('@/api/imageStudio')>(),
  submitImageStudioTask: mocks.submit, getImageStudioTask: mocks.get, generateImageStudioImages: mocks.sync,
}))
vi.mock('../tasks', () => ({ listStudioTasks: mocks.list, saveStudioTask: mocks.save, deleteStudioTask: mocks.remove }))

let wrappers: VueWrapper[] = []
let storage: Map<string, StoredStudioTask[]>
const clone = (task: StoredStudioTask): StoredStudioTask => ({ ...task, request: { ...task.request, images: [...(task.request.images || [])] } })
const request = () => ({ model: 'gpt-image-2', prompt: 'A cat', count: 1, size: '1024x1024', outputFormat: 'png' })
const response = (status: ImageStudioTask['status'] = 'processing'): ImageStudioTask => ({
  id: 'imgtask_one', task_id: 'imgtask_one', status, created_at: Date.now() / 1000,
  expires_at: Date.now() / 1000 + 86400, execution_deadline: Date.now() / 1000 + 1800,
  ...(status === 'completed' ? { result: { data: [{ b64_json: 'aW1hZ2U=' }] } } : {}),
})

function stored(overrides: Partial<StoredStudioTask> = {}): StoredStudioTask {
  return { operationId: 'operation-one', apiKeyId: '10', apiKeyName: 'Images', request: request(),
    createdAt: Date.now(), expiresAt: Date.now() + 86400000, taskId: 'imgtask_one', submitAttempts: 1, state: 'paused', ...overrides }
}

function harness() {
  const scope = ref<string | null>('7')
  const key = ref<{ id: number; key: string; name: string; user_id: number } | null>({ id: 10, key: 'secret-seven', name: 'Images', user_id: 7 })
  const completed = vi.fn().mockResolvedValue(true)
  let runner!: ReturnType<typeof useImageStudioTasks>
  const wrapper = mount(defineComponent({ setup() {
    runner = useImageStudioTasks({ scope, apiKey: key, onCompleted: completed, pollInterval: 100 })
    return () => h('div')
  } }))
  wrappers.push(wrapper)
  return { runner, scope, key, completed, wrapper }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.clearAllMocks()
  storage = new Map()
  mocks.list.mockImplementation(async (scope, key) => (storage.get(`${scope}:${key}`) || []).map(clone))
  mocks.save.mockImplementation(async (scope, task: StoredStudioTask) => {
    const name = `${scope}:${task.apiKeyId}`
    storage.set(name, [...(storage.get(name) || []).filter((item) => item.operationId !== task.operationId), clone(task)])
  })
  mocks.remove.mockImplementation(async (scope, key, id) => {
    const name = `${scope}:${key}`
    storage.set(name, (storage.get(name) || []).filter((item) => item.operationId !== id))
  })
  mocks.submit.mockResolvedValue(response())
  mocks.get.mockResolvedValue(response('completed'))
  mocks.sync.mockResolvedValue([{ b64Json: 'legacy' }])
})

afterEach(() => {
  wrappers.forEach((wrapper) => wrapper.unmount())
  wrappers = []
  vi.useRealTimers()
})

describe('recoverable Image Studio tasks', () => {
  it('restores the accepted task after a lost GET and refresh without sending another POST', async () => {
    mocks.get.mockRejectedValueOnce(new TypeError('offline'))
    const first = harness()
    await flushPromises()
    const submitted = first.runner.submit(request())
    await flushPromises()
    await vi.advanceTimersByTimeAsync(100)
    await submitted
    expect(first.runner.tasks.value[0]).toMatchObject({ taskId: 'imgtask_one', state: 'error' })
    expect(mocks.submit).toHaveBeenCalledOnce()
    expect(first.completed).not.toHaveBeenCalled()
    first.wrapper.unmount()

    const reopened = harness()
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledOnce()
    expect(reopened.completed).toHaveBeenCalledOnce()
    expect(reopened.completed.mock.calls[0][0]).toMatchObject({ taskId: 'imgtask_one', request: request() })
    expect(reopened.runner.tasks.value).toEqual([])
    expect(storage.get('7:10')).toEqual([])
  })

  it('retries a lost submission only with the same idempotency key and unchanged payload', async () => {
    mocks.submit.mockRejectedValueOnce(new TypeError('lost response')).mockResolvedValueOnce(response('completed'))
    const { runner, completed } = harness()
    await flushPromises()
    const payload = request()
    const operation = runner.submit(payload)
    payload.prompt = 'Changed after clicking'
    await operation
    expect(mocks.submit).toHaveBeenCalledTimes(2)
    const [first, second] = mocks.submit.mock.calls
    expect(first[2]).toBe(second[2])
    expect(first[2]).toMatch(/^[A-Za-z0-9._:-]{1,128}$/)
    expect(first[1]).toEqual({ ...request(), images: [] })
    expect(second[1]).toEqual({ ...request(), images: [] })
    expect(mocks.sync).not.toHaveBeenCalled()
    expect(completed).toHaveBeenCalledOnce()
    expect(mocks.save.mock.calls.every(([, task]) => !JSON.stringify(task).includes('secret-seven'))).toBe(true)
  })

  it.each([new TypeError('offline'), Object.assign(new Error('bad gateway'), { status: 502 })])('keeps ambiguous submissions after bounded failures: %s', async (error) => {
    mocks.submit.mockRejectedValue(error)
    const { runner } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(mocks.submit).toHaveBeenCalledTimes(2)
    expect(mocks.sync).not.toHaveBeenCalled()
    expect(runner.tasks.value[0]).toMatchObject({ state: 'error', submitAttempts: 2 })
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledTimes(2)
  })

  it('uses synchronous fallback only on the explicit first-attempt disabled response', async () => {
    mocks.submit.mockRejectedValue(Object.assign(new Error('async image tasks are not enabled'), { status: 404 }))
    const { runner, completed } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(mocks.sync).toHaveBeenCalledOnce()
    expect(runner.synchronousMode.value).toBe(true)
    expect(runner.tasks.value).toEqual([])
    expect(completed).toHaveBeenCalledOnce()
  })

  it.each(['Images API is not supported for this platform', 'task missing'])('never treats another 404 as permission to regenerate: %s', async (error) => {
    mocks.submit.mockRejectedValue(Object.assign(new Error(error), { status: 404 }))
    const { runner } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(mocks.submit).toHaveBeenCalledOnce()
    expect(mocks.sync).not.toHaveBeenCalled()
    expect(runner.tasks.value[0].state).toBe('error')
  })

  it('does not use synchronous fallback if a prior submission response was lost', async () => {
    mocks.submit.mockRejectedValueOnce(new TypeError('lost response'))
      .mockRejectedValueOnce(Object.assign(new Error('async image tasks are not enabled'), { status: 404 }))
    const { runner } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(mocks.sync).not.toHaveBeenCalled()
    expect(runner.tasks.value).toHaveLength(1)
  })

  it('handles an immediately completed 202 and never polls or duplicates its result', async () => {
    mocks.submit.mockResolvedValue(response('completed'))
    const { runner, completed } = harness()
    await flushPromises()
    await runner.submit(request())
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(mocks.get).not.toHaveBeenCalled()
    expect(completed).toHaveBeenCalledOnce()
    expect(runner.tasks.value).toEqual([])
  })

  it('keeps unknown results for manual GET recovery without a new generation', async () => {
    storage.set('7:10', [stored()])
    mocks.get.mockResolvedValueOnce({ ...response('failed'), error: { code: 'image_task_result_unknown', message: 'Result not confirmed' } })
    const { runner, completed } = harness()
    await flushPromises()
    expect(runner.tasks.value[0].state).toBe('unknown')
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledOnce()
    await runner.resume(runner.tasks.value[0])
    expect(completed).toHaveBeenCalledOnce()
    expect(mocks.submit).not.toHaveBeenCalled()
  })

  it('does not discard a task when the generated image could not be saved locally', async () => {
    storage.set('7:10', [stored()])
    const { runner, completed } = harness()
    completed.mockResolvedValue(false)
    await flushPromises()
    expect(runner.tasks.value[0]).toMatchObject({ taskId: 'imgtask_one', state: 'error' })
    completed.mockResolvedValue(true)
    await runner.resume(runner.tasks.value[0])
    expect(runner.tasks.value).toEqual([])
    expect(mocks.submit).not.toHaveBeenCalled()
  })

  it('retains explicit failures without automatically running the model again', async () => {
    mocks.submit.mockResolvedValue({ ...response('failed'), error: { error: { code: 'upstream_error', message: 'Generation failed' } } })
    const { runner } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(runner.tasks.value[0]).toMatchObject({ state: 'failed', error: 'Generation failed' })
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledOnce()
    expect(mocks.get).not.toHaveBeenCalled()
  })

  it('stops waiting while preserving the task, then resumes the same GET on reconnect', async () => {
    const { runner, completed } = harness()
    await flushPromises()
    const submitted = runner.submit(request())
    await flushPromises()
    runner.stop()
    await submitted
    expect(runner.tasks.value[0].state).toBe('paused')
    expect(runner.waiting.value).toBe(false)
    window.dispatchEvent(new Event('online'))
    await flushPromises()
    expect(completed).toHaveBeenCalledOnce()
    expect(mocks.submit).toHaveBeenCalledOnce()
  })

  it('ignores a late GET response after a user switch and never exposes the previous user task', async () => {
    storage.set('7:10', [stored()])
    let finish!: (value: ImageStudioTask) => void
    mocks.get.mockImplementationOnce(() => new Promise((resolve) => { finish = resolve }))
    const { runner, scope, key, completed } = harness()
    await flushPromises()
    scope.value = '8'
    key.value = { id: 20, user_id: 8, key: 'secret-eight', name: 'Other user' }
    await flushPromises()
    finish(response('completed'))
    await flushPromises()
    expect(completed).not.toHaveBeenCalled()
    expect(runner.tasks.value).toEqual([])
    expect(storage.get('7:10')).toHaveLength(1)
    expect(mocks.list).toHaveBeenCalledWith('8', '20')
    expect(mocks.list).not.toHaveBeenCalledWith('8', '10')
    expect(mocks.get).toHaveBeenCalledWith('secret-seven', 'imgtask_one', expect.any(AbortSignal))
  })

  it('displays expired tasks without resubmitting or querying them', async () => {
    storage.set('7:10', [stored({ taskId: undefined, expiresAt: Date.now() - 1 })])
    const { runner } = harness()
    await flushPromises()
    expect(runner.tasks.value[0].state).toBe('expired')
    expect(mocks.get).not.toHaveBeenCalled()
    expect(mocks.submit).not.toHaveBeenCalled()
  })

  it('checks an existing task past the locally known expiry and updates server retention/deadline on every GET', async () => {
    storage.set('7:10', [stored({ expiresAt: Date.now() - 1 })])
    const later = Date.now() / 1000 + 86400
    mocks.get.mockResolvedValueOnce({ ...response(), expires_at: later, execution_deadline: later - 80000 })
      .mockResolvedValueOnce({ ...response('completed'), expires_at: later + 60, execution_deadline: later - 79000 })
    const { runner, completed } = harness()
    completed.mockResolvedValue(false)
    await flushPromises()
    expect(runner.tasks.value[0]).toMatchObject({ expiresAt: later * 1000, executionDeadline: (later - 80000) * 1000 })
    await vi.advanceTimersByTimeAsync(100)
    expect(runner.tasks.value[0]).toMatchObject({ expiresAt: (later + 60) * 1000, executionDeadline: (later - 79000) * 1000 })
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(completed).toHaveBeenCalledOnce()
  })

  it('reports unavailable task persistence without pretending refresh recovery works', async () => {
    mocks.save.mockRejectedValue(new Error('quota exceeded'))
    mocks.submit.mockResolvedValue(response('completed'))
    const { runner } = harness()
    await flushPromises()
    await runner.submit(request())
    expect(runner.storageUnavailable.value).toBe(true)
  })
})
