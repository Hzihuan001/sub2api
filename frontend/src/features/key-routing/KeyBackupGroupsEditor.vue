<template>
  <fieldset class="min-w-0 space-y-3" :disabled="disabled" data-test="key-route-groups">
    <legend class="flex items-center gap-1 px-1 text-sm font-medium text-gray-900 dark:text-white">
      {{ t('custom.keyRouting.title') }}
      <button type="button" class="inline-flex h-4 w-4 items-center justify-center rounded-full border border-gray-400 text-[10px] text-gray-500" :disabled="disabled" :title="t('custom.keyRouting.help')" :aria-label="t('custom.keyRouting.help')">?</button>
    </legend>
    <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.description') }}</p>

    <ol v-if="modelValue.length" class="space-y-2" :aria-label="t('custom.keyRouting.title')" data-test="key-route-priority-list">
      <li v-for="(entry, index) in selectedGroups" :key="`${entry.id}-${index}`" class="rounded-xl border border-gray-200 bg-white p-3 dark:border-dark-600 dark:bg-dark-800/60" :data-route-group-id="entry.id">
        <div class="mb-2 flex items-center justify-between gap-2">
          <span class="text-xs font-semibold uppercase tracking-wide text-gray-600 dark:text-gray-300">{{ t('custom.keyRouting.priority', { position: index + 1 }) }}</span>
          <div class="flex items-center gap-1">
            <button type="button" :class="actionButtonClass" :disabled="disabled || index === 0" :aria-label="t('custom.keyRouting.moveUp', { name: groupName(entry.id) })" :data-test="`route-up-${index}`" @click="move(index, -1)"><Icon name="arrowUp" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled || index === modelValue.length - 1" :aria-label="t('custom.keyRouting.moveDown', { name: groupName(entry.id) })" :data-test="`route-down-${index}`" @click="move(index, 1)"><Icon name="arrowDown" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.edit', { name: groupName(entry.id) })" :data-test="`route-edit-${index}`" @click="openPicker(index)"><Icon name="edit" size="sm" /></button>
            <button type="button" :class="actionButtonClass" :disabled="disabled" :aria-label="t('custom.keyRouting.remove', { name: groupName(entry.id) })" :data-test="`route-remove-${index}`" @click="remove(index)"><Icon name="trash" size="sm" /></button>
          </div>
        </div>
        <button type="button" class="flex w-full min-w-0 rounded-md text-left focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-default" :disabled="disabled" :aria-label="t('custom.keyRouting.edit', { name: groupName(entry.id) })" :data-test="`route-group-name-${index}`" @click="openPicker(index)">
          <GroupOptionItem v-if="entry.group" class="key-route-group-option w-full" :name="entry.group.name" :platform="entry.group.platform" :subscription-type="entry.group.subscription_type" :rate-multiplier="entry.group.rate_multiplier" :user-rate-multiplier="userGroupRates[entry.id]" :peak-rate-enabled="entry.group.peak_rate_enabled" :peak-start="entry.group.peak_start" :peak-end="entry.group.peak_end" :peak-rate-multiplier="entry.group.peak_rate_multiplier" :description="entry.group.description" :show-checkmark="false" />
          <span v-else class="text-sm text-amber-700 dark:text-amber-300">{{ groupName(entry.id) }}</span>
        </button>
      </li>
    </ol>
    <p v-else class="rounded-lg border border-dashed border-gray-200 p-3 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t('custom.keyRouting.noGroups') }}</p>

    <p v-if="modelValue.length === 1 && selectedGroups[0]?.group?.subscription_type === 'subscription'" class="text-xs text-amber-700 dark:text-amber-300" data-test="route-subscription-hint">{{ t('custom.keyRouting.standardOnlyHint') }}</p>
    <p v-else-if="modelValue.length === 1 && selectedGroups[0]?.group?.claude_code_only" class="text-xs text-amber-700 dark:text-amber-300" data-test="route-claude-code-hint">{{ t('custom.keyRouting.claudeCodeOnlyHint') }}</p>
    <button v-if="canAdd" type="button" class="rounded-md px-1 py-1 text-xs font-medium text-primary-700 hover:bg-primary-50 disabled:opacity-50 dark:text-primary-300 dark:hover:bg-primary-900/20" :disabled="disabled" data-test="route-add" @click="openPicker(null)">{{ t('custom.keyRouting.addGroup', { count: modelValue.length, limit: MAX_ROUTE_GROUPS }) }}</button>
    <p v-if="modelValue.length >= MAX_ROUTE_GROUPS" class="text-xs text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.limitReached', { limit: MAX_ROUTE_GROUPS }) }}</p>
    <p v-if="error" role="alert" class="text-xs text-red-600 dark:text-red-400" data-test="route-error">{{ t(`custom.keyRouting.${error}`) }}</p>
    <p class="rounded-lg bg-amber-50 p-2 text-xs leading-5 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ t('custom.keyRouting.billing') }}</p>
    <KeyGroupPicker :show="pickerIndex !== undefined" :title="pickerTitle" :groups="pickerGroups" :selected-group-id="pickerSelectedId" :user-group-rates="userGroupRates" :disabled="disabled" :swap-hint="pickerIndex !== null && canSwapInPicker" @select="selectGroup" @close="pickerIndex = undefined" />
  </fieldset>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Icon from '@/components/icons/Icon.vue'
