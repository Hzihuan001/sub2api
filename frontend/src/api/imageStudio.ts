import { buildGatewayUrl } from './client'
import { normalizeImageGenerationCount } from '@/utils/imageGenerationPolicy'

export interface ImageStudioModel {
  id: string
  name: string
}

export interface ImageStudioGenerateRequest {
  model: string
  prompt: string
  count: number
  size?: string
  quality?: string
  outputFormat?: 'png' | 'jpeg' | 'webp' | string
  background?: string
  inputFidelity?: string
  images?: File[]
  mask?: File | null
  nativeGemini?: boolean
}

export interface ImageStudioOutput {
  url?: string
  b64Json?: string
  revisedPrompt?: string
  mimeType?: string
}

interface GatewayModelItem {
  id?: string
  name?: string
  display_name?: string
  displayName?: string
}

interface GatewayModelsResponse {
  data?: unknown
}

interface GeminiModelsResponse {
  models?: unknown
}

interface GatewayImageItem {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

interface GatewayImagesResponse {
  data?: GatewayImageItem[]
}

export interface ImageStudioTask {
  id: string
  task_id?: string
  status: 'processing' | 'completed' | 'failed'
  created_at: number
  expires_at: number
  execution_deadline?: number
  result?: GatewayImagesResponse
  error?: { message?: string; code?: string; error?: { message?: string; code?: string } }
}

export function isImageStudioAsyncDisabled(error: unknown): boolean {
  return error instanceof Error
    && (error as Error & { status?: number }).status === 404
    && error.message === 'async image tasks are not enabled'
}

export function imageStudioTaskOutputs(task: ImageStudioTask): ImageStudioOutput[] {
  return imageOutputs(task.result || {})
}

function imageOutputs(payload: GatewayImagesResponse): ImageStudioOutput[] {
  return (payload.data || []).map((item) => ({
    url: item.url,
    b64Json: item.b64_json,
    revisedPrompt: item.revised_prompt,
  })).filter((item) => item.url || item.b64Json)
}

async function withImageStudioTimeout<T>(signal: AbortSignal | undefined, timeout: number, run: (signal: AbortSignal) => Promise<T>): Promise<T> {
  const controller = new AbortController()
  const abort = () => controller.abort(signal?.reason)
  signal?.addEventListener('abort', abort, { once: true })
  if (signal?.aborted) abort()
  let timedOut = false
  const timer = setTimeout(() => { timedOut = true; controller.abort() }, timeout)
  try { return await run(controller.signal) } catch (error) {
    if (timedOut) throw new Error('Image task request timed out')
    throw error
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
  }
}

export async function submitImageStudioTask(
  apiKey: string,
  request: ImageStudioGenerateRequest,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<ImageStudioTask> {
  return withImageStudioTimeout(signal, 30_000, async (requestSignal) => {
    const editing = (request.images?.length || 0) > 0
    const response = await fetch(buildGatewayUrl(editing ? '/v1/images/edits/async' : '/v1/images/generations/async'), {
      method: 'POST',
      headers: authHeaders(apiKey, {
        ...(!editing ? { 'Content-Type': 'application/json' } : {}),
        'Idempotency-Key': idempotencyKey,
      }),
      body: editing ? buildEditForm(request) : JSON.stringify(buildJSONBody(request)),
      signal: requestSignal,
    })
    if (!response.ok) throw await parseGatewayError(response)
    const task = await response.json() as ImageStudioTask
    if (!(task.task_id || task.id) || !['processing', 'completed', 'failed'].includes(task.status)) {
      throw new Error('Image task response is invalid')
    }
    return task
  })
}

async function retryImageStudioGet<T>(request: () => Promise<T>, signal?: AbortSignal): Promise<T> {
  for (let attempt = 0; ; attempt += 1) {
    try { return await request() } catch (error) {
      const status = (error as { status?: number })?.status
      if (signal?.aborted || attempt >= 2 || (status !== undefined && status < 500)) throw error
      await new Promise<void>((resolve, reject) => {
        const finish = () => { signal?.removeEventListener('abort', abort); resolve() }
        const timer = setTimeout(finish, attempt === 0 ? 500 : 1500)
        const abort = () => { clearTimeout(timer); signal?.removeEventListener('abort', abort); reject(new DOMException('Aborted', 'AbortError')) }
        signal?.addEventListener('abort', abort, { once: true })
        if (signal?.aborted) abort()
      })
    }
  }
}

export async function getImageStudioTask(apiKey: string, taskID: string, signal?: AbortSignal): Promise<ImageStudioTask> {
  return retryImageStudioGet(() => withImageStudioTimeout(signal, 30_000, async (requestSignal) => {
    const response = await fetch(buildGatewayUrl(`/v1/images/tasks/${encodeURIComponent(taskID)}`), {
      headers: authHeaders(apiKey), signal: requestSignal, cache: 'no-store',
    })
    if (!response.ok) throw await parseGatewayError(response)
    return response.json() as Promise<ImageStudioTask>
  }), signal)
}

/** Only this task's exact protected path may receive an API-key header. */
export async function fetchImageStudioTaskImage(apiKey: string, taskID: string, url: string, signal?: AbortSignal): Promise<Blob> {
  const prefix = `/v1/images/tasks/${encodeURIComponent(taskID)}/images/`
  let requestURL = url
  let protectedPath = url.startsWith(prefix) && /^\d+\.(png|jpe?g|webp|gif|avif)$/i.test(url.slice(prefix.length))
  if (protectedPath) requestURL = buildGatewayUrl(url)
  try {
    const parsed = new URL(url, typeof window === 'undefined' ? 'http://localhost' : window.location.origin)
    const gatewayOrigin = new URL(buildGatewayUrl('/'), parsed).origin
    const sameGateway = parsed.origin === gatewayOrigin
    const relativePath = `${parsed.pathname}${parsed.search}`
    if (sameGateway && relativePath.startsWith(prefix) && /^\d+\.(png|jpe?g|webp|gif|avif)$/i.test(relativePath.slice(prefix.length))) {
      requestURL = buildGatewayUrl(relativePath)
      protectedPath = true
    }
  } catch { /* treat malformed or foreign URLs as unauthenticated */ }
  return retryImageStudioGet(() => withImageStudioTimeout(signal, 120_000, async (requestSignal) => {
    const response = await fetch(protectedPath ? requestURL : url, {
      ...(protectedPath ? { headers: authHeaders(apiKey), redirect: 'error' as const } : { credentials: 'omit' as const }),
      signal: requestSignal,
    })
    if (!response.ok) throw await parseGatewayError(response)
    return response.blob()
  }), signal)
}

interface GeminiImageResponse {
  candidates?: Array<{
    content?: {
      parts?: Array<{
        inlineData?: { mimeType?: string; data?: string }
        inline_data?: { mime_type?: string; data?: string }
      }>
    }
  }>
}

const imageModelPattern = /(gpt-image|dall-e|grok-imagine|imagen|imagegen|image-generation|gemini.*image|nano[-_ ]?banana|flux|seedream|midjourney|stable[-_ ]?diffusion|\bsd3\b)/i

export function isLikelyImageModel(modelID: string): boolean {
  return imageModelPattern.test(modelID.trim())
}

async function parseGatewayError(response: Response): Promise<Error> {
  let message = response.statusText || `HTTP ${response.status}`
  let code: string | number = response.status
  try {
    const payload = await response.json()
    message = payload?.error?.message || payload?.message || message
    code = payload?.error?.code || payload?.code || code
  } catch {
    // Keep the HTTP fallback when the upstream returned a non-JSON error page.
  }
  const error = new Error(message)
  ;(error as Error & { code?: string | number; status?: number }).code = code
  ;(error as Error & { code?: string | number; status?: number }).status = response.status
  return error
}

function authHeaders(apiKey: string, extra?: HeadersInit): HeadersInit {
  return { Authorization: `Bearer ${apiKey}`, ...extra }
}

export interface ImageStudioModelListOptions {
  nativeGemini?: boolean
}

function normalizeModelID(value: unknown): string {
  return typeof value === 'string' ? value.trim().replace(/^models\//i, '') : ''
}

function modelItems(value: unknown): GatewayModelItem[] {
  if (!Array.isArray(value)) return []
  return value.filter((item): item is GatewayModelItem =>
    item !== null && typeof item === 'object' && !Array.isArray(item)
    && (normalizeModelID(item.id) !== '' || normalizeModelID(item.name) !== ''),
  )
}

export async function listImageStudioModels(
  apiKey: string,
  signal?: AbortSignal,
  options?: ImageStudioModelListOptions,
): Promise<ImageStudioModel[]> {
  const nativeGemini = options?.nativeGemini === true
  const response = await fetch(buildGatewayUrl(nativeGemini ? '/v1beta/models' : '/v1/models'), {
    headers: authHeaders(apiKey),
    signal,
  })
  if (!response.ok) throw await parseGatewayError(response)
  const payload = await response.json() as (GatewayModelsResponse & GeminiModelsResponse) | null
  // An empty native list may be the result of server-side permission filtering.
  const useNativeItems = nativeGemini && Array.isArray(payload?.models)
  const items = useNativeItems ? modelItems(payload?.models) : modelItems(payload?.data)
  const seen = new Set<string>()
  return items
    .map((item) => ({
      id: normalizeModelID(useNativeItems ? item.name || item.id : item.id || item.name),
      name: String(item.displayName || item.display_name || item.name || item.id || '').trim(),
    }))
    .filter((item) => item.id && isLikelyImageModel(item.id))
    .filter((item) => {
      if (seen.has(item.id)) return false
      seen.add(item.id)
      return true
    })
}

function appendOptionalFormField(form: FormData, name: string, value?: string): void {
  if (value && value !== 'auto') form.append(name, value)
}

function buildJSONBody(request: ImageStudioGenerateRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {
    model: request.model,
    prompt: request.prompt,
    n: normalizeImageGenerationCount(request.count),
    response_format: 'b64_json',
  }
  if (request.size && request.size !== 'auto') body.size = request.size
  if (request.quality && request.quality !== 'auto') body.quality = request.quality
  if (request.outputFormat) body.output_format = request.outputFormat
  if (request.background && request.background !== 'auto') body.background = request.background
  return body
}

function buildEditForm(request: ImageStudioGenerateRequest): FormData {
  const form = new FormData()
  form.append('model', request.model)
  form.append('prompt', request.prompt)
  form.append('n', String(normalizeImageGenerationCount(request.count)))
  form.append('response_format', 'b64_json')
  appendOptionalFormField(form, 'size', request.size)
  appendOptionalFormField(form, 'quality', request.quality)
  appendOptionalFormField(form, 'output_format', request.outputFormat)
  appendOptionalFormField(form, 'background', request.background)
  appendOptionalFormField(form, 'input_fidelity', request.inputFidelity)
  for (const image of request.images || []) form.append('image', image, image.name)
  if (request.mask) form.append('mask', request.mask, request.mask.name)
  return form
}

export async function generateImageStudioImages(
  apiKey: string,
  request: ImageStudioGenerateRequest,
  signal?: AbortSignal,
): Promise<ImageStudioOutput[]> {
  if (request.nativeGemini) return generateNativeGeminiImageStudioImages(apiKey, request, signal)
  const editing = (request.images?.length || 0) > 0
  const response = await fetch(buildGatewayUrl(editing ? '/v1/images/edits' : '/v1/images/generations'), {
    method: 'POST',
    headers: editing ? authHeaders(apiKey) : authHeaders(apiKey, { 'Content-Type': 'application/json' }),
    body: editing ? buildEditForm(request) : JSON.stringify(buildJSONBody(request)),
    signal,
  })
  if (!response.ok) throw await parseGatewayError(response)
  const payload = await response.json() as GatewayImagesResponse
  const outputs = imageOutputs(payload)
  if (outputs.length === 0) throw new Error('Image provider returned no image data')
  return outputs
}

function fileAsBase64(file: File): Promise<string> {
  return file.arrayBuffer().then((buffer) => {
    const bytes = new Uint8Array(buffer)
    let binary = ''
    const chunkSize = 0x8000
    for (let offset = 0; offset < bytes.length; offset += chunkSize) {
      binary += String.fromCharCode(...bytes.subarray(offset, Math.min(offset + chunkSize, bytes.length)))
    }
    return btoa(binary)
  })
}

function aspectRatioForImageSize(size: string | undefined): string | undefined {
  const match = /^\s*(\d+)x(\d+)\s*$/i.exec(String(size || ''))
  if (!match) return undefined
  const width = Number(match[1])
  const height = Number(match[2])
  if (!width || !height) return undefined
  const ratio = width / height
  const supported: Array<[number, number, string]> = [
    [1, 1, '1:1'], [4, 3, '4:3'], [3, 4, '3:4'], [16, 9, '16:9'], [9, 16, '9:16'],
    [3, 2, '3:2'], [2, 3, '2:3'], [5, 4, '5:4'], [4, 5, '4:5'], [21, 9, '21:9'],
  ]
  return supported.reduce((best, candidate) =>
    Math.abs(candidate[0] / candidate[1] - ratio) < Math.abs(best[0] / best[1] - ratio) ? candidate : best,
  )[2]
}

async function generateNativeGeminiImageStudioImages(
  apiKey: string,
  request: ImageStudioGenerateRequest,
  signal?: AbortSignal,
): Promise<ImageStudioOutput[]> {
  const references = await Promise.all((request.images || []).map(async (file) => ({
    inlineData: { mimeType: file.type || 'image/png', data: await fileAsBase64(file) },
  })))
  const mask = request.mask
    ? [{ inlineData: { mimeType: request.mask.type || 'image/png', data: await fileAsBase64(request.mask) } }]
    : []
  const count = normalizeImageGenerationCount(request.count)
  const outputs: ImageStudioOutput[] = []
  for (let index = 0; index < count; index += 1) {
    const size = String(request.size || '2K').trim().toUpperCase()
    const tier = /^(1K|2K|4K)$/.test(size) ? size : '2K'
    const imageConfig: Record<string, string> = { imageSize: tier }
    const aspectRatio = aspectRatioForImageSize(request.size)
    if (aspectRatio) imageConfig.aspectRatio = aspectRatio
    const body = {
      contents: [{ role: 'user', parts: [{ text: request.prompt }, ...references, ...mask] }],
      generationConfig: { responseModalities: ['IMAGE'], imageConfig },
    }
    const response = await fetch(buildGatewayUrl(`/v1beta/models/${encodeURIComponent(request.model)}:generateContent`), {
      method: 'POST',
      headers: authHeaders(apiKey, { 'Content-Type': 'application/json' }),
      body: JSON.stringify(body),
      signal,
    })
    if (!response.ok) throw await parseGatewayError(response)
    const payload = await response.json() as GeminiImageResponse
    const parts = payload.candidates?.flatMap((candidate) => candidate.content?.parts || []) || []
    for (const part of parts) {
      const inline = part.inlineData || part.inline_data
      const data = inline?.data?.trim()
      const inlineRecord = inline as { mimeType?: string; mime_type?: string } | undefined
      const mimeType = inlineRecord?.mimeType || inlineRecord?.mime_type
      if (data) outputs.push({ b64Json: data, mimeType })
    }
  }
  if (outputs.length === 0) throw new Error('Gemini image provider returned no image data')
  return outputs
}

