<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.title') }}</h1>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="loadData">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-16">
        <LoadingSpinner />
      </div>

      <div v-else-if="accounts.length === 0" class="card p-8 text-center">
        <p class="font-medium text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.emptyTitle') }}</p>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.emptyBody') }}</p>
      </div>

      <div v-else class="grid gap-4 xl:grid-cols-2">
        <article v-for="account in accounts" :key="account.id" class="card p-5">
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2 class="truncate text-lg font-semibold text-gray-900 dark:text-white">{{ accountLabel(account) }}</h2>
                <span :class="statusClass(account.status)" class="rounded-full px-2 py-0.5 text-xs font-medium">
                  {{ statusLabel(account.status) }}
                </span>
              </div>
            </div>
            <button class="btn btn-secondary shrink-0" @click="openTest(account)">
              <Icon name="play" size="sm" />
              {{ t('admin.moshuUpstream.test') }}
            </button>
          </div>

          <dl class="mt-5 grid grid-cols-2 gap-3 text-sm">
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.costMultiplier') }}</dt>
              <dd class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ formatMultiplier(account.rate_multiplier) }}</dd>
            </div>
            <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.platform') }}</dt>
              <dd class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ account.platform }}</dd>
            </div>
          </dl>

          <div class="mt-4">
            <p class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('admin.moshuUpstream.boundGroups') }}</p>
            <div class="mt-2 flex flex-wrap gap-2">
              <router-link
                v-for="group in groupsFor(account)"
                :key="group.id"
                to="/admin/groups"
                class="rounded-lg border border-gray-200 px-3 py-2 text-sm hover:border-primary-400 hover:text-primary-600 dark:border-dark-600 dark:hover:border-primary-500"
              >
                <span class="font-medium">{{ group.name }}</span>
                <span class="ml-2 text-gray-500 dark:text-gray-400">
                  {{ t('admin.moshuUpstream.retailMultiplier') }} {{ formatMultiplier(group.rate_multiplier) }}
                </span>
              </router-link>
              <span v-if="groupsFor(account).length === 0" class="text-sm text-amber-600 dark:text-amber-400">
                {{ t('admin.moshuUpstream.noBoundGroups') }}
              </span>
            </div>
          </div>

          <p v-if="account.error_message" class="mt-4 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
            {{ account.error_message }}
          </p>
        </article>
      </div>
    </div>

    <AccountTestModal :show="showTestModal" :account="selectedAccount" @close="closeTest" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import AccountTestModal from '@/components/admin/account/AccountTestModal.vue'
import Icon from '@/components/icons/Icon.vue'
import { accountsAPI, groupsAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { Account, AccountListItem, AdminGroup } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const accounts = ref<AccountListItem[]>([])
const groups = ref<AdminGroup[]>([])
const selectedAccount = ref<Account | null>(null)
const showTestModal = ref(false)

async function loadData() {
  loading.value = true
  try {
    const [accountPage, allGroups] = await Promise.all([
      accountsAPI.list(1, 100, { sort_by: 'name', sort_order: 'asc' }),
      groupsAPI.getAllIncludingInactive()
    ])
    accounts.value = accountPage.items
    groups.value = allGroups
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.moshuUpstream.loadFailed')))
  } finally {
    loading.value = false
  }
}

function openTest(account: AccountListItem) {
  selectedAccount.value = { ...account, name: accountLabel(account) }
  showTestModal.value = true
}

function closeTest() {
  showTestModal.value = false
  selectedAccount.value = null
  void loadData()
}

function groupsFor(account: AccountListItem): AdminGroup[] {
  const ids = new Set(account.group_ids ?? [])
  return groups.value.filter((group) => ids.has(group.id))
}

function accountLabel(account: AccountListItem): string {
  const names = groupsFor(account).map((group) => group.name)
  return names.length ? names.join(' / ') : t('admin.moshuUpstream.accountLabel', { id: account.id })
}

function formatMultiplier(value: number | null | undefined): string {
  return Number(value ?? 1).toFixed(2)
}

function statusLabel(status: AccountListItem['status']): string {
  return t(`admin.moshuUpstream.status.${status}`)
}

function statusClass(status: AccountListItem['status']): string {
  if (status === 'active') return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
  if (status === 'error') return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
  return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
}

function errorMessage(error: unknown, fallback: string): string {
  const candidate = error as {
    message?: string
    response?: { data?: { message?: string; detail?: string } }
  }
  return candidate.response?.data?.message || candidate.response?.data?.detail || candidate.message || fallback
}

onMounted(loadData)
</script>
