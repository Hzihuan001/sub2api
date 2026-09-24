import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

function read(relativePath: string): string {
  return readFileSync(fileURLToPath(new URL(relativePath, import.meta.url)), 'utf8')
}

describe('operator UI contract', () => {
  it('filters every management module through the operator permission policy', () => {
    const sidebar = read('../../components/layout/AppSidebar.vue')
    const expected = [
      ['/admin/dashboard', 'dashboard'],
      ['/admin/ops', 'ops'],
      ['/admin/users', 'users'],
      ['/admin/announcements', 'announcements'],
      ['/admin/redeem', 'redeemCodes'],
      ['/admin/promo-codes', 'promoCodes'],
      ['/admin/usage', 'usage'],
      ['/admin/groups', 'groups'],
      ['/admin/channels', 'channels'],
      ['/admin/accounts', 'accounts'],
      ['/admin/subscriptions', 'subscriptions'],
      ['/admin/plugins', 'plugins'],
      ['/admin/proxies', 'proxies'],
      ['/admin/risk-control', 'riskControl'],
      ['/admin/prompt-audit', 'promptAudit'],
      ['/admin/affiliates', 'affiliates'],
      ['/admin/orders', 'orders'],
      ['/admin/audit-logs', 'auditLogs'],
    ]

    for (const [path, permission] of expected) {
      expect(sidebar).toContain(`path: '${path}'`)
      expect(sidebar).toContain(`permission: '${permission}'`)
    }
    expect(sidebar).toContain('function applyOperatorPermissions(items: NavItem[]): NavItem[]')
    expect(sidebar).toContain('if (item.permission && !authStore.can(item.permission)) return out')
  })

  it('guards privileged user actions and role controls', () => {
    const users = read('../../views/admin/UsersView.vue')
    const createModal = read('../../components/admin/user/UserCreateModal.vue')
    const editModal = read('../../components/admin/user/UserEditModal.vue')

    expect(users).toContain("authStore.canOperator('users.write')")
    expect(users).toContain("authStore.canOperator('users.balance.write')")
    expect(users).toContain(':row-selectable="canMutateUser"')
    expect(users).toContain('v-if="canMutateUser(row)"')
    expect(users).toContain('v-if="canMutateUser(user)"')
    expect(createModal).toContain('v-if="authStore.isAdmin"')
    expect(editModal).toContain('v-if="authStore.isAdmin || authStore.isOperator"')
    expect(editModal).toContain('if (authStore.isAdmin || authStore.isOperator) data.role = form.role')
  })

  it('makes ops configuration and cleanup controls admin-only', () => {
    const dashboard = read('../../views/admin/ops/OpsDashboard.vue')
    const logs = read('../../views/admin/ops/components/OpsSystemLogTable.vue')
    const alertRules = read('../../views/admin/ops/components/OpsAlertRulesCard.vue')

    expect(dashboard).toContain('v-if="authStore.isAdmin"')
    expect(dashboard).toContain(':read-only="authStore.isOperator"')
    expect(dashboard).toContain(":read-only=\"!authStore.canOperator('ops.disposition')\"")
    expect(dashboard).toContain('if (!authStore.isAdmin)')
    expect(logs).toContain('v-if="authStore.isAdmin"')
    expect(alertRules).toContain('v-if="!props.readOnly"')
  })

  it('hides dashboard links to admin-only modules from operators', () => {
    const dashboard = read('../../views/admin/DashboardView.vue')

    expect(dashboard).toContain('v-if="canUseBatchImage || authStore.isAdmin"')
    expect(dashboard).toContain('v-if="authStore.isAdmin"')
    expect(dashboard).toContain("router.push('/admin/groups')")
  })

  it('keeps usage cleanup task history readable while hiding mutations', () => {
    const usage = read('../../views/admin/UsageView.vue')
    const cleanup = read('../../components/admin/usage/UsageCleanupDialog.vue')

    expect(usage).toContain(':read-only="authStore.isOperator"')
    expect(cleanup).toContain('v-if="!props.readOnly"')
    expect(cleanup).toContain('if (props.readOnly) return')
  })

  it('declares all three persisted roles', () => {
    const types = read('../../types/index.ts')
    expect(types).toContain("'admin' | 'operator' | 'user'")
  })
})
