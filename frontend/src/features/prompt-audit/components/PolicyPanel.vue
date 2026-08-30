<template>
  <section aria-labelledby="prompt-policy-title" class="py-6">
    <div>
      <h2 id="prompt-policy-title" class="text-base font-semibold text-gray-950 dark:text-white">{{ t('admin.promptAudit.policy.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('admin.promptAudit.policy.description') }}</p>
    </div>

    <div class="mt-5 grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,0.45fr)]">
      <div class="rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:bg-dark-900/20 sm:p-5">
        <fieldset>
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.scope') }}</legend>
          <div class="mt-3 flex flex-wrap gap-5 text-sm text-gray-700 dark:text-dark-200">
            <label class="flex items-center gap-2">
              <input type="radio" name="prompt-audit-scope" :checked="draft.all_groups" @change="patch({ all_groups: true, group_ids: [] })" />
              {{ t('admin.promptAudit.policy.allGroups') }}
            </label>
            <label class="flex items-center gap-2">
              <input type="radio" name="prompt-audit-scope" :checked="!draft.all_groups" @change="patch({ all_groups: false })" />
              {{ t('admin.promptAudit.policy.selectedGroups') }}
            </label>
          </div>
        </fieldset>

        <div v-if="!draft.all_groups" class="mt-4">
          <label class="block text-sm text-gray-700 dark:text-dark-200">
            <span>{{ t('admin.promptAudit.policy.searchGroups') }}</span>
            <input v-model="groupSearch" type="search" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.searchGroups')" />
          </label>
          <div class="mt-3 max-h-52 overflow-y-auto rounded-lg border border-gray-200 p-2 dark:border-dark-700">
            <label v-for="group in filteredGroups" :key="group.id" class="flex cursor-pointer items-center justify-between gap-3 rounded-md px-2 py-2 text-sm hover:bg-gray-50 dark:hover:bg-dark-800">
              <span class="flex items-center gap-2 text-gray-800 dark:text-dark-100">
                <input type="checkbox" :checked="draft.group_ids.includes(group.id)" @change="toggleGroup(group.id)" />
                {{ group.name }}
              </span>
              <span class="text-xs text-gray-500 dark:text-dark-400">{{ group.platform }} · {{ group.status }}</span>
            </label>
            <p v-if="filteredGroups.length === 0" class="px-2 py-4 text-center text-sm text-gray-500">{{ t('admin.promptAudit.policy.noGroups') }}</p>
          </div>
          <div v-if="missingGroupIds.length" class="mt-3 rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
            {{ t('admin.promptAudit.policy.missingGroups') }}: {{ missingGroupIds.join(', ') }}
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.policy.selectedCount', { count: draft.group_ids.length }) }}</p>
        </div>

        <fieldset v-if="!draft.capture_only_enabled" class="mt-5 border-t border-gray-100 pt-5 dark:border-dark-800">
          <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.promptAudit.policy.scanners') }}</legend>
          <div class="mt-3 grid gap-2 sm:grid-cols-2">
            <label v-for="scanner in SCANNER_CATALOG" :key="scanner.id" class="flex items-center gap-2 rounded-md px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:text-dark-200 dark:hover:bg-dark-800">
              <input type="checkbox" :checked="draft.scanners.includes(scanner.id)" :aria-label="scannerLabel(scanner.id)" @change="toggleScanner(scanner.id)" />
              <span>{{ scannerLabel(scanner.id) }}</span>
            </label>
          </div>
        </fieldset>
      </div>

      <div v-if="!draft.capture_only_enabled" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700/60 dark:bg-dark-900/20 sm:p-5">
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.policy.workerCount') }}</span>
          <input :value="draft.worker_count" type="number" min="1" max="32" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.workerCount')" @input="patch({ worker_count: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.policy.queueCapacity') }}</span>
          <input :value="draft.queue_capacity" type="number" min="1" max="100000" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.policy.queueCapacity')" @input="patch({ queue_capacity: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <div class="rounded-lg bg-gray-50 px-4 py-3 text-sm text-gray-600 dark:bg-dark-900/50 dark:text-dark-300">
          <p class="font-medium text-gray-800 dark:text-dark-100">{{ t('admin.promptAudit.policy.strategy') }}</p>
          <p class="mt-1">priority · {{ t('admin.promptAudit.policy.strategyHint') }}</p>
        </div>
      </div>

      <div v-else class="space-y-4 rounded-xl border border-primary-200 bg-primary-50/60 p-4 dark:border-primary-900/60 dark:bg-primary-950/20 sm:p-5">
        <p class="text-sm font-medium text-primary-900 dark:text-primary-100">{{ t('admin.promptAudit.recording.latestOnlyTitle') }}</p>
        <p class="text-sm text-primary-800/80 dark:text-primary-200/80">{{ t('admin.promptAudit.recording.latestOnlyHint') }}</p>
        <div>
          <label class="block text-sm text-gray-700 dark:text-dark-200">
            <span>{{ t('admin.promptAudit.recording.excludedUsers') }}</span>
            <input
              v-model="excludedUserSearch"
              data-test="capture-excluded-user-search"
              type="search"
              class="input mt-1.5 w-full"
              :placeholder="t('admin.promptAudit.recording.excludedUsersSearch')"
              :aria-label="t('admin.promptAudit.recording.excludedUsers')"
            />
          </label>
          <div v-if="excludedUserSearching || excludedUserResults.length" class="mt-2 rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-700 dark:bg-dark-900">
            <p v-if="excludedUserSearching" class="px-2 py-2 text-xs text-gray-500">{{ t('common.loading') }}</p>
            <template v-else>
              <button
                v-for="user in excludedUserResults"
                :key="user.id"
                type="button"
                class="block w-full rounded-md px-2 py-2 text-left text-sm hover:bg-gray-50 dark:hover:bg-dark-800"
                @click="addExcludedUser(user)"
              >
                {{ userLabel(user) }}
              </button>
            </template>
          </div>
          <div v-if="draft.capture_excluded_user_ids.length" class="mt-2 flex flex-wrap gap-2">
            <button
              v-for="userId in draft.capture_excluded_user_ids"
              :key="userId"
              type="button"
              class="rounded-full border border-primary-200 bg-white px-3 py-1 text-xs text-primary-800 dark:border-primary-800 dark:bg-dark-900 dark:text-primary-200"
              :aria-label="t('admin.promptAudit.recording.removeExcludedUser', { user: excludedUserLabel(userId) })"
              @click="removeExcludedUser(userId)"
            >
              {{ excludedUserLabel(userId) }} ×
            </button>
          </div>
          <p class="mt-2 text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.recording.excludedUsersHint') }}</p>
        </div>
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.recording.retentionDays') }}</span>
          <input :value="draft.retention_days" type="number" min="1" max="365" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.recording.retentionDays')" @input="patch({ retention_days: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <label class="block text-sm text-gray-700 dark:text-dark-200">
          <span>{{ t('admin.promptAudit.recording.maxStorageMB') }}</span>
          <input :value="draft.max_storage_mb" type="number" min="16" max="10240" class="input mt-1.5 w-full" :aria-label="t('admin.promptAudit.recording.maxStorageMB')" @input="patch({ max_storage_mb: Number(($event.target as HTMLInputElement).value) })" />
        </label>
        <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.promptAudit.recording.cleanupPolicyHint') }}</p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminUser } from '@/types'
