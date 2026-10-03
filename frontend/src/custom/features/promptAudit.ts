import { h } from 'vue'

import type { CustomFeatureManifest } from '../registry'

const PromptAuditIcon = {
  render: () =>
    h(
      'svg',
      { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor', 'stroke-width': '1.5' },
      [
        h('path', {
          'stroke-linecap': 'round',
          'stroke-linejoin': 'round',
          d: 'M9 12.75L11.25 15 15 9.75m-3-7.036A11.959 11.959 0 013.598 6 11.99 11.99 0 003 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285z'
        })
      ]
    )
}

/** Prompt audit is registered independently so the host only owns placement. */
export const promptAuditFeature: CustomFeatureManifest = {
  id: 'prompt-audit',
  routes: [
    {
      path: '/admin/prompt-audit',
      name: 'AdminPromptAudit',
      component: () => import('@/features/prompt-audit/PromptAuditView.vue'),
      meta: {
        requiresAuth: true,
        requiredPermission: 'promptAudit',
        title: 'Prompt Audit',
        titleKey: 'admin.promptAudit.title',
        descriptionKey: 'admin.promptAudit.description'
      }
    }
  ],
  menuItems: [
    {
      id: 'prompt-audit',
      path: '/admin/prompt-audit',
      parentPath: '/admin/security-audit',
      labelKey: 'nav.promptAudit',
      icon: PromptAuditIcon,
      permission: 'promptAudit'
    }
  ],
  permissions: ['promptAudit']
}
