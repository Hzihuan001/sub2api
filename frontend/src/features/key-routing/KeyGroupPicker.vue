<template>
  <BaseDialog :show="show" :title="title" width="normal" :z-index="75" @close="emit('close')">
    <div class="space-y-3" data-test="key-group-picker">
      <input ref="searchInput" v-model="search" type="search" class="input w-full" :placeholder="t('custom.keyRouting.pickerSearch')" :aria-label="t('custom.keyRouting.pickerSearch')" data-test="key-group-picker-search" />
      <p v-if="swapHint" class="text-xs leading-5 text-gray-500 dark:text-gray-400">{{ t('custom.keyRouting.pickerSwapHint') }}</p>
      <div v-if="filteredGroups.length" class="max-h-[55vh] space-y-2 overflow-y-auto" role="listbox" :aria-label="title">
        <button v-for="group in filteredGroups" :key="group.id" type="button" role="option" :aria-selected="group.id === selectedGroupId" :disabled="disabled" class="flex w-full rounded-xl border p-3 text-left transition-colors disabled:opacity-50" :class="group.id === selectedGroupId ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-900/20' : 'border-gray-200 hover:border-primary-300 hover:bg-primary-50/30 dark:border-dark-600 dark:hover:border-primary-700 dark:hover:bg-primary-900/10'" :data-group-id="group.id" @click="select(group.id)">
          <GroupOptionItem :name="group.name" :platform="group.platform" :subscription-type="group.subscription_type" :rate-multiplier="group.rate_multiplier" :user-rate-multiplier="userGroupRates[group.id]" :peak-rate-enabled="group.peak_rate_enabled" :peak-start="group.peak_start" :peak-end="group.peak_end" :peak-rate-multiplier="group.peak_rate_multiplier" :description="group.description" :selected="group.id === selectedGroupId" />
        </button>
      </div>
      <p v-else class="rounded-xl border border-dashed border-gray-200 p-5 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400" data-test="key-group-picker-empty">{{ t(search.trim() ? 'custom.keyRouting.pickerNoResults' : 'custom.keyRouting.emptyOptions') }}</p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import type { Group } from '@/types'

const props = withDefaults(defineProps<{
  show: boolean
  title: string
  groups: Group[]
  selectedGroupId?: number | null
  userGroupRates?: Record<number, number>
  disabled?: boolean
  swapHint?: boolean
}>(), { selectedGroupId: null, userGroupRates: () => ({}), disabled: false, swapHint: false })
const emit = defineEmits<{ select: [groupId: number]; close: [] }>()
const { t } = useI18n()
const search = ref('')
const searchInput = ref<HTMLInputElement | null>(null)
const filteredGroups = computed(() => {
  const query = search.value.trim().toLocaleLowerCase()
  if (!query) return props.groups
  return props.groups.filter((group) => [group.name, group.description, group.platform].some((value) => value?.toLocaleLowerCase().includes(query)))
})

watch(() => props.show, async (show) => {
  if (!show) return
  search.value = ''
  await nextTick()
  // Run after the host dialog's own initial-focus callback as well.
  await nextTick()
  if (props.show) searchInput.value?.focus()
}, { immediate: true, flush: 'post' })

function select(groupId: number) {
  if (!props.show || props.disabled || !props.groups.some((group) => group.id === groupId)) return
  emit('select', groupId)
}
</script>
