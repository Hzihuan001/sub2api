<template>
  <section aria-labelledby="prompt-events-title" class="py-6">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 id="prompt-events-title" class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.events.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.events.description') }}</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <label
          class="flex cursor-pointer items-center gap-2 rounded-lg border border-gray-200 px-3 py-1.5 text-xs text-gray-600 dark:border-dark-700 dark:text-dark-200"
          :title="t('admin.promptAudit.events.collapseRepeatsHint')"
        >
          <input v-model="collapseRepeats" type="checkbox" data-test="collapse-repeats" />
          <span>{{ t('admin.promptAudit.events.collapseRepeats') }}</span>
        </label>
        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('export', 'csv')">{{ t('admin.promptAudit.events.exportCsv') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" @click="$emit('export', 'jsonl')">{{ t('admin.promptAudit.events.exportJsonl') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="selectedIds.length === 0" @click="$emit('batch-delete')">
          {{ t('admin.promptAudit.events.deleteSelected', { count: selectedIds.length }) }}
        </button>
        <button type="button" class="btn btn-danger btn-sm" data-test="filter-delete" @click="$emit('preview-delete')">
          {{ t('admin.promptAudit.events.deleteByFilter') }}
        </button>
      </div>
    </div>

    <form class="mt-5 grid gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-5" @submit.prevent="applyFilters">
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.captureMode') }}</span>
        <Select
          v-model="localFilters.capture_mode"
          :options="captureModeOptions"
          size="sm"
          class="mt-1"
          :aria-label="t('admin.promptAudit.events.captureMode')"
          @change="filtersChanged"
        />
      </label>
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.decision') }}</span>
        <Select
          v-model="localFilters.decision"
          :options="decisionOptions"
          size="sm"
          class="mt-1"
          :aria-label="t('admin.promptAudit.events.decision')"
          @change="filtersChanged"
        />
      </label>
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.risk') }}</span>
        <Select
          v-model="localFilters.risk_level"
          :options="riskLevelOptions"
          size="sm"
          class="mt-1"
          :aria-label="t('admin.promptAudit.events.risk')"
          @change="filtersChanged"
        />
      </label>
      <FilterInput v-model="localFilters.endpoint" :label="t('admin.promptAudit.events.endpoint')" @change="filtersChanged" />
      <div ref="userSearchRef" class="relative text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.userAccount') }}</span>
        <input
          v-model="userKeyword"
          type="text"
          autocomplete="off"
          class="input mt-1 w-full pr-8"
          data-test="event-user-search"
          :placeholder="t('admin.promptAudit.events.searchUserPlaceholder')"
          @input="debounceUserSearch"
          @focus="showUserDropdown = !localFilters.user_id"
        />
        <button v-if="localFilters.user_id" type="button" class="absolute right-2 top-7 text-gray-400" data-test="clear-event-user" @click="clearUser">✕</button>
        <div v-if="showUserDropdown && userKeyword.trim()" class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800">
          <button v-for="user in userResults" :key="user.id" type="button" class="block w-full px-3 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700" @click="selectUser(user)">
            {{ user.email }}<span v-if="user.deleted" class="ml-1 text-gray-400">({{ t('admin.promptAudit.events.deletedUser') }})</span>
          </button>
          <p v-if="!searchingUsers && userResults.length === 0" class="px-3 py-2 text-gray-400">{{ t('admin.promptAudit.events.noUserMatches') }}</p>
        </div>
      </div>
      <div ref="apiKeySearchRef" class="relative text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.apiKey') }}</span>
        <input
          v-model="apiKeyKeyword"
          type="text"
          autocomplete="off"
          class="input mt-1 w-full pr-8"
          data-test="event-api-key-search"
          :placeholder="t('admin.promptAudit.events.searchApiKeyPlaceholder')"
          @input="debounceApiKeySearch"
          @focus="showAPIKeyDropdown = !localFilters.api_key_id"
        />
        <button v-if="localFilters.api_key_id" type="button" class="absolute right-2 top-7 text-gray-400" data-test="clear-event-api-key" @click="clearAPIKey()">✕</button>
        <div v-if="showAPIKeyDropdown && apiKeyKeyword.trim()" class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800">
          <button v-for="apiKey in apiKeyResults" :key="apiKey.id" type="button" class="block w-full px-3 py-2 text-left hover:bg-gray-100 dark:hover:bg-dark-700" @click="selectAPIKey(apiKey)">
            {{ apiKey.name }}
          </button>
          <p v-if="!searchingAPIKeys && apiKeyResults.length === 0" class="px-3 py-2 text-gray-400">{{ t('admin.promptAudit.events.noApiKeyMatches') }}</p>
        </div>
      </div>
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.group') }}</span>
        <Select v-model="localFilters.group_id" :options="groupOptions" searchable size="sm" class="mt-1" data-test="event-group-select" @change="filtersChanged" />
      </label>
      <FilterInput v-model="localFilters.request_id" :label="t('admin.promptAudit.events.requestId')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.prompt_hash" :label="t('admin.promptAudit.events.promptHash')" @change="filtersChanged" />
      <FilterInput v-model="localFilters.keyword" :label="t('admin.promptAudit.events.keyword')" @change="filtersChanged" />
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.startAt') }}</span>
        <input v-model="localFilters.start_at" type="datetime-local" class="input mt-1 w-full" :aria-label="t('admin.promptAudit.events.startAt')" @change="filtersChanged" />
      </label>
      <label class="text-xs text-gray-600 dark:text-dark-200">
        <span>{{ t('admin.promptAudit.events.endAt') }}</span>
        <input v-model="localFilters.end_at" type="datetime-local" class="input mt-1 w-full" :aria-label="t('admin.promptAudit.events.endAt')" @change="filtersChanged" />
      </label>
      <div class="flex items-end gap-2 sm:col-span-2">
        <button type="submit" class="btn btn-primary btn-sm">{{ t('common.search') }}</button>
        <button type="button" class="btn btn-ghost btn-sm" @click="resetFilters">{{ t('common.reset') }}</button>
      </div>
    </form>
    <div v-if="error" role="alert" class="mt-4 rounded-lg bg-red-50 px-4 py-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ error }}</div>
    <div class="mt-5 overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700/60">
      <table class="min-w-[1120px] w-full text-left text-sm">
        <thead class="bg-gray-50 text-xs uppercase tracking-wide text-gray-500 dark:bg-dark-900/70 dark:text-dark-400">
          <tr>
            <th class="w-10 px-3 py-3"><input type="checkbox" :checked="allSelected" :aria-label="t('admin.promptAudit.events.selectAll')" @change="toggleAll" /></th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.time') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.identity') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.group') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.route') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.result') }}</th>
            <th class="px-3 py-3 font-medium">{{ t('admin.promptAudit.events.preview') }}</th>
            <th class="px-3 py-3 text-right font-medium">{{ t('admin.promptAudit.common.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-transparent">
          <tr v-if="loading"><td colspan="8" class="px-4 py-12 text-center text-gray-500" aria-busy="true">{{ t('common.loading') }}</td></tr>
          <tr v-else-if="events.length === 0"><td colspan="8" class="px-4 py-12 text-center text-gray-500">{{ t('admin.promptAudit.events.empty') }}</td></tr>
          <tr v-for="event in displayedEvents" v-else :key="event.id" :data-test="`event-${event.id}`" class="align-top hover:bg-gray-50/70 dark:hover:bg-dark-800/70">
            <td class="px-3 py-3"><input type="checkbox" :checked="isRowSelected(event)" :indeterminate="isRowPartiallySelected(event)" :aria-label="t('admin.promptAudit.events.selectEvent', { id: event.id })" @change="toggleIDs(event.collapsedIds)" /></td>
            <td class="whitespace-nowrap px-3 py-3 text-xs text-gray-600 dark:text-dark-300">
              <p>{{ formatDate(event.created_at) }}</p>
              <span v-if="event.repeatCount > 1" data-test="event-repeat-count" :data-repeat-count="event.repeatCount" class="mt-1 inline-flex rounded-full bg-gray-100 px-2 py-0.5 font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                {{ t('admin.promptAudit.events.repeatCount', { count: event.repeatCount }) }}
              </span>
            </td>
            <td class="px-3 py-3">
              <CopyLine :label="t('admin.promptAudit.events.user')" :value="event.snapshot.username" />
              <CopyLine :label="t('admin.promptAudit.events.email')" :value="event.snapshot.user_email" />
              <CopyLine :label="t('admin.promptAudit.events.apiKey')" :value="event.snapshot.api_key_name" />
            </td>
            <td class="px-3 py-3 text-gray-700 dark:text-dark-200">{{ event.snapshot.group_name || '—' }}</td>
            <td class="px-3 py-3">
              <p class="font-medium text-gray-900 dark:text-white">{{ event.snapshot.endpoint }}</p>
              <p class="mt-1 text-xs text-gray-500">{{ event.snapshot.model }} · {{ event.snapshot.protocol }} · {{ event.snapshot.stage || 'http' }}</p>
            </td>
            <td class="px-3 py-3">
              <span v-if="event.capture_mode === 'capture_only'" class="rounded-full bg-blue-100 px-2 py-0.5 text-xs font-medium text-blue-700 dark:bg-blue-950/50 dark:text-blue-300">{{ t('admin.promptAudit.events.captureOnly') }}</span>
              <span v-else class="rounded-full px-2 py-0.5 text-xs font-medium" :class="decisionClass(event.decision)">{{ formatDecisionRisk(event.decision, event.risk_level) }}</span>
              <p v-if="event.capture_mode !== 'capture_only'" class="mt-2 max-w-48 truncate text-xs text-gray-500" :title="formatCategories(event.categories)">{{ formatCategories(event.categories) }}</p>
            </td>
            <td class="max-w-xs px-3 py-3"><p class="line-clamp-2 break-words text-gray-600 dark:text-dark-300">{{ event.snapshot.redacted_preview || '—' }}</p></td>
            <td class="whitespace-nowrap px-3 py-3 text-right">
              <button type="button" class="btn btn-ghost btn-sm" @click="$emit('view', event.id)">{{ t('common.view') }}</button>
              <button type="button" class="btn btn-ghost btn-sm text-red-600" :data-test="`delete-event-${event.id}`" @click="requestDelete(event)">{{ t('common.delete') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
      <Pagination :total="total" :page="page" :page-size="pageSize" @update:page="$emit('page', $event)" @update:page-size="$emit('page-size', $event)" />
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { SimpleApiKey, SimpleUser } from '@/api/admin/usage'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import type { PromptAuditEvent, PromptAuditGroup, PromptEventFilters } from '../types'
import { cloneData, emptyEventFilters, SCANNER_CATALOG } from '../viewModel'

const props = withDefaults(defineProps<{
  events: PromptAuditEvent[]; total: number; page: number; pageSize: number
  filters: PromptEventFilters; groups: PromptAuditGroup[]; selectedIds: number[]; loading: boolean; error: string
}>(), { groups: () => [] })
const emit = defineEmits<{
  (event: 'filters-change', value: PromptEventFilters): void
  (event: 'search', value: PromptEventFilters): void
  (event: 'selection', value: number[]): void
  (event: 'page', value: number): void
  (event: 'page-size', value: number): void
  (event: 'view', id: number): void
  (event: 'delete', id: number): void
  (event: 'delete-group', ids: number[]): void
  (event: 'batch-delete'): void
  (event: 'preview-delete'): void
  (event: 'export', format: 'csv' | 'jsonl'): void
}>()
const { t, locale } = useI18n()
const collapseRepeats = ref(true)
const repeatWindowMS = 5 * 60 * 1000
type DisplayPromptAuditEvent = PromptAuditEvent & { collapsedIds: number[]; repeatCount: number }
const captureModeOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'capture_only', label: t('admin.promptAudit.events.captureOnly') },
  { value: 'guard_audit', label: t('admin.promptAudit.events.guardAudit') },
])
const decisionOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'unreviewed', label: t('admin.promptAudit.decisions.unreviewed') },
  { value: 'pass', label: t('admin.promptAudit.decisions.pass') },
  { value: 'flag', label: t('admin.promptAudit.decisions.flag') },
  { value: 'critical', label: t('admin.promptAudit.decisions.critical') },
])
const riskLevelOptions = computed(() => [
  { value: '', label: t('common.all') },
  { value: 'unknown', label: t('admin.promptAudit.riskLevels.unknown') },
  { value: 'low', label: t('admin.promptAudit.riskLevels.low') },
  { value: 'medium', label: t('admin.promptAudit.riskLevels.medium') },
  { value: 'high', label: t('admin.promptAudit.riskLevels.high') },
  { value: 'critical', label: t('admin.promptAudit.riskLevels.critical') },
])
const groupOptions = computed(() => [
  { value: '', label: t('common.all') },
  ...props.groups.map((group) => ({ value: String(group.id), label: group.name })),
])
const localFilters = reactive<PromptEventFilters>(cloneData(props.filters))
const userSearchRef = ref<HTMLElement | null>(null)
const apiKeySearchRef = ref<HTMLElement | null>(null)
const userKeyword = ref('')
const apiKeyKeyword = ref('')
const userResults = ref<SimpleUser[]>([])
const apiKeyResults = ref<SimpleApiKey[]>([])
const showUserDropdown = ref(false)
const showAPIKeyDropdown = ref(false)
const searchingUsers = ref(false)
const searchingAPIKeys = ref(false)
let userSearchTimer: ReturnType<typeof setTimeout> | null = null
let apiKeySearchTimer: ReturnType<typeof setTimeout> | null = null
let userSearchSequence = 0
let apiKeySearchSequence = 0

