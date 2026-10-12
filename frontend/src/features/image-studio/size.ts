/**
 * Image Studio size helpers.
 *
 * The gateway accepts the OpenAI image-size syntax (`WIDTHxHEIGHT`).  Keep
 * validation in one small, dependency-free module so the form and tests use
 * exactly the same rules.
 */
export const IMAGE_STUDIO_CUSTOM_SIZE = 'custom'
export const IMAGE_STUDIO_MIN_DIMENSION = 256
export const IMAGE_STUDIO_MAX_DIMENSION = 3840
export const IMAGE_STUDIO_MAX_PIXEL_AREA = 3840 * 2160
export const IMAGE_STUDIO_SEEDREAM_MAX_PIXEL_AREA = 4_624_220

/** Preset tiers shown by the workbench and the concrete dimensions sent to GPT-image. */
export const IMAGE_STUDIO_SIZE_PRESETS = {
  '1K': '1024x1024',
  '2K': '2048x2048',
  '4K': '3840x2160',
} as const

export type ImageStudioSizePreset = keyof typeof IMAGE_STUDIO_SIZE_PRESETS

/** Native Gemini Nano Banana accepts image-size tiers, not WIDTHxHEIGHT. */
export function isGeminiNanoBananaModel(model: string | undefined | null): boolean {
  const normalized = String(model || '').trim().toLowerCase()
  return normalized === 'gemini-nano-banana-2.1' || normalized.startsWith('gemini-nano-banana-2.1-')
}

/** Seedream aliases are forwarded through an OpenAI-compatible image adapter. */
export function isSeedreamImageModel(model: string | undefined | null): boolean {
  const normalized = String(model || '').trim().toLowerCase()
  return normalized === 'seedream-5.0-pro' || normalized.startsWith('seedream-5.0-pro-')
}

export function supportsImageStudioCustomSize(model: string | undefined | null): boolean {
  return !isGeminiNanoBananaModel(model)
}

/** Seedream 5.0 Pro's published contract tops out at the 2K tier. */
export function supportsImageStudio4K(model: string | undefined | null): boolean {
  return !isSeedreamImageModel(model)
}

export function normalizeImageStudioCustomSize(model: string | undefined | null, width: number | string, height: number | string): string | null {
  const normalized = normalizeCustomImageSize(width, height)
  if (!normalized) return null
  if (!isSeedreamImageModel(model)) return normalized
  const dimensions = parseImageDimensions(normalized)
  return dimensions && dimensions.width * dimensions.height <= IMAGE_STUDIO_SEEDREAM_MAX_PIXEL_AREA ? normalized : null
}

/** Resolve the request representation for a model-specific image-size contract. */
export function resolveImageStudioRequestSize(
  model: string | undefined | null,
  value: string | undefined | null,
  customSize = '',
): string {
  const normalized = String(value || '').trim().toUpperCase()
  if (isGeminiNanoBananaModel(model)) {
    return normalized in IMAGE_STUDIO_SIZE_PRESETS ? normalized : ''
  }
  return resolveImageStudioPresetSize(value, customSize)
}

export function imageStudioPresetForDimensions(value: string | undefined | null): ImageStudioSizePreset | null {
  const dimensions = parseImageDimensions(value)
  if (!dimensions) return null
  const entry = (Object.entries(IMAGE_STUDIO_SIZE_PRESETS) as Array<[ImageStudioSizePreset, string]>)
    .find(([, presetSize]) => presetSize === `${dimensions.width}x${dimensions.height}`)
  return entry?.[0] || null
}

export function resolveImageStudioPresetSize(value: string | undefined | null, customSize = ''): string {
  const normalized = String(value || '').trim().toUpperCase()
  if (normalized in IMAGE_STUDIO_SIZE_PRESETS) {
    return IMAGE_STUDIO_SIZE_PRESETS[normalized as ImageStudioSizePreset]
  }
  if (value === IMAGE_STUDIO_CUSTOM_SIZE) return customSize
  return String(value || '').trim()
}

export interface ImageDimensions {
  width: number
  height: number
}

export type ImageSizeVerification = 'matched' | 'mismatch' | 'observed' | 'unknown'

export function parseImageDimensions(value: string | undefined | null): ImageDimensions | null {
  if (!value) return null
  const match = /^\s*(\d+)\s*x\s*(\d+)\s*$/i.exec(value)
  if (!match) return null
  const width = Number(match[1])
  const height = Number(match[2])
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height)) return null
  return { width, height }
}

/**
 * Compare the requested dimensions with the intrinsic dimensions of the
 * returned image.  A request of `auto` (or another non-dimensional value)
 * cannot be compared, but a readable response is still useful to report as
 * observed.  This deliberately compares pixels only; it does not claim to
 * prove how the upstream model produced those pixels.
 */
export function verifyImageSize(
  requestedSize: string | undefined | null,
  actualSize: string | undefined | null,
): ImageSizeVerification {
  const actual = parseImageDimensions(actualSize)
  if (!actual) return 'unknown'
  const requested = parseImageDimensions(requestedSize)
  if (!requested) return 'observed'
  return requested.width === actual.width && requested.height === actual.height ? 'matched' : 'mismatch'
}

export function normalizeCustomImageSize(width: number | string, height: number | string): string | null {
  const dimensions = { width: Number(width), height: Number(height) }
  if (!Number.isSafeInteger(dimensions.width) || !Number.isSafeInteger(dimensions.height)) return null
  if (dimensions.width < IMAGE_STUDIO_MIN_DIMENSION || dimensions.height < IMAGE_STUDIO_MIN_DIMENSION) return null
  if (dimensions.width > IMAGE_STUDIO_MAX_DIMENSION || dimensions.height > IMAGE_STUDIO_MAX_DIMENSION) return null
  if (dimensions.width % 16 !== 0 || dimensions.height % 16 !== 0) return null
  if (dimensions.width * dimensions.height > IMAGE_STUDIO_MAX_PIXEL_AREA) return null
  const ratio = dimensions.width / dimensions.height
  if (ratio < 1 / 3 || ratio > 3) return null
  return `${dimensions.width}x${dimensions.height}`
}

export function isSupportedCustomImageSize(width: number | string, height: number | string): boolean {
  return normalizeCustomImageSize(width, height) !== null
}

/** Resolve an image's intrinsic dimensions without making assumptions about its file format. */
export async function detectImageDimensions(blob: Blob): Promise<string | undefined> {
  try {
    if (typeof createImageBitmap === 'function') {
      const bitmap = await createImageBitmap(blob)
      const size = `${bitmap.width}x${bitmap.height}`
      bitmap.close()
      return size
    }
    if (typeof Image === 'undefined' || typeof URL === 'undefined' || typeof URL.createObjectURL !== 'function') return undefined
    const objectURL = URL.createObjectURL(blob)
    try {
      const image = new Image()
      const loaded = await new Promise<string>((resolve, reject) => {
        image.onload = () => resolve(`${image.naturalWidth || image.width}x${image.naturalHeight || image.height}`)
        image.onerror = () => reject(new Error('Unable to read image dimensions'))
        image.src = objectURL
      })
      return loaded
    } finally {
      URL.revokeObjectURL(objectURL)
    }
  } catch {
    return undefined
  }
}
