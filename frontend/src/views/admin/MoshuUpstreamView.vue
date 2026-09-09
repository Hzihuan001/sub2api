<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex justify-center py-16"><LoadingSpinner /></div>
      <template v-else>
        <section v-if="status && !status.enabled" class="card border-amber-200 p-5 dark:border-amber-900">
          <h2 class="font-semibold text-amber-800 dark:text-amber-300">{{ t('admin.moshuUpstream.protocolDisabled') }}</h2>
          <p class="mt-2 text-sm text-amber-700 dark:text-amber-400">{{ t('admin.moshuUpstream.protocolDisabledHint') }}</p>
        </section>

        <section v-else-if="status && !status.connected" class="card p-5">
          <p v-if="!authStore.isSuperAdmin" class="text-sm text-gray-500">请联系最高管理员完成上游授权接入。</p>
          <template v-else>
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.enrollTitle') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.moshuUpstream.enrollHint') }}</p>
          <div class="mt-4 grid gap-4 lg:grid-cols-2">
            <label class="text-sm"><span class="font-medium">{{ t('admin.moshuUpstream.baseUrl') }}</span><input v-model.trim="enrollment.base_url" class="input mt-1 w-full" type="url" placeholder="https://moshu.example.com" /></label>
            <label class="text-sm"><span class="font-medium">{{ t('admin.moshuUpstream.enrollmentCode') }}</span><input v-model.trim="enrollment.enrollment_code" class="input mt-1 w-full" autocomplete="off" /></label>
          </div>
          <div class="mt-4 flex justify-end"><button class="btn btn-primary" :disabled="busy || !enrollment.enrollment_code" @click="enroll">{{ t('admin.moshuUpstream.connect') }}</button></div>
          </template>
        </section>

        <template v-else-if="status?.connected">
          <section class="card p-5">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.accountBalance') }}</h2>
              <button class="btn btn-secondary" :disabled="busy" @click="syncCatalog">{{ t('admin.moshuUpstream.syncCatalog') }}</button>
            </div>
            <div v-if="upstreamBalance" class="mt-4 grid gap-3 sm:grid-cols-3">
              <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.moshuUpstream.totalBalance') }}</p><p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCurrency(upstreamBalance.balance) }}</p></div>
              <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.moshuUpstream.frozenBalance') }}</p><p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCurrency(upstreamBalance.frozen_balance) }}</p></div>
              <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-800"><p class="text-xs text-gray-500">{{ t('admin.moshuUpstream.availableBalance') }}</p><p class="mt-1 text-xl font-semibold" :class="upstreamBalance.warning ? 'text-amber-600 dark:text-amber-400' : 'text-emerald-600 dark:text-emerald-400'">{{ formatCurrency(availableBalance) }}</p></div>
            </div>
            <p v-else-if="!loadingBalance" class="mt-4 rounded-lg bg-gray-50 p-3 text-sm text-gray-500 dark:bg-dark-800">{{ t('admin.moshuUpstream.balanceUnavailable') }}</p>
            <p v-if="upstreamBalance?.warning" class="mt-3 rounded-lg bg-amber-50 p-3 text-sm text-amber-700 dark:bg-amber-950/30 dark:text-amber-300">{{ t('admin.moshuUpstream.lowBalance') }}</p>
            <div class="mt-3 flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500">
              <span>{{ t('admin.moshuUpstream.balanceHint') }}</span>
              <button class="text-primary-600 hover:underline disabled:cursor-not-allowed disabled:opacity-50" :disabled="loadingBalance" @click="loadBalance(true)">{{ t('admin.moshuUpstream.refreshBalance') }}</button>
            </div>
            <p v-if="status.connection?.last_error" class="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ status.connection.last_error }}</p>
            <p class="mt-2 text-xs text-gray-500">上游成本和模型每 5 分钟自动同步，也可手动同步；不会修改本地分组名称、倍率和容量。</p>
            <details v-if="authStore.isSuperAdmin" class="mt-4">
              <summary>重新授权 / 计费账号换绑后同步 Key</summary>
              <p class="my-2 text-sm text-gray-500">仅支持同一主站、同一代理商重新授权。新增产品需兑换新授权码；已授权产品保留。主站更换计费账号后，请为全部需要的产品生成新授权码。成功后页面会完整刷新，用户、日志和设置保留。</p>
              <input v-model.trim="enrollment.enrollment_code" class="input w-full" placeholder="输入一次性授权码" autocomplete="off" :disabled="busy" />
              <button class="btn btn-primary mt-2" :disabled="busy || !enrollment.enrollment_code" @click="enroll">重新授权并同步</button>
            </details>
          </section>

          <section class="space-y-3">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.moshuUpstream.authorizedProducts') }}</h2>
            <div class="card space-y-2 p-4">
              <p class="text-sm text-gray-500">倍率自主设置，支持 0 及低于成本的倍率，不设成本下限。勾选后按各自名称、倍率和容量批量启用/保存；失败项保留勾选，便于重试。</p>
              <div class="flex gap-2"><button class="btn btn-secondary" :disabled="busy" @click="batchIDs = status.products.filter(p => p.authorized && !p.selected).map(p => p.id)">选择尚未启用的产品</button><button class="btn btn-primary" :disabled="busy || !batchIDs.length" @click="saveBatch">批量启用/保存（{{ batchIDs.length }}）</button></div>
              <p v-for="result in batchResults" :key="result.id" class="text-sm" :class="result.success ? 'text-green-600' : 'text-red-600'">{{ result.name }}：{{ result.success ? '成功' : result.error }}</p>
            </div>
            <article v-for="product in authorizedProducts" :key="product.id" class="card p-5">
              <label class="mb-2 flex items-center gap-2 text-sm"><input v-model="batchIDs" type="checkbox" :value="product.id" :disabled="busy || !product.authorized" />批量选择</label>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_minmax(170px,220px)_minmax(120px,160px)_minmax(120px,160px)_auto] xl:items-end">
                <div>
                  <div class="flex flex-wrap items-center gap-2"><h3 class="font-semibold text-gray-900 dark:text-white">{{ product.display_name }}</h3><span class="rounded bg-gray-100 px-2 py-0.5 text-xs dark:bg-dark-700">{{ product.platform }}</span><span v-if="!product.authorized" class="rounded bg-red-100 px-2 py-0.5 text-xs text-red-700">{{ t('admin.moshuUpstream.revoked') }}</span></div>
                  <p class="mt-1 text-xs text-gray-500">{{ product.product_code }} · {{ t('admin.moshuUpstream.costMultiplier') }} {{ multiplier(product.cost_rate_multiplier) }}<template v-if="detectedModelCount(product.id) !== null"> · {{ t('admin.moshuUpstream.modelCount', { count: detectedModelCount(product.id) }) }}</template></p>
                </div>
                <label class="text-sm"><span class="text-gray-600 dark:text-gray-400">{{ t('admin.moshuUpstream.salesName') }}</span><input v-model.trim="drafts[product.id].name" class="input mt-1 w-full" :disabled="!product.authorized" /></label>
                <label class="text-sm"><span class="text-gray-600 dark:text-gray-400">{{ t('admin.moshuUpstream.retailMultiplier') }}</span><input v-model.number="drafts[product.id].multiplier" class="input mt-1 w-full" type="number" min="0" step="0.01" :disabled="!product.authorized" /></label>
                <label class="text-sm"><span class="text-gray-600 dark:text-gray-400" :title="t('admin.moshuUpstream.capacityHint')">{{ t('admin.moshuUpstream.capacity') }}</span><input v-model.number="drafts[product.id].capacity" class="input mt-1 w-full" type="number" min="1" step="1" :disabled="!product.authorized" /></label>
                <div class="flex flex-wrap gap-2"><button class="btn btn-primary" :disabled="busy || !product.authorized" @click="saveProduct(product)">{{ product.selected ? t('common.save') : t('admin.moshuUpstream.enableSale') }}</button><button v-if="product.selected && authStore.isSuperAdmin" class="btn btn-secondary" :title="t('admin.moshuUpstream.rotateHint')" :disabled="busy" @click="rotate(product)">{{ t('admin.moshuUpstream.rotate') }}</button><button class="btn btn-secondary" :disabled="busy || testingProductID === product.id" @click="openProductTest(product)">{{ t('admin.accounts.testConnection') }}</button></div>
              </div>
              <div v-if="product.selected" class="mt-3 flex items-center justify-between border-t border-gray-100 pt-3 text-xs text-gray-500 dark:border-dark-700"><span>{{ t('admin.moshuUpstream.saleActive') }}</span><button class="text-red-600 hover:underline" :disabled="busy" @click="disableProduct(product)">{{ t('admin.moshuUpstream.stopSale') }}</button></div>
            </article>
          </section>

        </template>
      </template>
    </div>
    <AccountTestModal :show="showTest" :account="testingAccount" @close="closeProductTest" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import AccountTestModal from '@/components/admin/account/AccountTestModal.vue'
