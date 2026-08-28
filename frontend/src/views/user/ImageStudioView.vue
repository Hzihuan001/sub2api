<template>
  <AppLayout>
    <div class="mx-auto max-w-[1600px] space-y-6">
      <section class="overflow-hidden rounded-2xl border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="border-b border-gray-100 bg-gradient-to-r from-violet-50 via-white to-sky-50 px-5 py-5 dark:border-dark-800 dark:from-violet-950/30 dark:via-dark-900 dark:to-sky-950/20 md:px-6">
          <div class="flex flex-wrap items-start justify-between gap-4">
            <div>
              <div class="flex items-center gap-2">
                <span class="flex h-10 w-10 items-center justify-center rounded-xl bg-violet-600 text-white shadow-lg shadow-violet-500/20">
                  <Icon name="sparkles" size="lg" />
                </span>
                <div>
                  <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('imageStudio.title') }}</h2>
                  <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('imageStudio.description') }}</p>
                </div>
              </div>
            </div>
            <router-link to="/batch-image" class="btn btn-secondary btn-sm">
              {{ t('imageStudio.openBatchImage') }}
              <Icon name="arrowRight" size="sm" class="ml-1" />
            </router-link>
          </div>
        </div>

        <div class="grid gap-0 xl:grid-cols-[420px_minmax(0,1fr)]">
          <form class="space-y-5 border-b border-gray-100 p-5 dark:border-dark-800 xl:border-b-0 xl:border-r xl:p-6" @submit.prevent="generate">
            <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-1">
              <label class="block">
                <span class="input-label">{{ t('imageStudio.apiKey') }}</span>
                <Select v-model="form.apiKeyId" :options="apiKeyOptions" :disabled="loadingKeys || generating" class="mt-1 w-full" data-test="image-studio-key" />
                <span v-if="!loadingKeys && apiKeys.length === 0" class="input-hint text-amber-600 dark:text-amber-400">
                  {{ t('imageStudio.noKeys') }}
                </span>
              </label>

              <label class="block">
                <span class="input-label">{{ t('imageStudio.model') }}</span>
                <input v-model.trim="form.model" list="image-studio-models" class="input mt-1 w-full" :disabled="!selectedKey || generating" :placeholder="modelPlaceholder" data-test="image-studio-model" />
                <datalist id="image-studio-models">
                  <option v-for="model in models" :key="model.id" :value="model.id">{{ model.name }}</option>
                </datalist>
                <span v-if="modelError" class="input-hint text-red-600 dark:text-red-400">{{ modelError }}</span>
                <span v-else class="input-hint">{{ t('imageStudio.modelHint') }}</span>
              </label>
            </div>

            <label class="block">
              <span class="input-label">{{ t('imageStudio.prompt') }}</span>
              <textarea v-model="form.prompt" rows="7" maxlength="32000" class="input mt-1 w-full resize-y" :disabled="generating" :placeholder="t('imageStudio.promptPlaceholder')" data-test="image-studio-prompt"></textarea>
              <span class="input-hint flex justify-between"><span>{{ t('imageStudio.promptHint') }}</span><span>{{ form.prompt.length }}/32000</span></span>
            </label>

            <div>
              <div class="flex items-center justify-between gap-3">
                <span class="input-label">{{ t('imageStudio.referenceImages') }}</span>
                <button type="button" class="btn btn-ghost btn-sm" :disabled="generating || inputImages.length >= 4" @click="referenceInput?.click()">
                  <Icon name="upload" size="sm" class="mr-1" />{{ t('imageStudio.addImages') }}
                </button>
              </div>
              <input ref="referenceInput" type="file" accept="image/png,image/jpeg,image/webp" multiple class="hidden" @change="addReferenceImages" />
              <div v-if="inputImages.length" class="mt-2 grid grid-cols-4 gap-2">
                <div v-for="(item, index) in inputImages" :key="item.url" class="group relative aspect-square overflow-hidden rounded-lg border border-gray-200 bg-gray-50 dark:border-dark-700 dark:bg-dark-800">
                  <img :src="item.url" :alt="item.file.name" class="h-full w-full object-cover" />
                  <button type="button" class="absolute right-1 top-1 rounded-full bg-black/65 p-1 text-white opacity-0 transition-opacity group-hover:opacity-100" :aria-label="t('imageStudio.removeImage')" @click="removeReferenceImage(index)">
                    <Icon name="x" size="xs" />
                  </button>
                </div>
              </div>
              <div v-else class="mt-2 rounded-xl border border-dashed border-gray-300 px-4 py-5 text-center text-xs text-gray-500 dark:border-dark-600 dark:text-dark-300">
                {{ t('imageStudio.referenceHint') }}
              </div>
            </div>

            <div v-if="inputImages.length" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-1">
              <label class="block">
                <span class="input-label">{{ t('imageStudio.mask') }}</span>
                <div class="mt-1 flex items-center gap-2">
                  <button type="button" class="btn btn-secondary btn-sm" :disabled="generating" @click="maskInput?.click()">
                    {{ maskFile ? t('imageStudio.replaceMask') : t('imageStudio.selectMask') }}
                  </button>
                  <button v-if="maskFile" type="button" class="btn btn-ghost btn-sm text-red-600" :disabled="generating" @click="clearMask">{{ t('common.remove') }}</button>
                </div>
                <input ref="maskInput" type="file" accept="image/png" class="hidden" @change="selectMask" />
                <span class="input-hint">{{ maskFile?.name || t('imageStudio.maskHint') }}</span>
              </label>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <label class="block">
                <span class="input-label">{{ t('imageStudio.size') }}</span>
                <Select v-model="form.size" :options="sizeOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('imageStudio.count') }}</span>
                <Select v-model="form.count" :options="countOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('imageStudio.quality') }}</span>
                <Select v-model="form.quality" :options="qualityOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('imageStudio.outputFormat') }}</span>
                <Select v-model="form.outputFormat" :options="formatOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
              <label class="block">
                <span class="input-label">{{ t('imageStudio.background') }}</span>
                <Select v-model="form.background" :options="backgroundOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
              <label v-if="inputImages.length" class="block">
                <span class="input-label">{{ t('imageStudio.inputFidelity') }}</span>
                <Select v-model="form.inputFidelity" :options="fidelityOptions" :disabled="generating" class="mt-1 w-full" />
              </label>
            </div>

            <div v-if="generationError" role="alert" class="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
              {{ generationError }}
            </div>

            <div class="flex gap-3">
              <button type="submit" class="btn btn-primary min-w-32 flex-1" :disabled="!canGenerate" data-test="image-studio-generate">
                <Icon :name="generating ? 'refresh' : 'sparkles'" size="sm" class="mr-2" :class="generating ? 'animate-spin' : ''" />
                {{ generating ? t('imageStudio.generating') : (inputImages.length ? t('imageStudio.editImage') : t('imageStudio.generate')) }}
              </button>
              <button v-if="generating" type="button" class="btn btn-secondary" @click="cancelGeneration">{{ t('common.cancel') }}</button>
            </div>
            <p class="text-xs leading-5 text-gray-500 dark:text-dark-300">{{ t('imageStudio.billingHint') }}</p>
          </form>

          <div class="min-w-0 p-5 md:p-6">
            <div class="mb-4 flex flex-wrap items-center justify-between gap-3">
              <div>
                <h3 class="font-semibold text-gray-950 dark:text-white">{{ t('imageStudio.libraryTitle') }}</h3>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('imageStudio.libraryHint', { count: galleryItems.length }) }}</p>
              </div>
              <div class="flex gap-2">
                <button type="button" class="btn btn-secondary btn-sm" :disabled="loadingLibrary" @click="loadLibrary">
                  <Icon name="refresh" size="sm" :class="loadingLibrary ? 'animate-spin' : ''" />
                </button>
                <button type="button" class="btn btn-ghost btn-sm text-red-600" :disabled="storedGallery.length === 0" @click="clearLibrary">{{ t('imageStudio.clearLibrary') }}</button>
              </div>
            </div>

            <div v-if="generating" class="mb-4 overflow-hidden rounded-xl border border-violet-200 bg-violet-50 p-4 dark:border-violet-900/60 dark:bg-violet-950/20">
              <div class="flex items-center gap-3">
                <span class="h-2.5 w-2.5 animate-pulse rounded-full bg-violet-500"></span>
                <div>
                  <p class="text-sm font-medium text-violet-900 dark:text-violet-200">{{ t('imageStudio.requestRunning') }}</p>
                  <p class="mt-0.5 text-xs text-violet-700 dark:text-violet-300">{{ t('imageStudio.requestRunningHint') }}</p>
                </div>
              </div>
            </div>

            <div v-if="loadingLibrary" class="flex min-h-72 items-center justify-center"><LoadingSpinner /></div>
            <div v-else-if="galleryItems.length === 0" class="flex min-h-72 flex-col items-center justify-center rounded-2xl border border-dashed border-gray-300 px-6 text-center dark:border-dark-600">
              <span class="flex h-16 w-16 items-center justify-center rounded-2xl bg-violet-50 text-violet-500 dark:bg-violet-950/30"><Icon name="sparkles" size="xl" /></span>
              <p class="mt-4 font-medium text-gray-900 dark:text-white">{{ t('imageStudio.emptyLibrary') }}</p>
              <p class="mt-1 max-w-sm text-sm text-gray-500 dark:text-dark-300">{{ t('imageStudio.emptyLibraryHint') }}</p>
            </div>
            <div v-else class="grid grid-cols-1 gap-4 sm:grid-cols-2 2xl:grid-cols-3">
              <article v-for="item in galleryItems" :key="item.id" class="group overflow-hidden rounded-xl border border-gray-200 bg-white shadow-sm transition hover:-translate-y-0.5 hover:shadow-md dark:border-dark-700 dark:bg-dark-800">
                <button type="button" class="block aspect-square w-full overflow-hidden bg-gray-100 dark:bg-dark-900" @click="previewItem = item">
                  <img :src="item.url" :alt="item.prompt" class="h-full w-full object-contain transition-transform duration-300 group-hover:scale-[1.015]" />
                </button>
                <div class="space-y-3 p-3">
                  <p class="line-clamp-2 min-h-10 text-sm text-gray-700 dark:text-dark-200" :title="item.prompt">{{ item.prompt }}</p>
                  <div class="flex flex-wrap gap-1.5 text-[11px] text-gray-500 dark:text-dark-300">
                    <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700">{{ item.model }}</span>
                    <span class="rounded bg-gray-100 px-1.5 py-0.5 dark:bg-dark-700">{{ item.size || 'auto' }}</span>
                    <span v-if="!item.persisted" class="rounded bg-amber-100 px-1.5 py-0.5 text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">{{ t('imageStudio.temporary') }}</span>
                  </div>
                  <div class="flex items-center justify-between gap-2">
                    <time class="text-[11px] text-gray-400">{{ formatDate(item.createdAt) }}</time>
                    <div class="flex gap-1">
                      <button type="button" class="btn btn-ghost btn-sm px-2" :title="t('imageStudio.reuse')" @click="reuseItem(item)"><Icon name="refresh" size="sm" /></button>
                      <button type="button" class="btn btn-ghost btn-sm px-2" :title="t('common.download')" @click="downloadItem(item)"><Icon name="download" size="sm" /></button>
                      <button type="button" class="btn btn-ghost btn-sm px-2 text-red-600" :title="t('common.delete')" @click="deleteItem(item)"><Icon name="trash" size="sm" /></button>
                    </div>
                  </div>
                </div>
              </article>
            </div>
          </div>
        </div>
      </section>
    </div>

    <BaseDialog :show="!!previewItem" :title="t('imageStudio.previewTitle')" width="extra-wide" @close="previewItem = null">
      <div v-if="previewItem" class="space-y-4">
        <div class="flex max-h-[68vh] items-center justify-center overflow-hidden rounded-xl bg-gray-950">
          <img :src="previewItem.url" :alt="previewItem.prompt" class="max-h-[68vh] max-w-full object-contain" />
        </div>
        <div class="rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="whitespace-pre-wrap text-gray-800 dark:text-dark-100">{{ previewItem.prompt }}</p>
          <p v-if="previewItem.revisedPrompt" class="mt-2 border-t border-gray-200 pt-2 text-gray-500 dark:border-dark-700 dark:text-dark-300">{{ previewItem.revisedPrompt }}</p>
        </div>
      </div>
      <template #footer>
        <button type="button" class="btn btn-secondary" @click="previewItem && reuseItem(previewItem)">{{ t('imageStudio.reuse') }}</button>
        <button type="button" class="btn btn-primary" @click="previewItem && downloadItem(previewItem)"><Icon name="download" size="sm" class="mr-1.5" />{{ t('common.download') }}</button>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { keysAPI } from '@/api/keys'
