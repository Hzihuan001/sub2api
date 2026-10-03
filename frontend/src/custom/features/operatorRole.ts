import type { CustomFeatureManifest } from '../registry'

/**
 * Role permissions remain an administrator-only surface. Keeping the menu
 * declaration in a feature manifest lets the host place it without coupling
 * the role page to the sidebar implementation. The actual authorization
 * policy stays in authz/permissions.ts and is intentionally not duplicated.
 */
export const operatorRoleFeature: CustomFeatureManifest = {
  id: 'operator',
  menuItems: [
    {
      id: 'operator-role',
      path: '/admin/roles',
      labelKey: 'nav.rolePermissions',
      adminOnly: true,
      hideInSimpleMode: true
    }
  ]
}
