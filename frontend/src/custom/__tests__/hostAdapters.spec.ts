import { describe, expect, it } from 'vitest'
import {
  createCustomFeatureRegistry,
  getHostCustomMenuItems,
  getHostCustomSettingsTabs,
  imageStudioFeature,
  mapCustomMenuItems,
  mergeCustomRoutes,
  operatorRoleFeature,
  placeCustomMenuItems,
  promptAuditFeature,
  usageExtrasFeature
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
        { id: 'a', path: '/a', labelKey: 'a.label', order: 1, permission: 'users', adminOnly: true }
      ],
      (key) => `translated:${key}`
    )
    expect(items.map((item) => [item.path, item.label, item.permission, item.adminOnly])).toEqual([
      ['/a', 'translated:a.label', 'users', true],
      ['/z', 'translated:z.label', undefined, undefined]
    ])
  })

  it('returns registry menus through the host convenience adapter', () => {
    const registry = createCustomFeatureRegistry()
    registry.register({ id: 'image', menuItems: [{ id: 'image', path: '/image', labelKey: 'image' }] })
    expect(getHostCustomMenuItems((key) => key.toUpperCase(), registry)).toEqual([
      expect.objectContaining({ path: '/image', label: 'IMAGE' })
    ])
  })

  it('returns custom settings tabs in stable order without mutating registry output', () => {
    const registry = createCustomFeatureRegistry()
    const component = {}
    registry.register({
      id: 'settings-feature',
      settingsTabs: [
        { id: 'z-settings', labelKey: 'z', component },
        { id: 'a-settings', labelKey: 'a', component, order: 1 }
      ]
    })

    const tabs = getHostCustomSettingsTabs(registry)
    expect(tabs.map((tab) => tab.id)).toEqual(['a-settings', 'z-settings'])
    tabs.reverse()
    expect(getHostCustomSettingsTabs(registry).map((tab) => tab.id)).toEqual(['a-settings', 'z-settings'])
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
    expect(operatorRoleFeature.menuItems?.[0]).toEqual(
      expect.objectContaining({ path: '/admin/roles', adminOnly: true })
    )
    expect(usageExtrasFeature.routes?.[0]).toEqual(
      expect.objectContaining({ path: '/admin/usage', name: 'AdminUsage' })
    )
    expect(usageExtrasFeature.menuItems?.[0]).toEqual(
      expect.objectContaining({ path: '/admin/usage', permission: 'usage' })
    )
  })

  it.each([
    { path: '/host', component: {} },
    { path: '/custom', name: 'HostRoute', component: {} },
    { path: '/alias', component: {} }
  ])('rejects custom routes that shadow official host resources: $path', (route) => {
    const registry = createCustomFeatureRegistry()
    registry.register({ id: 'conflict', routes: [route] })
    const base = [{ path: '/host', name: 'HostRoute', alias: '/alias', component: {} }]
    expect(() => mergeCustomRoutes(base, registry)).toThrow('Duplicate custom route')
    expect(base).toHaveLength(1)
  })

  it.each(['general', ' general '])('fails on reserved settings id %s instead of silently hiding a contribution', (id) => {
    const registry = createCustomFeatureRegistry()
    registry.register({ id: 'conflict', settingsTabs: [{ id, labelKey: 'test', component: {} }] })
    expect(() => getHostCustomSettingsTabs(registry, ['general'])).toThrow('conflicts with host:')
  })

  it('places menu declarations by stable sibling and parent metadata without changing native nodes', () => {
    const base = [{ path: '/first' }, { path: '/parent', children: [{ path: '/native-child' }] }, { path: '/last' }]
    const result = placeCustomMenuItems(base, [
      { path: '/after-one', afterPath: '/first' },
      { path: '/after-two', afterPath: '/first' },
      { path: '/before', beforePath: '/last' },
      { path: '/child', parentPath: '/parent' }
    ])
    expect(result.map((item) => item.path)).toEqual(['/first', '/after-one', '/after-two', '/parent', '/before', '/last'])
    expect(result.find((item) => item.path === '/parent')?.children?.map((item) => item.path)).toEqual(['/native-child', '/child'])
    expect(base[1].children).toEqual([{ path: '/native-child' }])
  })

  it('fails rather than silently dropping a menu whose host anchor disappeared', () => {
    expect(() => placeCustomMenuItems([{ path: '/native' }], [{ path: '/probe', beforePath: '/missing' }])).toThrow('anchor not found')
    expect(() => placeCustomMenuItems([{ path: '/native' }], [{ path: '/probe', parentPath: '/missing' }])).toThrow('parent not found')
    expect(() => placeCustomMenuItems([{ path: '/native' }], [{ path: '/native' }])).toThrow('conflicts with host')
  })
})
