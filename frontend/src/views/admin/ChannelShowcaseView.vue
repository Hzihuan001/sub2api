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
          <div class="flex gap-2">
            <button class="btn btn-secondary" :disabled="loading" type="button" @click="load">
              {{ t('common.refresh') }}
            </button>
            <button class="btn btn-primary" type="button" @click="openCreate">
              {{ t('admin.channelShowcase.addPlan') }}
            </button>
          </div>
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
          <button class="btn btn-primary mt-4" type="button" @click="openCreate">
            {{ t('admin.channelShowcase.addPlan') }}
          </button>
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
                  <button class="btn btn-secondary btn-sm" type="button" @click="openForAccount(row.accountId)">
                    {{ t('admin.channelShowcase.edit') }}
                  </button>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
    <BaseDialog :show="accountPickerOpen" :title="t('admin.channelShowcase.addPlan')" width="narrow" @close="accountPickerOpen = false">
      <div class="space-y-4">
        <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.channelShowcase.formHint') }}</p>
        <select v-model="accountPickerId" class="input w-full">
          <option :value="null">{{ t('admin.channelShowcase.selectAccount') }}</option>
          <option v-for="account in accounts" :key="account.id" :value="account.id">{{ account.name }} · {{ account.platform }}</option>
        </select>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" type="button" @click="accountPickerOpen = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" type="button" :disabled="!accountPickerId" @click="beginForAccount">{{ t('admin.channelShowcase.configure') }}</button>
        </div>
      </div>
    </BaseDialog>
    <ScheduledTestsPanel :show="scheduleAccountId !== null" :account-id="scheduleAccountId" :model-options="scheduleModelOptions" :group-options="scheduleGroupOptions" @close="closeSchedulePanel" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ScheduledTestsPanel from '@/components/admin/account/ScheduledTestsPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { SelectOption } from '@/components/common/Select.vue'
import type { AccountListItem, AdminGroup, ScheduledTestPlan } from '@/types'

const { t } = useI18n()
const loading = ref(false)
const error = ref('')
const rows = ref<Array<{
  key: string
  planId: number
  accountId: number
  accountName: string
  platform: string
  groupName: string
  model: string
  cron: string
  enabled: boolean
}>>([])
const accountCount = ref(0)
const accounts = ref<AccountListItem[]>([])
const groups = ref<AdminGroup[]>([])
const accountPickerOpen = ref(false)
const accountPickerId = ref<number | null>(null)
const scheduleAccountId = ref<number | null>(null)
const scheduleModelOptions = ref<SelectOption[]>([])

const enabledCount = computed(() => rows.value.filter(row => row.enabled).length)

function groupNameMap(groups: AdminGroup[]) {
  return new Map(groups.map(group => [group.id, group.name]))
}

const scheduleGroupOptions = computed(() => {
  const account = accounts.value.find(item => item.id === scheduleAccountId.value)
  return (account?.group_ids || []).map(id => groups.value.find(group => group.id === id)).filter((group): group is AdminGroup => Boolean(group))
})

async function loadScheduleModels(accountId: number) {
  try {
    const models = await adminAPI.accounts.getAvailableModels(accountId)
    scheduleModelOptions.value = models.map(model => ({ value: model.id, label: model.display_name || model.id }))
  } catch {
    scheduleModelOptions.value = []
  }
}

function openCreate() {
  accountPickerId.value = accounts.value[0]?.id ?? null
  accountPickerOpen.value = true
}

async function beginForAccount() {
  if (!accountPickerId.value) return
  const accountId = accountPickerId.value
  accountPickerOpen.value = false
  scheduleAccountId.value = accountId
  scheduleModelOptions.value = []
  await loadScheduleModels(accountId)
}

async function openForAccount(accountId: number) {
  scheduleAccountId.value = accountId
  scheduleModelOptions.value = []
  await loadScheduleModels(accountId)
}

function closeSchedulePanel() {
  scheduleAccountId.value = null
  scheduleModelOptions.value = []
  void load()
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [accountResponse, allGroups] = await Promise.all([
      adminAPI.accounts.list(1, 1000, { lite: '1', sort_by: 'name', sort_order: 'asc' }),
      adminAPI.groups.getAllIncludingInactive(),
    ])
    accounts.value = accountResponse.items || []
    groups.value = allGroups
    accountCount.value = accounts.value.length
    const names = groupNameMap(allGroups)
    const next: typeof rows.value = []
    for (let offset = 0; offset < accounts.value.length; offset += 20) {
      const batch = accounts.value.slice(offset, offset + 20)
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
            planId: plan.id,
            accountId: account.id,
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
