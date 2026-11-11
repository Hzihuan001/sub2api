import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch, type Ref } from 'vue'
import {
  generateImageStudioImages, getImageStudioTask, imageStudioTaskOutputs,
  isImageStudioAsyncDisabled, submitImageStudioTask,
  type ImageStudioGenerateRequest, type ImageStudioOutput, type ImageStudioTask,
} from '@/api/imageStudio'
import { deleteStudioTask, listStudioTasks, saveStudioTask, type StoredStudioTask } from './tasks'

interface StudioTaskKey { id: number; key: string; name: string; user_id?: number }
interface Options {
  scope: Readonly<Ref<string | null>>
  apiKey: Readonly<Ref<StudioTaskKey | null>>
  onCompleted: (task: StoredStudioTask, outputs: ImageStudioOutput[], isCurrent: () => boolean) => Promise<boolean>
  pollInterval?: number
  errorText?: (code: 'submitUnconfirmed' | 'notSaved' | 'emptyResult') => string
}

function message(error: unknown): string {
  return error instanceof Error ? error.message : 'Unable to query image task'
}

function operationID(): string {
  return globalThis.crypto?.randomUUID?.() || `studio-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

/** Task orchestration is independent from form state and local gallery rendering. */
export function useImageStudioTasks(options: Options) {
  const tasks = shallowRef<StoredStudioTask[]>([])
  const activeIDs = ref<string[]>([])
  const storageUnavailable = ref(false)
  const synchronousMode = ref(false)
  const lastError = ref('')
  const controllers = new Map<string, AbortController>()
  let epoch = 0
  let disposed = false
  const waiting = computed(() => activeIDs.value.length > 0)

  function context() {
    const scope = options.scope.value
    const key = options.apiKey.value
    if (!scope || !key || (key.user_id !== undefined && String(key.user_id) !== scope)) return null
    return { scope, key, epoch }
  }

  async function persist(scope: string, task: StoredStudioTask) {
    try { await saveStudioTask(scope, task) } catch {
      if (options.scope.value === scope && String(options.apiKey.value?.id) === task.apiKeyId) storageUnavailable.value = true
    }
  }

  function stop() {
    controllers.forEach((controller) => controller.abort())
    controllers.clear()
    activeIDs.value = []
    const current = context()
    if (!current) return
    tasks.value.filter((task) => task.state === 'pending').forEach((task) => {
      task.state = 'paused'
      void persist(current.scope, { ...task })
    })
    tasks.value = [...tasks.value]
  }

  async function remove(task: StoredStudioTask, scope: string, isCurrent: () => boolean) {
    await deleteStudioTask(scope, task.apiKeyId, task.operationId).catch(() => { storageUnavailable.value = true })
    if (isCurrent()) tasks.value = tasks.value.filter((item) => item.operationId !== task.operationId)
  }

  async function run(task: StoredStudioTask, automatic = false): Promise<void> {
    const current = context()
    if (!current || task.apiKeyId !== String(current.key.id) || controllers.has(task.operationId)) return
    lastError.value = ''
    const controller = new AbortController()
    controllers.set(task.operationId, controller)
    activeIDs.value = [...activeIDs.value, task.operationId]
    const isCurrent = () => !disposed && epoch === current.epoch && !controller.signal.aborted
      && options.scope.value === current.scope && String(options.apiKey.value?.id) === task.apiKeyId
    const update = async () => {
      if (!isCurrent()) return
      tasks.value = tasks.value.map((item) => item.operationId === task.operationId ? { ...task } : item)
      await persist(current.scope, task)
    }
    try {
      // A known task may have completed later and gained a newer retention
      // deadline while this page was closed. Ask the server before expiring it.
      if (!task.taskId && task.expiresAt <= Date.now()) {
        task.state = 'expired'
        await update()
        return
      }
      task.state = 'pending'
      task.error = undefined
      await update()
      if (!isCurrent()) return
      let response: ImageStudioTask | undefined
      if (!task.taskId) {
        const attempts = automatic ? Math.max(0, 2 - task.submitAttempts) : 2
        for (let index = 0; index < attempts; index += 1) {
          task.submitAttempts += 1
          await update()
          if (!isCurrent()) return
          try {
            response = await submitImageStudioTask(current.key.key, task.request, task.operationId, controller.signal)
            break
          } catch (error) {
            if (!isCurrent()) return
            // A previous lost response may already have created a task. Never
            // fall back after an ambiguous attempt, even if the switch changed.
            if (isImageStudioAsyncDisabled(error) && task.submitAttempts === 1) {
              synchronousMode.value = true
              await remove(task, current.scope, isCurrent)
              if (!isCurrent()) return
              const outputs = await generateImageStudioImages(current.key.key, task.request, controller.signal)
              if (isCurrent()) await options.onCompleted(task, outputs, isCurrent)
              return
            }
            const status = (error as { status?: number })?.status
            if ((status !== undefined && status < 500) || index === attempts - 1) throw error
          }
        }
        if (!response) throw new Error(options.errorText?.('submitUnconfirmed') || 'Submission not confirmed. Continue with the same request to check it safely.')
        task.taskId = response.task_id || response.id
        if (response.expires_at) task.expiresAt = response.expires_at * 1000
        if (response.execution_deadline) task.executionDeadline = response.execution_deadline * 1000
        await update()
      }
      while (isCurrent()) {
        response ||= await getImageStudioTask(current.key.key, task.taskId!, controller.signal)
        if (!isCurrent()) return
        const expiresAt = response.expires_at ? response.expires_at * 1000 : task.expiresAt
        const executionDeadline = response.execution_deadline ? response.execution_deadline * 1000 : task.executionDeadline
        if (expiresAt !== task.expiresAt || executionDeadline !== task.executionDeadline) {
          task.expiresAt = expiresAt
          task.executionDeadline = executionDeadline
          await update()
          if (!isCurrent()) return
        }
        if (response.status === 'completed') {
          const outputs = imageStudioTaskOutputs(response)
          if (!outputs.length) throw new Error(options.errorText?.('emptyResult') || 'Image provider returned no image data')
          const saved = await options.onCompleted(task, outputs, isCurrent)
          if (!isCurrent()) return
          if (saved) await remove(task, current.scope, isCurrent)
          else { task.state = 'error'; task.error = options.errorText?.('notSaved') || 'Result is not saved locally. Continue querying to save it again.'; await update() }
          return
        }
        if (response.status === 'failed') {
          const error = response.error?.error || response.error
          task.state = error?.code === 'image_task_result_unknown' ? 'unknown' : 'failed'
          task.error = error?.message || 'Image generation failed'
          await update()
          return
        }
        response = undefined
        await new Promise<void>((resolve) => {
          const finish = () => { clearTimeout(timer); controller.signal.removeEventListener('abort', finish); resolve() }
          const timer = setTimeout(finish, options.pollInterval ?? 3000)
          controller.signal.addEventListener('abort', finish, { once: true })
        })
      }
    } catch (error) {
      if (isCurrent()) {
        lastError.value = message(error)
        task.state = (error as { status?: number })?.status === 404 && task.expiresAt <= Date.now() ? 'expired' : 'error'
        task.error = message(error)
        await update()
      }
    } finally {
      if (controllers.get(task.operationId) === controller) {
        controllers.delete(task.operationId)
        if (epoch === current.epoch) activeIDs.value = activeIDs.value.filter((id) => id !== task.operationId)
      }
    }
  }

  async function submit(request: ImageStudioGenerateRequest): Promise<void> {
    const current = context()
    if (!current) return
    synchronousMode.value = false
    lastError.value = ''
    const task: StoredStudioTask = {
      operationId: operationID(), apiKeyId: String(current.key.id), apiKeyName: current.key.name,
      request: { ...request, images: [...(request.images || [])] }, createdAt: Date.now(),
      expiresAt: Date.now() + 24 * 60 * 60 * 1000, submitAttempts: 0, state: 'pending',
    }
    tasks.value = [task, ...tasks.value]
    await run(task)
  }

  function resume(task: StoredStudioTask) { return run({ ...task }) }
  function onOnline() {
    tasks.value.filter((task) => task.state !== 'failed' && task.state !== 'expired' && task.state !== 'unknown')
      .forEach((task) => { void run({ ...task }, true) })
  }

  watch([options.scope, () => options.apiKey.value?.id], async () => {
    epoch += 1
    controllers.forEach((controller) => controller.abort())
    controllers.clear()
    activeIDs.value = []
    tasks.value = []
    synchronousMode.value = false
    lastError.value = ''
    storageUnavailable.value = false
    const current = context()
    if (!current) return
    try {
      const stored = await listStudioTasks(current.scope, String(current.key.id))
      if (disposed || epoch !== current.epoch) return
      tasks.value = stored
      onOnline()
    } catch { if (epoch === current.epoch) storageUnavailable.value = true }
  }, { immediate: true, flush: 'sync' })

  onMounted(() => { window.addEventListener('online', onOnline) })
  onBeforeUnmount(() => { disposed = true; epoch += 1; stop(); window.removeEventListener('online', onOnline) })
  return { tasks, waiting, storageUnavailable, synchronousMode, lastError, submit, resume, stop }
}
