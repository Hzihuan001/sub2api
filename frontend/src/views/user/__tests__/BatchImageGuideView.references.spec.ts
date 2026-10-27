import { flushPromises, shallowMount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import BatchImageGuideView from '../BatchImageGuideView.vue'
import type { BatchImageReferenceImage, BatchImageSubmitRequest } from '@/api/batchImage'

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
  sharedReferenceImageDrafts: Array<{ id: string; type: string; mime_type: string; data: string; name: string; size: number }>
  promptRows: Array<{ localId: string; custom_id: string; prompt: string; output_count: number; reference_images: BatchImageReferenceImage[] }>
}

let wrapper: ReturnType<typeof mountView> | undefined

beforeEach(() => {
  vi.clearAllMocks()
  mocks.listKeys.mockResolvedValue({ items: [{ id: 1, key: 'batch-test-key', name: 'Batch key', status: 'active', group: { status: 'active', allow_batch_image_generation: true } }] })
  mocks.listModels.mockResolvedValue({ data: models.map(model => ({ ...model, provider: 'app_managed', supports_shared_reference_images: true })) })
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

async function serializedRequest(request: BatchImageSubmitRequest) {
  const api = await vi.importActual<typeof import('@/api/batchImage')>('@/api/batchImage')
  const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: async () => ({ id: 'wire-test' }) })
  vi.stubGlobal('fetch', fetchMock)
  await api.submitBatchImageJob('wire-test-key', request, 'wire-test-idempotency')
  expect(fetchMock.mock.calls[0]![0]).toMatch(/\/v1\/images\/batches$/)
  return String(fetchMock.mock.calls[0]![1].body)
}

function promptRows(outputCounts: number[]) {
  return outputCounts.map((count, index) => ({
    localId: `local-${index}`,
    custom_id: `prompt-${index}`,
    prompt: `Prompt ${index}`,
    output_count: count,
    reference_images: [] as [],
  }))
}

