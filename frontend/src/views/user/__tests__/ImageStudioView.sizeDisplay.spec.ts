import { flushPromises, shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ImageStudioView from '../ImageStudioView.vue'
import type { StoredStudioImage } from '@/features/image-studio/library'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  generate: vi.fn(),
  listModels: vi.fn(),
  listStored: vi.fn(),
  saveStored: vi.fn(),
  decodeImage: vi.fn(),
  detectSize: vi.fn(),
  showError: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/keys', () => ({ keysAPI: { list: mocks.listKeys } }))
vi.mock('@/api/imageStudio', () => ({
  generateImageStudioImages: mocks.generate,
  listImageStudioModels: mocks.listModels,
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 7 } }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/features/image-studio/library', () => ({
  base64ImageToBlob: mocks.decodeImage,
  clearStoredStudioImages: vi.fn(),
  deleteStoredStudioImage: vi.fn(),
  listStoredStudioImages: mocks.listStored,
  saveStoredStudioImages: mocks.saveStored,
}))
vi.mock('@/features/image-studio/size', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/features/image-studio/size')>(),
  detectImageDimensions: mocks.detectSize,
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string, values?: { max?: number }) => values?.max ? `${key}: ${values.max}` : key, locale: { value: 'en' } }),
}))

const mountView = () => shallowMount(ImageStudioView, {
  global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      BaseDialog: { props: ['show'], template: '<div v-if="show" data-test="preview"><slot /></div>' },
      Select: {
        props: ['modelValue', 'options'],
        emits: ['update:modelValue'],
        template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
      },
      RouterLink: true,
    },
  },
})

let wrapper: ReturnType<typeof mountView> | undefined
let stored: StoredStudioImage[]
let imageBlob: Blob

