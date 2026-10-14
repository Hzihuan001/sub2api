import { describe, expect, it } from 'vitest'
import {
  createCustomFeatureRegistry,
  getCustomFeatureContributions,
  getCustomMenuItems,
  getCustomPermissions,
  getCustomRoutes,
  getCustomSettingsTabs,
  type CustomFeatureManifest
} from '../registry'

const feature = (id: string, overrides: Partial<CustomFeatureManifest> = {}): CustomFeatureManifest => ({
  id,
  ...overrides
})

describe('custom feature registry', () => {
  it('returns features and contributions in deterministic id order', () => {
    const registry = createCustomFeatureRegistry()
    registry.registerMany([
      feature('z-feature', {
        permissions: ['z.read', 'shared.read'],
        menuItems: [{ id: 'z-menu', path: '/z', labelKey: 'z' }]
      }),
      feature('a-feature', {
        permissions: ['a.read', 'shared.read'],
        menuItems: [{ id: 'a-menu', path: '/a', labelKey: 'a' }]
      })
    ])

    expect(registry.features().map((item) => item.id)).toEqual(['a-feature', 'z-feature'])
    expect(registry.menuItems().map((item) => item.id)).toEqual(['a-menu', 'z-menu'])
    expect(registry.permissions()).toEqual(['a.read', 'shared.read', 'z.read'])
  })

  it('rejects blank and duplicate feature IDs without replacing the original', () => {
    const registry = createCustomFeatureRegistry()
    expect(() => registry.register(feature('  '))).toThrow('id must not be empty')

    registry.register(feature('image-studio', { i18nNamespace: 'custom.imageStudio' }))
    expect(() => registry.register(feature(' image-studio '))).toThrow('already registered')
    expect(registry.get('image-studio')?.i18nNamespace).toBe('custom.imageStudio')
  })

  it('does not partially register a batch when one ID conflicts', () => {
    const registry = createCustomFeatureRegistry()
    registry.register(feature('existing'))

    expect(() => registry.registerMany([feature('new'), feature('existing')])).toThrow('already registered')
    expect(registry.has('new')).toBe(false)
    expect(registry.features().map((item) => item.id)).toEqual(['existing'])
  })

  it('aggregates routes and settings tabs while preserving each feature order', () => {
    const registry = createCustomFeatureRegistry()
    const component = {}
    registry.registerMany([
      feature('feature-b', {
        routes: [{ path: '/b', component }],
        settingsTabs: [{ id: 'b-settings', labelKey: 'b', component }]
      }),
      feature('feature-a', {
        routes: [{ path: '/a', component }],
        settingsTabs: [{ id: 'a-settings', labelKey: 'a', component }]
      })
    ])

    expect(registry.routes().map((route) => route.path)).toEqual(['/a', '/b'])
    expect(registry.settingsTabs().map((tab) => tab.id)).toEqual(['a-settings', 'b-settings'])
  })

  it('exposes host adapters without requiring host surfaces to know registry internals', () => {
    const registry = createCustomFeatureRegistry()
    const route = { path: '/image-studio', component: {} }
    const menuItem = { id: 'image-studio', path: '/image-studio', labelKey: 'imageStudio' }
    const settingsTab = { id: 'image-settings', labelKey: 'imageSettings', component: {} }
    registry.register(feature('image', {
      routes: [route],
      menuItems: [menuItem],
      settingsTabs: [settingsTab],
      permissions: ['image.read']
    }))

    expect(getCustomFeatureContributions(registry)).toEqual({
      routes: [route],
      menuItems: [menuItem],
      settingsTabs: [settingsTab],
      permissions: ['image.read']
    })
    expect(getCustomRoutes(registry)).toEqual([route])
    expect(getCustomMenuItems(registry)).toEqual([menuItem])
    expect(getCustomSettingsTabs(registry)).toEqual([settingsTab])
    expect(getCustomPermissions(registry)).toEqual(['image.read'])
  })

  it('supports unregistering and clearing an isolated registry', () => {
    const registry = createCustomFeatureRegistry()
    registry.registerMany([feature('one'), feature('two')])
    expect(registry.unregister(' one ')).toBe(true)
    expect(registry.has('one')).toBe(false)
    expect(registry.unregister('missing')).toBe(false)
    registry.clear()
    expect(registry.features()).toEqual([])
  })

  it.each([
    ['route path', { routes: [{ path: '/same', component: {} }] }, { routes: [{ path: '/same/', component: {} }] }],
    ['route name', { routes: [{ path: '/one', name: 'Same', component: {} }] }, { routes: [{ path: '/two', name: 'Same', component: {} }] }],
    ['menu id', { menuItems: [{ id: 'same', path: '/one', labelKey: 'one' }] }, { menuItems: [{ id: 'same', path: '/two', labelKey: 'two' }] }],
    ['menu path', { menuItems: [{ id: 'one', path: '/same', labelKey: 'one' }] }, { menuItems: [{ id: 'two', path: '/same', labelKey: 'two' }] }],
    ['settings tab id', { settingsTabs: [{ id: 'same', labelKey: 'one', component: {} }] }, { settingsTabs: [{ id: 'same', labelKey: 'two', component: {} }] }],
  ] satisfies [string, Partial<CustomFeatureManifest>, Partial<CustomFeatureManifest>][])(
    'rejects duplicate %s atomically across features and batches', (kind, first, conflicting) => {
      const registry = createCustomFeatureRegistry()
      registry.register(feature('existing', first))
      expect(() => registry.register(feature('conflict', conflicting))).toThrow(`Duplicate custom ${kind}`)
      expect(() => registry.registerMany([feature('unrelated'), feature('conflict', conflicting)])).toThrow(`Duplicate custom ${kind}`)
      expect(registry.features().map((item) => item.id)).toEqual(['existing'])
      const fresh = createCustomFeatureRegistry()
      expect(() => fresh.registerMany([feature('one', first), feature('two', conflicting)])).toThrow(`Duplicate custom ${kind}`)
      expect(fresh.features()).toEqual([])
    }
  )

  it('checks nested route names, resolved paths and aliases', () => {
    const registry = createCustomFeatureRegistry()
    registry.register(feature('parent', { routes: [{
      path: '/parent', component: {}, children: [
        { path: '', name: 'ParentIndex', component: {} },
        { path: 'child', name: 'NestedName', alias: '/alternate', component: {} }
      ]
    }] }))
    for (const route of [
      { path: '/parent/child', component: {} },
      { path: '/alternate', component: {} },
      { path: '/other', name: 'NestedName', component: {} }
    ]) expect(() => registry.register(feature('conflict', { routes: [route] }))).toThrow('Duplicate custom route')
    expect(registry.has('conflict')).toBe(false)
  })

  it('checks nested menu resources and rejects empty contribution keys', () => {
    const registry = createCustomFeatureRegistry()
    registry.register(feature('parent', { menuItems: [{
      id: 'parent', path: '/parent', labelKey: 'parent', children: [{ id: 'child', path: '/child', labelKey: 'child' }]
    }] }))
    expect(() => registry.register(feature('conflict', { menuItems: [{ id: 'child', path: '/other', labelKey: 'other' }] }))).toThrow('Duplicate custom menu id')
    expect(() => registry.register(feature('blank', { settingsTabs: [{ id: ' ', labelKey: 'test', component: {} }] }))).toThrow('must not be empty')
    expect(() => registry.register(feature('blank', { menuItems: [{ id: 'test', path: '/test', labelKey: ' ' }] }))).toThrow('must not be empty')
  })
})
