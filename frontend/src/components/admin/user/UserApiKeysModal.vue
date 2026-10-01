<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div><p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p><p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p></div>
      </div>
      <div v-if="loading" class="flex justify-center py-8"><svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg></div>
      <div class="flex justify-end">
        <button
          v-if="!props.readOnly"
          type="button"
          class="btn btn-primary btn-sm"
          data-test="create-user-api-key"
          :disabled="creating"
          @click="beginCreate"
        >
          <span class="mr-1 text-base leading-none">+</span>
          {{ t('admin.users.createApiKey') }}
        </button>
      </div>
      <form
        v-if="!props.readOnly && showCreateForm"
        class="space-y-3 rounded-xl border border-primary-200 bg-primary-50/50 p-4 dark:border-primary-800 dark:bg-primary-900/10"
        data-test="create-user-api-key-form"
        @submit.prevent="createApiKey"
      >
        <div>
          <label class="input-label" for="admin-user-api-key-name">{{ t('admin.users.apiKeyName') }}</label>
          <input
            id="admin-user-api-key-name"
            v-model="createName"
            type="text"
            required
            maxlength="100"
            class="input"
            :placeholder="t('admin.users.apiKeyNamePlaceholder')"
            data-test="create-user-api-key-name"
          />
        </div>
        <div>
          <label class="input-label" for="admin-user-api-key-group">{{ t('admin.users.group') }}</label>
          <select id="admin-user-api-key-group" v-model="createGroupId" class="input" data-test="create-user-api-key-group">
            <option :value="null">{{ t('admin.users.none') }}</option>
            <option v-for="group in allGroups" :key="group.id" :value="group.id">{{ group.name }}</option>
          </select>
        </div>
        <div class="flex justify-end gap-2">
          <button type="button" class="btn btn-secondary btn-sm" :disabled="creating" @click="cancelCreate">
            {{ t('common.cancel') }}
          </button>
          <button type="submit" class="btn btn-primary btn-sm" :disabled="creating || !createName.trim()" data-test="submit-create-user-api-key">
            <span v-if="creating" class="mr-1 inline-block h-3 w-3 animate-spin rounded-full border-2 border-white border-t-transparent" />
            {{ creating ? t('admin.users.creatingApiKey') : t('admin.users.createApiKey') }}
          </button>
        </div>
      </form>
      <div v-if="!loading && apiKeys.length === 0" class="py-8 text-center"><p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p></div>
      <div v-else-if="!loading" ref="scrollContainerRef" class="max-h-96 space-y-3 overflow-y-auto" @scroll="closeGroupSelector">
        <div v-for="key in apiKeys" :key="key.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2"><span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span><span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">{{ key.status }}</span></div>
              <p class="truncate font-mono text-sm text-gray-500">{{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}</p>
            </div>
            <div class="flex shrink-0 items-center gap-1">
              <button
                type="button"
                class="rounded-lg px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400"
                :title="t('admin.users.copyApiKey')"
                data-test="copy-user-api-key"
                @click="copyApiKey(key)"
              >
                {{ t('admin.users.copyApiKey') }}
              </button>
              <button
                v-if="!props.readOnly"
                type="button"
                class="rounded-lg px-2 py-1 text-xs text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
                :title="t('admin.users.deleteApiKey')"
                data-test="delete-user-api-key"
                :disabled="deletingKeyId === key.id"
                @click="requestDelete(key)"
              >
                {{ t('common.delete') }}
              </button>
            </div>
          </div>
          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                v-if="!props.readOnly"
                :ref="(el) => setGroupButtonRef(key.id, el)"
                @click="openGroupSelector(key)"
                class="-mx-1 -my-0.5 flex cursor-pointer items-center gap-1 rounded-md px-1 py-0.5 transition-colors hover:bg-gray-100 dark:hover:bg-dark-700"
                :disabled="updatingKeyIds.has(key.id)"
              >
                <GroupBadge
                  v-if="key.group_id && key.group"
                  :name="key.group.name"
                  :platform="key.group.platform"
                  :subscription-type="key.group.subscription_type"
                  :rate-multiplier="key.group.rate_multiplier"
                  :peak-rate-enabled="key.group.peak_rate_enabled"
                  :peak-start="key.group.peak_start"
                  :peak-end="key.group.peak_end"
                  :peak-rate-multiplier="key.group.peak_rate_multiplier"
                />
                <span v-else class="text-gray-400 italic">{{ t('admin.users.none') }}</span>
                <svg v-if="updatingKeyIds.has(key.id)" class="h-3 w-3 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
                <svg v-else class="h-3 w-3 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9" /></svg>
              </button>
              <GroupBadge
                v-else-if="key.group_id && key.group"
                :name="key.group.name"
                :platform="key.group.platform"
                :subscription-type="key.group.subscription_type"
                :rate-multiplier="key.group.rate_multiplier"
                :peak-rate-enabled="key.group.peak_rate_enabled"
                :peak-start="key.group.peak_start"
                :peak-end="key.group.peak_end"
                :peak-rate-multiplier="key.group.peak_rate_multiplier"
              />
              <span v-else class="text-gray-400 italic">{{ t('admin.users.none') }}</span>
            </div>
            <div class="flex items-center gap-1"><span>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</span></div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>

  <ConfirmDialog
    :show="showDeleteDialog"
    :title="t('admin.users.deleteApiKey')"
    :message="t('admin.users.deleteApiKeyConfirm', { name: deletingKey?.name || '' })"
    :confirm-text="t('common.delete')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="deleteApiKey"
    @cancel="cancelDelete"
  />

  <!-- Group Selector Dropdown -->
  <Teleport to="body">
    <div
      v-if="!props.readOnly && groupSelectorKeyId !== null && dropdownPosition"
      ref="dropdownRef"
      class="animate-in fade-in slide-in-from-top-2 fixed z-[100000020] w-64 overflow-hidden rounded-xl bg-white shadow-lg ring-1 ring-black/5 duration-200 dark:bg-dark-800 dark:ring-white/10"
      :style="{ top: dropdownPosition.top + 'px', left: dropdownPosition.left + 'px' }"
    >
      <div class="max-h-64 overflow-y-auto p-1.5">
        <!-- Unbind option -->
        <button
          @click="changeGroup(selectedKeyForGroup!, null)"
          :class="[
            'flex w-full items-center rounded-lg px-3 py-2 text-sm transition-colors',
            !selectedKeyForGroup?.group_id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
        >
          <span class="text-gray-500 italic">{{ t('admin.users.none') }}</span>
          <svg
            v-if="!selectedKeyForGroup?.group_id"
            class="ml-auto h-4 w-4 shrink-0 text-primary-600 dark:text-primary-400"
            fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2"
          ><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
        </button>
        <!-- Group options -->
        <button
          v-for="group in allGroups"
          :key="group.id"
          @click="changeGroup(selectedKeyForGroup!, group.id)"
          :class="[
            'flex w-full items-center justify-between rounded-lg px-3 py-2 text-sm transition-colors',
            selectedKeyForGroup?.group_id === group.id
              ? 'bg-primary-50 dark:bg-primary-900/20'
              : 'hover:bg-gray-100 dark:hover:bg-dark-700'
          ]"
        >
          <GroupOptionItem
            :name="group.name"
            :platform="group.platform"
            :subscription-type="group.subscription_type"
            :rate-multiplier="group.rate_multiplier"
            :peak-rate-enabled="group.peak_rate_enabled"
            :peak-start="group.peak_start"
            :peak-end="group.peak_end"
            :peak-rate-multiplier="group.peak_rate_multiplier"
            :description="group.description"
            :selected="selectedKeyForGroup?.group_id === group.id"
          />
        </button>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime } from '@/utils/format'
