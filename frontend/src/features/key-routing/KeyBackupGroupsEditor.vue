<template>
  <fieldset class="min-w-0 space-y-3" :disabled="disabled" data-test="key-backup-groups">
    <legend class="flex items-center gap-1 px-1 text-sm font-medium text-gray-900 dark:text-white">
      {{ t('custom.keyRouting.title') }}
      <button type="button" class="inline-flex h-4 w-4 items-center justify-center rounded-full border border-gray-400 text-[10px] text-gray-500" :disabled="disabled" :title="t('custom.keyRouting.help')" :aria-label="t('custom.keyRouting.help')">?</button>
    </legend>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.description') }}</p>

    <div class="rounded-xl border border-primary-200 bg-primary-50/40 p-3 dark:border-primary-800 dark:bg-primary-900/10" data-test="primary-group-card">
      <div class="mb-2 flex items-center justify-between gap-2">
        <span class="text-xs font-semibold uppercase tracking-wide text-primary-700 dark:text-primary-300">{{ t('custom.keyRouting.primaryLabel') }}</span>
        <div v-if="primaryGroupId !== null" class="flex items-center gap-1">
          <button type="button" :class="actionButtonClass" disabled :aria-label="t('custom.keyRouting.primaryMoveUp')" data-test="primary-up"><Icon name="arrowUp" size="sm" /></button>
          <button type="button" :class="actionButtonClass" :disabled="disabled || modelValue.length === 0" :aria-label="t('custom.keyRouting.primaryMoveDown')" data-test="primary-down" @click="movePrimaryDown"><Icon name="arrowDown" size="sm" /></button>
          <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.primaryEdit')" data-test="primary-edit" @click="openPicker('primary')"><Icon name="edit" size="sm" /></button>
          <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.primaryRemove')" data-test="primary-remove" @click="removePrimary"><Icon name="trash" size="sm" /></button>
        </div>
      </div>
      <button type="button" class="flex w-full min-w-0 rounded-md text-left focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-default" :disabled="disabled" :aria-label="t('custom.keyRouting.primaryEdit')" :data-test="primaryGroupId === null ? 'primary-select' : 'primary-group-name'" @click="openPicker('primary')">
        <GroupOptionItem v-if="primaryGroup" class="key-route-group-option w-full" :name="primaryGroup.name" :platform="primaryGroup.platform" :subscription-type="primaryGroup.subscription_type" :rate-multiplier="primaryGroup.rate_multiplier" :user-rate-multiplier="primaryGroupId === null ? null : userGroupRates[primaryGroupId]" :peak-rate-enabled="primaryGroup.peak_rate_enabled" :peak-start="primaryGroup.peak_start" :peak-end="primaryGroup.peak_end" :peak-rate-multiplier="primaryGroup.peak_rate_multiplier" :description="primaryGroup.description" :show-checkmark="false" />
        <span v-else-if="primaryGroupId !== null" class="text-sm text-amber-700 dark:text-amber-300">{{ groupName(primaryGroupId) }}</span>
        <span v-else class="text-sm text-primary-700 dark:text-primary-300">{{ t('custom.keyRouting.selectPrimary') }}</span>
      </button>
    </div>

    <ol v-if="modelValue.length" class="space-y-2" :aria-label="t('custom.keyRouting.title')">
      <li v-for="(entry, index) in selectedGroups" :key="entry.id" class="rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800/60" :data-backup-id="entry.id">
        <div class="mb-2 flex items-center justify-between gap-2">
          <span class="text-xs font-semibold uppercase tracking-wide text-gray-600 dark:text-gray-300">{{ t('custom.keyRouting.position', { position: index + 1 }) }}</span>
          <div class="flex items-center gap-1">
            <button type="button" :class="actionButtonClass" :disabled="disabled || (index === 0 && primaryGroupId === null)" :aria-label="t('custom.keyRouting.moveUp', { name: groupName(entry.id) })" data-test="backup-up" @click="move(index, -1)"><Icon name="arrowUp" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled || index === modelValue.length - 1" :aria-label="t('custom.keyRouting.moveDown', { name: groupName(entry.id) })" data-test="backup-down" @click="move(index, 1)"><Icon name="arrowDown" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.edit', { name: groupName(entry.id) })" data-test="backup-edit" @click="openPicker('backup', entry.id)"><Icon name="edit" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.remove', { name: groupName(entry.id) })" data-test="backup-remove" @click="remove(index)"><Icon name="trash" size="sm" /></button>
          </div>
        </div>
        <button type="button" class="flex w-full min-w-0 rounded-md text-left focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-default" :disabled="disabled" :aria-label="t('custom.keyRouting.edit', { name: groupName(entry.id) })" data-test="backup-group-name" @click="openPicker('backup', entry.id)">
          <GroupOptionItem v-if="entry.group" class="key-route-group-option w-full" :name="entry.group.name" :platform="entry.group.platform" :subscription-type="entry.group.subscription_type" :rate-multiplier="entry.group.rate_multiplier" :user-rate-multiplier="userGroupRates[entry.id]" :peak-rate-enabled="entry.group.peak_rate_enabled" :peak-start="entry.group.peak_start" :peak-end="entry.group.peak_end" :peak-rate-multiplier="entry.group.peak_rate_multiplier" :description="entry.group.description" :show-checkmark="false" />
          <span v-else class="text-sm text-amber-700 dark:text-amber-300">{{ groupName(entry.id) }}</span>
        </button>
      </li>
    </ol>
    <p v-else class="rounded-lg border border-dashed border-gray-200 p-3 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('custom.keyRouting.noBackups') }}</p>
    <p v-if="primaryGroupId === null" class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.choosePrimaryHint') }}</p>
    <p v-else-if="primaryGroup?.subscription_type === 'subscription'" class="text-xs text-amber-700 dark:text-amber-300" data-test="backup-subscription-hint">{{ t('custom.keyRouting.standardOnlyHint') }}</p>
    <p v-else-if="primaryGroup?.claude_code_only" class="text-xs text-amber-700 dark:text-amber-300" data-test="backup-claude-code-hint">{{ t('custom.keyRouting.claudeCodeOnlyHint') }}</p>
    <button v-if="canAdd" type="button" class="rounded-md px-1 py-1 text-xs font-medium text-primary-700 hover:bg-primary-50 disabled:opacity-50 dark:text-primary-300 dark:hover:bg-primary-900/20" :disabled="disabled" data-test="backup-add" @click="openPicker('add')">{{ t('custom.keyRouting.addBackup', { count: modelValue.length }) }}</button>
    <p v-if="modelValue.length >= MAX_BACKUP_GROUPS" class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.limitReached') }}</p>
    <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400" data-test="backup-error">{{ t(`custom.keyRouting.${error}`) }}</p>
    <p class="rounded-lg bg-amber-50 p-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('custom.keyRouting.billing') }}</p>
    <KeyGroupPicker :show="picker !== null" :title="pickerTitle" :groups="pickerGroups" :selected-group-id="pickerSelectedId" :user-group-rates="userGroupRates" :disabled="disabled" :swap-hint="picker?.kind !== 'add' && modelValue.length > 0" @select="selectGroup" @close="picker = null" />
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyGroupPicker from './KeyGroupPicker.vue'
import type { Group } from '@/types'
import { availableBackupGroups, MAX_BACKUP_GROUPS, moveBackupGroup, validateKeyRouting } from './keyRouting'