import { generateImageStudioImages, listImageStudioModels, type ImageStudioModel } from '@/api/imageStudio'
import { useAppStore } from '@/stores/app'
import type { ApiKey } from '@/types'
import {
  base64ImageToBlob,
  clearStoredStudioImages,
  deleteStoredStudioImage,
  listStoredStudioImages,
  saveStoredStudioImages,
  type StoredStudioImage,
} from '@/features/image-studio/library'

interface InputImage {
  file: File
  url: string
}

interface GalleryItem {
  id: string
  createdAt: number
  prompt: string
  revisedPrompt?: string
  model: string
  size: string
  outputFormat: string
  apiKeyName: string
  url: string
  blob?: Blob
  persisted: boolean
}

const { t, locale } = useI18n()
const appStore = useAppStore()
const apiKeys = ref<ApiKey[]>([])
const models = ref<ImageStudioModel[]>([])
const inputImages = ref<InputImage[]>([])
const maskFile = ref<File | null>(null)
const referenceInput = ref<HTMLInputElement | null>(null)
const maskInput = ref<HTMLInputElement | null>(null)
const loadingKeys = ref(false)
const loadingModels = ref(false)
const loadingLibrary = ref(false)
const generating = ref(false)
const modelError = ref('')
const generationError = ref('')
const storedGallery = ref<GalleryItem[]>([])
const temporaryGallery = ref<GalleryItem[]>([])
const previewItem = ref<GalleryItem | null>(null)
let generationController: AbortController | null = null
let modelController: AbortController | null = null

