import { describe, expect, it } from 'vitest'

import {
  defaultOperatorRolePolicy,
  failClosedOperatorRolePolicy,
  hasManagementPermission,
  hasOperatorPermission,
  type ManagementPermission
} from '../permissions'

const operatorPermissions: ManagementPermission[] = [
  'dashboard',
  'ops',
  'users',
  'announcements',
  'redeemCodes',
  'promoCodes',
  'usage',
]

describe('management permissions', () => {
  it.each(operatorPermissions)('allows operator permission %s', (permission) => {
    expect(hasManagementPermission('operator', permission)).toBe(true)
  })

  it.each(operatorPermissions)('allows admin permission %s', (permission) => {
    expect(hasManagementPermission('admin', permission)).toBe(true)
  })

  it.each(operatorPermissions)('denies regular user permission %s', (permission) => {
    expect(hasManagementPermission('user', permission)).toBe(false)
  })

  it('honors a disabled operator page permission without affecting admin', () => {
    const policy = defaultOperatorRolePolicy()
    policy.permissions['dashboard.read'] = false
    expect(hasManagementPermission('operator', 'dashboard', policy)).toBe(false)
    expect(hasManagementPermission('admin', 'dashboard', policy)).toBe(true)
  })

  it('keeps all financial permissions off by default and fails closed on load errors', () => {
    const defaults = defaultOperatorRolePolicy()
    expect(hasOperatorPermission('operator', 'finance.user_charge.read', defaults)).toBe(false)
    expect(hasOperatorPermission('operator', 'finance.upstream_cost.read', defaults)).toBe(false)

    const failed = failClosedOperatorRolePolicy()
    expect(hasManagementPermission('operator', 'users', failed)).toBe(false)
    expect(hasOperatorPermission('operator', 'users.write', failed)).toBe(false)
  })

  it('requires the parent read permission before allowing a write action', () => {
    const policy = defaultOperatorRolePolicy()
    policy.permissions['users.read'] = false
    expect(hasOperatorPermission('operator', 'users.write', policy)).toBe(false)
    expect(hasOperatorPermission('operator', 'users.balance.write', policy)).toBe(false)
  })
})
