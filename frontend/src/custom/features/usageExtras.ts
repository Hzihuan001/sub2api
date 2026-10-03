import { h } from 'vue'

import type { CustomFeatureManifest } from '../registry'

const UsageIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z'
        })
      ]
    )
}

/** Usage presentation and administration helpers owned by the custom layer. */
export const usageExtrasFeature: CustomFeatureManifest = {
  id: 'usage-extras',
  routes: [
    {
      path: '/admin/usage',
      name: 'AdminUsage',
      component: () => import('@/views/admin/UsageView.vue'),
      meta: {
        requiresAuth: true,
        requiredPermission: 'usage',
        title: 'Usage Records',
        titleKey: 'admin.usage.title',
        descriptionKey: 'admin.usage.description'
      }
    }
  ],
  menuItems: [
    {
      id: 'admin-usage',
      path: '/admin/usage',
      labelKey: 'nav.usage',
      icon: UsageIcon,
      permission: 'usage'
    }
  ],
  permissions: ['usage']
}
