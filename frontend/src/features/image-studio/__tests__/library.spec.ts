import { describe, expect, it } from 'vitest'

import { base64ImageToBlob, chooseStudioImagesToDelete } from '../library'

describe('image studio local library', () => {
  it('deletes the oldest images first to enforce both item and byte limits', () => {
    const images = [
      { id: 'oldest', createdAt: 1, bytes: 4 },
      { id: 'middle', createdAt: 2, bytes: 5 },
      { id: 'newest', createdAt: 3, bytes: 6 },
    ]

    expect(chooseStudioImagesToDelete(images, 2, 11)).toEqual(['oldest'])
    expect(chooseStudioImagesToDelete(images, 3, 10)).toEqual(['oldest', 'middle'])
  })

  it('drops an image that cannot fit even when it is the newest item', () => {
    expect(chooseStudioImagesToDelete([
      { id: 'too-large', createdAt: 10, bytes: 11 },
    ], 200, 10)).toEqual(['too-large'])
  })

  it('converts plain and data-url base64 values into typed blobs', async () => {
    const png = base64ImageToBlob('aGVsbG8=', 'png')
    const jpeg = base64ImageToBlob('data:image/jpeg;base64,d29ybGQ=', 'jpeg')

    expect(png.type).toBe('image/png')
    expect(png.size).toBe(5)
    expect(jpeg.type).toBe('image/jpeg')
    expect(jpeg.size).toBe(5)
  })
})
