<template>
  <section class="border-t border-gray-200 pt-3 dark:border-dark-600" data-test="key-advanced-settings" @invalid.capture="revealInvalidControl">
    <button
      type="button"
      class="flex w-full items-center justify-between gap-3 rounded-md py-1 text-left text-xs text-gray-600 transition-colors hover:text-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500 disabled:cursor-default disabled:opacity-60 dark:text-gray-300 dark:hover:text-primary-300"
      :aria-expanded="expanded"
      :aria-controls="contentId"
      :disabled="disabled"
      data-test="key-advanced-settings-toggle"
      @click="expanded = !expanded"
    >
      <span class="flex flex-shrink-0 items-center gap-1.5">
        <Icon name="chevronRight" size="xs" class="transition-transform" :class="expanded && 'rotate-90'" aria-hidden="true" />
        <span>{{ t('custom.keyRouting.advancedSettings') }}</span>
      </span>
      <span v-if="summary" class="min-w-0 truncate text-right text-[11px] text-gray-400 dark:text-gray-500" :title="summary" data-test="key-advanced-settings-summary">{{ summary }}</span>
    </button>
    <!-- Keep the original form controls mounted, including their local drafts. -->
    <div v-show="expanded" :id="contentId" class="space-y-5 pt-4" data-test="key-advanced-settings-content">
      <slot />
    </div>
  </section>
</template>

<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

withDefaults(defineProps<{ summary?: string; disabled?: boolean }>(), { summary: '', disabled: false })

const { t } = useI18n()
const contentId = useId()
const expanded = ref(false)
let pendingInvalidControl: HTMLElement | null = null

async function revealInvalidControl(event: Event) {
  const target = event.target
  if (!(target instanceof HTMLElement) || (expanded.value && pendingInvalidControl === null)) return
  // Native validation still fails. Only defer its hidden-control focus until
  // the slot is visible, keeping the first invalid control for this pass.
  event.preventDefault()
  if (pendingInvalidControl) return
  pendingInvalidControl = target
  expanded.value = true
  await nextTick()
  const control = pendingInvalidControl
  pendingInvalidControl = null
  if (control?.isConnected) control.focus()
}
</script>