import type { SimpleUser } from '@/api/admin/usage'
import type { PromptAuditDraft, PromptAuditGroup } from '../types'
import { cloneData, SCANNER_CATALOG } from '../viewModel'

const props = defineProps<{ draft: PromptAuditDraft; groups: PromptAuditGroup[] }>()
const emit = defineEmits<{ (event: 'update:draft', value: PromptAuditDraft): void }>()
const { t } = useI18n()
const groupSearch = ref('')
const excludedUserSearch = ref('')
const excludedUserResults = ref<SimpleUser[]>([])
const excludedUserSearching = ref(false)
const excludedUsers = ref<Record<number, Pick<AdminUser, 'id' | 'email' | 'username'>>>({})
let excludedUserSearchTimer: ReturnType<typeof setTimeout> | null = null

const filteredGroups = computed(() => {
  const query = groupSearch.value.trim().toLowerCase()
  if (!query) return props.groups
  return props.groups.filter((group) => `${group.name} ${group.id} ${group.platform}`.toLowerCase().includes(query))
})
const knownGroupIds = computed(() => new Set(props.groups.map((group) => group.id)))
const missingGroupIds = computed(() => props.draft.group_ids.filter((id) => !knownGroupIds.value.has(id)))

watch(
  () => props.draft.capture_excluded_user_ids,
  async (ids) => {
    const missing = ids.filter((id) => !excludedUsers.value[id])
    if (!missing.length) return
    const settled = await Promise.allSettled(missing.map((id) => adminAPI.users.getById(id)))
    const next = { ...excludedUsers.value }
    for (const result of settled) {
      if (result.status === 'fulfilled') next[result.value.id] = result.value
    }
    excludedUsers.value = next
  },
  { immediate: true },
)

