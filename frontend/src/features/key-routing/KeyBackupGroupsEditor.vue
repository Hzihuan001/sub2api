<template>
  <fieldset class="space-y-3 rounded-xl border border-gray-200 p-3 dark:border-dark-600" :disabled="disabled" data-test="key-backup-groups">
    <legend class="px-1 text-sm font-medium text-gray-900 dark:text-white">{{ t('custom.keyRouting.title') }}</legend>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.description') }}</p>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.scope') }}</p>
    <ol v-if="modelValue.length" class="space-y-2" :aria-label="t('custom.keyRouting.title')">
      <li v-for="(entry, index) in selectedGroups" :key="`${entry.id}-${index}`" class="flex flex-wrap items-center gap-2 rounded-lg bg-gray-50 p-2 dark:bg-dark-800" :data-backup-id="entry.id">
        <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.position', { position: index + 1 }) }}</span>
        <div class="min-w-0 flex-1">
          <GroupBadge v-if="entry.group" :name="entry.group.name" :platform="entry.group.platform" :subscription-type="entry.group.subscription_type" :rate-multiplier="entry.group.rate_multiplier" :user-rate-multiplier="userGroupRates[entry.id]" :peak-rate-enabled="entry.group.peak_rate_enabled" :peak-start="entry.group.peak_start" :peak-end="entry.group.peak_end" :peak-rate-multiplier="entry.group.peak_rate_multiplier" />
          <span v-else class="text-sm text-amber-700 dark:text-amber-300">{{ groupName(entry.id) }}</span>
        </div>
        <div class="flex items-center gap-1">
          <button type="button" class="btn btn-secondary btn-sm px-2" :disabled="disabled || index === 0" :aria-label="t('custom.keyRouting.moveUp', { name: groupName(entry.id) })" data-test="backup-up" @click="move(index, -1)"><Icon name="arrowUp" size="sm" /></button>
          <button type="button" class="btn btn-secondary btn-sm px-2" :disabled="disabled || index === modelValue.length - 1" :aria-label="t('custom.keyRouting.moveDown', { name: groupName(entry.id) })" data-test="backup-down" @click="move(index, 1)"><Icon name="arrowDown" size="sm" /></button>
          <button type="button" class="btn btn-secondary btn-sm px-2" :disabled="disabled" :aria-label="t('custom.keyRouting.remove', { name: groupName(entry.id) })" data-test="backup-remove" @click="remove(index)"><Icon name="trash" size="sm" /></button>
        </div>
      </li>
    </ol>
    <p v-else class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.noBackups') }}</p>
    <p v-if="primaryGroupId === null" class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.choosePrimary') }}</p>
    <p v-else-if="primaryGroup?.subscription_type === 'subscription'" class="text-xs text-amber-700 dark:text-amber-300" data-test="backup-subscription-hint">{{ t('custom.keyRouting.standardOnlyHint') }}</p>
    <p v-else-if="primaryGroup?.claude_code_only" class="text-xs text-amber-700 dark:text-amber-300" data-test="backup-claude-code-hint">{{ t('custom.keyRouting.claudeCodeOnlyHint') }}</p>
    <Select v-if="canAdd" :model-value="null" :options="options" :disabled="disabled" :placeholder="t('custom.keyRouting.select')" :aria-label="t('custom.keyRouting.select')" :empty-text="t('custom.keyRouting.emptyOptions')" searchable data-test="backup-select" @update:model-value="add">
      <template #option="{ option, selected }">
        <GroupOptionItem :name="option.label" :platform="option.group.platform" :subscription-type="option.group.subscription_type" :rate-multiplier="option.group.rate_multiplier" :user-rate-multiplier="userGroupRates[option.value]" :peak-rate-enabled="option.group.peak_rate_enabled" :peak-start="option.group.peak_start" :peak-end="option.group.peak_end" :peak-rate-multiplier="option.group.peak_rate_multiplier" :description="option.group.description" :selected="selected" />
      </template>
    </Select>
    <p v-if="modelValue.length >= MAX_BACKUP_GROUPS" class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.limitReached') }}</p>
    <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400" data-test="backup-error">{{ t(`custom.keyRouting.${error}`) }}</p>
    <p class="rounded-lg bg-amber-50 p-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('custom.keyRouting.billing') }}</p>
  </fieldset>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Group } from '@/types'
import { availableBackupGroups, MAX_BACKUP_GROUPS, moveBackupGroup, validateKeyRouting } from './keyRouting'

const props = withDefaults(defineProps<{
  modelValue: number[]
  primaryGroupId: number | null
  groups: Group[]
  userGroupRates?: Record<number, number>
  disabled?: boolean
}>(), { userGroupRates: () => ({}), disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()
const { t } = useI18n()
const primaryGroup = computed(() => props.groups.find((group) => group.id === props.primaryGroupId))
const selectedGroups = computed(() => props.modelValue.map((id) => ({ id, group: props.groups.find((group) => group.id === id) })))
const error = computed(() => validateKeyRouting(props.primaryGroupId, props.modelValue, props.groups))
const canAdd = computed(() => primaryGroup.value?.status === 'active' && primaryGroup.value.subscription_type === 'standard' && !primaryGroup.value.claude_code_only && props.modelValue.length < MAX_BACKUP_GROUPS)
const options = computed(() => availableBackupGroups(props.groups, props.primaryGroupId, props.modelValue).map((group) => ({ value: group.id, label: group.name, description: group.description, group })))
const groupName = (id: number) => props.groups.find((group) => group.id === id)?.name ?? t('custom.keyRouting.unavailable', { id })

function add(value: string | number | boolean | null) {
  if (props.disabled || !canAdd.value || typeof value !== 'number' || !options.value.some((option) => option.value === value)) return
  emit('update:modelValue', [...props.modelValue, value])
}

function remove(index: number) {
  if (!props.disabled) emit('update:modelValue', props.modelValue.filter((_, current) => current !== index))
}

function move(index: number, direction: -1 | 1) {
  if (!props.disabled) emit('update:modelValue', moveBackupGroup(props.modelValue, index, direction))
}
</script>