describe('Batch Image shared references', () => {
  it('uploads 16 shared references once in the serialized request for multiple prompts and outputs', async () => {
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
    expect(request.shared_reference_images).toEqual(files.map((file, index) => ({
      id: file.name, type: 'reference', mime_type: 'image/png', data: btoa(`content-${index}`),
    })))
    expect(request.items.every(item => !('reference_images' in item))).toBe(true)
    const body = await serializedRequest(request)
    expect(JSON.parse(body)).toEqual(request)
    for (const [index, file] of files.entries()) {
      expect(body.split(file.name)).toHaveLength(2)
      expect(body.split(JSON.stringify(btoa(`content-${index}`)))).toHaveLength(2)
    }
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
    expect(request.shared_reference_images).toEqual([])
    expect(request.items[0]).not.toHaveProperty('reference_images')
    const body = JSON.parse(await serializedRequest(request))
    expect(body.shared_reference_images).toEqual([])
    expect(body.items[0]).not.toHaveProperty('reference_images')
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
    expect(request.shared_reference_images?.length || 0).toBe(limit)
    expect(request.items[0]).not.toHaveProperty('reference_images')
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
    expect(request.shared_reference_images).toHaveLength(3)
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
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request.items[0]!.reference_images).toHaveLength(16)
    expect(request).not.toHaveProperty('shared_reference_images')
    const body = JSON.parse(await serializedRequest(request))
    expect(body.items[0].reference_images).toHaveLength(16)
    expect(body).not.toHaveProperty('shared_reference_images')
  })

  it('preserves shared references on every item when an older server omits the compact capability', async () => {
    mocks.listModels.mockResolvedValue({ data: [{ ...models[0], provider: 'app_managed' }] })
    await openShared()
    await upload(references(), 16)
    await addPrompt('First legacy prompt', 2)
    await addPrompt('Second legacy prompt')
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request).not.toHaveProperty('shared_reference_images')
    expect(request.items.map(item => item.reference_images?.length)).toEqual([16, 16])
    const body = JSON.parse(await serializedRequest(request))
    expect(body.items[0].reference_images).toEqual(body.items[1].reference_images)
    expect(body).not.toHaveProperty('shared_reference_images')
  })

  it('clears old per-prompt references in shared mode on an older server too', async () => {
    mocks.listModels.mockResolvedValue({ data: [{ ...models[0], provider: 'app_managed', supports_shared_reference_images: false }] })
    await openShared()
    await wrapper!.get('input[type="radio"][value="per_item"]').setValue(true)
    await upload(references(2), 2)
    await addPrompt('Legacy prompt with removed references')
    await wrapper!.get('input[type="radio"][value="shared"]').setValue(true)
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    const request = mocks.submit.mock.calls[0]![1] as BatchImageSubmitRequest
    expect(request).not.toHaveProperty('shared_reference_images')
    expect(request.items[0]!.reference_images).toEqual([])
  })

  it.each([
    [62, true],
    [63, false],
  ])('counts 16 shared references across %i outputs against the 1000 attachment limit', async (outputs, allowed) => {
    await openShared()
    await upload(references(), 16)
    const state = wrapper!.vm as unknown as ViewState
    state.promptRows = promptRows([...Array(15).fill(4), Number(outputs) - 60])
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    if (allowed) {
      expect(mocks.submit).toHaveBeenCalledOnce()
    } else {
      expect(mocks.submit).not.toHaveBeenCalled()
      expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.tooManyReferenceAttachments')
    }
  })

  it.each([
    [16, true],
    [17, false],
  ])('counts decoded shared bytes across %i outputs against the 128 MiB limit', async (outputs, allowed) => {
    await openShared()
    const state = wrapper!.vm as unknown as ViewState
    // One 8 MiB reference: padded base64 must count decoded bytes, rather
    // than the larger string sent once in the HTTP body.
    const bytes = 8 * 1024 * 1024
    const encoded = `${'A'.repeat(Math.ceil(bytes / 3) * 4 - 1)}=`
    state.sharedReferenceImageDrafts = [{
      id: 'large.png', type: 'reference', mime_type: 'image/png',
      data: encoded, name: 'large.png', size: bytes,
    }]
    state.promptRows = promptRows([...Array(4).fill(4), ...(Number(outputs) > 16 ? [1] : [])])
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    if (allowed) {
      expect(mocks.submit).toHaveBeenCalledOnce()
    } else {
      expect(mocks.submit).not.toHaveBeenCalled()
      expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.referenceImagesTooLarge')
    }
  })

  it('shows a clear error when the server rejects the request body size', async () => {
    await openShared()
    await addPrompt('A cat')
    mocks.submit.mockRejectedValue(Object.assign(new Error('request body too large'), { code: 'BATCH_IMAGE_REQUEST_TOO_LARGE', status: 413 }))
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.requestTooLarge')
  })

  it.each(['BATCH_IMAGE_INVALID_JSON', 'BATCH_IMAGE_INVALID_REQUEST'])('explains the %s format error', async (code) => {
    await openShared()
    await addPrompt('A cat')
    mocks.submit.mockRejectedValue(Object.assign(new Error('invalid request'), { code, status: 400 }))
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.invalidRequest')
  })

  it('explains an interrupted request upload', async () => {
    await openShared()
    await addPrompt('A cat')
    mocks.submit.mockRejectedValue(Object.assign(new Error('request read failed'), { code: 'BATCH_IMAGE_REQUEST_READ_FAILED', status: 400 }))
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.requestReadFailed')
  })

  it('enforces the expanded attachment limit for per-prompt references too', async () => {
    await openShared()
    await wrapper!.get('input[type="radio"][value="per_item"]').setValue(true)
    const state = wrapper!.vm as unknown as ViewState
    const refs = references().map(file => ({ id: file.name, mime_type: 'image/png', data: 'eA==' }))
    state.promptRows = promptRows([...Array(15).fill(4), 3]).map(row => ({ ...row, reference_images: refs }))
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.tooManyReferenceAttachments')
  })

  it('enforces decoded byte limits for per-prompt references too', async () => {
    await openShared()
    await wrapper!.get('input[type="radio"][value="per_item"]').setValue(true)
    const state = wrapper!.vm as unknown as ViewState
    const data = `${'A'.repeat(Math.ceil(8 * 1024 * 1024 / 3) * 4 - 1)}=`
    state.promptRows = promptRows([4, 4, 4, 4, 1]).map(row => ({
      ...row, reference_images: [{ id: 'large.png', mime_type: 'image/png', data }],
    }))
    await wrapper!.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.submit).not.toHaveBeenCalled()
    expect(mocks.showError).toHaveBeenLastCalledWith('batchImage.messages.referenceImagesTooLarge')
  })

  it('keeps the server body-size error code through real API response parsing', async () => {
    const api = await vi.importActual<typeof import('@/api/batchImage')>('@/api/batchImage')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false, status: 413, statusText: 'Request Entity Too Large',
      headers: new Headers(),
      json: async () => ({ error: { code: 'BATCH_IMAGE_REQUEST_TOO_LARGE', message: 'batch request body is too large' } }),
    }))
    await expect(api.submitBatchImageJob('wire-test-key', {
      model: models[0].id, items: [{ custom_id: 'cat', prompt: 'A cat' }],
    }, 'wire-test-idempotency')).rejects.toMatchObject({ code: 'BATCH_IMAGE_REQUEST_TOO_LARGE', status: 413 })
  })
})
