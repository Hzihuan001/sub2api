import { describe, expect, it } from 'vitest'
import {
  getImageOutputFormats,
  getImageStudioModelPolicy,
  normalizeImageGenerationCount,
  normalizeImageOutputFormat,
} from '../imageGenerationPolicy'

describe('shared image output policy', () => {
  it.each([
    { model: 'gpt-image-2.5-sunburst', native: false, formats: ['png', 'jpeg', 'webp'] },
    { model: 'seedream-5.0-pro', native: false, formats: ['png', 'jpeg'] },
    { model: ' SEEDREAM-5.0-PRO-CUSTOM ', native: false, formats: ['png', 'jpeg'] },
    { model: 'vendor/seedream-5-0-pro', native: false, formats: ['png', 'jpeg'] },
    { model: 'gemini-nano-banana-2.1', native: false, formats: ['png', 'jpeg', 'webp'] },
    { model: 'gemini-3.1-flash-image', native: false, formats: ['png', 'jpeg', 'webp'] },
    { model: 'custom-alias', native: true, formats: ['png'] },
    { model: 'seedream-5.0-pro', native: true, formats: ['png'] },
  ])('keeps the caller-selected protocol and formats for $model (native=$native)', ({ model, native, formats }) => {
    expect(getImageOutputFormats(model, native)).toEqual(formats)
  })

  it.each(['format', 'mime'] as const)('preserves Seedream/native restrictions in %s representation', (representation) => {
    const prefix = representation === 'mime' ? 'image/' : ''
    for (const requested of ['', `${prefix}png`, `${prefix}jpeg`, `${prefix}webp`, `${prefix}avif`, 'JPEG', 'image/jpeg', 'jpeg']) {
      const seedream = requested === `${prefix}jpeg` ? `${prefix}jpeg` : `${prefix}png`
      expect(normalizeImageOutputFormat('seedream-5.0-pro', false, requested, representation)).toBe(seedream)
      expect(normalizeImageOutputFormat('gemini-3-pro-image', true, requested, representation)).toBe(`${prefix}png`)
    }
  })

  it.each(['format', 'mime'] as const)('does not rewrite compatible-provider unknown or empty values in %s representation', (representation) => {
    for (const model of ['gpt-image-2', 'gemini-3.1-flash-image', 'gemini-nano-banana-2.1', 'custom-image']) {
      for (const requested of ['', 'jpeg', 'image/jpeg', 'avif', 'image/avif', 'PNG']) {
        expect(normalizeImageOutputFormat(model, false, requested, representation)).toBe(requested)
      }
    }
  })

  it('keeps the batch empty-MIME fallback at the caller, separate from Studio format passthrough', () => {
    expect(normalizeImageOutputFormat('gpt-image-2', false, '')).toBe('')
    expect(normalizeImageOutputFormat('gpt-image-2', false, '', 'mime') || 'image/png').toBe('image/png')
    expect(normalizeImageOutputFormat('seedream-5.0-pro', false, 'jpeg', 'mime')).toBe('image/png')
    expect(normalizeImageOutputFormat('seedream-5.0-pro', false, 'image/jpeg', 'mime')).toBe('image/jpeg')
  })
})

describe('Studio-only model policy', () => {
  it.each([
    { model: 'gpt-image-2', native: false, references: 16, advanced: true, single: false },
    { model: ' GPT-IMAGE-2.5-SUNBURST ', native: false, references: 16, advanced: true, single: false },
    { model: 'gpt-image-2.5-flare', native: false, references: 16, advanced: true, single: false },
    { model: 'gpt-image-2-2026-04-21', native: false, references: 16, advanced: true, single: false },
    { model: 'vendor/gpt-image-1.5', native: false, references: 16, advanced: true, single: false },
    { model: 'chatgpt-image-latest', native: false, references: 16, advanced: true, single: false },
    { model: 'gpt-image-2', native: true, references: 14, advanced: false, single: false },
    { model: 'custom-image-alias', native: false, references: 4, advanced: true, single: false },
    { model: 'not-gpt-image-2', native: false, references: 4, advanced: true, single: false },
    { model: 'gpt-imagery', native: false, references: 4, advanced: true, single: false },
    { model: 'seedream-5.0-pro', native: false, references: 10, advanced: false, single: true },
    { model: 'gemini-nano-banana-2.1-preview', native: false, references: 14, advanced: true, single: false },
    { model: 'gemini-3.1-flash-image', native: false, references: 4, advanced: true, single: false },
    { model: 'gemini-3.1-flash-image', native: true, references: 14, advanced: false, single: false },
    { model: 'custom-alias', native: true, references: 14, advanced: false, single: false },
    { model: 'seedream-5.0-pro', native: true, references: 14, advanced: false, single: true },
  ])('applies the model-specific Studio limits for $model (native=$native)', ({ model, native, references, advanced, single }) => {
    expect(getImageStudioModelPolicy(model, native)).toEqual({
      referenceImageLimit: references,
      supportsAdvancedOptions: advanced,
      forceSingleOutput: single,
      countOptions: single ? [1] : [1, 2, 3, 4],
      outputFormats: getImageOutputFormats(model, native),
    })
  })
})

describe('image generation count normalization', () => {
  it.each([
    [0, 1], [-3, 1], [0.9, 1], [1, 1], [2.9, 2], [4, 4], [9, 4],
    [Number.NaN, 1], [Number.POSITIVE_INFINITY, 4], [Number.NEGATIVE_INFINITY, 1],
  ])('normalizes %s to %s for all three request transports', (count, expected) => {
    expect(normalizeImageGenerationCount(count)).toBe(expected)
  })
})
