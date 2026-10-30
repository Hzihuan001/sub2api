<template>
  <AppLayout>
    <div class="w-full min-w-0 space-y-6 pb-8">
      <header class="page-header rounded-3xl bg-white p-5 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h1 class="page-title flex items-center gap-2 text-xl font-black text-gray-900 dark:text-white">
              <span class="inline-flex h-8 w-8 items-center justify-center rounded-xl bg-blue-50 text-blue-500 dark:bg-blue-900/30 dark:text-blue-400">
                <Icon name="chart" size="sm" />
              </span>
              {{ t('admin.channelShowcase.title') }}
            </h1>
            <p class="page-description mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.channelShowcase.description') }}
            </p>
          </div>
          <button class="btn btn-secondary" :disabled="loading" type="button" @click="load">
            {{ t('common.refresh') }}
          </button>
        </div>
        <div class="mt-4 grid gap-3 sm:grid-cols-3">
          <div class="rounded-2xl bg-gray-50 px-4 py-3 dark:bg-dark-700/60">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelShowcase.accounts') }}</div>
            <div class="mt-1 text-lg font-bold text-gray-900 dark:text-white">{{ accountCount }}</div>
          </div>
          <div class="rounded-2xl bg-gray-50 px-4 py-3 dark:bg-dark-700/60">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelShowcase.plans') }}</div>
            <div class="mt-1 text-lg font-bold text-gray-900 dark:text-white">{{ rows.length }}</div>
          </div>
          <div class="rounded-2xl bg-gray-50 px-4 py-3 dark:bg-dark-700/60">
            <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.channelShowcase.enabledPlans') }}</div>
            <div class="mt-1 text-lg font-bold text-emerald-600 dark:text-emerald-400">{{ enabledCount }}</div>
          </div>
        </div>
      </header>

      <section class="rounded-3xl bg-white p-4 shadow-sm ring-1 ring-gray-900/5 dark:bg-dark-800 dark:ring-dark-700 sm:p-6">
        <div v-if="error" class="mb-4 rounded-2xl border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {{ error }}
        </div>
        <div v-if="loading" class="py-12 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="rows.length === 0" class="rounded-2xl border border-dashed border-gray-200 px-6 py-12 text-center dark:border-dark-600">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.channelShowcase.empty') }}</p>
          <router-link to="/admin/accounts" class="btn btn-primary mt-4 inline-flex">
            {{ t('admin.channelShowcase.goAccounts') }}
          </router-link>
        </div>
        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[760px] text-left text-sm">
            <thead class="border-b border-gray-100 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
              <tr>
                <th class="px-3 py-3 font-medium">{{ t('admin.channelShowcase.account') }}</th>
                <th class="px-3 py-3 font-medium">{{ t('admin.channelShowcase.group') }}</th>
                <th class="px-3 py-3 font-medium">{{ t('admin.channelShowcase.model') }}</th>
                <th class="px-3 py-3 font-medium">{{ t('admin.channelShowcase.schedule') }}</th>
                <th class="px-3 py-3 font-medium">{{ t('admin.channelShowcase.status') }}</th>
                <th class="px-3 py-3 text-right font-medium">{{ t('admin.channelShowcase.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr v-for="row in rows" :key="row.key" class="text-gray-700 dark:text-gray-200">
                <td class="px-3 py-3">
                  <div class="font-medium text-gray-900 dark:text-white">{{ row.accountName }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">{{ row.platform }}</div>
                </td>
                <td class="px-3 py-3">{{ row.groupName || '—' }}</td>
                <td class="px-3 py-3 font-mono text-xs">{{ row.model || '—' }}</td>
                <td class="px-3 py-3 font-mono text-xs">{{ row.cron || '—' }}</td>
                <td class="px-3 py-3">
                  <span :class="row.enabled ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-400'">
                    {{ row.enabled ? t('admin.channelShowcase.enabled') : t('admin.channelShowcase.disabled') }}
                  </span>
                </td>
                <td class="px-3 py-3 text-right">
                  <router-link to="/admin/accounts" class="btn btn-secondary btn-sm">
                    {{ t('admin.channelShowcase.editInAccounts') }}
                  </router-link>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { AccountListItem, AdminGroup, ScheduledTestPlan } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const rows = ref<Array<{
  key: string
  accountName: string
  platform: string
  groupName: string
  model: string
  cron: string
  enabled: boolean
}>>([])
const accountCount = ref(0)

const enabledCount = computed(() => rows.value.filter(row => row.enabled).length)

function groupNameMap(groups: AdminGroup[]) {
  return new Map(groups.map(group => [group.id, group.name]))
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [accountResponse, groups] = await Promise.all([
      adminAPI.accounts.list(1, 1000, { lite: '1', sort_by: 'name', sort_order: 'asc' }),
      adminAPI.groups.getAllIncludingInactive(),
    ])
    const accounts = accountResponse.items || []
    accountCount.value = accounts.length
    const names = groupNameMap(groups)
    const next: typeof rows.value = []
    for (let offset = 0; offset < accounts.length; offset += 20) {
      const batch = accounts.slice(offset, offset + 20)
      const batchResults = await Promise.all(batch.map(async (account: AccountListItem) => {
        try {
          return { account, plans: await adminAPI.scheduledTests.listByAccount(account.id) }
        } catch {
          return { account, plans: [] as ScheduledTestPlan[] }
        }
      }))
      for (const { account, plans: planList } of batchResults) {
        for (const plan of planList.filter(item => item.test_kind === 'pelican')) {
          next.push({
            key: `${account.id}-${plan.id}`,
            accountName: account.name,
            platform: account.platform,
            groupName: plan.group_id ? (names.get(plan.group_id) || `#${plan.group_id}`) : '—',
            model: plan.model_id,
            cron: plan.cron_expression,
            enabled: plan.enabled,
          })
        }
      }
    }
    rows.value = next
  } catch (cause) {
    error.value = cause instanceof Error ? cause.message : t('admin.channelShowcase.loadFailed')
    rows.value = []
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