watch(() => props.filters, (value) => {
  Object.assign(localFilters, cloneData(value))
  if (!value.user_id) userKeyword.value = ''
  if (!value.api_key_id) apiKeyKeyword.value = ''
}, { deep: true })
const allSelected = computed(() => props.events.length > 0 && props.events.every((event) => props.selectedIds.includes(event.id)))
const displayedEvents = computed<DisplayPromptAuditEvent[]>(() => {
  if (!collapseRepeats.value) {
    return props.events.map((event) => ({ ...event, collapsedIds: [event.id], repeatCount: 1 }))
  }
  const rows: DisplayPromptAuditEvent[] = []
  for (const event of props.events) {
    const previous = rows.at(-1)
    if (previous && canCollapseEvents(previous, event)) {
      previous.collapsedIds.push(event.id)
      previous.repeatCount += 1
      continue
    }
    rows.push({ ...event, collapsedIds: [event.id], repeatCount: 1 })
  }
  return rows
})

const FilterInput = defineComponent({
  props: { modelValue: { type: String, required: true }, label: { type: String, required: true }, type: { type: String, default: 'text' } },
  emits: ['update:modelValue', 'change'],
  setup(componentProps, { emit: componentEmit }) {
    return () => h('label', { class: 'text-xs text-gray-600 dark:text-dark-200' }, [
      h('span', componentProps.label),
      h('input', {
        value: componentProps.modelValue, type: componentProps.type, class: 'input mt-1 w-full', 'aria-label': componentProps.label,
        onInput: (event: Event) => componentEmit('update:modelValue', (event.target as HTMLInputElement).value),
        onChange: () => componentEmit('change'),
      }),
    ])
  },
})

