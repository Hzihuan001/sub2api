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

export interface ImageDimensions {
  width: number
  height: number
}

export function parseImageDimensions(value: string | undefined | null): ImageDimensions | null {
  if (!value) return null
  const match = /^\s*(\d+)\s*x\s*(\d+)\s*$/i.exec(value)
  if (!match) return null
  const width = Number(match[1])
  const height = Number(match[2])
  if (!Number.isSafeInteger(width) || !Number.isSafeInteger(height)) return null
  return { width, height }
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