beforeEach(() => {
  vi.clearAllMocks()
  vi.stubGlobal('indexedDB', {})
  vi.stubGlobal('URL', class extends URL {
    static createObjectURL = vi.fn(() => 'blob:studio-image')
    static revokeObjectURL = vi.fn()
  })
  imageBlob = new Blob(['original image bytes'], { type: 'image/png' })
  stored = []
  mocks.listStored.mockImplementation(async () => stored)
  mocks.saveStored.mockImplementation(async (images: StoredStudioImage[]) => { stored.push(...images) })
  mocks.listKeys.mockResolvedValue({
    pages: 1,
    items: [{ id: 1, key: 'test-key', name: 'Image key', status: 'active', group: { status: 'active', platform: 'openai', allow_image_generation: true } }],
  })
  mocks.listModels.mockResolvedValue([{ id: 'gpt-image-2.5-sunburst', name: 'Image model' }])
  mocks.generate.mockResolvedValue([{ b64Json: 'image-data' }])
  mocks.decodeImage.mockReturnValue(imageBlob)
  mocks.detectSize.mockResolvedValue('1672x941')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function formSelect(label: string) {
  return wrapper!.findAll('label').find((element) => element.text().includes(label))!.get('select')
}

async function attachReferences(files: File[]) {
  const input = wrapper!.get<HTMLInputElement>('input[type="file"][multiple]')
  Object.defineProperty(input.element, 'files', { configurable: true, value: files })
  await input.trigger('change')
}

describe('Image Studio actual size visibility', () => {
  it('accepts and submits 16 GPT Image references, but ignores a seventeenth upload', async () => {
    vi.mocked(URL.createObjectURL).mockImplementation((blob) => `blob:reference-${(blob as File).name}`)
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('imageStudio.referenceHint: 16')
    const references = Array.from({ length: 16 }, (_, index) => new File(['image'], `reference-${index}.png`, { type: 'image/png' }))
    await attachReferences(references.slice(0, 4))
    const addButton = () => wrapper!.findAll('form button').find((button) => button.text().includes('imageStudio.addImages'))!
    expect(addButton().attributes('disabled')).toBeUndefined()
    await attachReferences(references.slice(4))
    expect(wrapper.findAll('form img').map((image) => image.attributes('alt'))).toEqual(references.map((file) => file.name))
    expect(addButton().attributes('disabled')).toBeDefined()
    await attachReferences([new File(['extra'], 'seventeenth.png', { type: 'image/png' })])
    expect(wrapper.findAll('form img')).toHaveLength(16)
    expect(URL.createObjectURL).toHaveBeenCalledTimes(16)
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('Use every reference image')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({
      model: 'gpt-image-2.5-sunburst', images: references,
    }), expect.any(AbortSignal))
  })

  it('caps an oversized GPT Image file selection at the first 16 references', async () => {
    vi.mocked(URL.createObjectURL).mockImplementation((blob) => `blob:reference-${(blob as File).name}`)
    wrapper = mountView()
    await flushPromises()
    const references = Array.from({ length: 17 }, (_, index) => new File(['image'], `reference-${index}.png`, { type: 'image/png' }))
    await attachReferences(references)
    expect(wrapper.findAll('form img').map((image) => image.attributes('alt'))).toEqual(references.slice(0, 16).map((file) => file.name))
    expect(URL.createObjectURL).toHaveBeenCalledTimes(16)
  })

  it.each([
    { model: 'gemini-nano-banana-2.1', limit: 14 },
    { model: 'seedream-5.0-pro', limit: 10 },
    { model: 'custom-image-alias', limit: 4 },
  ])('trims GPT Image references and releases previews when switching to $model', async ({ model, limit }) => {
    mocks.listModels.mockResolvedValue([
      { id: 'gpt-image-2.5-sunburst', name: 'GPT Image' },
      { id: model, name: 'Other image model' },
    ])
    vi.mocked(URL.createObjectURL).mockImplementation((blob) => `blob:reference-${(blob as File).name}`)
    wrapper = mountView()
    await flushPromises()
    const references = Array.from({ length: 16 }, (_, index) => new File(['image'], `reference-${index}.png`, { type: 'image/png' }))
    await attachReferences(references)
    await wrapper.get('[data-test="image-studio-model"]').setValue(model)
    expect(wrapper.findAll('form img').map((image) => image.attributes('alt'))).toEqual(references.slice(0, limit).map((file) => file.name))
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(16 - limit)
    for (const file of references.slice(limit)) expect(URL.revokeObjectURL).toHaveBeenCalledWith(`blob:reference-${file.name}`)
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('Use the remaining reference images')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({
      model, images: references.slice(0, limit),
    }), expect.any(AbortSignal))
  })

  it('keeps actual dimensions internal without a mismatch toast after generation', async () => {
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[data-test="image-studio-model"]').element.tagName).toBe('SELECT')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A cat by a window')
    await wrapper.get('[data-test="image-studio-size"]').setValue('4K')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({ size: '3840x2160' }), expect.any(AbortSignal))
    expect(mocks.detectSize).toHaveBeenCalledWith(imageBlob)
    expect(mocks.saveStored).toHaveBeenCalledWith([
      expect.objectContaining({ size: '3840x2160', actualSize: '1672x941', blob: imageBlob }),
    ], '7')
    expect(mocks.showSuccess).toHaveBeenCalledWith('imageStudio.messages.generated')
    expect(mocks.showWarning).not.toHaveBeenCalled()
    expect(mocks.showError).not.toHaveBeenCalled()
    expect(wrapper.get('article').text()).toContain('imageStudio.requestedSize: 3840x2160')
    expect(wrapper.html()).not.toContain('1672x941')
  })

  it('does not show stored actual dimensions in the library or preview', async () => {
    stored.push({
      id: 'saved-image', createdAt: 1, prompt: 'A cat', model: 'gpt-image-2.5-sunburst',
      size: '3840x2160', actualSize: '1672x941', outputFormat: 'png', apiKeyName: 'Image key',
      blob: imageBlob, bytes: imageBlob.size,
    })
    wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('article').text()).toContain('imageStudio.requestedSize: 3840x2160')
    expect(wrapper.html()).not.toContain('1672x941')
    await wrapper.get('article button').trigger('click')
    expect(wrapper.get('[data-test="preview"]').text()).toContain('imageStudio.requestedSize: 3840x2160')
    expect(wrapper.html()).not.toContain('1672x941')
    expect(wrapper.html()).not.toContain('imageStudio.actualSize')
  })

  it('keeps the batch entry and limits Seedream choices after switching from GPT Image', async () => {
    mocks.listModels.mockResolvedValue([
      { id: 'gpt-image-2.5-sunburst', name: 'GPT Image' },
      { id: 'seedream-5.0-pro', name: 'Seedream' },
    ])
    wrapper = mountView()
    await flushPromises()
    expect(wrapper.getComponent({ name: 'RouterLink' }).attributes('to')).toBe('/batch-image')
    await wrapper.get('[data-test="image-studio-size"]').setValue('4K')
    await formSelect('imageStudio.count').setValue('4')
    await formSelect('imageStudio.quality').setValue('max')
    await formSelect('imageStudio.outputFormat').setValue('webp')
    await formSelect('imageStudio.background').setValue('transparent')
    await attachReferences([new File(['image'], 'reference.png', { type: 'image/png' })])
    expect(wrapper.text()).toContain('imageStudio.mask')

    await wrapper.get('[data-test="image-studio-model"]').setValue('seedream-5.0-pro')
    expect(wrapper.get<HTMLSelectElement>('[data-test="image-studio-size"]').element.value).toBe('2K')
    expect(wrapper.get('[data-test="image-studio-size"]').findAll('option').map((option) => option.attributes('value'))).toEqual(['1K', '2K', 'custom'])
    expect(formSelect('imageStudio.count').findAll('option').map((option) => option.attributes('value'))).toEqual(['1'])
    expect(formSelect('imageStudio.outputFormat').findAll('option').map((option) => option.attributes('value'))).toEqual(['png', 'jpeg'])
    expect(wrapper.text()).not.toContain('imageStudio.quality')
    expect(wrapper.text()).not.toContain('imageStudio.background')
    expect(wrapper.text()).not.toContain('imageStudio.mask')
    expect(wrapper.text()).not.toContain('imageStudio.inputFidelity')

    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A mountain at dawn')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({
      model: 'seedream-5.0-pro', size: '2048x2048', count: 1,
      outputFormat: 'png', quality: 'auto', background: 'auto', inputFidelity: 'auto', mask: null,
      images: [expect.objectContaining({ name: 'reference.png' })],
    }), expect.any(AbortSignal))
  })

  it.each(['gemini-nano-banana-2.1', 'gemini-3.1-flash-image', 'gemini-3-pro-image'])('offers native Gemini tiers for %s and keeps reference images and download MIME type', async (model) => {
    mocks.listKeys.mockResolvedValue({
      pages: 1,
      items: [{ id: 1, key: 'gemini-key', name: 'Gemini key', status: 'active', group: { status: 'active', platform: 'gemini', allow_image_generation: true } }],
    })
    mocks.listModels.mockResolvedValue([{ id: model, name: 'Gemini Image' }])
    mocks.generate.mockResolvedValue([{ b64Json: 'jpeg-image', mimeType: 'image/jpeg' }])
    const downloads: Array<{ name: string; url: string }> = []
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      downloads.push({ name: this.download, url: this.href })
    })
    wrapper = mountView()
    await flushPromises()
    expect(mocks.listModels).toHaveBeenCalledWith('gemini-key', expect.any(AbortSignal), { nativeGemini: true })
    expect(wrapper.get('[data-test="image-studio-size"]').findAll('option').map((option) => option.attributes('value'))).toEqual(['1K', '2K', '4K'])
    expect(wrapper.text()).not.toContain('imageStudio.outputFormat')
    await attachReferences([new File(['image'], 'reference.png', { type: 'image/png' })])
    expect(wrapper.text()).not.toContain('imageStudio.mask')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A cat by a window')
    await wrapper.get('[data-test="image-studio-size"]').setValue('4K')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.generate).toHaveBeenCalledWith('gemini-key', expect.objectContaining({
      model, size: '4K', nativeGemini: true,
      images: [expect.objectContaining({ name: 'reference.png' })], mask: null,
    }), expect.any(AbortSignal))
    expect(mocks.decodeImage).toHaveBeenCalledWith('jpeg-image', 'jpeg')
    await wrapper.get('article button[title="common.download"]').trigger('click')
    expect(downloads).toEqual([{ name: expect.stringMatching(/^moshu-image-.*\.jpg$/), url: 'blob:studio-image' }])
  })

  it('clears unsupported custom size when switching keys without changing the model', async () => {
    mocks.listKeys.mockResolvedValue({
      pages: 1,
      items: [
        { id: 1, key: 'compatible-key', name: 'Compatible key', status: 'active', group: { status: 'active', platform: 'openai', allow_image_generation: true } },
        { id: 2, key: 'native-key', name: 'Native key', status: 'active', group: { status: 'active', platform: 'gemini', allow_image_generation: true } },
      ],
    })
    mocks.listModels.mockResolvedValue([{ id: 'gemini-3.1-flash-image', name: 'Gemini Image' }])
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="image-studio-size"]').setValue('custom')
    await wrapper.get('[data-test="image-studio-key"]').setValue('2')
    await flushPromises()

    expect(wrapper.get<HTMLSelectElement>('[data-test="image-studio-model"]').element.value).toBe('gemini-3.1-flash-image')
    expect(wrapper.get<HTMLSelectElement>('[data-test="image-studio-size"]').element.value).toBe('1K')
    expect(wrapper.get('[data-test="image-studio-size"]').findAll('option').map((option) => option.attributes('value'))).toEqual(['1K', '2K', '4K'])
    await wrapper.get('[data-test="image-studio-size"]').setValue('4K')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A quiet garden')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledWith('native-key', expect.objectContaining({ model: 'gemini-3.1-flash-image', size: '4K', nativeGemini: true }), expect.any(AbortSignal))
  })

  it('drops removed model capabilities when the same key reloads its upstream model list', async () => {
    mocks.listModels.mockResolvedValueOnce([{ id: 'gemini-nano-banana-2.1', name: 'Nano Banana' }])
      .mockResolvedValueOnce([{ id: 'seedream-5.0-pro', name: 'Seedream' }])
    vi.mocked(URL.createObjectURL).mockImplementation((blob) => `blob:reference-${(blob as File).name}`)
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="image-studio-size"]').setValue('4K')
    await formSelect('imageStudio.count').setValue('4')
    await formSelect('imageStudio.outputFormat').setValue('webp')
    const references = Array.from({ length: 14 }, (_, index) => new File(['image'], `reference-${index}.png`, { type: 'image/png' }))
    await attachReferences(references)
    expect(wrapper.findAll('form img')).toHaveLength(14)
    const maskInput = wrapper.get<HTMLInputElement>('input[type="file"]:not([multiple])')
    Object.defineProperty(maskInput.element, 'files', { configurable: true, value: [new File(['mask'], 'mask.png', { type: 'image/png' })] })
    await maskInput.trigger('change')
    expect(wrapper.text()).toContain('mask.png')

    // Studio has no capability metadata API or refresh button. Invoke its real
    // model loader directly, preserving the selected key and all form state.
    await (wrapper.vm as unknown as { loadModels: () => Promise<void> }).loadModels()
    await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('[data-test="image-studio-key"]').element.value).toBe('1')
    expect(mocks.listModels).toHaveBeenCalledTimes(2)
    for (const call of mocks.listModels.mock.calls) expect(call).toEqual(['test-key', expect.any(AbortSignal), { nativeGemini: false }])
    expect(wrapper.get('[data-test="image-studio-model"]').findAll('option').map((option) => option.attributes('value'))).toEqual(['seedream-5.0-pro'])
    expect(wrapper.get<HTMLSelectElement>('[data-test="image-studio-size"]').element.value).toBe('2K')
    expect(wrapper.get('[data-test="image-studio-size"]').findAll('option').map((option) => option.attributes('value'))).not.toContain('4K')
    expect(wrapper.findAll('form img').map((image) => image.attributes('alt'))).toEqual(references.slice(0, 10).map((file) => file.name))
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(4)
    for (const file of references.slice(10)) expect(URL.revokeObjectURL).toHaveBeenCalledWith(`blob:reference-${file.name}`)
    expect(wrapper.text()).not.toContain('imageStudio.mask')
    expect(wrapper.text()).not.toContain('mask.png')
    expect(wrapper.text()).not.toContain('imageStudio.inputFidelity')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A mountain at dawn')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.generate).toHaveBeenCalledOnce()
    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({
      model: 'seedream-5.0-pro', size: '2048x2048', count: 1,
      outputFormat: 'png', quality: 'auto', background: 'auto', inputFidelity: 'auto',
      mask: null, images: references.slice(0, 10), nativeGemini: false,
    }), expect.any(AbortSignal))
  })

  it('keeps compatible Gemini options and edits without selecting native protocol from the model name', async () => {
    mocks.listModels.mockResolvedValue([{ id: 'gemini-nano-banana-2.1', name: 'Compatible Gemini' }])
    wrapper = mountView()
    await flushPromises()
    await formSelect('imageStudio.outputFormat').setValue('webp')
    await formSelect('imageStudio.quality').setValue('max')
    await formSelect('imageStudio.count').setValue('3')
    await attachReferences([new File(['image'], 'reference.png', { type: 'image/png' })])
    expect(wrapper.text()).toContain('imageStudio.mask')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A quiet garden')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.generate).toHaveBeenCalledWith('test-key', expect.objectContaining({
      model: 'gemini-nano-banana-2.1', nativeGemini: false,
      outputFormat: 'webp', quality: 'max', count: 3,
    }), expect.any(AbortSignal))
  })

  it('still allows native Gemini multi-output selection after its initial model-change reset', async () => {
    mocks.listKeys.mockResolvedValue({
      pages: 1,
      items: [{ id: 1, key: 'gemini-key', name: 'Gemini key', status: 'active', group: { status: 'active', platform: 'gemini', allow_image_generation: true } }],
    })
    mocks.listModels.mockResolvedValue([{ id: 'custom-image-alias', name: 'Native Gemini' }])
    wrapper = mountView()
    await flushPromises()
    expect(formSelect('imageStudio.count').findAll('option').map((option) => option.attributes('value'))).toEqual(['1', '2', '3', '4'])
    await formSelect('imageStudio.count').setValue('4')
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A quiet garden')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(mocks.generate).toHaveBeenCalledWith('gemini-key', expect.objectContaining({
      count: 4, nativeGemini: true, outputFormat: 'png', quality: 'auto', background: 'auto', inputFidelity: 'auto', mask: null,
    }), expect.any(AbortSignal))
  })
})
