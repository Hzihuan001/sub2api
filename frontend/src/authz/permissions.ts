import type { UserRole } from '@/types'

export type ManagementPermission =
  | 'dashboard'
  | 'ops'
  | 'users'
  | 'announcements'
  | 'redeemCodes'
  | 'promoCodes'
  | 'usage'

export type OperatorPermission =
  | 'dashboard.read'
  | 'ops.read'
  | 'ops.disposition'
  | 'users.read'
  | 'users.write'
  | 'users.balance.write'
  | 'users.support'
  | 'announcements.read'
  | 'announcements.write'
  | 'redeem_codes.read'
  | 'redeem_codes.write'
  | 'promo_codes.read'
  | 'promo_codes.write'
  | 'usage.read'
  | 'finance.user_balance.read'
  | 'finance.user_charge.read'
  | 'finance.standard_cost.read'
  | 'finance.upstream_cost.read'
  | 'finance.profit.read'

export interface OperatorRolePolicy {
  permissions: Record<OperatorPermission, boolean>
}

export const defaultOperatorRolePolicy = (): OperatorRolePolicy => ({
  permissions: {
    'dashboard.read': true,
    'ops.read': true,
    'ops.disposition': true,
    'users.read': true,
    'users.write': true,
    'users.balance.write': true,
    'users.support': true,
    'announcements.read': true,
    'announcements.write': true,
    'redeem_codes.read': true,
    'redeem_codes.write': true,
    'promo_codes.read': true,
    'promo_codes.write': true,
    'usage.read': true,
    'finance.user_balance.read': false,
    'finance.user_charge.read': false,
    'finance.standard_cost.read': false,
    'finance.upstream_cost.read': false,
    'finance.profit.read': false
  }
})

export const failClosedOperatorRolePolicy = (): OperatorRolePolicy => {
  const policy = defaultOperatorRolePolicy()
  for (const permission of Object.keys(policy.permissions) as OperatorPermission[]) {
    policy.permissions[permission] = false
  }
  return policy
}

const managementPermissionMap: Record<ManagementPermission, OperatorPermission> = {
  dashboard: 'dashboard.read',
  ops: 'ops.read',
  users: 'users.read',
  announcements: 'announcements.read',
  redeemCodes: 'redeem_codes.read',
  promoCodes: 'promo_codes.read',
  usage: 'usage.read'
}

export function hasManagementPermission(
  role: UserRole | undefined,
  permission: ManagementPermission,
  policy: OperatorRolePolicy = defaultOperatorRolePolicy()
): boolean {
  if (role === 'admin') return true
  return role === 'operator' && policy.permissions[managementPermissionMap[permission]] === true
}

export function hasOperatorPermission(
  role: UserRole | undefined,
  permission: OperatorPermission,
  policy: OperatorRolePolicy
): boolean {
  if (role === 'admin') return true
  if (role !== 'operator' || policy.permissions[permission] !== true) return false
  switch (permission) {
    case 'ops.disposition': return policy.permissions['ops.read'] === true
    case 'users.write':
    case 'users.support': return policy.permissions['users.read'] === true
    case 'users.balance.write': return policy.permissions['users.read'] === true && policy.permissions['users.write'] === true
    case 'announcements.write': return policy.permissions['announcements.read'] === true
    case 'redeem_codes.write': return policy.permissions['redeem_codes.read'] === true
    case 'promo_codes.write': return policy.permissions['promo_codes.read'] === true
    default: return true
  }
}
