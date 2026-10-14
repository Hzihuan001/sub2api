import { isGeminiNanoBananaModel, isSeedreamImageModel } from '@/features/image-studio/size'

export type ImageOutputFormat = 'png' | 'jpeg' | 'webp'

/** Protocol selection belongs to the caller; model names alone do not select native Gemini. */
export function getImageOutputFormats(model: string, nativeGemini = false): readonly ImageOutputFormat[] {
  if (nativeGemini) return ['png']
  return isSeedreamImageModel(model) ? ['png', 'jpeg'] : ['png', 'jpeg', 'webp']
}

/** Keep unknown compatible-provider formats unchanged, including the caller's empty-value policy. */
export function normalizeImageOutputFormat(
  model: string,
  nativeGemini: boolean,
  requested: string,
  representation: 'format' | 'mime' = 'format',
): string {
  const prefix = representation === 'mime' ? 'image/' : ''
  if (nativeGemini) return `${prefix}png`
  if (isSeedreamImageModel(model)) return requested === `${prefix}jpeg` ? `${prefix}jpeg` : `${prefix}png`
  return requested
}

/** Studio limits differ from batch limits, so this policy is intentionally Studio-only. */
export function getImageStudioModelPolicy(model: string, nativeGemini = false) {
  const seedream = isSeedreamImageModel(model)
  return {
    outputFormats: getImageOutputFormats(model, nativeGemini),
    referenceImageLimit: nativeGemini || isGeminiNanoBananaModel(model) ? 14 : seedream ? 10 : 4,
    countOptions: seedream ? [1] : [1, 2, 3, 4],
    forceSingleOutput: seedream,
    supportsAdvancedOptions: !nativeGemini && !seedream,
  }
}

/** Shared by JSON generations, multipart edits, and native Gemini fan-out. */
export function normalizeImageGenerationCount(count: number): number {
  return Math.min(4, Math.max(1, Math.trunc(count || 1)))
}
