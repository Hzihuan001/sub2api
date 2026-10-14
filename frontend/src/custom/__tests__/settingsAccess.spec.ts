import { describe, expect, it } from 'vitest'
import {
  defaultOperatorRolePolicy,
  hasOperatorPermission,
  type OperatorPermission,
} from '@/authz/permissions'
import type { CustomSettingsTab } from '../registry'
import { getCustomSettingsAccess, type CustomSettingsAuthz } from '../settingsAccess'

type Role = 'admin' | 'operator' | 'user'
type Grants = Partial<Record<OperatorPermission, boolean>>

function authFor(role: Role, grants: Grants = {}): CustomSettingsAuthz {
  const policy = defaultOperatorRolePolicy()
  Object.assign(policy.permissions, grants)
  return {
    isAdmin: role === 'admin',
    isOperator: role === 'operator',
    canOperator: (permission) => hasOperatorPermission(role, permission, policy),
  }
}

function tab(overrides: Partial<CustomSettingsTab> = {}): CustomSettingsTab {
  return { id: 'settings-access-probe', labelKey: 'settingsAccess.probe', component: {}, ...overrides }
}

const scopedTab = () => tab({
  readPermission: 'settings.gateway.read',
  writePermission: 'settings.gateway.write',
})
const globalGrants: Grants = { 'settings.read': true, 'settings.write': true }
const scopedGrants: Grants = { 'settings.gateway.read': true, 'settings.gateway.write': true }
const denied = { canRead: false, canWrite: false, readOnly: true }
const readOnly = { canRead: true, canWrite: false, readOnly: true }
const writable = { canRead: true, canWrite: true, readOnly: false }

describe('custom settings access', () => {
  it.each([
    { role: 'admin' as const, grants: {}, expected: writable },
    { role: 'operator' as const, grants: {}, expected: denied },
    { role: 'user' as const, grants: { ...globalGrants, ...scopedGrants }, expected: denied },
  ])('applies the actual $role policy to declared read and write operations', ({ role, grants, expected }) => {
    expect(getCustomSettingsAccess(scopedTab(), authFor(role, grants))).toEqual(expected)
  })

  it.each(['admin', 'operator', 'user'] as const)('denies missing or invalid read metadata for %s even with global grants', (role) => {
    for (const readPermission of [undefined, '', 'settings.unknown.read', 'settings.gateway.write']) {
      const access = getCustomSettingsAccess(tab({ readPermission, writePermission: 'settings.gateway.write' }), authFor(role, globalGrants))
      expect(access, `readPermission=${String(readPermission)}`).toEqual(denied)
    }
  })

  it.each(['admin', 'operator'] as const)('keeps missing or invalid write metadata read-only for %s even with global grants', (role) => {
    for (const writePermission of [undefined, '', 'settings.unknown.write', 'settings.gateway.read']) {
      const access = getCustomSettingsAccess(tab({ readPermission: 'settings.gateway.read', writePermission }), authFor(role, globalGrants))
      expect(access, `writePermission=${String(writePermission)}`).toEqual(readOnly)
    }
  })

  it.each(['admin', 'operator'] as const)('uses the legacy permission only as a read alias for %s', (role) => {
    const auth = authFor(role, { ...globalGrants, ...scopedGrants })
    expect(getCustomSettingsAccess(tab({ permission: 'settings.gateway.read' }), auth)).toEqual(readOnly)
    expect(getCustomSettingsAccess(tab({ permission: 'settings.gateway.write' }), auth)).toEqual(denied)
    expect(getCustomSettingsAccess(tab({ permission: 'settings.unknown.read' }), auth)).toEqual(denied)
  })

  it('requires a separate explicit write declaration when the legacy read alias is used', () => {
    const auth = authFor('operator', scopedGrants)
    expect(getCustomSettingsAccess(tab({ permission: 'settings.gateway.read' }), auth)).toEqual(readOnly)
    expect(getCustomSettingsAccess(tab({
      permission: 'settings.gateway.read',
      writePermission: 'settings.gateway.write',
    }), auth)).toEqual(writable)
  })

  it('does not fall back to a granted legacy alias when explicit read metadata is invalid or denied', () => {
    const auth = authFor('operator', scopedGrants)
    for (const readPermission of ['', 'settings.unknown.read', 'settings.email.read']) {
      expect(getCustomSettingsAccess(tab({
        permission: 'settings.gateway.read',
        readPermission,
        writePermission: 'settings.gateway.write',
      }), auth)).toEqual(denied)
    }
  })

  it('prefers a valid explicit read permission over an invalid legacy alias', () => {
    expect(getCustomSettingsAccess(tab({
      permission: 'settings.unknown.read',
      readPermission: 'settings.gateway.read',
      writePermission: 'settings.gateway.write',
    }), authFor('operator', scopedGrants))).toEqual(writable)
  })

  it('grants scoped read access without implicitly granting a write operation', () => {
    expect(getCustomSettingsAccess(scopedTab(), authFor('operator', {
      'settings.gateway.read': true,
    }))).toEqual(readOnly)
  })

  it('grants explicitly authorized scoped read and write operations', () => {
    const auth = authFor('operator', scopedGrants)
    expect(auth.canOperator('settings.read')).toBe(false)
    expect(auth.canOperator('settings.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(writable)
  })

  it('does not substitute another settings scope for the declared permission', () => {
    expect(getCustomSettingsAccess(scopedTab(), authFor('operator', {
      'settings.email.read': true,
      'settings.email.write': true,
    }))).toEqual(denied)
  })

  it('inherits global settings read access but not a missing global write grant', () => {
    expect(getCustomSettingsAccess(scopedTab(), authFor('operator', {
      'settings.read': true,
    }))).toEqual(readOnly)
  })

  it('inherits global settings read and write grants for valid declared operations', () => {
    const auth = authFor('operator', globalGrants)
    expect(auth.canOperator('settings.gateway.read')).toBe(false)
    expect(auth.canOperator('settings.gateway.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(writable)
  })

  it('denies scoped write when its parent read permission is absent', () => {
    const auth = authFor('operator', { 'settings.gateway.write': true })
    expect(auth.canOperator('settings.gateway.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(denied)
  })

  it('denies global write when its parent global read permission is absent', () => {
    const auth = authFor('operator', { 'settings.write': true })
    expect(auth.canOperator('settings.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(denied)
  })

  it('does not turn a rejected global write into a grant merely because scoped read is allowed', () => {
    const auth = authFor('operator', {
      'settings.gateway.read': true,
      'settings.write': true,
    })
    expect(auth.canOperator('settings.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(readOnly)
  })

  it('does not bypass scoped read-before-write validation through a global read grant', () => {
    const auth = authFor('operator', {
      'settings.read': true,
      'settings.gateway.write': true,
    })
    expect(auth.canOperator('settings.gateway.write')).toBe(false)
    expect(getCustomSettingsAccess(scopedTab(), auth)).toEqual(readOnly)
  })
})
