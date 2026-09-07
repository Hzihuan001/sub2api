<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.resellers.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.resellers.description') }}</p>
      </div>

      <section class="card p-5">
        <h2 class="font-semibold text-gray-900 dark:text-white">新建代理商</h2>
        <p class="mt-2 text-sm text-gray-500">选择代理商自己的主站普通用户账号。充值余额、授权 Key、扣费和使用记录均归属该账号。没有账号时，请先在用户管理中创建。</p>
        <div class="mt-4 grid gap-3 md:grid-cols-3">
          <Select v-model="newTenant.user_id" :options="billingUserOptions" remote clearable :loading="searchingUsers" placeholder="选择代理商充值账号" search-placeholder="输入部分邮箱或用户名" @search="searchBillingUsers" />
          <input v-model.trim="newTenant.name" class="input" placeholder="代理商名称" />
          <input v-model.trim="newTenant.cidrs" class="input" placeholder="允许 IP/CIDR，逗号分隔（可选）" />
        </div>
        <div class="mt-3 flex justify-end"><button class="btn btn-primary" :disabled="busy || !newTenant.user_id || !newTenant.name" @click="createTenant">创建</button></div>
      </section>

      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else class="grid gap-5 xl:grid-cols-[320px_minmax(0,1fr)]">
        <section class="card overflow-hidden self-start">
          <button v-for="tenant in tenants" :key="tenant.id" class="block w-full border-b border-gray-100 px-4 py-3 text-left last:border-0 hover:bg-gray-50 dark:border-dark-700 dark:hover:bg-dark-800" :class="selectedID === tenant.id ? 'bg-primary-50 dark:bg-primary-950/20' : ''" @click="selectTenant(tenant.id)">
            <div class="flex items-center justify-between gap-2"><span class="font-medium text-gray-900 dark:text-white">{{ tenant.name }}</span><span class="rounded px-2 py-0.5 text-xs" :class="tenant.status === 'active' ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-600'">{{ tenant.status }}</span></div>
            <p class="mt-1 text-xs text-gray-500">{{ tenant.billing_account?.email || `User #${tenant.user_id}` }} · {{ tenant.protocol_version }}</p>
          </button>
          <p v-if="!tenants.length" class="p-8 text-center text-sm text-gray-500">暂无代理商</p>
        </section>

        <div v-if="selected" class="space-y-5">
          <section class="card p-5">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div><h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ selected.name }}</h2><p class="mt-1 text-xs text-gray-500">实例：{{ selected.instance_id || '尚未兑换授权码' }}</p></div>
              <select v-model="selected.status" class="input w-36" @change="saveTenantStatus"><option value="active">active</option><option value="suspended">suspended</option><option value="disabled">disabled</option></select>
            </div>
            <div v-if="selected.billing_account" class="mt-4 space-y-2 text-sm text-gray-600 dark:text-gray-300">
              <p>充值及计费账号：{{ selected.billing_account.email }}（#{{ selected.user_id }}）</p>
              <p>余额 ${{ money(selected.billing_account.balance) }} · 冻结 ${{ money(selected.billing_account.frozen_balance) }} · 可用 ${{ money(selected.billing_account.balance - selected.billing_account.frozen_balance) }}</p>
              <p v-if="selected.billing_account.role !== 'user'" class="text-amber-600">当前绑定了管理账号，请先调整为代理商自己的普通充值账号，再重新签发授权 Key。</p>
              <p v-else-if="selected.billing_account.balance - selected.billing_account.frozen_balance <= 0" class="text-amber-600">可用余额不足，代理商需要先充值才能调用。</p>
              <div class="flex gap-4"><RouterLink class="text-primary-600 hover:underline" to="/admin/users">用户管理 / 充值</RouterLink><RouterLink class="text-primary-600 hover:underline" :to="{ path: '/admin/usage', query: { user_id: selected.user_id } }">查看该账号使用记录</RouterLink></div>
            </div>
            <div class="mt-4 flex flex-wrap gap-3">
              <Select v-model="replacementUserID" class="min-w-64 flex-1" :options="billingUserOptions" remote clearable :loading="searchingUsers" placeholder="选择新的代理商充值账号" search-placeholder="输入部分邮箱或用户名" @search="searchBillingUsers" />
              <button class="btn btn-secondary" :disabled="busy || !replacementUserID || replacementUserID === selected.user_id" @click="changeBillingAccount">更换计费账号</button>
            </div>
            <p class="mt-2 text-xs text-gray-500">更换后旧授权 Key 停用，请重新生成授权码并在 L1 连接。原账号余额和历史使用记录保持原归属。</p>
          </section>

          <section class="card p-5">
            <h2 class="font-semibold text-gray-900 dark:text-white">授权产品</h2>
            <details class="mt-4 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
              <summary>批量添加分组</summary>
              <p class="my-2 text-xs text-gray-500">按平台排列；已授权分组自动跳过。代码自动使用 group-ID，显示名称使用分组名称。</p>
              <div class="grid max-h-64 gap-2 overflow-y-auto md:grid-cols-2">
                <label v-for="group in batchGroups" :key="group.id" class="flex items-center gap-2 text-sm">
                  <input v-model="batchGroupIDs" type="checkbox" :value="group.id" :disabled="busy" />{{ group.platform }} · {{ group.name }}
                </label>
              </div>
              <div class="mt-3 flex gap-2"><button class="btn btn-secondary" :disabled="busy" @click="batchGroupIDs = batchGroups.map(g => g.id)">全选</button><button class="btn btn-primary" :disabled="busy || !batchGroupIDs.length" @click="addProductsBatch">添加所选 {{ batchGroupIDs.length }} 个分组</button></div>
              <p v-for="result in batchResults" :key="result.id" class="mt-1 text-sm" :class="result.success ? 'text-green-600' : 'text-red-600'">{{ result.name }}：{{ result.success ? '已添加' : result.error }}</p>
            </details>
            <div class="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-4">
              <select v-model.number="newProduct.moshu_group_id" class="input"><option :value="0">选择按余额计费的 Moshu 分组</option><option v-for="group in balanceGroups" :key="group.id" :value="group.id">{{ group.name }} · {{ group.platform }}</option></select>
              <input v-model.trim="newProduct.product_code" class="input" placeholder="稳定产品代码" />
              <input v-model.trim="newProduct.display_name" class="input" placeholder="下游显示名称" />
              <button class="btn btn-primary" :disabled="busy || !newProduct.moshu_group_id || !newProduct.product_code || !newProduct.display_name" @click="addProduct">添加/更新</button>
            </div>
            <div class="mt-4 space-y-2">
              <label v-for="product in products" :key="product.id" class="flex flex-wrap items-center gap-3 rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-600">
                <input v-model="selectedProducts" type="checkbox" :value="product.id" :disabled="!product.enabled" />
                <span class="min-w-0 flex-1"><strong class="text-gray-900 dark:text-white">{{ product.display_name }}</strong><span class="ml-2 text-xs text-gray-500">{{ product.product_code }} · {{ product.platform }} · 成本 {{ product.cost_rate_multiplier.toFixed(4) }} · {{ product.models.length }} models</span></span>
                <span class="text-xs" :class="product.credential_configured ? 'text-green-600' : 'text-amber-600'">{{ product.credential_configured ? '已签发凭证' : '待签发' }}</span>
                <button class="text-sm text-primary-600 hover:underline" @click.prevent="rotate(product)">轮换</button>
              </label>
            </div>
            <div class="mt-4 flex justify-end"><button class="btn btn-primary" :disabled="busy || !selectedProducts.length" @click="createEnrollment">生成 30 分钟一次性授权码</button></div>
            <div v-if="enrollmentCode" class="mt-4 rounded-lg border border-amber-300 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/20">
              <p class="text-sm font-semibold text-amber-800 dark:text-amber-300">授权码只显示这一次，请立即复制给 L1 管理员：</p>
              <div class="mt-2 flex gap-2"><code class="min-w-0 flex-1 break-all rounded bg-white p-3 text-xs dark:bg-dark-900">{{ enrollmentCode }}</code><button class="btn btn-secondary" @click="copy(enrollmentCode)">复制</button></div>
            </div>
          </section>

          <section class="card overflow-hidden">
            <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700"><h2 class="font-semibold text-gray-900 dark:text-white">最近权威成本记录</h2></div>
            <div class="overflow-x-auto"><table class="min-w-full text-sm"><thead class="bg-gray-50 text-left text-gray-500 dark:bg-dark-800"><tr><th class="px-4 py-3">Request</th><th class="px-4 py-3">产品</th><th class="px-4 py-3">标准价</th><th class="px-4 py-3">成本倍率</th><th class="px-4 py-3">实际成本</th><th class="px-4 py-3">状态</th></tr></thead><tbody><tr v-for="item in settlements" :key="item.id" class="border-t border-gray-100 dark:border-dark-700"><td class="max-w-[200px] truncate px-4 py-3 font-mono text-xs">{{ item.request_id }}</td><td class="px-4 py-3">{{ item.product_code }}</td><td class="px-4 py-3">${{ money(item.standard_cost) }}</td><td class="px-4 py-3">{{ item.cost_rate_multiplier.toFixed(4) }}</td><td class="px-4 py-3">${{ money(item.actual_cost) }}</td><td class="px-4 py-3">{{ item.status }}</td></tr><tr v-if="!settlements.length"><td colspan="6" class="px-4 py-8 text-center text-gray-500">暂无记录</td></tr></tbody></table></div>
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Select from '@/components/common/Select.vue'
import { groupsAPI, resellersAPI, usersAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { runResellerBatch, type BatchResult } from '@/utils/resellerBatch'
import type { AdminGroup } from '@/types'
import type { ResellerProduct, ResellerSettlement, ResellerTenant } from '@/api/admin/resellers'

const { t } = useI18n(), app = useAppStore()
const loading = ref(true), busy = ref(false), selectedID = ref<number | null>(null)
const tenants = ref<ResellerTenant[]>([]), products = ref<ResellerProduct[]>([]), groups = ref<AdminGroup[]>([]), settlements = ref<ResellerSettlement[]>([])
const selectedProducts = ref<number[]>([]), enrollmentCode = ref('')
const newTenant = reactive({ user_id: null as number | null, name: '', cidrs: '' })
const billingUserOptions = ref<{ value: number; label: string }[]>([])
const searchingUsers = ref(false)
const replacementUserID = ref<number | null>(null)
const batchGroupIDs = ref<number[]>([]), batchResults = ref<BatchResult[]>([])
let userSearchController: AbortController | undefined
const newProduct = reactive({ moshu_group_id: 0, product_code: '', display_name: '' })
const selected = computed(() => tenants.value.find((item) => item.id === selectedID.value))
const balanceGroups = computed(() => groups.value.filter((group) => group.subscription_type === 'standard'))
const batchGroups = computed(() => balanceGroups.value.filter(g => !products.value.some(p => p.moshu_group_id === g.id)).sort((a, b) => a.platform.localeCompare(b.platform) || a.name.localeCompare(b.name)))

async function addProductsBatch() {
  const tenantID = selectedID.value
  if (!tenantID || busy.value) return
  const targets = batchGroups.value.filter(g => batchGroupIDs.value.includes(g.id))
  busy.value = true
  try {
    batchResults.value = await runResellerBatch(targets, g => g.name, g => {
      if (products.value.some(p => p.product_code === `group-${g.id}` && p.moshu_group_id !== g.id)) throw new Error('自动产品代码已被其他分组使用，请手动添加')
      return resellersAPI.upsertProduct(tenantID, { moshu_group_id: g.id, product_code: `group-${g.id}`, display_name: g.name, enabled: true })
    })
    batchGroupIDs.value = batchResults.value.filter(r => !r.success).map(r => r.id)
    if (selectedID.value === tenantID) await loadTenant(tenantID)
  } catch (e) { app.showError(errorText(e)) } finally { busy.value = false }
}

async function load() { loading.value = true; try { [tenants.value, groups.value] = await Promise.all([resellersAPI.listTenants(), groupsAPI.getAllIncludingInactive()]); if (!selectedID.value && tenants.value[0]) selectedID.value = tenants.value[0].id; if (selectedID.value) await loadTenant(selectedID.value) } catch (e) { app.showError(errorText(e)) } finally { loading.value = false } }
async function loadTenant(id: number) { const [productList, page] = await Promise.all([resellersAPI.listProducts(id), resellersAPI.settlements(id)]); products.value = productList; settlements.value = page.items; selectedProducts.value = productList.filter((item) => item.enabled).map((item) => item.id) }
async function selectTenant(id: number) { if (busy.value) return; selectedID.value = id; enrollmentCode.value = ''; replacementUserID.value = null; batchGroupIDs.value = []; batchResults.value = []; await loadTenant(id) }
async function act(action: () => Promise<unknown>, message: string) { busy.value = true; try { await action(); app.showSuccess(message); if (selectedID.value) await loadTenant(selectedID.value) } catch (e) { app.showError(errorText(e)) } finally { busy.value = false } }
async function createTenant() { const userID = newTenant.user_id; if (!userID) return; await act(async () => { const created = await resellersAPI.createTenant({ user_id: userID, name: newTenant.name, allowed_cidrs: newTenant.cidrs.split(',').map((v) => v.trim()).filter(Boolean) }); tenants.value = await resellersAPI.listTenants(); selectedID.value = created.id }, '代理商已创建') }
async function searchBillingUsers(query: string) {
  userSearchController?.abort()
  const controller = new AbortController()
  userSearchController = controller
  searchingUsers.value = true
  try {
    const result = await usersAPI.list(1, 20, { search: query.trim(), role: 'user', status: 'active' }, { signal: controller.signal })
    if (controller.signal.aborted) return
    const current = billingUserOptions.value.filter((option) => option.value === newTenant.user_id || option.value === replacementUserID.value)
    const options = result.items.filter((user) => user.role === 'user' && user.status === 'active').map((user) => ({ value: user.id, label: `${user.email} · 余额 $${money(user.balance)}` }))
    billingUserOptions.value = [...current.filter((entry) => !options.some((option) => option.value === entry.value)), ...options]
  } catch (e) {
    if (!controller.signal.aborted) app.showError(errorText(e))
  } finally {
    if (userSearchController === controller) searchingUsers.value = false
  }
}
async function changeBillingAccount() {
  const tenantID = selectedID.value, userID = replacementUserID.value
  if (!tenantID || !userID || !confirm('更换计费账号将立即停用此代理商的旧授权 Key，需重新连接 L1 后才能恢复调用。确认更换？')) return
  await act(async () => {
    await resellersAPI.changeBillingAccount(tenantID, userID)
    enrollmentCode.value = ''
    replacementUserID.value = null
    tenants.value = await resellersAPI.listTenants()
  }, '计费账号已更换，请重新生成授权码并连接 L1')
}
async function saveTenantStatus() { if (!selected.value) return; await act(() => resellersAPI.updateTenant(selected.value!.id, { status: selected.value!.status, allowed_cidrs: selected.value!.allowed_cidrs }), '状态已更新') }
async function addProduct() { if (!selectedID.value) return; await act(() => resellersAPI.upsertProduct(selectedID.value!, { ...newProduct, enabled: true }), '产品已保存') }
async function createEnrollment() { if (!selectedID.value) return; busy.value = true; try { const result = await resellersAPI.createEnrollment(selectedID.value, selectedProducts.value); enrollmentCode.value = result.enrollment_code; app.showSuccess('一次性授权码已生成') } catch (e) { app.showError(errorText(e)) } finally { busy.value = false } }
async function rotate(product: ResellerProduct) { if (!selectedID.value || !confirm('轮换后旧凭证仅短时继续有效，确认继续？')) return; busy.value = true; try { const result = await resellersAPI.rotate(selectedID.value, product.id); alert(`新凭证只显示一次：\n${result.api_key}`); await loadTenant(selectedID.value) } catch (e) { app.showError(errorText(e)) } finally { busy.value = false } }
async function copy(value: string) { await navigator.clipboard.writeText(value); app.showSuccess('已复制') }
function money(value: number) { return Number(value || 0).toFixed(6) }
function errorText(error: unknown) { return (error as { message?: string }).message || '操作失败' }
onMounted(() => { void load(); void searchBillingUsers('') })
onUnmounted(() => userSearchController?.abort())
</script>
