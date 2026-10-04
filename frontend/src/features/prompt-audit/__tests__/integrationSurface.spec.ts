import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import { createCustomFeatureRegistry } from '@/custom/registry'
import { mergeCustomRoutes, mapCustomMenuItems } from '@/custom/hostAdapters'
import { promptAuditFeature } from '@/custom/features/promptAudit'

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')

describe('Prompt Audit integration surface', () => {
  it('registers an admin route that remains available when Guard risk control is off', () => {
    const registry = createCustomFeatureRegistry()
    registry.register(promptAuditFeature)
    const route = registry.routes().find((item) => item.path === '/admin/prompt-audit')
    expect(route).toBeDefined()
    expect(route?.meta).toMatchObject({ requiresAuth: true, requiredPermission: 'promptAudit' })
    expect(route?.meta).not.toHaveProperty('requiresRiskControl')
    const merged = mergeCustomRoutes([{ name: 'NotFound', path: '/:pathMatch(.*)*' }], registry)
    expect(merged.map((item) => item.path)).toEqual(['/admin/prompt-audit', '/:pathMatch(.*)*'])
  })

  it('keeps the legacy content moderation route and adds both pages under an expand-only security group', () => {
    const sidebar = read('../../../components/layout/AppSidebar.vue')
    expect(sidebar).toContain("path: '/admin/security-audit'")
    expect(sidebar).toContain("path: '/admin/risk-control'")
    expect(sidebar).toContain('registryAdminMenuItems')
    const menu = mapCustomMenuItems(promptAuditFeature.menuItems ?? [], (key) => key)
    expect(menu).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: '/admin/prompt-audit', parentPath: '/admin/security-audit', permission: 'promptAudit' })
    ]))
  })

  it('keeps Prompt Audit locale trees symmetric and all operational controls named', () => {
    expect(Object.keys(zh.admin.promptAudit)).toEqual(Object.keys(en.admin.promptAudit))
    expect(zh.nav.securityAudit).toBeTruthy()
    expect(en.nav.securityAudit).toBeTruthy()
    const endpoint = read('../components/EndpointPool.vue')
    const events = read('../components/EventWorkspace.vue')
    expect(endpoint).toContain('aria-label')
    expect(events).toContain('aria-label')
    expect(events).toContain('overflow-x-auto')
    expect(events).toContain('sm:grid-cols-2')
    const view = read('../PromptAuditView.vue')
    for (const mode of ['off', 'capture_only', 'async_audit', 'blocking']) {
      expect(view).toContain(`value: '${mode}'`)
    }
  })
})
