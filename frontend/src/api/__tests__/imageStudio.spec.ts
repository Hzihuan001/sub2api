import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  generateImageStudioImages,
  isLikelyImageModel,
  listImageStudioModels,
} from '../imageStudio'

describe('imageStudio API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('recognizes common image model IDs without treating chat models as image models', () => {
    expect(isLikelyImageModel('gpt-image-1')).toBe(true)
    expect(isLikelyImageModel('grok-imagine-1.0')).toBe(true)
    expect(isLikelyImageModel('gemini-2.5-flash-image')).toBe(true)
    expect(isLikelyImageModel('gemini-nano-banana-2.1')).toBe(true)
    expect(isLikelyImageModel('seedream-5.0-pro')).toBe(true)
    expect(isLikelyImageModel('claude-sonnet-4')).toBe(false)
  })

  it('uses the native Gemini generateContent route for Gemini image keys', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ candidates: [{ content: { parts: [{ inlineData: { mimeType: 'image/png', data: 'aW1hZ2U=' } }] } }] }),
    } as Response)

    const result = await generateImageStudioImages('sk-gemini', {
      model: 'gemini-nano-banana-2.1',
      prompt: 'a lighthouse',
      count: 1,
      size: '4K',
      nativeGemini: true,
    })

    expect(result).toEqual([{ b64Json: 'aW1hZ2U=', mimeType: 'image/png' }])
    const [url, options] = vi.mocked(fetch).mock.calls[0]
    expect(String(url)).toMatch(/\/v1beta\/models\/gemini-nano-banana-2\.1:generateContent$/)
    expect(JSON.parse(String(options?.body))).toMatchObject({
      generationConfig: { responseModalities: ['IMAGE'], imageConfig: { imageSize: '4K' } },
    })
  })

  it('loads, filters, and deduplicates models through the selected API key', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        data: [
          { id: 'gpt-image-1', name: 'GPT Image' },
          { id: 'gpt-image-1', name: 'Duplicate' },
          { id: 'gpt-5', name: 'Chat' },
          { id: 'grok-imagine-1.0' },
        ],
      }),
    } as Response)

    await expect(listImageStudioModels('sk-image')).resolves.toEqual([
      { id: 'gpt-image-1', name: 'GPT Image' },
      { id: 'grok-imagine-1.0', name: 'grok-imagine-1.0' },
    ])
    expect(fetch).toHaveBeenCalledWith(
      expect.stringMatching(/\/v1\/models$/),
      expect.objectContaining({ headers: { Authorization: 'Bearer sk-image' } }),
    )
  })

  it('loads native Gemini models from the Gemini v1beta envelope', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({
        models: [
          { name: 'models/gemini-nano-banana-2.1', display_name: 'Nano Banana 2.1' },
          { name: 'models/gemini-2.5-flash', display_name: 'Chat' },
          { name: 'models/gemini-nano-banana-2.1', display_name: 'Duplicate' },
        ],
      }),
    } as Response)

    await expect(listImageStudioModels('sk-gemini', undefined, { nativeGemini: true })).resolves.toEqual([
      { id: 'gemini-nano-banana-2.1', name: 'Nano Banana 2.1' },
    ])
    expect(fetch).toHaveBeenCalledWith(
      expect.stringMatching(/\/v1beta\/models$/),
      expect.objectContaining({ headers: { Authorization: 'Bearer sk-gemini' } }),
    )
  })

  it('uses the JSON generations endpoint for text-to-image requests', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ data: [{ b64_json: 'aW1hZ2U=', revised_prompt: 'revised' }] }),
    } as Response)

    const result = await generateImageStudioImages('sk-image', {
      model: 'gpt-image-1',
      prompt: 'a lighthouse',
      count: 9,
      size: '1024x1024',
      quality: 'auto',
      outputFormat: 'png',
      background: 'transparent',
    })

    expect(result).toEqual([{ b64Json: 'aW1hZ2U=', revisedPrompt: 'revised' }])
    const [url, options] = vi.mocked(fetch).mock.calls[0]
    expect(String(url)).toMatch(/\/v1\/images\/generations$/)
    expect(options?.headers).toEqual({ Authorization: 'Bearer sk-image', 'Content-Type': 'application/json' })
    expect(JSON.parse(String(options?.body))).toEqual({
      model: 'gpt-image-1',
      prompt: 'a lighthouse',
      n: 4,
      response_format: 'b64_json',
      size: '1024x1024',
      output_format: 'png',
      background: 'transparent',
    })
  })

  it('forwards experimental 4K dimensions and extended quality values unchanged', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ data: [{ b64_json: 'aW1hZ2U=' }] }),
    } as Response)

    await generateImageStudioImages('sk-image', {
      model: 'gpt-image-2.5-sunburst',
      prompt: 'a mountain at sunrise',
      count: 1,
      size: '3840x2160',
      quality: 'max',
      outputFormat: 'png',
    })

    const [, options] = vi.mocked(fetch).mock.calls[0]
    expect(JSON.parse(String(options?.body))).toMatchObject({
      model: 'gpt-image-2.5-sunburst',
      size: '3840x2160',
      quality: 'max',
    })
  })

  it('uses multipart edits without overriding the browser content type', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: true,
      json: async () => ({ data: [{ url: 'https://images.example/result.png' }] }),
    } as Response)
    const image = new File(['image'], 'reference.png', { type: 'image/png' })
    const mask = new File(['mask'], 'mask.png', { type: 'image/png' })

    await generateImageStudioImages('sk-edit', {
      model: 'gpt-image-1',
      prompt: 'make it blue',
      count: 1,
      size: '2048x2048',
      inputFidelity: 'high',
      images: [image],
      mask,
    })

    const [url, options] = vi.mocked(fetch).mock.calls[0]
    expect(String(url)).toMatch(/\/v1\/images\/edits$/)
    expect(options?.headers).toEqual({ Authorization: 'Bearer sk-edit' })
    expect(options?.body).toBeInstanceOf(FormData)
    const form = options?.body as FormData
    expect(form.get('prompt')).toBe('make it blue')
    expect(form.get('size')).toBe('2048x2048')
    expect(form.get('input_fidelity')).toBe('high')
    expect(form.getAll('image')).toHaveLength(1)
    expect((form.get('image') as File).name).toBe('reference.png')
    expect((form.get('mask') as File).name).toBe('mask.png')
  })

  it('surfaces the gateway error message', async () => {
    vi.mocked(fetch).mockResolvedValue({
      ok: false,
      status: 400,
      statusText: 'Bad Request',
      json: async () => ({ error: { message: 'model is not enabled', code: 'model_disabled' } }),
    } as Response)

    await expect(generateImageStudioImages('sk-image', {
      model: 'missing-image-model',
      prompt: 'test',
      count: 1,
    })).rejects.toMatchObject({ message: 'model is not enabled', code: 'model_disabled', status: 400 })
  })
})