const props = withDefaults(defineProps<{
  modelValue: number[]
  primaryGroupId: number | null
  groups: Group[]
  userGroupRates?: Record<number, number>
  disabled?: boolean
}>(), { userGroupRates: () => ({}), disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: number[]]; 'update:primaryGroupId': [value: number | null] }>()
const { t } = useI18n()
const actionButtonClass = 'inline-flex h-6 w-6 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-default disabled:opacity-30 dark:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-gray-200'
type PickerTarget = { kind: 'primary' } | { kind: 'add' } | { kind: 'backup'; groupId: number }
const picker = ref<PickerTarget | null>(null)
const primaryGroup = computed(() => props.groups.find((group) => group.id === props.primaryGroupId))
const selectedGroups = computed(() => props.modelValue.map((id) => ({ id, group: props.groups.find((group) => group.id === id) })))
const error = computed(() => validateKeyRouting(props.primaryGroupId, props.modelValue, props.groups))
const routable = (group: Group) => group.status === 'active' && group.subscription_type === 'standard' && !group.claude_code_only
const canAdd = computed(() => primaryGroup.value !== undefined && routable(primaryGroup.value) && props.modelValue.length < MAX_BACKUP_GROUPS)
const groupName = (id: number) => props.groups.find((group) => group.id === id)?.name ?? t('custom.keyRouting.unavailable', { id })
const pickerSelectedId = computed(() => picker.value?.kind === 'primary' ? props.primaryGroupId : picker.value?.kind === 'backup' ? picker.value.groupId : null)
const pickerTitle = computed(() => t(picker.value?.kind === 'primary' ? 'custom.keyRouting.choosePrimary' : picker.value?.kind === 'backup' ? 'custom.keyRouting.replace' : 'custom.keyRouting.select'))
const pickerGroups = computed(() => {
  if (picker.value?.kind === 'add') return availableBackupGroups(props.groups, props.primaryGroupId, props.modelValue)
  const isMulti = props.modelValue.length > 0 || picker.value?.kind === 'backup'
  const current = props.groups.find((group) => group.id === pickerSelectedId.value)
  // Invalid selections stay visible until explicitly repaired. Do not swap
  // an unavailable selection into another slot when replacing it.
  const maySwap = current !== undefined && (!isMulti ? current.status === 'active' : routable(current))
  const selected = new Set([props.primaryGroupId, ...props.modelValue])
  return props.groups.filter((group) => group.status === 'active' && (!isMulti || routable(group)) && (maySwap || !selected.has(group.id) || group.id === pickerSelectedId.value))
})