import type { AdminUser, AdminGroup, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'

const props = withDefaults(defineProps<{ show: boolean; user: AdminUser | null; readOnly?: boolean }>(), {
  readOnly: false
})
const emit = defineEmits(['close'])
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
let requestVersion = 0
const updatingKeyIds = ref(new Set<number>())
const groupSelectorKeyId = ref<number | null>(null)
const dropdownPosition = ref<{ top: number; left: number } | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const scrollContainerRef = ref<HTMLElement | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())
const showCreateForm = ref(false)
const createName = ref('')
const createGroupId = ref<number | null>(null)
const creating = ref(false)
const showDeleteDialog = ref(false)
const deletingKey = ref<ApiKey | null>(null)
const deletingKeyId = ref<number | null>(null)

const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

watch(() => [props.show, props.user?.id] as const, ([show], _, onCleanup) => {
  onCleanup(() => { requestVersion++ })
  closeGroupSelector()
  if (show && props.user) {
    load()
    if (!props.readOnly) loadGroups()
    showCreateForm.value = false
    createName.value = ''
    createGroupId.value = null
  } else {
    closeGroupSelector()
    showCreateForm.value = false
  }
})

const load = async () => {
  if (!props.user) return
  const version = ++requestVersion
  apiKeys.value = []
  loading.value = true
  groupButtonRefs.value.clear()
  try {
    const res = await adminAPI.users.getUserApiKeys(props.user.id)
    if (version === requestVersion) apiKeys.value = res.items || []
  } catch (error) {
    if (version !== requestVersion) return
    console.error('Failed to load API keys:', error)
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

const loadGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    allGroups.value = groups
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const beginCreate = () => {
  createName.value = ''
  createGroupId.value = null
  showCreateForm.value = true
}

const cancelCreate = () => {
  if (creating.value) return
  showCreateForm.value = false
}

const createApiKey = async () => {
  if (props.readOnly || !props.user) return
  const name = createName.value.trim()
  if (!name || creating.value) return

  creating.value = true
  try {
    const key = await adminAPI.users.createUserApiKey(props.user.id, {
      name,
      group_id: createGroupId.value
    })
    apiKeys.value.unshift(key)
    showCreateForm.value = false
    createName.value = ''
    createGroupId.value = null
    appStore.showSuccess(t('admin.users.apiKeyCreated'))
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.apiKeyCreateFailed'))
  } finally {
    creating.value = false
  }
}

const copyApiKey = async (key: ApiKey) => {
  await copyToClipboard(key.key, t('admin.users.apiKeyCopied'))
}

const requestDelete = (key: ApiKey) => {
  if (props.readOnly) return
  deletingKey.value = key
  showDeleteDialog.value = true
}

const cancelDelete = () => {
  if (deletingKeyId.value !== null) return
  deletingKey.value = null
  showDeleteDialog.value = false
}

const deleteApiKey = async () => {
  if (props.readOnly || !deletingKey.value || deletingKeyId.value !== null) return
  const key = deletingKey.value
  deletingKeyId.value = key.id
  try {
    await adminAPI.apiKeys.deleteApiKey(key.id)
    apiKeys.value = apiKeys.value.filter((item) => item.id !== key.id)
    appStore.showSuccess(t('admin.users.apiKeyDeleted'))
    showDeleteDialog.value = false
    deletingKey.value = null
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.apiKeyDeleteFailed'))
  } finally {
    deletingKeyId.value = null
  }
}

const DROPDOWN_HEIGHT = 272 // max-h-64 = 16rem = 256px + padding
const DROPDOWN_GAP = 4

const openGroupSelector = (key: ApiKey) => {
  if (props.readOnly) return
  if (groupSelectorKeyId.value === key.id) {
    closeGroupSelector()
  } else {
    const buttonEl = groupButtonRefs.value.get(key.id)
    if (buttonEl) {
      const rect = buttonEl.getBoundingClientRect()
      const spaceBelow = window.innerHeight - rect.bottom
      const openUpward = spaceBelow < DROPDOWN_HEIGHT && rect.top > spaceBelow
      dropdownPosition.value = {
        top: openUpward ? rect.top - DROPDOWN_HEIGHT - DROPDOWN_GAP : rect.bottom + DROPDOWN_GAP,
        left: rect.left
      }
    }
    groupSelectorKeyId.value = key.id
  }
}

const closeGroupSelector = () => {
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  if (props.readOnly) return
  closeGroupSelector()
  if (key.group_id === newGroupId || (!key.group_id && newGroupId === null)) return

  updatingKeyIds.value.add(key.id)
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroup(key.id, newGroupId)
    // Update local data
    const idx = apiKeys.value.findIndex((k) => k.id === key.id)
    if (idx !== -1) {
      apiKeys.value[idx] = result.api_key
    }
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    }
  } catch (error: any) {
    appStore.showError(error?.message || t('admin.users.groupChangeFailed'))
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const handleKeyDown = (event: KeyboardEvent) => {
  if (event.key === 'Escape' && groupSelectorKeyId.value !== null) {
    event.stopPropagation()
    closeGroupSelector()
  }
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (dropdownRef.value && !dropdownRef.value.contains(target)) {
    // Check if the click is on one of the group trigger buttons
    for (const el of groupButtonRefs.value.values()) {
      if (el.contains(target)) return
    }
    closeGroupSelector()
  }
}

const handleClose = () => {
  closeGroupSelector()
  showCreateForm.value = false
  showDeleteDialog.value = false
  deletingKey.value = null
  emit('close')
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('keydown', handleKeyDown, true)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('keydown', handleKeyDown, true)
})
</script>
