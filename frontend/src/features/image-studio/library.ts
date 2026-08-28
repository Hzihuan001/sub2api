export const IMAGE_STUDIO_LIBRARY_MAX_ITEMS = 200
export const IMAGE_STUDIO_LIBRARY_MAX_BYTES = 512 * 1024 * 1024

const databaseName = 'sub2api-image-studio'
const databaseVersion = 1
const storeName = 'images'

export interface StoredStudioImage {
  id: string
  createdAt: number
  prompt: string
  revisedPrompt?: string
  model: string
  size: string
  outputFormat: string
  apiKeyName: string
  blob: Blob
  bytes: number
}

function openLibrary(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(databaseName, databaseVersion)
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

export async function listStoredStudioImages(): Promise<StoredStudioImage[]> {
  const database = await openLibrary()
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

export async function saveStoredStudioImages(images: StoredStudioImage[]): Promise<string[]> {
  if (images.length === 0) return []
  let database = await openLibrary()
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    for (const image of images) transaction.objectStore(storeName).put(image)
    await transactionDone(transaction)
  } finally {
    database.close()
  }

  const allImages = await listStoredStudioImages()
  const idsToDelete = chooseStudioImagesToDelete(allImages)
  if (idsToDelete.length === 0) return []
  database = await openLibrary()
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    idsToDelete.forEach((id) => transaction.objectStore(storeName).delete(id))
    await transactionDone(transaction)
  } finally {
    database.close()
  }
  return idsToDelete
}

export async function deleteStoredStudioImage(id: string): Promise<void> {
  const database = await openLibrary()
  try {
    const transaction = database.transaction(storeName, 'readwrite')
    transaction.objectStore(storeName).delete(id)
    await transactionDone(transaction)
  } finally {
    database.close()
  }
}

export async function clearStoredStudioImages(): Promise<void> {
  const database = await openLibrary()
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
