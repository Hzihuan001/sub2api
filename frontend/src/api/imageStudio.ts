import { buildGatewayUrl } from './client'

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
}

interface GatewayModelsResponse {
  data?: GatewayModelItem[]
}

interface GatewayImageItem {
  url?: string
  b64_json?: string
  revised_prompt?: string
}

interface GatewayImagesResponse {
  data?: GatewayImageItem[]
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

export async function listImageStudioModels(apiKey: string, signal?: AbortSignal): Promise<ImageStudioModel[]> {
  const response = await fetch(buildGatewayUrl('/v1/models'), {
    headers: authHeaders(apiKey),
    signal,
  })
  if (!response.ok) throw await parseGatewayError(response)
  const payload = await response.json() as GatewayModelsResponse
  const seen = new Set<string>()
  return (payload.data || [])
    .map((item) => ({
      id: String(item.id || item.name || '').trim(),
      name: String(item.display_name || item.name || item.id || '').trim(),
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
    n: Math.min(4, Math.max(1, Math.trunc(request.count || 1))),
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
  form.append('n', String(Math.min(4, Math.max(1, Math.trunc(request.count || 1)))))
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
  const outputs = (payload.data || []).map((item) => ({
    url: item.url,
    b64Json: item.b64_json,
    revisedPrompt: item.revised_prompt,
  })).filter((item) => item.url || item.b64Json)
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
  const count = Math.min(4, Math.max(1, Math.trunc(request.count || 1)))
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

