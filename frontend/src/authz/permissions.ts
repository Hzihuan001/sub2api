import type { UserRole } from '@/types'

export type ManagementPermission =
  | 'dashboard'
  | 'ops'
  | 'users'
  | 'announcements'
  | 'redeemCodes'
  | 'promoCodes'
  | 'usage'

const operatorPermissions = new Set<ManagementPermission>([
  'dashboard',
  'ops',
  'users',
  'announcements',
  'redeemCodes',
  'promoCodes',
  'usage'
])

export function hasManagementPermission(
  role: UserRole | undefined,
  permission: ManagementPermission
): boolean {
  if (role === 'admin') return true
  return role === 'operator' && operatorPermissions.has(permission)
}
