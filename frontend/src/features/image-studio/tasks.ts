import type { ImageStudioGenerateRequest } from '@/api/imageStudio'

export type StudioTaskState = 'pending' | 'paused' | 'error' | 'unknown' | 'failed' | 'expired'

/** No API-key secret is stored. Files use IndexedDB's structured clone support. */
export interface StoredStudioTask {
  operationId: string
  apiKeyId: string
  apiKeyName: string
  request: ImageStudioGenerateRequest
  createdAt: number
  expiresAt: number
  taskId?: string
  executionDeadline?: number
  submitAttempts: number
  state: StudioTaskState
  error?: string
}

function databaseName(scope: string, apiKeyId: string): string {
  if (!scope.trim() || !apiKeyId.trim()) throw new Error('Image task scope is required')
  return `sub2api-image-studio-tasks-v1-${encodeURIComponent(scope)}-${encodeURIComponent(apiKeyId)}`
}

async function withTaskStore<T>(scope: string, keyId: string, mode: IDBTransactionMode, operation: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = indexedDB.open(databaseName(scope, keyId), 1)
    request.onupgradeneeded = () => { request.result.createObjectStore('tasks', { keyPath: 'operationId' }) }
    request.onerror = () => reject(request.error || new Error('Unable to open image tasks'))
    request.onsuccess = () => resolve(request.result)
  })
  try {
    return await new Promise<T>((resolve, reject) => {
      const tx = database.transaction('tasks', mode)
      const request = operation(tx.objectStore('tasks'))
      tx.oncomplete = () => resolve(request.result)
      tx.onerror = () => reject(tx.error || new Error('Image task storage failed'))
      tx.onabort = () => reject(tx.error || new Error('Image task storage aborted'))
    })
  } finally {
    database.close()
  }
}

export async function listStudioTasks(scope: string, keyId: string): Promise<StoredStudioTask[]> {
  const tasks = await withTaskStore(scope, keyId, 'readonly', (store) => store.getAll() as IDBRequest<StoredStudioTask[]>)
  return tasks.filter((task) => task.apiKeyId === keyId).sort((a, b) => b.createdAt - a.createdAt)
}

export async function saveStudioTask(scope: string, task: StoredStudioTask): Promise<void> {
  await withTaskStore(scope, task.apiKeyId, 'readwrite', (store) => store.put(task))
}

export async function deleteStudioTask(scope: string, keyId: string, operationId: string): Promise<void> {
  await withTaskStore(scope, keyId, 'readwrite', (store) => store.delete(operationId))
}
