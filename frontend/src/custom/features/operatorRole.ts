import { h } from 'vue'
import type { CustomFeatureManifest } from '../registry'

const RoleIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' }, [
    h('path', {
      'stroke-linecap': 'round', 'stroke-linejoin': 'round',
      d: 'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z'
    })
  ])
}

/**
 * Role permissions remain an administrator-only surface. Keeping the menu
 * declaration in a feature manifest lets the host place it without coupling
 * the role page to the sidebar implementation. The actual authorization
 * policy stays in authz/permissions.ts and is intentionally not duplicated.
 */
export const operatorRoleFeature: CustomFeatureManifest = {
  id: 'operator',
  routes: [{
    path: '/admin/roles',
    name: 'AdminRolePermissions',
    component: () => import('@/views/admin/RolePermissionsView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Role Permissions',
      titleKey: 'admin.rolePermissions.title',
      descriptionKey: 'admin.rolePermissions.description'
    }
  }],
  menuItems: [
    {
      id: 'operator-role',
      path: '/admin/roles',
      labelKey: 'nav.rolePermissions',
      icon: RoleIcon,
      afterPath: '/admin/users',
      adminOnly: true,
      hideInSimpleMode: true
    }
  ]
}
