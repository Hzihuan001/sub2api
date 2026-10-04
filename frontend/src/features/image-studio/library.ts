export const IMAGE_STUDIO_LIBRARY_MAX_ITEMS = 200
export const IMAGE_STUDIO_LIBRARY_MAX_BYTES = 512 * 1024 * 1024

/**
 * The v1 library used one global database name for every signed-in user on a
 * browser profile.  Keep that database untouched and start a versioned,
 * user-scoped namespace instead; this intentionally makes old works
 * disappear from the new view rather than risking a cross-account read.
 */
const databaseNamePrefix = 'sub2api-image-studio-v2'
const databaseVersion = 1
const storeName = 'images'

export type StudioLibraryScope = string | number

/** Return the IndexedDB database name for one authenticated user. */
export function getStudioLibraryDatabaseName(scope: StudioLibraryScope): string {
  if (scope === null || scope === undefined) throw new Error('Image library scope is required')
  if (typeof scope !== 'string' && typeof scope !== 'number') throw new Error('Image library scope is invalid')
  if (typeof scope === 'number' && !Number.isFinite(scope)) throw new Error('Image library scope is invalid')
  const normalized = String(scope).trim()
  if (!normalized) throw new Error('Image library scope is required')
  return `${databaseNamePrefix}-${encodeURIComponent(normalized)}`
}

export interface StoredStudioImage {
  id: string
  createdAt: number
  prompt: string
  revisedPrompt?: string
  model: string
  size: string
  actualSize?: string
  outputFormat: string
  apiKeyName: string
  blob: Blob
  bytes: number
}

function openLibrary(scope: StudioLibraryScope): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(getStudioLibraryDatabaseName(scope), databaseVersion)
    request.onerror = () => reject(request.error || new Error('Unable to open image library'))
    request.onupgradeneeded = () => {
      const database = request.result
      if (!database.objectStoreNames.contains(storeName)) {
        const store = database.createObjectStore(storeName, { keyPath: 'id' })
        store.createIndex('createdAt', 'createdAt')
      }
    }
    request.onsuccess = () => resolve(request.result)
  })
}

function transactionDone(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onabort = () => reject(transaction.error || new Error('Image library transaction aborted'))
    transaction.onerror = () => reject(transaction.error || new Error('Image library transaction failed'))
  })
}

function requestResult<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error || new Error('Image library request failed'))
  })
}

export function chooseStudioImagesToDelete(
  images: Array<Pick<StoredStudioImage, 'id' | 'createdAt' | 'bytes'>>,
  maxItems = IMAGE_STUDIO_LIBRARY_MAX_ITEMS,
  maxBytes = IMAGE_STUDIO_LIBRARY_MAX_BYTES,
): string[] {
  const oldestFirst = [...images].sort((a, b) => a.createdAt - b.createdAt)
  let retainedItems = oldestFirst.length
  let retainedBytes = oldestFirst.reduce((total, image) => total + Math.max(0, image.bytes || 0), 0)
  const deleted: string[] = []
  for (const image of oldestFirst) {
    if (retainedItems <= maxItems && retainedBytes <= maxBytes) break
    deleted.push(image.id)
    retainedItems -= 1
    retainedBytes -= Math.max(0, image.bytes || 0)
  }
  return deleted
}

export async function listStoredStudioImages(scope: StudioLibraryScope): Promise<StoredStudioImage[]> {
  const database = await openLibrary(scope)
  try {
    const transaction = database.transaction(storeName, 'readonly')
    const done = transactionDone(transaction)
    const result = await requestResult(transaction.objectStore(storeName).getAll() as IDBRequest<StoredStudioImage[]>)
    await done
    return result.sort((a, b) => b.createdAt - a.createdAt)
  } finally {
    database.close()
  }
}

export async function saveStoredStudioImages(images: StoredStudioImage[], scope: StudioLibraryScope): Promise<string[]> {
  // Validate the scope even when there is nothing to persist; callers should
  // never be able to accidentally bypass the isolation contract via an empty
  // write.
  getStudioLibraryDatabaseName(scope)
  if (images.length === 0) return []
  let database = await openLibrary(scope)
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    for (const image of images) transaction.objectStore(storeName).put(image)
    await transactionDone(transaction)
  } finally {
    database.close()
  }

  const allImages = await listStoredStudioImages(scope)
  const idsToDelete = chooseStudioImagesToDelete(allImages)
  if (idsToDelete.length === 0) return []
  database = await openLibrary(scope)
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    idsToDelete.forEach((id) => transaction.objectStore(storeName).delete(id))
    await transactionDone(transaction)
  } finally {
    database.close()
  }
  return idsToDelete
}

export async function deleteStoredStudioImage(id: string, scope: StudioLibraryScope): Promise<void> {
  const database = await openLibrary(scope)
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    transaction.objectStore(storeName).delete(id)
    await transactionDone(transaction)
  } finally {
    database.close()
  }
}

export async function clearStoredStudioImages(scope: StudioLibraryScope): Promise<void> {
  const database = await openLibrary(scope)
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    transaction.objectStore(storeName).clear()
    await transactionDone(transaction)
  } finally {
    database.close()
  }
}

export function base64ImageToBlob(value: string, outputFormat = 'png'): Blob {
  const normalized = value.replace(/^data:[^;]+;base64,/, '').replace(/\s+/g, '')
  const binary = atob(normalized)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) bytes[index] = binary.charCodeAt(index)
  const mime = outputFormat === 'jpg' || outputFormat === 'jpeg' ? 'image/jpeg' : `image/${outputFormat || 'png'}`
  return new Blob([bytes], { type: mime })
}
