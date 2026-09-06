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
            <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.moshuUpstream.assignGroups') }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.assignGroupsHint') }}</p>
            <div v-if="compatibleGroups(account).length" class="mt-3 grid gap-2 sm:grid-cols-2">
              <label
                v-for="group in compatibleGroups(account)"
                :key="group.id"
                class="flex cursor-pointer items-center gap-3 rounded-lg border border-gray-200 px-3 py-2 text-sm hover:border-primary-400 dark:border-dark-600 dark:hover:border-primary-500"
              >
                <input
                  v-model="bindingDrafts[account.id]"
                  type="checkbox"
                  :value="group.id"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                />
                <span class="min-w-0 flex-1 truncate font-medium text-gray-800 dark:text-gray-200">{{ group.name }}</span>
                <span class="text-gray-500 dark:text-gray-400">
                  {{ t('admin.moshuUpstream.retailMultiplier') }} {{ formatMultiplier(group.rate_multiplier) }}
                </span>
              </label>
            </div>
            <router-link v-else to="/admin/groups" class="mt-3 inline-flex text-sm text-primary-600 hover:underline dark:text-primary-400">
              {{ t('admin.moshuUpstream.noCompatibleGroups') }}
            </router-link>
            <div class="mt-3 flex justify-end">
              <button
                class="btn btn-primary"
                :disabled="savingAccountID === account.id || !bindingsChanged(account)"
                @click="saveBindings(account)"
              >
                <Icon name="check" size="sm" />
                {{ savingAccountID === account.id ? t('common.saving') : t('admin.moshuUpstream.saveAssignments') }}
              </button>
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
import { onMounted, reactive, ref } from 'vue'
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
const bindingDrafts = reactive<Record<number, number[]>>({})
const savingAccountID = ref<number | null>(null)
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
    for (const account of accountPage.items) {
      bindingDrafts[account.id] = [...(account.group_ids ?? [])]
    }
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

function compatibleGroups(account: AccountListItem): AdminGroup[] {
  return groups.value.filter((group) => group.platform === account.platform)
}

function normalizedGroupIDs(values: number[] | undefined): number[] {
  return [...new Set(values ?? [])].sort((a, b) => a - b)
}

function bindingsChanged(account: AccountListItem): boolean {
  return normalizedGroupIDs(bindingDrafts[account.id]).join(',') !== normalizedGroupIDs(account.group_ids).join(',')
}

async function saveBindings(account: AccountListItem) {
  savingAccountID.value = account.id
  try {
    await accountsAPI.update(account.id, { group_ids: normalizedGroupIDs(bindingDrafts[account.id]) })
    appStore.showSuccess(t('admin.moshuUpstream.assignmentsSaved'))
    await loadData()
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.moshuUpstream.assignmentsSaveFailed')))
  } finally {
    savingAccountID.value = null
  }
}

function accountLabel(account: AccountListItem): string {
  const name = account.name.replace(/^moshu[\s_-]*/i, '').trim()
  return name || t('admin.moshuUpstream.accountLabel')
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
