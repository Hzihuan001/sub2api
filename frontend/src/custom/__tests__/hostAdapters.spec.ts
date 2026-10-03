import { describe, expect, it } from 'vitest'
import {
  createCustomFeatureRegistry,
  getHostCustomMenuItems,
  mapCustomMenuItems,
  mergeCustomRoutes
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
})
