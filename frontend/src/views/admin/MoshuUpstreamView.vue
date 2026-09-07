<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.protocolSubtitle') }}</p>
        </div>
        <button class="btn btn-secondary" :disabled="loading" @click="loadData">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />{{ t('common.refresh') }}
        </button>
      </div>

      <div v-if="loading" class="flex justify-center py-16"><LoadingSpinner /></div>
      <template v-else>
        <template v-if="authStore.isSuperAdmin">
        <section v-if="status && !status.enabled" class="card border-amber-200 p-5 dark:border-amber-900">
          <h2 class="font-semibold text-amber-800 dark:text-amber-300">{{ t('admin.moshuUpstream.protocolDisabled') }}</h2>
          <p class="mt-2 text-sm text-amber-700 dark:text-amber-400">{{ t('admin.moshuUpstream.protocolDisabledHint') }}</p>
        </section>

        <section v-else-if="status && !status.connected" class="card p-5">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.enrollTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.enrollHint') }}</p>
          <div class="mt-4 grid gap-4 lg:grid-cols-2">
            <label class="text-sm"><span class="font-medium">{{ t('admin.moshuUpstream.baseUrl') }}</span><input v-model.trim="enrollment.base_url" class="input mt-1 w-full" type="url" placeholder="https://moshu.example.com" /></label>
            <label class="text-sm"><span class="font-medium">{{ t('admin.moshuUpstream.enrollmentCode') }}</span><input v-model.trim="enrollment.enrollment_code" class="input mt-1 w-full" autocomplete="off" /></label>
          </div>
          <div class="mt-4 flex justify-end"><button class="btn btn-primary" :disabled="busy || !enrollment.enrollment_code" @click="enroll">{{ t('admin.moshuUpstream.connect') }}</button></div>
        </section>

        <template v-else-if="status?.connected">
          <section class="card p-5">
            <div class="flex flex-wrap items-start justify-between gap-3">
              <div>
                <div class="flex items-center gap-2"><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ status.connection?.reseller_name }}</h2><span class="rounded-full bg-green-100 px-2 py-0.5 text-xs text-green-700 dark:bg-green-900/30 dark:text-green-300">v1</span></div>
                <p class="mt-1 text-xs text-gray-500">{{ status.connection?.base_url }} · {{ t('admin.moshuUpstream.catalogVersion') }} {{ status.connection?.catalog_version }}</p>
              </div>
              <div class="flex gap-2"><button class="btn btn-secondary" :disabled="busy" @click="syncCatalog">{{ t('admin.moshuUpstream.syncCatalog') }}</button><button class="btn btn-secondary" :disabled="busy" @click="syncSettlements">{{ t('admin.moshuUpstream.syncSettlements') }}</button></div>
            </div>
            <p v-if="status.connection?.last_error" class="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ status.connection.last_error }}</p>
            <p class="mt-2 text-xs text-gray-500">上游成本和模型每 5 分钟自动同步，也可手动同步；不会修改本地销售名称和倍率。</p>
            <details class="mt-4">
              <summary>重新授权 / 计费账号换绑后同步 Key</summary>
              <p class="my-2 text-sm text-gray-500">使用同一主站、同一代理商的新授权码。自动更新已启用账号的 Key，保留本地售价、分组和历史记录。换绑后请为全部需要的产品生成授权码。</p>
              <input v-model.trim="enrollment.enrollment_code" class="input w-full" placeholder="输入一次性授权码" autocomplete="off" :disabled="busy" />
              <button class="btn btn-primary mt-2" :disabled="busy || !enrollment.enrollment_code" @click="enroll">重新授权并同步</button>
            </details>
          </section>

          <section class="space-y-3">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.authorizedProducts') }}</h2>
            <div class="card space-y-2 p-4">
              <p class="text-sm text-gray-500">销售倍率自主设置，支持 0 及低于成本销售，不设成本下限。勾选后按各自名称和倍率批量启用/保存；失败项保留勾选，便于重试。</p>
              <div class="flex gap-2"><button class="btn btn-secondary" :disabled="busy" @click="batchIDs = status.products.filter(p => p.authorized && !p.selected).map(p => p.id)">选择尚未启用的产品</button><button class="btn btn-primary" :disabled="busy || !batchIDs.length" @click="saveBatch">批量启用/保存（{{ batchIDs.length }}）</button></div>
              <p v-for="result in batchResults" :key="result.id" class="text-sm" :class="result.success ? 'text-green-600' : 'text-red-600'">{{ result.name }}：{{ result.success ? '成功' : result.error }}</p>
            </div>
            <article v-for="product in status.products" :key="product.id" class="card p-5">
              <label class="mb-2 flex items-center gap-2 text-sm"><input v-model="batchIDs" type="checkbox" :value="product.id" :disabled="busy || !product.authorized" />批量选择</label>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(180px,240px)_minmax(150px,190px)_auto] xl:items-end">
                <div>
                  <div class="flex flex-wrap items-center gap-2"><h3 class="font-semibold text-gray-900 dark:text-white">{{ product.display_name }}</h3><span class="rounded bg-gray-100 px-2 py-0.5 text-xs dark:bg-dark-700">{{ product.platform }}</span><span v-if="!product.authorized" class="rounded bg-red-100 px-2 py-0.5 text-xs text-red-700">{{ t('admin.moshuUpstream.revoked') }}</span></div>
                  <p class="mt-1 text-xs text-gray-500">{{ product.product_code }} · {{ t('admin.moshuUpstream.costMultiplier') }} {{ multiplier(product.cost_rate_multiplier) }} · {{ product.models.length }} models</p>
                </div>
                <label class="text-sm"><span class="text-gray-600 dark:text-gray-400">{{ t('admin.moshuUpstream.salesName') }}</span><input v-model.trim="drafts[product.id].name" class="input mt-1 w-full" :disabled="!product.authorized" /></label>
                <label class="text-sm"><span class="text-gray-600 dark:text-gray-400">{{ t('admin.moshuUpstream.retailMultiplier') }}</span><input v-model.number="drafts[product.id].multiplier" class="input mt-1 w-full" type="number" min="0" step="0.01" :disabled="!product.authorized" /></label>
                <div class="flex gap-2"><button class="btn btn-primary" :disabled="busy || !product.authorized" @click="saveProduct(product)">{{ product.selected ? t('common.save') : t('admin.moshuUpstream.enableSale') }}</button><button v-if="product.selected" class="btn btn-secondary" :disabled="busy" @click="rotate(product)">{{ t('admin.moshuUpstream.rotate') }}</button></div>
              </div>
              <div v-if="product.selected" class="mt-3 flex items-center justify-between border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700"><span>{{ t('admin.moshuUpstream.localConfigured') }} #{{ product.local_group_id }} / #{{ product.local_account_id }}</span><button class="text-red-600 hover:underline" :disabled="busy" @click="disableProduct(product)">{{ t('admin.moshuUpstream.stopSale') }}</button></div>
              <p v-else-if="product.local_group_id || product.local_account_id" class="mt-3 text-xs text-amber-600">已有本地资源：分组 #{{ product.local_group_id ?? '未创建' }} / 账号 #{{ product.local_account_id ?? '未创建' }}。启用或重试时会复用，不会重复创建。</p>
            </article>
          </section>

          <section class="card overflow-hidden">
            <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700"><h2 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.profitTitle') }}</h2></div>
            <div class="overflow-x-auto"><table class="min-w-full text-sm"><thead class="bg-gray-50 text-left text-gray-500 dark:bg-dark-800"><tr><th class="px-5 py-3">Request</th><th class="px-5 py-3">Product</th><th class="px-5 py-3">Moshu</th><th class="px-5 py-3">L1</th><th class="px-5 py-3">{{ t('admin.moshuUpstream.grossProfit') }}</th></tr></thead><tbody>
              <tr v-for="item in profits" :key="item.id" class="border-t border-gray-100 dark:border-dark-700"><td class="max-w-[220px] truncate px-5 py-3 font-mono text-xs">{{ item.request_id }}</td><td class="px-5 py-3">{{ item.product_code }}</td><td class="px-5 py-3">${{ money(item.moshu_actual_cost) }}</td><td class="px-5 py-3">${{ money(item.l1_customer_charge) }}</td><td class="px-5 py-3" :class="item.gross_profit < 0 ? 'text-red-600' : 'text-green-600'">${{ money(item.gross_profit) }}</td></tr>
              <tr v-if="!profits.length"><td colspan="5" class="px-5 py-8 text-center text-gray-500">{{ t('common.noData') }}</td></tr>
            </tbody></table></div>
          </section>
        </template>
        </template>

        <section class="space-y-3">
          <div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.legacyTitle') }}</h2><p class="mt-1 text-sm text-gray-500">{{ t('admin.moshuUpstream.legacyHint') }}</p></div>
          <div v-if="!accounts.length" class="card p-8 text-center text-sm text-gray-500">{{ t('admin.moshuUpstream.emptyBody') }}</div>
          <div v-else class="grid gap-4 xl:grid-cols-2">
            <article v-for="account in accounts" :key="account.id" class="card p-5">
              <div class="flex items-start justify-between gap-4"><div><h3 class="font-semibold text-gray-900 dark:text-white">{{ label(account) }}</h3><p class="mt-1 text-xs text-gray-500">{{ account.platform }} · {{ multiplier(account.rate_multiplier) }}</p></div><button class="btn btn-secondary" @click="openTest(account)"><Icon name="play" size="sm" />{{ t('admin.moshuUpstream.test') }}</button></div>
              <div class="mt-4 grid gap-2 sm:grid-cols-2"><label v-for="group in compatibleGroups(account)" :key="group.id" class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm dark:border-dark-600"><input v-model="bindings[account.id]" type="checkbox" :value="group.id" /><span class="min-w-0 flex-1 truncate">{{ group.name }}</span></label></div>
              <div class="mt-3 flex justify-end"><button class="btn btn-primary" :disabled="savingAccountID === account.id || !bindingsChanged(account)" @click="saveBindings(account)">{{ t('common.save') }}</button></div>
            </article>
          </div>
        </section>
      </template>
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
import { accountsAPI, groupsAPI, moshuResellerAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { runResellerBatch, type BatchResult } from '@/utils/resellerBatch'
import type { MoshuProduct, MoshuProfitRecord, MoshuResellerStatus } from '@/api/admin/moshuReseller'
import type { Account, AccountListItem, AdminGroup } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const loading = ref(true), busy = ref(false)
const status = ref<MoshuResellerStatus | null>(null), profits = ref<MoshuProfitRecord[]>([])
const enrollment = reactive({ base_url: '', enrollment_code: '' })
const batchIDs = ref<number[]>([]), batchResults = ref<BatchResult[]>([])
const drafts = reactive<Record<number, { name: string; multiplier: number }>>({})
const accounts = ref<AccountListItem[]>([]), groups = ref<AdminGroup[]>([])
const bindings = reactive<Record<number, number[]>>({})
const savingAccountID = ref<number | null>(null), selectedAccount = ref<Account | null>(null), showTestModal = ref(false)

async function loadData() {
  loading.value = true
  try {
    const [current, accountPage, allGroups] = await Promise.all([
      authStore.isSuperAdmin ? moshuResellerAPI.status() : Promise.resolve(null),
      accountsAPI.list(1, 100, { sort_by: 'name', sort_order: 'asc' }),
      groupsAPI.getAllIncludingInactive()
    ])
    status.value = current
    if (current?.connection?.base_url) enrollment.base_url = current.connection.base_url
    accounts.value = accountPage.items.filter((account) => !account.extra?.moshu_reseller_managed)
    groups.value = allGroups
    current?.products.forEach((product) => { drafts[product.id] = { name: allGroups.find(g => g.id === product.local_group_id)?.name ?? product.display_name, multiplier: product.sales_rate_multiplier ?? 1 } })
    accounts.value.forEach((account) => { bindings[account.id] = [...(account.group_ids ?? [])] })
    profits.value = current?.connected ? (await moshuResellerAPI.profits(1, 20)).items : []
  } catch (error) { appStore.showError(errorText(error)) } finally { loading.value = false }
}
async function run(action: () => Promise<unknown>, message: string) {
  if (busy.value) return
  const previousDrafts = Object.fromEntries(Object.entries(drafts).map(([id, draft]) => [id, { ...draft }]))
  busy.value = true
  try { await action(); appStore.showSuccess(message); await loadData() }
  catch (error) {
    await loadData()
    for (const product of status.value?.products ?? []) if (previousDrafts[product.id]) drafts[product.id] = previousDrafts[product.id]
    appStore.showError(errorText(error))
  } finally { busy.value = false }
}
const enroll = () => run(async () => { await moshuResellerAPI.enroll({ ...enrollment }); enrollment.enrollment_code = '' }, t('admin.moshuUpstream.connected'))
async function saveBatch() {
  if (busy.value) return
  const targets = (status.value?.products ?? []).filter(p => p.authorized && batchIDs.value.includes(p.id)).map(p => ({ ...p, draft: { ...drafts[p.id] } }))
  if (!confirm(`确认按填写的售价启用/保存 ${targets.length} 个产品？`)) return
  busy.value = true
  try {
    batchResults.value = await runResellerBatch(targets, p => p.display_name, p => moshuResellerAPI.configureProduct(p.id, { selected: true, sales_name: p.draft.name, sales_multiplier: p.draft.multiplier }))
    batchIDs.value = batchResults.value.filter(r => !r.success).map(r => r.id)
    await loadData()
    for (const p of targets) if (batchIDs.value.includes(p.id)) drafts[p.id] = p.draft
  } finally { busy.value = false }
}
const syncCatalog = () => run(() => moshuResellerAPI.syncCatalog(), t('admin.moshuUpstream.catalogSynced'))
const syncSettlements = () => run(() => moshuResellerAPI.syncSettlements(), t('admin.moshuUpstream.settlementsSynced'))
const rotate = (product: MoshuProduct) => run(() => moshuResellerAPI.rotateCredential(product.id), t('admin.moshuUpstream.rotated'))
const saveProduct = (product: MoshuProduct) => run(() => moshuResellerAPI.configureProduct(product.id, { selected: true, sales_name: drafts[product.id].name, sales_multiplier: drafts[product.id].multiplier }), t('admin.moshuUpstream.productSaved'))
const disableProduct = (product: MoshuProduct) => run(() => moshuResellerAPI.configureProduct(product.id, { selected: false, sales_name: drafts[product.id].name, sales_multiplier: drafts[product.id].multiplier }), t('admin.moshuUpstream.productStopped'))
function openTest(account: AccountListItem) { selectedAccount.value = { ...account, name: label(account) }; showTestModal.value = true }
function closeTest() { showTestModal.value = false; selectedAccount.value = null; void loadData() }
function compatibleGroups(account: AccountListItem) { return groups.value.filter((group) => group.platform === account.platform) }
function normalized(values?: number[]) { return [...new Set(values ?? [])].sort((a, b) => a - b) }
function bindingsChanged(account: AccountListItem) { return normalized(bindings[account.id]).join(',') !== normalized(account.group_ids).join(',') }
async function saveBindings(account: AccountListItem) { savingAccountID.value = account.id; try { await accountsAPI.update(account.id, { group_ids: normalized(bindings[account.id]) }); appStore.showSuccess(t('admin.moshuUpstream.assignmentsSaved')); await loadData() } catch (error) { appStore.showError(errorText(error)) } finally { savingAccountID.value = null } }
function label(account: AccountListItem) { return account.name.replace(/^moshu[\s_-]*/i, '').trim() || t('admin.moshuUpstream.accountLabel') }
function multiplier(value?: number | null) { return Number(value ?? 1).toFixed(2) }
function money(value: number) { return Number(value || 0).toFixed(6) }
function errorText(error: unknown) { const value = error as { message?: string }; return value.message || t('admin.moshuUpstream.loadFailed') }
onMounted(loadData)
</script>