const form = reactive({
  apiKeyId: '',
  model: '',
  prompt: '',
  count: '1',
  size: '1024x1024',
  quality: 'auto',
  outputFormat: 'png',
  background: 'auto',
  inputFidelity: 'auto',
})

const selectedKey = computed(() => apiKeys.value.find((key) => String(key.id) === String(form.apiKeyId)) || null)
const galleryItems = computed(() => [...temporaryGallery.value, ...storedGallery.value].sort((a, b) => b.createdAt - a.createdAt))
const apiKeyOptions = computed(() => [
  { value: '', label: loadingKeys.value ? t('imageStudio.loadingKeys') : t('imageStudio.selectKey') },
  ...apiKeys.value.map((key) => ({ value: String(key.id), label: `${key.name} · ${key.group?.name || key.group?.platform || '—'}` })),
])
const modelPlaceholder = computed(() => loadingModels.value ? t('imageStudio.loadingModels') : t('imageStudio.modelPlaceholder'))
const canGenerate = computed(() => !!selectedKey.value && !!form.model.trim() && !!form.prompt.trim() && !generating.value)
const sizeOptions = computed(() => [
  { value: 'auto', label: t('imageStudio.auto') },
  { value: '1024x1024', label: '1024 × 1024' },
  { value: '1536x1024', label: '1536 × 1024' },
  { value: '1024x1536', label: '1024 × 1536' },
  { value: '2048x1152', label: '2048 × 1152' },
  { value: '1152x2048', label: '1152 × 2048' },
])
const countOptions = [1, 2, 3, 4].map((value) => ({ value: String(value), label: String(value) }))
const qualityOptions = computed(() => [
  { value: 'auto', label: t('imageStudio.auto') },
  { value: 'low', label: t('imageStudio.qualityLow') },
  { value: 'medium', label: t('imageStudio.qualityMedium') },
  { value: 'high', label: t('imageStudio.qualityHigh') },
])
const formatOptions = [{ value: 'png', label: 'PNG' }, { value: 'jpeg', label: 'JPEG' }, { value: 'webp', label: 'WebP' }]
const backgroundOptions = computed(() => [
  { value: 'auto', label: t('imageStudio.auto') },
  { value: 'opaque', label: t('imageStudio.backgroundOpaque') },
  { value: 'transparent', label: t('imageStudio.backgroundTransparent') },
])
const fidelityOptions = computed(() => [
  { value: 'auto', label: t('imageStudio.auto') },
  { value: 'low', label: t('imageStudio.fidelityLow') },
  { value: 'high', label: t('imageStudio.fidelityHigh') },
])