import { accountsAPI, groupsAPI, moshuResellerAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { runResellerBatch, type BatchResult } from '@/utils/resellerBatch'
import { formatCurrency } from '@/utils/format'
import { reloadAfterResellerEnrollment } from '@/utils/resellerRefresh'
import type { MoshuProduct, MoshuResellerBalance, MoshuResellerStatus } from '@/api/admin/moshuReseller'
import type { Account } from '@/types'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const loading = ref(true), busy = ref(false)
const status = ref<MoshuResellerStatus | null>(null)
const upstreamBalance = ref<MoshuResellerBalance | null>(null)
const loadingBalance = ref(false)
const availableBalance = computed(() => (upstreamBalance.value?.balance ?? 0) - (upstreamBalance.value?.frozen_balance ?? 0))
const enrollment = reactive({ base_url: '', enrollment_code: '' })
const batchIDs = ref<number[]>([]), batchResults = ref<BatchResult[]>([])
const drafts = reactive<Record<number, { name: string; multiplier: number; capacity: number }>>({})
const authorizedProducts = computed(() => status.value?.products.filter(product => product.authorized) ?? [])
const testingProductID = ref<number | null>(null)
const testingAccount = ref<Account | null>(null)
const showTest = ref(false)
const detectedModelCounts = reactive<Record<number, number>>({})
let modelProbeGeneration = 0

async function loadData() {
  loading.value = true
  let productsToProbe: MoshuProduct[] = []
  try {
    // Status reconciles upstream resources. Read groups after it completes.
    const current = await moshuResellerAPI.status()
    const allGroups = await groupsAPI.getAllIncludingInactive()
    status.value = current
    for (const id of Object.keys(drafts)) delete drafts[Number(id)]
    batchIDs.value = batchIDs.value.filter(id => current.products.some(p => p.id === id && p.authorized))
    if (current?.connection?.base_url) enrollment.base_url = current.connection.base_url
    current?.products.forEach((product) => { drafts[product.id] = { name: allGroups.find(g => g.id === product.local_group_id)?.name ?? product.display_name, multiplier: product.sales_rate_multiplier ?? 1, capacity: product.capacity > 0 ? product.capacity : 100 } })
    productsToProbe = current?.products.filter(product => product.authorized) ?? []
  } catch (error) { appStore.showError(errorText(error)) } finally { loading.value = false }
  if (status.value?.connected) void loadBalance(false)
  void probeProductModels(productsToProbe)
}
async function loadBalance(showError: boolean) {
  if (loadingBalance.value || !status.value?.connected) return
  loadingBalance.value = true
  try { upstreamBalance.value = await moshuResellerAPI.balance() }
  catch (error) {
    upstreamBalance.value = null
    if (showError) appStore.showError(errorText(error))
  } finally { loadingBalance.value = false }
}
async function probeProductModels(products: MoshuProduct[]) {
  const generation = ++modelProbeGeneration
  const activeIDs = new Set(products.map(product => product.id))
  for (const id of Object.keys(detectedModelCounts).map(Number)) if (!activeIDs.has(id)) delete detectedModelCounts[id]
  await Promise.allSettled(products.map(product => probeProductModel(product, generation)))
}
async function probeProductModel(product: MoshuProduct, generation = modelProbeGeneration) {
  if (!product.local_account_id) {
    delete detectedModelCounts[product.id]
    return
  }
  try {
    const result = await accountsAPI.probeUpstreamModels(product.local_account_id)
    if (generation !== modelProbeGeneration) return
    const count = new Set(result.models.map(model => model.trim()).filter(Boolean)).size
    if (count > 0) detectedModelCounts[product.id] = count
    else delete detectedModelCounts[product.id]
  } catch {
    if (generation === modelProbeGeneration) delete detectedModelCounts[product.id]
  }
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
async function enroll() {
  if (busy.value || !authStore.isSuperAdmin) return
  busy.value = true
  try {
    await moshuResellerAPI.enroll({ ...enrollment })
    enrollment.enrollment_code = ''
    ++modelProbeGeneration
    closeProductTest()
    upstreamBalance.value = null
    batchIDs.value = []; batchResults.value = []
    for (const id of Object.keys(detectedModelCounts)) delete detectedModelCounts[Number(id)]
    reloadAfterResellerEnrollment()
  } catch (error) { appStore.showError(errorText(error)) }
  finally { busy.value = false }
}
async function saveBatch() {
  if (busy.value) return
  const targets = authorizedProducts.value.filter(p => batchIDs.value.includes(p.id)).map(p => ({ ...p, draft: { ...drafts[p.id] } }))
  if (!confirm(`确认按填写的售价启用/保存 ${targets.length} 个产品？`)) return
  busy.value = true
  try {
    batchResults.value = await runResellerBatch(targets, p => p.display_name, p => moshuResellerAPI.configureProduct(p.id, { selected: true, sales_name: p.draft.name, sales_multiplier: p.draft.multiplier, capacity: p.draft.capacity }))
    batchIDs.value = batchResults.value.filter(r => !r.success).map(r => r.id)
    await loadData()
    for (const p of targets) if (batchIDs.value.includes(p.id)) drafts[p.id] = p.draft
  } finally { busy.value = false }
}
const syncCatalog = () => run(() => moshuResellerAPI.syncCatalog(), t('admin.moshuUpstream.catalogSynced'))
const rotate = (product: MoshuProduct) => run(() => moshuResellerAPI.rotateCredential(product.id), t('admin.moshuUpstream.rotated'))
const saveProduct = (product: MoshuProduct) => run(() => moshuResellerAPI.configureProduct(product.id, { selected: true, sales_name: drafts[product.id].name, sales_multiplier: drafts[product.id].multiplier, capacity: drafts[product.id].capacity }), t('admin.moshuUpstream.productSaved'))
const disableProduct = (product: MoshuProduct) => run(() => moshuResellerAPI.configureProduct(product.id, { selected: false, sales_name: drafts[product.id].name, sales_multiplier: drafts[product.id].multiplier, capacity: drafts[product.id].capacity }), t('admin.moshuUpstream.productStopped'))
async function openProductTest(product: MoshuProduct) {
  if (testingProductID.value !== null) return
  testingProductID.value = product.id
  try {
    const accountID = product.local_account_id ?? (await moshuResellerAPI.ensureTestAccount(product.id)).account_id
    product.local_account_id = accountID
    void probeProductModel(product)
    testingAccount.value = await accountsAPI.getById(accountID)
    showTest.value = true
  } catch (error) { appStore.showError(errorText(error)) }
  finally { testingProductID.value = null }
}
function closeProductTest() { showTest.value = false; testingAccount.value = null }
function multiplier(value?: number | null) { return Number(value ?? 1).toFixed(2) }
function detectedModelCount(productID: number) { return detectedModelCounts[productID] ?? null }
function errorText(error: unknown) { const value = error as { message?: string }; return value.message || t('admin.moshuUpstream.loadFailed') }
onMounted(loadData)
</script>
