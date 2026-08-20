import { describe, expect, it } from 'vitest'

import { hasManagementPermission, type ManagementPermission } from '../permissions'

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
})