import KeyGroupPicker from './KeyGroupPicker.vue'
import type { Group } from '@/types'
import { availableKeyRouteGroups, isKeyRouteGroupEligible, MAX_ROUTE_GROUPS, moveRouteGroup, validateOrderedKeyRouting } from './keyRouting'

const props = withDefaults(defineProps<{
  modelValue: number[]
  groups: Group[]
  userGroupRates?: Record<number, number>
  disabled?: boolean
}>(), { userGroupRates: () => ({}), disabled: false })
const emit = defineEmits<{ 'update:modelValue': [value: number[]] }>()
const { t } = useI18n()
const actionButtonClass = 'inline-flex h-6 w-6 items-center justify-center rounded-md text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 disabled:cursor-default disabled:opacity-30 dark:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-gray-200'
const pickerIndex = ref<number | null | undefined>(undefined)
const selectedGroups = computed(() => props.modelValue.map((id) => ({ id, group: props.groups.find((group) => group.id === id) })))
const error = computed(() => validateOrderedKeyRouting(props.modelValue, props.groups))
const canAdd = computed(() => props.modelValue.length < MAX_ROUTE_GROUPS && (props.modelValue.length === 0 || props.modelValue.every((id) => isKeyRouteGroupEligible(props.groups.find((group) => group.id === id)))))
const groupName = (id: number) => props.groups.find((group) => group.id === id)?.name ?? t('custom.keyRouting.unavailable', { id })
const pickerSelectedId = computed(() => pickerIndex.value === null || pickerIndex.value === undefined ? null : props.modelValue[pickerIndex.value] ?? null)
const pickerTitle = computed(() => pickerIndex.value === null ? t('custom.keyRouting.selectGroup') : t('custom.keyRouting.replaceGroup'))
const canSwapInPicker = computed(() => {
  const current = pickerSelectedId.value === null ? undefined : props.groups.find((group) => group.id === pickerSelectedId.value)
  return current !== undefined && (props.modelValue.length === 1 || isKeyRouteGroupEligible(current))
})
const pickerGroups = computed(() => availableKeyRouteGroups(props.groups, props.modelValue, pickerIndex.value ?? null))

function openPicker(index: number | null) {
  if (props.disabled || (index === null && !canAdd.value)) return
  if (index !== null && (index < 0 || index >= props.modelValue.length)) return
  pickerIndex.value = index
}

function selectGroup(groupId: number) {
  if (props.disabled || pickerIndex.value === undefined || !pickerGroups.value.some((group) => group.id === groupId)) return
  const route = [...props.modelValue]
  if (pickerIndex.value === null) route.push(groupId)
  else {
    const target = pickerIndex.value
    const existing = route.indexOf(groupId)
    if (existing === target) {
      pickerIndex.value = undefined
      return
    }
    if (existing >= 0) [route[target], route[existing]] = [route[existing], route[target]]
    else route[target] = groupId
  }
  if (route.some((id, index) => id !== props.modelValue[index]) || route.length !== props.modelValue.length) emit('update:modelValue', route)
  pickerIndex.value = undefined
}

function remove(index: number) {
  if (props.disabled || index < 0 || index >= props.modelValue.length) return
  pickerIndex.value = undefined
  emit('update:modelValue', props.modelValue.filter((_, current) => current !== index))
}

function move(index: number, direction: -1 | 1) {
  if (props.disabled) return
  pickerIndex.value = undefined
  emit('update:modelValue', moveRouteGroup(props.modelValue, index, direction))
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