function openPicker(kind: 'primary' | 'backup' | 'add', groupId?: number) {
  if (props.disabled || (kind === 'add' && !canAdd.value)) return
  if (kind === 'backup') {
    if (groupId === undefined || !props.modelValue.includes(groupId)) return
    picker.value = { kind, groupId }
  } else picker.value = { kind }
}

function selectGroup(groupId: number) {
  if (props.disabled || !picker.value || !pickerGroups.value.some((group) => group.id === groupId)) return
  const target = picker.value
  if (target.kind === 'add') {
    if (!canAdd.value) return
    emit('update:modelValue', [...props.modelValue, groupId])
  } else {
    const slot = target.kind === 'primary' ? 0 : props.modelValue.indexOf(target.groupId) + 1
    if (target.kind === 'backup' && slot === 0) return
    const route = [props.primaryGroupId, ...props.modelValue]
    const existing = route.indexOf(groupId)
    if (existing !== -1) [route[slot], route[existing]] = [route[existing], route[slot]]
    else route[slot] = groupId
    if (route[0] !== props.primaryGroupId) emit('update:primaryGroupId', route[0])
    const backups = route.slice(1) as number[]
    if (backups.some((id, index) => id !== props.modelValue[index])) emit('update:modelValue', backups)
  }
  picker.value = null
}

function remove(index: number) {
  if (props.disabled) return
  picker.value = null
  emit('update:modelValue', props.modelValue.filter((_, current) => current !== index))
}

function movePrimaryDown() {
  if (props.disabled || props.primaryGroupId === null || props.modelValue.length === 0) return
  picker.value = null
  const [nextPrimary, ...remaining] = props.modelValue
  emit('update:primaryGroupId', nextPrimary)
  emit('update:modelValue', [props.primaryGroupId, ...remaining])
}

function removePrimary() {
  if (props.disabled || props.primaryGroupId === null) return
  picker.value = null
  if (props.modelValue.length) {
    const [nextPrimary, ...remaining] = props.modelValue
    emit('update:primaryGroupId', nextPrimary)
    emit('update:modelValue', remaining)
  } else emit('update:primaryGroupId', null)
}

function move(index: number, direction: -1 | 1) {
  if (props.disabled) return
  if (index === 0 && direction === -1) return movePrimaryDown()
  picker.value = null
  emit('update:modelValue', moveBackupGroup(props.modelValue, index, direction))
}
</script>

<style scoped>
.key-route-group-option :deep(.groupOptionItemBadge) {
  max-width: 100%;
}

.key-route-group-option :deep(.groupOptionItemBadge .truncate) {
  overflow: visible;
  text-overflow: clip;
  white-space: normal;
  overflow-wrap: anywhere;
}
</style>
