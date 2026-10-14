import { defaultOperatorRolePolicy, type OperatorPermission } from '@/authz/permissions'
import type { CustomSettingsTab } from './registry'

export interface CustomSettingsAuthz {
  isAdmin: boolean
  isOperator: boolean
  canOperator(permission: OperatorPermission): boolean
}

const knownPermissions = new Set(Object.keys(defaultOperatorRolePolicy().permissions))

function knownPermission(value: string | undefined, action: 'read' | 'write'): value is OperatorPermission {
  return !!value && value.endsWith(`.${action}`) && knownPermissions.has(value)
}

/** Metadata opts in to each operation, even when global settings grants are inherited. */
export function getCustomSettingsAccess(tab: CustomSettingsTab, auth: CustomSettingsAuthz) {
  const readPermission = tab.readPermission ?? tab.permission
  const canRead = knownPermission(readPermission, 'read') && (
    auth.isAdmin || (auth.isOperator && (
      auth.canOperator('settings.read') || auth.canOperator(readPermission)
    ))
  )
  const canWrite = canRead && knownPermission(tab.writePermission, 'write') && (
    auth.isAdmin || (auth.isOperator && (
      auth.canOperator('settings.write') || auth.canOperator(tab.writePermission)
    ))
  )
  return { canRead, canWrite, readOnly: !canWrite }
}
