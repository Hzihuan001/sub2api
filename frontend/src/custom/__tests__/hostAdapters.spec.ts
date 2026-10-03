import { describe, expect, it } from 'vitest'
import {
  createCustomFeatureRegistry,
  getHostCustomMenuItems,
  imageStudioFeature,
  mapCustomMenuItems,
  mergeCustomRoutes,
  promptAuditFeature
} from '../index'

describe('custom host adapters', () => {
  it('keeps the base route array unchanged when the registry is empty', () => {
    const base = [
      { path: '/home', component: {} },
      { path: '/:pathMatch(.*)*', name: 'NotFound', component: {} }
    ]
    const registry = createCustomFeatureRegistry()
    expect(mergeCustomRoutes(base, registry)).toBe(base)
  })

  it('inserts custom routes before the catch-all route', () => {
    const base = [
      { path: '/home', component: {} },
      { path: '/:pathMatch(.*)*', name: 'NotFound', component: {} }
    ]
    const registry = createCustomFeatureRegistry()
    registry.register({ id: 'image-studio', routes: [{ path: '/custom-image', component: {} }] })
    expect(mergeCustomRoutes(base, registry).map((route) => route.path)).toEqual([
      '/home',
      '/custom-image',
      '/:pathMatch(.*)*'
    ])
  })

  it('maps and orders menu declarations without coupling to i18n', () => {
    const items = mapCustomMenuItems(
      [
        { id: 'z', path: '/z', labelKey: 'z.label' },
        { id: 'a', path: '/a', labelKey: 'a.label', order: 1, permission: 'users' }
      ],
      (key) => `translated:${key}`
    )
    expect(items.map((item) => [item.path, item.label, item.permission])).toEqual([
      ['/a', 'translated:a.label', 'users'],
      ['/z', 'translated:z.label', undefined]
    ])
  })

  it('returns registry menus through the host convenience adapter', () => {
    const registry = createCustomFeatureRegistry()
    registry.register({ id: 'image', menuItems: [{ id: 'image', path: '/image', labelKey: 'image' }] })
    expect(getHostCustomMenuItems((key) => key.toUpperCase(), registry)).toEqual([
      expect.objectContaining({ path: '/image', label: 'IMAGE' })
    ])
  })

  it('keeps built-in feature routes and menu paths stable', () => {
    expect(imageStudioFeature.routes?.[0]).toEqual(
      expect.objectContaining({ path: '/image-studio', name: 'ImageStudio' })
    )
    expect(imageStudioFeature.menuItems?.[0]).toEqual(
      expect.objectContaining({ path: '/image-studio', hideInSimpleMode: true })
    )
    expect(promptAuditFeature.routes?.[0]).toEqual(
      expect.objectContaining({ path: '/admin/prompt-audit', name: 'AdminPromptAudit' })
    )
    expect(promptAuditFeature.menuItems?.[0]).toEqual(
      expect.objectContaining({ path: '/admin/prompt-audit', parentPath: '/admin/security-audit' })
    )
  })
})
