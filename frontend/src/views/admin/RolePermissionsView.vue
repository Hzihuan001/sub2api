<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6">
      <header>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('admin.rolePermissions.operatorTitle') }}</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.rolePermissions.operatorHint') }}</p>
      </header>

      <div v-if="loading" class="card flex min-h-48 items-center justify-center"><LoadingSpinner /></div>
      <template v-else>
        <PermissionSection :title="t('admin.rolePermissions.menuSection')" :description="t('admin.rolePermissions.menuDescription')" :items="menuPermissions" v-model="draft" />
        <PermissionSection :title="t('admin.rolePermissions.actionSection')" :description="t('admin.rolePermissions.actionDescription')" :items="actionPermissions" v-model="draft" />
        <PermissionSection :title="t('admin.rolePermissions.financeSection')" :description="t('admin.rolePermissions.financeDescription')" :items="financePermissions" v-model="draft" sensitive />

        <div class="flex flex-wrap justify-end gap-3">
          <button class="btn btn-secondary" type="button" :disabled="saving" @click="restoreDefaults">{{ t('admin.rolePermissions.reset') }}</button>
          <button class="btn btn-primary" type="button" :disabled="saving" @click="save">{{ t('admin.rolePermissions.save') }}</button>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Toggle from '@/components/common/Toggle.vue'
import { adminAPI } from '@/api/admin'
import { useAppStore } from '@/stores/app'
import { defaultOperatorRolePolicy, type OperatorPermission, type OperatorRolePolicy } from '@/authz/permissions'

interface PermissionItem { key: OperatorPermission; label: string; description: string }

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const draft = ref<OperatorRolePolicy>(defaultOperatorRolePolicy())

const item = (key: OperatorPermission, name: string): PermissionItem => ({
  key,
  label: t(`admin.rolePermissions.permissions.${name}.label`),
  description: t(`admin.rolePermissions.permissions.${name}.description`)
})

const menuPermissions = computed(() => [
  item('dashboard.read', 'dashboardRead'), item('ops.read', 'opsRead'), item('users.read', 'usersRead'),
  item('announcements.read', 'announcementsRead'), item('redeem_codes.read', 'redeemCodesRead'),
  item('promo_codes.read', 'promoCodesRead'), item('usage.read', 'usageRead')
])
const actionPermissions = computed(() => [
  item('ops.disposition', 'opsDisposition'), item('users.write', 'usersWrite'),
  item('users.balance.write', 'usersBalanceWrite'), item('users.support', 'usersSupport'),
  item('announcements.write', 'announcementsWrite'), item('redeem_codes.write', 'redeemCodesWrite'),
  item('promo_codes.write', 'promoCodesWrite')
])
const financePermissions = computed(() => [
  item('finance.user_balance.read', 'userBalanceRead'), item('finance.user_charge.read', 'userChargeRead'),
  item('finance.standard_cost.read', 'standardCostRead'), item('finance.upstream_cost.read', 'upstreamCostRead'),
  item('finance.profit.read', 'profitRead')
])

const PermissionSection = defineComponent({
  props: {
    title: { type: String, required: true }, description: { type: String, required: true },
    items: { type: Array as PropType<PermissionItem[]>, required: true },
    modelValue: { type: Object as PropType<OperatorRolePolicy>, required: true },
    sensitive: Boolean
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const update = (key: OperatorPermission, enabled: boolean) => emit('update:modelValue', {
      permissions: { ...props.modelValue.permissions, [key]: enabled }
    })
    return () => h('section', { class: ['card overflow-hidden', props.sensitive ? 'border border-amber-200 dark:border-amber-900/60' : ''] }, [
      h('div', { class: 'border-b border-gray-100 p-5 dark:border-dark-700' }, [
        h('h2', { class: 'text-base font-semibold text-gray-900 dark:text-white' }, props.title),
        h('p', { class: 'mt-1 text-sm text-gray-500 dark:text-gray-400' }, props.description)
      ]),
      h('div', { class: 'divide-y divide-gray-100 dark:divide-dark-700' }, props.items.map(entry =>
        h('div', { class: 'flex items-center justify-between gap-5 px-5 py-4', key: entry.key }, [
          h('div', [h('p', { class: 'text-sm font-medium text-gray-900 dark:text-white' }, entry.label), h('p', { class: 'mt-1 text-xs text-gray-500 dark:text-gray-400' }, entry.description)]),
          h(Toggle, { modelValue: props.modelValue.permissions[entry.key], 'onUpdate:modelValue': (value: boolean) => update(entry.key, value) })
        ])
      ))
    ])
  }
})

async function load() {
  loading.value = true
  try { draft.value = await adminAPI.roles.getOperatorPermissions() }
  catch { appStore.showError(t('admin.rolePermissions.loadFailed')) }
  finally { loading.value = false }
}

function restoreDefaults() {
  draft.value = defaultOperatorRolePolicy()
  appStore.showInfo(t('admin.rolePermissions.defaultRestored'))
}

async function save() {
  saving.value = true
  try {
    draft.value = await adminAPI.roles.updateOperatorPermissions(draft.value)
    appStore.showSuccess(t('admin.rolePermissions.saved'))
  } catch { appStore.showError(t('admin.rolePermissions.saveFailed')) }
  finally { saving.value = false }
}

onMounted(load)
</script>