const CopyLine = defineComponent({
  props: { label: { type: String, required: true }, value: { type: String, default: '' } },
  setup(componentProps) {
    return () => h('div', { class: 'flex max-w-56 items-center gap-1 text-xs' }, [
      h('span', { class: 'w-16 flex-none text-gray-500 dark:text-dark-400' }, componentProps.label),
      h('span', { class: 'min-w-0 flex-1 truncate text-gray-800 dark:text-dark-100' }, componentProps.value || '—'),
      componentProps.value ? h('button', {
        type: 'button', class: 'text-primary-600 hover:underline', 'aria-label': `${t('common.copy')} ${componentProps.label}`,
        onClick: () => navigator.clipboard?.writeText(componentProps.value),
      }, t('common.copy')) : null,
    ])
  },
})

function filtersChanged() {
  emit('filters-change', cloneData(localFilters))
}
function debounceUserSearch() {
  if (userSearchTimer) clearTimeout(userSearchTimer)
  const query = userKeyword.value.trim()
  localFilters.user_id = ''
  clearAPIKey(false)
  userResults.value = []
  showUserDropdown.value = true
  const sequence = ++userSearchSequence
  if (!query) return
  searchingUsers.value = true
  userSearchTimer = setTimeout(async () => {
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === userSearchSequence) userResults.value = results.sort((a, b) => Number(a.deleted) - Number(b.deleted))
    } catch {
      if (sequence === userSearchSequence) userResults.value = []
    } finally {
      if (sequence === userSearchSequence) searchingUsers.value = false
    }
  }, 300)
}
function selectUser(user: SimpleUser) {
  userSearchSequence++
  if (userSearchTimer) clearTimeout(userSearchTimer)
  userKeyword.value = user.email
  localFilters.user_id = String(user.id)
  userResults.value = []
  searchingUsers.value = false
  showUserDropdown.value = false
  clearAPIKey(false)
  filtersChanged()
}
function clearUser() {
  userSearchSequence++
  if (userSearchTimer) clearTimeout(userSearchTimer)
  userKeyword.value = ''
  userResults.value = []
  searchingUsers.value = false
  showUserDropdown.value = false
  localFilters.user_id = ''
  clearAPIKey(false)
  filtersChanged()
}
function debounceApiKeySearch() {
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  const query = apiKeyKeyword.value.trim()
  localFilters.api_key_id = ''
  apiKeyResults.value = []
  showAPIKeyDropdown.value = true
  const sequence = ++apiKeySearchSequence
  if (!query) return
  searchingAPIKeys.value = true
  apiKeySearchTimer = setTimeout(async () => {
    try {
      const userID = Number(localFilters.user_id) || undefined
      const results = await adminAPI.usage.searchApiKeys(userID, query)
      if (sequence === apiKeySearchSequence) apiKeyResults.value = results
    } catch {
      if (sequence === apiKeySearchSequence) apiKeyResults.value = []
    } finally {
      if (sequence === apiKeySearchSequence) searchingAPIKeys.value = false
    }
  }, 300)
}
function selectAPIKey(apiKey: SimpleApiKey) {
  apiKeySearchSequence++
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  apiKeyKeyword.value = apiKey.name
  localFilters.api_key_id = String(apiKey.id)
  apiKeyResults.value = []
  searchingAPIKeys.value = false
  showAPIKeyDropdown.value = false
  filtersChanged()
}
function clearAPIKey(shouldEmit = true) {
  apiKeySearchSequence++
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  apiKeyKeyword.value = ''
  apiKeyResults.value = []
  searchingAPIKeys.value = false
  showAPIKeyDropdown.value = false
  localFilters.api_key_id = ''
  if (shouldEmit) filtersChanged()
}
function onDocumentClick(event: MouseEvent) {
  const target = event.target as Node | null
  if (!target) return
  if (!userSearchRef.value?.contains(target)) showUserDropdown.value = false
  if (!apiKeySearchRef.value?.contains(target)) showAPIKeyDropdown.value = false
}
function applyFilters() {
  const value = cloneData(localFilters)
  emit('filters-change', value)
  emit('search', value)
}
function resetFilters() {
  Object.assign(localFilters, emptyEventFilters())
  userKeyword.value = ''
  apiKeyKeyword.value = ''
  userResults.value = []
  apiKeyResults.value = []
  applyFilters()
}
function canCollapseEvents(newest: PromptAuditEvent, candidate: PromptAuditEvent): boolean {
  if (!newest.snapshot.prompt_hash || newest.snapshot.prompt_hash !== candidate.snapshot.prompt_hash) return false
  if (newest.capture_mode !== candidate.capture_mode) return false
  if (newest.snapshot.user_id !== candidate.snapshot.user_id || newest.snapshot.api_key_id !== candidate.snapshot.api_key_id || newest.snapshot.group_id !== candidate.snapshot.group_id) return false
  if (newest.snapshot.endpoint !== candidate.snapshot.endpoint || newest.snapshot.protocol !== candidate.snapshot.protocol || newest.snapshot.model !== candidate.snapshot.model) return false
  if (newest.capture_mode !== 'capture_only' && (newest.decision !== candidate.decision || newest.risk_level !== candidate.risk_level || newest.action !== candidate.action)) return false
  const newestAt = Date.parse(newest.created_at)
  const candidateAt = Date.parse(candidate.created_at)
  return Number.isFinite(newestAt) && Number.isFinite(candidateAt) && Math.abs(newestAt - candidateAt) <= repeatWindowMS
}
function isRowSelected(event: DisplayPromptAuditEvent): boolean {
  return event.collapsedIds.every((id) => props.selectedIds.includes(id))
}
function isRowPartiallySelected(event: DisplayPromptAuditEvent): boolean {
  const selectedCount = event.collapsedIds.filter((id) => props.selectedIds.includes(id)).length
  return selectedCount > 0 && selectedCount < event.collapsedIds.length
}
function toggleIDs(ids: number[]) {
  const selected = new Set(props.selectedIds)
  if (ids.every((id) => selected.has(id))) ids.forEach((id) => selected.delete(id))
  else ids.forEach((id) => selected.add(id))
  emit('selection', [...selected])
}
function toggleAll() {
  emit('selection', allSelected.value ? [] : props.events.map((event) => event.id))
}
function requestDelete(event: DisplayPromptAuditEvent) {
  if (event.collapsedIds.length > 1) emit('delete-group', [...event.collapsedIds])
  else emit('delete', event.id)
}
function formatDate(value: string): string {
  return new Intl.DateTimeFormat(locale.value, { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value))
}
function decisionClass(decision: string): string {
  if (decision === 'critical') return 'bg-red-100 text-red-700 dark:bg-red-950/50 dark:text-red-300'
  if (decision === 'flag') return 'bg-amber-100 text-amber-700 dark:bg-amber-950/50 dark:text-amber-300'
  return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-950/50 dark:text-emerald-300'
}
const DECISIONS = new Set(['unreviewed', 'pass', 'flag', 'critical'])
const RISK_LEVELS = new Set(['unknown', 'low', 'medium', 'high', 'critical'])

function translateDecision(decision: string): string {
  return DECISIONS.has(decision) ? t(`admin.promptAudit.decisions.${decision}`) : decision
}
function translateRiskLevel(riskLevel: string): string {
  return RISK_LEVELS.has(riskLevel) ? t(`admin.promptAudit.riskLevels.${riskLevel}`) : riskLevel
}
function translateCategory(category: string): string {
  return SCANNER_CATALOG.some((scanner) => scanner.id === category)
    ? t(`admin.promptAudit.scanners.${category}`)
    : category
}
function formatDecisionRisk(decision: string, riskLevel: string): string {
  return `${translateDecision(decision)} · ${translateRiskLevel(riskLevel)}`
}
function formatCategories(categories: string[]): string {
  if (!categories.length) return '—'
  return categories.map(translateCategory).join(', ')
}

onMounted(() => document.addEventListener('click', onDocumentClick))
onUnmounted(() => {
  if (userSearchTimer) clearTimeout(userSearchTimer)
  if (apiKeySearchTimer) clearTimeout(apiKeySearchTimer)
  document.removeEventListener('click', onDocumentClick)
})
</script>