watch(excludedUserSearch, (value) => {
  if (excludedUserSearchTimer) clearTimeout(excludedUserSearchTimer)
  const query = value.trim()
  if (!query) {
    excludedUserResults.value = []
    excludedUserSearching.value = false
    return
  }
  excludedUserSearching.value = true
  excludedUserSearchTimer = setTimeout(async () => {
    try {
      const users = await adminAPI.usage.searchUsers(query)
      excludedUserResults.value = users.filter((user) => !user.deleted && !props.draft.capture_excluded_user_ids.includes(user.id))
    } catch {
      excludedUserResults.value = []
    } finally {
      excludedUserSearching.value = false
    }
  }, 300)
})

onBeforeUnmount(() => {
  if (excludedUserSearchTimer) clearTimeout(excludedUserSearchTimer)
})

function patch(value: Partial<PromptAuditDraft>) {
  emit('update:draft', { ...cloneData(props.draft), ...value })
}
function toggleGroup(id: number) {
  const selected = new Set(props.draft.group_ids)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  patch({ group_ids: [...selected].sort((a, b) => a - b) })
}
function toggleScanner(id: string) {
  const selected = new Set(props.draft.scanners)
  if (selected.has(id)) selected.delete(id)
  else selected.add(id)
  patch({ scanners: SCANNER_CATALOG.map((item) => item.id).filter((item) => selected.has(item)) })
}
function userLabel(user: Pick<SimpleUser, 'id' | 'email'>): string {
  return user.email || `#${user.id}`
}
function excludedUserLabel(userId: number): string {
  const user = excludedUsers.value[userId]
  if (!user) return t('admin.promptAudit.recording.unknownExcludedUser')
  return user.username ? `${user.email} (${user.username})` : user.email
}
function addExcludedUser(user: SimpleUser) {
  excludedUsers.value = { ...excludedUsers.value, [user.id]: { id: user.id, email: user.email, username: '' } }
  patch({ capture_excluded_user_ids: [...new Set([...props.draft.capture_excluded_user_ids, user.id])].sort((a, b) => a - b) })
  excludedUserSearch.value = ''
  excludedUserResults.value = []
}
function removeExcludedUser(userId: number) {
  patch({ capture_excluded_user_ids: props.draft.capture_excluded_user_ids.filter((id) => id !== userId) })
}
function scannerLabel(id: string): string {
  return t(`admin.promptAudit.scanners.${id}`)
}
</script>