function apiKeyCanGenerateImages(key: ApiKey): boolean {
  return key.status === 'active' && key.group?.status === 'active' && key.group?.allow_image_generation === true && ['openai', 'grok', 'composite'].includes(key.group.platform)
}

async function loadKeys(): Promise<void> {
  loadingKeys.value = true
  try {
    const collected: ApiKey[] = []
    let page = 1
    while (true) {
      const response = await keysAPI.list(page, 100, { status: 'active', sort_by: 'created_at', sort_order: 'desc' })
      collected.push(...(response.items || []).filter(apiKeyCanGenerateImages))
      if (page >= response.pages || (response.items || []).length === 0) break
      page += 1
    }
    apiKeys.value = collected
    if (!selectedKey.value && collected.length) form.apiKeyId = String(collected[0].id)
  } catch (error) {
    appStore.showError(errorMessage(error, t('imageStudio.errors.loadKeys')))
  } finally {
    loadingKeys.value = false
  }
}

async function loadModels(): Promise<void> {
  modelController?.abort()
  models.value = []
  modelError.value = ''
  const key = selectedKey.value
  if (!key) {
    form.model = ''
    return
  }
  const controller = new AbortController()
  modelController = controller
  loadingModels.value = true
  try {
    const result = await listImageStudioModels(key.key, controller.signal)
    if (controller.signal.aborted) return
    models.value = result
    if (!result.some((model) => model.id === form.model)) form.model = result[0]?.id || ''
    if (result.length === 0) modelError.value = t('imageStudio.errors.noModels')
  } catch (error) {
    if (controller.signal.aborted) return
    modelError.value = errorMessage(error, t('imageStudio.errors.loadModels'))
  } finally {
    if (modelController === controller) {
      modelController = null
      loadingModels.value = false
    }
  }
}

