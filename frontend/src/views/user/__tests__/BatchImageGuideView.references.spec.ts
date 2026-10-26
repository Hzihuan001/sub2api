import { flushPromises, shallowMount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BatchImageGuideView from '../BatchImageGuideView.vue'
import type { BatchImageSubmitRequest } from '@/api/batchImage'

const mocks = vi.hoisted(() => ({
  listKeys: vi.fn(),
  listModels: vi.fn(),
  listJobs: vi.fn(),
  listItems: vi.fn(),
  submit: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copy: vi.fn(),
  fetchPublicSettings: vi.fn(),
}))

vi.mock('@/api', () => ({ keysAPI: { list: mocks.listKeys } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => mocks }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: mocks.copy }) }))
vi.mock('@/api/batchImage', () => ({
  cancelBatchImageJob: vi.fn(),
  deleteBatchImageJobRecord: vi.fn(),
  downloadBatchImageZip: vi.fn(),
  getBatchImageItemContent: vi.fn(),
  getBatchImageJob: vi.fn(),
  listBatchImageJobs: mocks.listJobs,
  listBatchImageItems: mocks.listItems,
  listBatchImageModels: mocks.listModels,
  saveBlob: vi.fn(),
  submitBatchImageJob: mocks.submit,
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({
    t: (key: string, values?: { limit?: number }) => values?.limit === undefined ? key : `${key}: ${values.limit}`,
    locale: { value: 'en' },
  }),
}))

const models = [
  { id: 'gpt-image-2.5-sunburst', reference_image_limit: 16 },
  { id: 'gemini-nano-banana-2.1', reference_image_limit: 14 },
  { id: 'seedream-5.0-pro', reference_image_limit: 10 },
  { id: 'gemini-2.5-flash-image', reference_image_limit: 3 },
  { id: 'unknown-image-model' },
  { id: 'advertised-image-alias', reference_image_limit: 5 },
]

const mountView = () => shallowMount(BatchImageGuideView, {
  global: {
    stubs: {
      'i18n-t': true,
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters" /></div>' },
      BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
      Select: {
        props: ['modelValue', 'options'],
        emits: ['update:modelValue'],
        template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
      },
    },
  },
})

type ViewState = {
  availableBatchImageModels: Array<{ value: string; referenceImageLimit?: number }>
  handleSharedReferenceImageFiles: (event: Event) => Promise<void>
}

let wrapper: ReturnType<typeof mountView> | undefined

