<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.moshuPricing.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuPricing.description') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="loadData">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('common.refresh') }}
        </button>
      </div>

      <div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-900 dark:border-emerald-800/60 dark:bg-emerald-950/30 dark:text-emerald-100">
        <p class="font-medium">{{ t('admin.moshuPricing.formulaTitle') }}</p>
        <p class="mt-1 font-mono">{{ t('admin.moshuPricing.formula') }}</p>
      </div>

      <div v-if="loading" class="flex justify-center py-16">
        <LoadingSpinner />
      </div>

      <div v-else-if="products.length === 0" class="card p-8 text-center">
        <p class="font-medium text-gray-900 dark:text-white">{{ t('admin.moshuPricing.emptyTitle') }}</p>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuPricing.emptyBody') }}</p>
      </div>

      <div v-else class="grid gap-5 xl:grid-cols-2">
        <article v-for="group in products" :key="group.id" class="card p-5">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ group.name }}</h2>
                <span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                  {{ group.platform }}
                </span>
              </div>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.moshuPricing.productID', { id: group.id }) }}</p>
            </div>
            <button class="btn btn-secondary" @click="openUserOverrides(group)">
              <Icon name="users" size="sm" />
              {{ t('admin.moshuPricing.userOverrides') }}
            </button>
          </div>

          <dl class="mt-5 grid grid-cols-2 gap-3 text-sm">
            <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
              <dt class="text-gray-500 dark:text-gray-400">{{ t('admin.moshuPricing.costMultiplier') }}</dt>
              <dd class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ costLabel(group) }}</dd>
              <p class="mt-1 text-xs text-gray-400">{{ t('admin.moshuPricing.costReadOnly') }}</p>
            </div>
            <label class="rounded-lg border border-primary-200 bg-primary-50/50 p-3 dark:border-primary-800/60 dark:bg-primary-950/20">
              <span class="text-gray-600 dark:text-gray-300">{{ t('admin.moshuPricing.retailMultiplier') }}</span>
              <input
                v-model.number="drafts[group.id].rate_multiplier"
                type="number"
                min="0"
                step="0.01"
                class="input mt-2 w-full text-lg font-semibold"
              />
            </label>
          </dl>

          <p v-if="isBelowCost(group)" class="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">
            {{ t('admin.moshuPricing.belowCostWarning') }}
          </p>
          <p v-else class="mt-3 text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.moshuPricing.marginPreview', { margin: marginLabel(group) }) }}
          </p>

          <label class="mt-4 block">
            <span class="text-sm text-gray-600 dark:text-gray-300">{{ t('admin.moshuPricing.descriptionLabel') }}</span>
            <textarea v-model="drafts[group.id].description" rows="2" class="input mt-2 w-full resize-y" />
          </label>

          <div class="mt-4 flex justify-end">
            <button class="btn btn-primary" :disabled="savingGroupID === group.id || isBelowCost(group)" @click="saveGroup(group)">
              <Icon name="check" size="sm" />
              {{ savingGroupID === group.id ? t('common.saving') : t('common.save') }}
            </button>
          </div>
        </article>
      </div>
    </div>

    <GroupRateMultipliersModal
      :show="showUserOverrides"
      :group="selectedGroup"
      @close="closeUserOverrides"
      @success="loadData"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupRateMultipliersModal from '@/components/admin/group/GroupRateMultipliersModal.vue'
import { accountsAPI, groupsAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import type { AccountListItem, AdminGroup } from '@/types'

interface PricingDraft {
  rate_multiplier: number
  description: string
}

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const savingGroupID = ref<number | null>(null)
const accounts = ref<AccountListItem[]>([])
const groups = ref<AdminGroup[]>([])
const drafts = reactive<Record<number, PricingDraft>>({})
const selectedGroup = ref<AdminGroup | null>(null)
const showUserOverrides = ref(false)

const boundGroupIDs = computed(() => {
  const ids = new Set<number>()
  for (const account of accounts.value) {
    for (const groupID of account.group_ids ?? []) ids.add(groupID)
  }
  return ids
})

const products = computed(() => groups.value.filter((group) => boundGroupIDs.value.has(group.id)))

async function loadData() {
  loading.value = true
  try {
    const [accountPage, allGroups] = await Promise.all([
      accountsAPI.list(1, 100, { sort_by: 'name', sort_order: 'asc' }),
      groupsAPI.getAllIncludingInactive()
    ])
    accounts.value = accountPage.items
    groups.value = allGroups
    for (const group of allGroups) {
      drafts[group.id] = {
        rate_multiplier: Number(group.rate_multiplier),
        description: group.description ?? ''
      }
    }
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.moshuPricing.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function saveGroup(group: AdminGroup) {
  if (isBelowCost(group)) return
  savingGroupID.value = group.id
  try {
    const draft = drafts[group.id]
    await groupsAPI.update(group.id, {
      rate_multiplier: Number(draft.rate_multiplier),
      description: draft.description.trim() || null
    })
    appStore.showSuccess(t('admin.moshuPricing.saved'))
    await loadData()
  } catch (error) {
    appStore.showError(errorMessage(error, t('admin.moshuPricing.saveFailed')))
  } finally {
    savingGroupID.value = null
  }
}

function accountCosts(group: AdminGroup): number[] {
  const values = accounts.value
    .filter((account) => account.group_ids?.includes(group.id))
    .map((account) => Number(account.rate_multiplier ?? 1))
  return [...new Set(values)].sort((a, b) => a - b)
}

function maximumCost(group: AdminGroup): number {
  const values = accountCosts(group)
  return values.length ? values[values.length - 1] : Number.POSITIVE_INFINITY
}

function costLabel(group: AdminGroup): string {
  const values = accountCosts(group)
  if (!values.length) return '-'
  if (values.length === 1) return values[0].toFixed(2)
  return `${values[0].toFixed(2)} – ${values[values.length - 1].toFixed(2)}`
}

function isBelowCost(group: AdminGroup): boolean {
  const retail = Number(drafts[group.id]?.rate_multiplier)
  return !Number.isFinite(retail) || retail < maximumCost(group)
}

function marginLabel(group: AdminGroup): string {
  const cost = maximumCost(group)
  const retail = Number(drafts[group.id]?.rate_multiplier)
  if (!Number.isFinite(cost) || !Number.isFinite(retail)) return '-'
  return (retail - cost).toFixed(2)
}

function openUserOverrides(group: AdminGroup) {
  selectedGroup.value = group
  showUserOverrides.value = true
}

function closeUserOverrides() {
  showUserOverrides.value = false
  selectedGroup.value = null
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