function validateImage(file: File): boolean {
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) {
    appStore.showError(t('imageStudio.errors.invalidImage'))
    return false
  }
  if (file.size > 10 * 1024 * 1024) {
    appStore.showError(t('imageStudio.errors.imageTooLarge', { name: file.name }))
    return false
  }
  return true
}

function addReferenceImages(event: Event): void {
  const input = event.target as HTMLInputElement
  const remaining = Math.max(0, 4 - inputImages.value.length)
  const files = Array.from(input.files || []).slice(0, remaining).filter(validateImage)
  inputImages.value.push(...files.map((file) => ({ file, url: URL.createObjectURL(file) })))
  input.value = ''
}

function removeReferenceImage(index: number): void {
  const [removed] = inputImages.value.splice(index, 1)
  if (removed) URL.revokeObjectURL(removed.url)
  if (inputImages.value.length === 0) clearMask()
}

function selectMask(event: Event): void {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (file.type !== 'image/png') {
    appStore.showError(t('imageStudio.errors.invalidMask'))
    return
  }
  if (file.size > 10 * 1024 * 1024) {
    appStore.showError(t('imageStudio.errors.imageTooLarge', { name: file.name }))
    return
  }
  maskFile.value = file
}

function clearMask(): void {
  maskFile.value = null
}

