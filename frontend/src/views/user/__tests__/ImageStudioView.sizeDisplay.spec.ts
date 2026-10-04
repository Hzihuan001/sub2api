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
  useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }),
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
  vi.unstubAllGlobals()
})

describe('Image Studio actual size visibility', () => {
  it('keeps actual dimensions internal without a mismatch toast after generation', async () => {
    wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-test="image-studio-prompt"]').setValue('A cat by a window')
    await wrapper.findAll('select')[1].setValue('3840x2160')
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
})