beforeEach(() => {
  vi.clearAllMocks()
  mocks.listKeys.mockResolvedValue({ items: [{ id: 1, key: 'batch-test-key', name: 'Batch key', status: 'active', group: { status: 'active', allow_batch_image_generation: true } }] })
  mocks.listModels.mockResolvedValue({ data: models.map(model => ({ ...model, provider: 'app_managed' })) })
  mocks.listJobs.mockResolvedValue({ data: [], has_more: false })
  mocks.listItems.mockResolvedValue({ data: [] })
  mocks.submit.mockResolvedValue({ id: 'batch-test', task_name: 'Test', status: 'completed', model: models[0].id, provider: 'app_managed', item_count: 3, success_count: 3, fail_count: 0, created_at: 1 })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

function button(text: string) {
  return wrapper!.findAll('button').find(item => item.text() === text)!
}

async function openShared() {
  wrapper = mountView()
  await flushPromises()
  await button('batchImage.actions.createJob').trigger('click')
  await wrapper.get('input[type="radio"][value="shared"]').setValue(true)
}

function input() {
  return wrapper!.get<HTMLInputElement>('form input[type="file"]')
}

function referenceNames() {
  return wrapper!.findAll('button[title="batchImage.create.removeReferenceImage"]')
    .map(item => item.element.parentElement!.querySelector('span')!.textContent)
}

function references(count = 16, offset = 0) {
  return Array.from({ length: count }, (_, index) => new File([`content-${index + offset}`], `reference-${index + offset}.png`, { type: 'image/png' }))
}

async function upload(files: File[], expectedCount: number) {
  Object.defineProperty(input().element, 'files', { configurable: true, value: files })
  await input().trigger('change')
  await vi.waitFor(() => expect(referenceNames()).toHaveLength(expectedCount))
}

async function addPrompt(prompt: string, outputCount = 1) {
  await wrapper!.get('form textarea').setValue(prompt)
  await wrapper!.get('select[aria-label="batchImage.create.outputCountPerPrompt"]').setValue(String(outputCount))
  await button('common.add').trigger('click')
}

async function selectModel(model: string) {
  await wrapper!.findAll('form select')[1]!.setValue(model)
}

describe('Batch Image shared references', () => {
  it('uploads 16 shared references and sends every reference in order to every prompt', async () => {
    await openShared()
    expect(input().attributes('multiple')).toBeDefined()
    expect(wrapper!.text()).toContain('batchImage.create.sharedReferenceHint: 16')
    const files = references()
    await upload(files.slice(0, 4), 4)
    expect(input().element.disabled).toBe(false)
    await upload(files.slice(4), 16)
    expect(referenceNames()).toEqual(files.map(file => file.name))
    expect(input().element.disabled).toBe(true)
    await addPrompt('First prompt', 2)
    await addPrompt('Second prompt')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledTimes(1)
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request.model).toBe(models[0].id)
    expect(request.items.map(item => item.prompt)).toEqual(['First prompt', 'Second prompt'])
    expect(request.items.map(item => item.output_count || 1)).toEqual([2, 1])
    for (const item of request.items) {
      expect(item.reference_images).toEqual(files.map((file, index) => ({
        id: file.name, type: 'reference', mime_type: 'image/png', data: btoa(`content-${index}`),
      })))
    }
    expect(request.items[0]!.reference_images).not.toBe(request.items[1]!.reference_images)
  })

  it('ignores a seventeenth reference and allows replacing a removed reference', async () => {
    await openShared()
    const files = references()
    await upload(files, 16)
    const state = wrapper!.vm as unknown as ViewState
    await state.handleSharedReferenceImageFiles({ target: { files: references(1, 16), value: '' } } as unknown as Event)
    expect(referenceNames()).toEqual(files.map(file => file.name))
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.create.refLimitReached: 16')
    await wrapper!.findAll('button[title="batchImage.create.removeReferenceImage"]')[3]!.trigger('click')
    expect(input().element.disabled).toBe(false)
    await upload(references(1, 17), 16)
    expect(referenceNames()).toEqual([...files.filter((_, index) => index !== 3).map(file => file.name), 'reference-17.png'])
  })

  it('caps a single oversized file selection at 16 references', async () => {
    await openShared()
    await upload(references(17), 16)
    expect(referenceNames()).toEqual(references().map(file => file.name))
    expect(mocks.showError).toHaveBeenCalledWith('batchImage.create.refLimitExceededIgnored: 16')
  })

  it('does not restore old per-prompt references after all shared references are removed', async () => {
    await openShared()
    await wrapper!.get('input[type="radio"][value="per_item"]').setValue(true)
    await upload(references(4), 4)
    await addPrompt('Keep this prompt, but remove its reference inputs')
    await wrapper!.get('input[type="radio"][value="shared"]').setValue(true)
    await upload(references(16, 10), 16)
    for (let index = 0; index < 16; index++) {
      await wrapper!.findAll('button[title="batchImage.create.removeReferenceImage"]')[0]!.trigger('click')
    }
    expect(referenceNames()).toEqual([])
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request.items[0]!.reference_images).toEqual([])
  })

  it.each([
    ['gemini-nano-banana-2.1', 14],
    ['seedream-5.0-pro', 10],
    ['gemini-2.5-flash-image', 3],
    ['unknown-image-model', 0],
    ['advertised-image-alias', 5],
  ])('respects the shared reference limit after switching to %s', async (model, limit) => {
    await openShared()
    const files = references()
    await upload(files, 16)
    await selectModel(String(model))
    expect(referenceNames()).toEqual(files.slice(0, Number(limit)).map(file => file.name))
    expect(input().element.disabled).toBe(true)
    await addPrompt('Use all available references')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request.items[0]!.reference_images?.length || 0).toBe(limit)
  })

  it('trims shared references when the advertised model limit changes without a model change', async () => {
    await openShared()
    await upload(references(), 16)
    const state = wrapper!.vm as unknown as ViewState
    state.availableBatchImageModels[0]!.referenceImageLimit = 3
    await nextTick()
    expect(referenceNames()).toEqual(references(3).map(file => file.name))
    expect(input().element.disabled).toBe(true)
  })

  it('rechecks the current limit after asynchronous reads and concurrent additions', async () => {
    await openShared()
    const pending: Array<() => void> = []
    vi.stubGlobal('FileReader', class {
      result = ''
      onload: (() => void) | null = null
      readAsDataURL(file: File) {
        pending.push(() => { this.result = `data:image/png;base64,${btoa(file.name)}`; this.onload?.() })
      }
    })
    const state = wrapper!.vm as unknown as ViewState
    const event = (files: File[]) => ({ target: { files, value: '' } }) as unknown as Event
    const first = state.handleSharedReferenceImageFiles(event(references(10)))
    const second = state.handleSharedReferenceImageFiles(event(references(10, 10)))
    await selectModel('gemini-2.5-flash-image')
    while (pending.length) {
      pending.splice(0).forEach(finish => finish())
      await flushPromises()
    }
    await Promise.all([first, second])
    expect(referenceNames()).toHaveLength(3)
    await addPrompt('Use the retained references')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request.items[0]!.reference_images).toHaveLength(3)
  })

  it('keeps per-prompt GPT references at 16 and blocks incompatible saved prompt rows', async () => {
    await openShared()
    await wrapper!.get('input[type="radio"][value="per_item"]').setValue(true)
    await upload(references(17), 16)
    await addPrompt('Prompt with 16 references')
    await selectModel('gemini-2.5-flash-image')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.tooManyReferenceImages')
    await selectModel(models[0].id)
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect((mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest).items[0]!.reference_images).toHaveLength(16)
  })
})