function makeID(): string {
  return globalThis.crypto?.randomUUID?.() || `studio-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

async function outputToBlob(output: { b64Json?: string; url?: string }, format: string): Promise<Blob | null> {
  if (output.b64Json) return base64ImageToBlob(output.b64Json, format)
  if (!output.url) return null
  if (output.url.startsWith('data:')) return (await fetch(output.url)).blob()
  try {
    const response = await fetch(output.url)
    if (!response.ok) return null
    return response.blob()
  } catch {
    return null
  }
}

async function generate(): Promise<void> {
  const key = selectedKey.value
  if (!key || !form.model.trim() || !form.prompt.trim()) return
  generationController?.abort()
  const controller = new AbortController()
  generationController = controller
  generating.value = true
  generationError.value = ''
  try {
    const outputs = await generateImageStudioImages(key.key, {
      model: form.model.trim(), prompt: form.prompt.trim(), count: Number(form.count) || 1,
      size: form.size, quality: form.quality, outputFormat: form.outputFormat,
      background: form.background, inputFidelity: form.inputFidelity,
      images: inputImages.value.map((item) => item.file), mask: maskFile.value,
    }, controller.signal)
    const now = Date.now()
    const stored: StoredStudioImage[] = []
    const temporary: GalleryItem[] = []
    for (let index = 0; index < outputs.length; index += 1) {
      const output = outputs[index]
      const blob = await outputToBlob(output, form.outputFormat)
      const metadata = {
        id: makeID(), createdAt: now + index, prompt: form.prompt.trim(), revisedPrompt: output.revisedPrompt,
        model: form.model.trim(), size: form.size, outputFormat: form.outputFormat, apiKeyName: key.name,
      }
      if (blob) {
        stored.push({ ...metadata, blob, bytes: blob.size })
      } else if (output.url) {
        temporary.push({ ...metadata, url: output.url, persisted: false })
      }
    }
    let storageFailed = false
    if (stored.length) {
      try {
        await saveStoredStudioImages(stored)
      } catch {
        storageFailed = true
        temporary.push(...stored.map((item) => ({
          ...item,
          url: URL.createObjectURL(item.blob),
          persisted: false,
        })))
      }
    }
    temporaryGallery.value.unshift(...temporary)
    if (stored.length && !storageFailed) await loadLibrary()
    if (storageFailed) {
      appStore.showWarning(t('imageStudio.messages.storageUnavailable', { count: stored.length }))
    } else if (temporary.length) {
      appStore.showWarning(t('imageStudio.messages.temporaryResults', { count: temporary.length }))
    }
    appStore.showSuccess(t('imageStudio.messages.generated', { count: outputs.length }))
  } catch (error) {
    if (controller.signal.aborted) return
    generationError.value = errorMessage(error, t('imageStudio.errors.generate'))
  } finally {
    if (generationController === controller) {
      generationController = null
      generating.value = false
    }
  }
}

function cancelGeneration(): void {
  generationController?.abort()
  generationController = null
  generating.value = false
}

function revokeGalleryURLs(items: GalleryItem[]): void {
  items.forEach((item) => { if (item.url.startsWith('blob:')) URL.revokeObjectURL(item.url) })
}

async function loadLibrary(): Promise<void> {
  if (typeof indexedDB === 'undefined') return
  loadingLibrary.value = true
  try {
    const images = await listStoredStudioImages()
    revokeGalleryURLs(storedGallery.value)
    storedGallery.value = images.map((image) => ({ ...image, url: URL.createObjectURL(image.blob), persisted: true }))
  } catch (error) {
    appStore.showError(errorMessage(error, t('imageStudio.errors.loadLibrary')))
  } finally {
    loadingLibrary.value = false
  }
}

async function deleteItem(item: GalleryItem): Promise<void> {
  if (item.persisted) {
    await deleteStoredStudioImage(item.id)
    await loadLibrary()
  } else {
    revokeGalleryURLs([item])
    temporaryGallery.value = temporaryGallery.value.filter((candidate) => candidate.id !== item.id)
  }
  if (previewItem.value?.id === item.id) previewItem.value = null
}

async function clearLibrary(): Promise<void> {
  if (!window.confirm(t('imageStudio.clearConfirm'))) return
  await clearStoredStudioImages()
  await loadLibrary()
}

function extensionFor(item: GalleryItem): string {
  if (item.outputFormat === 'jpeg') return 'jpg'
  return item.outputFormat || 'png'
}

function downloadItem(item: GalleryItem): void {
  const anchor = document.createElement('a')
  anchor.href = item.url
  anchor.download = `sub2api-image-${new Date(item.createdAt).toISOString().replace(/[:.]/g, '-')}.${extensionFor(item)}`
  anchor.rel = 'noopener'
  anchor.click()
}

function reuseItem(item: GalleryItem): void {
  form.prompt = item.prompt
  form.model = item.model
  form.size = item.size || 'auto'
  previewItem.value = null
  window.scrollTo({ top: 0, behavior: 'smooth' })
}

function formatDate(value: number): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
}

function errorMessage(error: unknown, fallback: string): string {
  if (error && typeof error === 'object' && 'message' in error && typeof error.message === 'string' && error.message.trim()) return error.message
  return fallback
}

watch(() => form.apiKeyId, () => { void loadModels() })

onMounted(() => {
  void Promise.allSettled([loadKeys(), loadLibrary()])
})

onBeforeUnmount(() => {
  generationController?.abort()
  modelController?.abort()
  inputImages.value.forEach((item) => URL.revokeObjectURL(item.url))
  revokeGalleryURLs(storedGallery.value)
  revokeGalleryURLs(temporaryGallery.value)
})
</script>
