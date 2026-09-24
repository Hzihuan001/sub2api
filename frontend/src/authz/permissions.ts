import type { UserRole } from '@/types'

export type ManagementPermission =
  | 'dashboard'
  | 'ops'
  | 'users'
  | 'announcements'
  | 'redeemCodes'
  | 'promoCodes'
  | 'usage'
  | 'groups'
  | 'channels'
  | 'accounts'
  | 'subscriptions'
  | 'promptRules'
  | 'riskControl'
  | 'promptAudit'
  | 'plugins'
  | 'proxies'
  | 'affiliates'
  | 'orders'
  | 'auditLogs'
  | 'dataManagement'
  | 'backups'
  | 'system'
  | 'userAttributes'
  | 'errorRules'
  | 'tlsProfiles'
  | 'settings'

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
  | 'groups.read' | 'groups.write'
  | 'channels.read' | 'channels.write'
  | 'accounts.read' | 'accounts.write'
  | 'subscriptions.read' | 'subscriptions.write'
  | 'prompt_rules.read' | 'prompt_rules.write'
  | 'risk_control.read' | 'risk_control.write'
  | 'prompt_audit.read' | 'prompt_audit.write'
  | 'plugins.read' | 'plugins.write'
  | 'proxies.read' | 'proxies.write'
  | 'affiliates.read' | 'affiliates.write'
  | 'orders.read' | 'orders.write'
  | 'audit_logs.read' | 'audit_logs.write'
  | 'data.read' | 'data.write'
  | 'backups.read' | 'backups.write'
  | 'system.read' | 'system.write'
  | 'user_attributes.read' | 'user_attributes.write'
  | 'error_rules.read' | 'error_rules.write'
  | 'tls_profiles.read' | 'tls_profiles.write'
  | 'settings.read' | 'settings.write'
  | 'settings.general.read' | 'settings.general.write'
  | 'settings.agreement.read' | 'settings.agreement.write'
  | 'settings.features.read' | 'settings.features.write'
  | 'settings.security.read' | 'settings.security.write'
  | 'settings.users.read' | 'settings.users.write'
  | 'settings.gateway.read' | 'settings.gateway.write'
  | 'settings.payment.read' | 'settings.payment.write'
  | 'settings.email.read' | 'settings.email.write'
  | 'settings.backup.read' | 'settings.backup.write'

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
    'finance.profit.read': false,
    'groups.read': false, 'groups.write': false,
    'channels.read': false, 'channels.write': false,
    'accounts.read': false, 'accounts.write': false,
    'subscriptions.read': false, 'subscriptions.write': false,
    'prompt_rules.read': false, 'prompt_rules.write': false,
    'risk_control.read': false, 'risk_control.write': false,
    'prompt_audit.read': false, 'prompt_audit.write': false,
    'plugins.read': false, 'plugins.write': false,
    'proxies.read': false, 'proxies.write': false,
    'affiliates.read': false, 'affiliates.write': false,
    'orders.read': false, 'orders.write': false,
    'audit_logs.read': false, 'audit_logs.write': false,
    'data.read': false, 'data.write': false,
    'backups.read': false, 'backups.write': false,
    'system.read': false, 'system.write': false,
    'user_attributes.read': false, 'user_attributes.write': false,
    'error_rules.read': false, 'error_rules.write': false,
    'tls_profiles.read': false, 'tls_profiles.write': false,
    'settings.read': false, 'settings.write': false,
    'settings.general.read': false, 'settings.general.write': false,
    'settings.agreement.read': false, 'settings.agreement.write': false,
    'settings.features.read': false, 'settings.features.write': false,
    'settings.security.read': false, 'settings.security.write': false,
    'settings.users.read': false, 'settings.users.write': false,
    'settings.gateway.read': false, 'settings.gateway.write': false,
    'settings.payment.read': false, 'settings.payment.write': false,
    'settings.email.read': false, 'settings.email.write': false,
    'settings.backup.read': false, 'settings.backup.write': false
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
  usage: 'usage.read',
  groups: 'groups.read',
  channels: 'channels.read',
  accounts: 'accounts.read',
  subscriptions: 'subscriptions.read',
  promptRules: 'prompt_rules.read',
  riskControl: 'risk_control.read',
  promptAudit: 'prompt_audit.read',
  plugins: 'plugins.read',
  proxies: 'proxies.read',
  affiliates: 'affiliates.read',
  orders: 'orders.read',
  auditLogs: 'audit_logs.read',
  dataManagement: 'data.read',
  backups: 'backups.read',
  system: 'system.read',
  userAttributes: 'user_attributes.read',
  errorRules: 'error_rules.read',
  tlsProfiles: 'tls_profiles.read',
  settings: 'settings.read'
}

export function hasManagementPermission(
  role: UserRole | undefined,
  permission: ManagementPermission,
  policy: OperatorRolePolicy = defaultOperatorRolePolicy()
): boolean {
  if (role === 'admin') return true
  if (role === 'operator' && permission === 'settings' && policy.permissions['settings.read'] !== true) {
    return [
      'settings.general.read', 'settings.agreement.read', 'settings.features.read',
      'settings.security.read', 'settings.users.read', 'settings.gateway.read',
      'settings.payment.read', 'settings.email.read', 'settings.backup.read'
    ].some((key) => policy.permissions[key as OperatorPermission] === true)
  }
  return role === 'operator' && policy.permissions[managementPermissionMap[permission]] === true
}

export function hasOperatorPermission(
  role: UserRole | undefined,
  permission: OperatorPermission,
  policy: OperatorRolePolicy
): boolean {
  if (role === 'admin') return true
  if (role !== 'operator' || policy.permissions[permission] !== true) return false
  if (permission.endsWith('.write') && permission !== 'users.balance.write') {
    const readPermission = `${permission.slice(0, -'.write'.length)}.read` as OperatorPermission
    if (policy.permissions[readPermission] !== true) return false
  }
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
