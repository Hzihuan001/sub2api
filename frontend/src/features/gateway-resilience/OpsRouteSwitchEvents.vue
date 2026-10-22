<template>
  <section v-if="events.length" class="rounded-xl bg-gray-50 p-6 dark:bg-dark-900" data-testid="route-switch-events">
    <h3 class="text-sm font-black uppercase tracking-wider text-gray-900 dark:text-white">
      {{ t('admin.ops.errorDetail.payloads.upstream_events') }}
    </h3>
    <div class="mt-4 space-y-3">
      <div
        v-for="event in events"
        :key="event.eventIndex"
        class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
        data-testid="route-switch-event"
      >
        <div class="flex flex-wrap items-center gap-2">
          <span class="text-xs font-black text-gray-900 dark:text-white">#{{ event.eventIndex + 1 }}</span>
          <span class="inline-flex max-w-full items-center break-words rounded bg-teal-100 px-1.5 py-0.5 text-xs font-medium text-teal-800 dark:bg-teal-900/40 dark:text-teal-200">
            {{ t('admin.ops.errorLog.routeSwitchToBackup', { group: event.groupName }) }}
          </span>
        </div>
        <div class="mt-3 grid grid-cols-1 gap-2 text-xs text-gray-600 dark:text-gray-300 sm:grid-cols-2">
          <div>
            <span class="text-gray-400">{{ t('admin.ops.errorDetail.upstreamEvent.status') }}:</span>
            <span class="ml-1 font-mono">{{ event.statusCode ?? '—' }}</span>
          </div>
          <div class="break-words">
            <span class="text-gray-400">{{ t('admin.ops.errorDetail.upstreamEvent.account') }}:</span>
            <span class="ml-1">{{ event.accountName || '—' }}</span>
          </div>
        </div>
        <div class="mt-3 whitespace-pre-wrap break-words text-sm font-medium text-gray-900 dark:text-white" data-testid="route-switch-event-message">{{ event.message || '—' }}</div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { parseOpsRouteSwitchEvents } from './opsRouteSwitchEvents'

const props = defineProps<{ upstreamErrors?: unknown }>()
const { t } = useI18n()
const events = computed(() => parseOpsRouteSwitchEvents(props.upstreamErrors))
</script>
