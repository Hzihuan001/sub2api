import { describe, expect, it } from 'vitest'

import {
  IMAGE_STUDIO_CUSTOM_SIZE,
  IMAGE_STUDIO_SIZE_PRESETS,
  normalizeCustomImageSize,
  parseImageDimensions,
  imageStudioPresetForDimensions,
  resolveImageStudioPresetSize,
  verifyImageSize,
} from '../size'

describe('image studio size helpers', () => {
  it('maps the workbench size tiers to concrete upstream dimensions', () => {
    expect(IMAGE_STUDIO_SIZE_PRESETS).toEqual({
      '1K': '1024x1024',
      '2K': '2048x2048',
      '4K': '3840x2160',
    })
    expect(resolveImageStudioPresetSize('1K')).toBe('1024x1024')
    expect(resolveImageStudioPresetSize('2K')).toBe('2048x2048')
    expect(resolveImageStudioPresetSize('4K')).toBe('3840x2160')
    expect(resolveImageStudioPresetSize(IMAGE_STUDIO_CUSTOM_SIZE, '2048x1152')).toBe('2048x1152')
    expect(imageStudioPresetForDimensions('1024x1024')).toBe('1K')
    expect(imageStudioPresetForDimensions('3840x2160')).toBe('4K')
    expect(imageStudioPresetForDimensions('1536x1024')).toBeNull()
  })

  it('accepts official-style custom dimensions and normalizes them', () => {
    expect(normalizeCustomImageSize(2048, 2048)).toBe('2048x2048')
    expect(normalizeCustomImageSize('2048', '1152')).toBe('2048x1152')
  })

  it('rejects sizes that are not aligned, too large, or outside the supported aspect ratio', () => {
    expect(normalizeCustomImageSize(1025, 1024)).toBeNull()
    expect(normalizeCustomImageSize(3840, 3840)).toBeNull()
    expect(normalizeCustomImageSize(4096, 256)).toBeNull()
    expect(normalizeCustomImageSize(256, 1024)).toBeNull()
  })

  it('parses persisted custom sizes and keeps the custom option distinct', () => {
    expect(parseImageDimensions(' 2048 x 2048 ')).toEqual({ width: 2048, height: 2048 })
    expect(parseImageDimensions(IMAGE_STUDIO_CUSTOM_SIZE)).toBeNull()
    expect(parseImageDimensions('1024×1024')).toBeNull()
  })

  it('verifies returned pixels without implying how the model rendered them', () => {
    expect(verifyImageSize('3840x2160', '3840x2160')).toBe('matched')
    expect(verifyImageSize('3840x2160', '1672x941')).toBe('mismatch')
    expect(verifyImageSize('auto', '1672x941')).toBe('observed')
    expect(verifyImageSize('3840x2160', undefined)).toBe('unknown')
    expect(verifyImageSize('3840x2160', 'not-an-image-size')).toBe('unknown')
  })
})
