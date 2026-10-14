import type { Component } from 'vue'
import type { RouteRecordRaw } from 'vue-router'

export type CustomLocale = 'en' | 'zh'
export type CustomLocaleMessages = { [key: string]: string | CustomLocaleMessages }
/** Flat, exact message keys keep additions and approved overrides auditable. */
export type CustomMessageMap = Readonly<Record<string, string>>
export type CustomMessageLoader = (hostMessages: Readonly<CustomLocaleMessages>) => Promise<CustomMessageMap>

/**
 * A menu item supplied by a custom feature.
 *
 * The shape intentionally stays independent from the concrete sidebar
 * implementation.  The host application can map these fields to its own
 * navigation component without requiring feature packages to import it.
 */
export interface CustomMenuItem {
  id: string
  path: string
  labelKey: string
  /** Optional host menu path that should receive this item as a child. */
  parentPath?: string
  /** Stable sibling anchors in the host menu; use at most one. */
  beforePath?: string
  afterPath?: string
  icon?: unknown
  iconSvg?: string
  permission?: string
  /** Only render this item for administrators; operators never see it. */
  adminOnly?: boolean
  featureFlag?: () => boolean | undefined
  /** Hide this item when the host is in simple mode. */
  hideInSimpleMode?: boolean
  children?: readonly CustomMenuItem[]
  order?: number
}

/** A tab that a custom feature contributes to an existing settings surface. */
export interface CustomSettingsTab {
  id: string
  labelKey: string
  /** Receives readOnly; panel-owned save handlers must honor it. Server authorization remains required. */
  component: Component
  /** @deprecated Read-only compatibility alias; never grants write access. */
  permission?: string
  readPermission?: string
  writePermission?: string
  order?: number
}

/**
 * Manifest for one independently deployable custom feature.
 *
 * Feature IDs are the stable extension boundary.  They must be unique within
 * one registry and should not be changed after a feature has shipped.
 */
export interface CustomFeatureManifest {
  id: string
  routes?: readonly RouteRecordRaw[]
  menuItems?: readonly CustomMenuItem[]
  settingsTabs?: readonly CustomSettingsTab[]
  permissions?: readonly string[]
  i18nNamespace?: string
  messages?: Partial<Record<CustomLocale, CustomMessageLoader>>
  /** Exact existing host keys this feature may override; no wildcard or prefix overrides. */
  messageOverrides?: readonly string[]
}

/**
 * A stable snapshot shape for host integrations.
 *
 * Router, sidebar, and settings code can consume this object without knowing
 * how individual features are stored or registered. Every array is newly
 * allocated by the adapter, so callers may sort/filter their copy safely.
 */
export interface CustomFeatureContributions {
  routes: readonly RouteRecordRaw[]
  menuItems: readonly CustomMenuItem[]
  settingsTabs: readonly CustomSettingsTab[]
  permissions: readonly string[]
}

export interface CustomFeatureRegistry {
  register(manifest: CustomFeatureManifest): void
  registerMany(manifests: readonly CustomFeatureManifest[]): void
  unregister(id: string): boolean
  has(id: string): boolean
  get(id: string): CustomFeatureManifest | undefined
  features(): readonly CustomFeatureManifest[]
  routes(): readonly RouteRecordRaw[]
  menuItems(): readonly CustomMenuItem[]
  settingsTabs(): readonly CustomSettingsTab[]
  permissions(): readonly string[]
  clear(): void
}

function normalizeId(id: string): string {
  return id.trim()
}

function ensureManifest(manifest: CustomFeatureManifest): CustomFeatureManifest {
  const id = normalizeId(manifest.id)
  if (!id) throw new Error('Custom feature manifest id must not be empty')
  return {
    ...manifest,
    id,
    routes: manifest.routes ? [...manifest.routes] : [],
    menuItems: manifest.menuItems ? [...manifest.menuItems] : [],
    settingsTabs: manifest.settingsTabs ? [...manifest.settingsTabs] : [],
    messageOverrides: manifest.messageOverrides ? [...manifest.messageOverrides] : [],
    permissions: manifest.permissions ? [...manifest.permissions] : []
  }
}

function sortedFeatures(features: Iterable<CustomFeatureManifest>): CustomFeatureManifest[] {
  return [...features].sort((left, right) => left.id.localeCompare(right.id))
}

function requiredResourceKey(value: string, kind: string): string {
  const key = value.trim()
  if (!key) throw new Error(`Custom ${kind} must not be empty`)
  return key
}

function routePath(path: string, parent: string): string {
  const resolved = path.startsWith('/') ? path : `${parent}/${path}`
  return resolved.replace(/\/{2,}/g, '/').replace(/\/$/, '') || '/'
}

/** Check incoming routes against host resources without changing existing host routes. */
export function assertCustomRouteResources(
  routes: readonly RouteRecordRaw[],
  hostRoutes: readonly RouteRecordRaw[] = []
): void {
  const paths = new Set<string>()
  const names = new Set<string | symbol>()
  const visit = (records: readonly RouteRecordRaw[], parent: string, validate: boolean): void => {
    for (const route of records) {
      const path = routePath(route.path, parent)
      // A default child intentionally shares its parent's path in Vue Router.
      const claimedPaths = route.path === '' && parent ? [] : [path]
      const aliases = typeof route.alias === 'string' ? [route.alias] : route.alias ?? []
      claimedPaths.push(...aliases.map((alias) => routePath(alias, parent)))
      for (const resource of claimedPaths) {
        if (validate && paths.has(resource)) throw new Error(`Duplicate custom route path: ${resource}`)
        paths.add(resource)
      }
      if (route.name !== undefined) {
        if (validate && names.has(route.name)) throw new Error(`Duplicate custom route name: ${String(route.name)}`)
        names.add(route.name)
      }
      if (route.children) visit(route.children, path, validate)
    }
  }
  visit(hostRoutes, '', false)
  visit(routes, '', true)
}

function assertContributionResources(features: readonly CustomFeatureManifest[]): void {
  assertCustomRouteResources(features.flatMap((feature) => feature.routes ?? []))
  const menuIds = new Set<string>()
  const menuPaths = new Set<string>()
  const tabIds = new Set<string>()
  const claim = (keys: Set<string>, value: string, kind: string): void => {
    const key = requiredResourceKey(value, kind)
    if (keys.has(key)) throw new Error(`Duplicate custom ${kind}: ${key}`)
    keys.add(key)
  }
  const visitMenus = (items: readonly CustomMenuItem[]): void => {
    for (const item of items) {
      claim(menuIds, item.id, 'menu id')
      claim(menuPaths, item.path.replace(/\/$/, '') || '/', 'menu path')
      requiredResourceKey(item.labelKey, 'menu labelKey')
      if (item.beforePath && item.afterPath) throw new Error(`Custom menu has conflicting placement anchors: ${item.id}`)
      if (item.children) visitMenus(item.children)
    }
  }
  for (const feature of features) {
    visitMenus(feature.menuItems ?? [])
    for (const tab of feature.settingsTabs ?? []) {
      claim(tabIds, tab.id, 'settings tab id')
      requiredResourceKey(tab.labelKey, 'settings tab labelKey')
    }
  }
}

/**
 * Create an isolated registry.  Tests and embedders can create their own
 * instance; the default singleton below is used by the application.
 */
export function createCustomFeatureRegistry(): CustomFeatureRegistry {
  const entries = new Map<string, CustomFeatureManifest>()

  const ordered = (): CustomFeatureManifest[] => sortedFeatures(entries.values())

  return {
    register(manifest) {
      const normalized = ensureManifest(manifest)
      if (entries.has(normalized.id)) {
        throw new Error(`Custom feature already registered: ${normalized.id}`)
      }
      assertContributionResources([...entries.values(), normalized])
      entries.set(normalized.id, normalized)
    },

    registerMany(manifests) {
      // Validate the complete batch before mutating the registry.  A typo in
      // one feature must not leave a partially registered extension set.
      const normalized = manifests.map(ensureManifest)
      const batchIds = new Set<string>()
      for (const feature of normalized) {
        if (entries.has(feature.id) || batchIds.has(feature.id)) {
          throw new Error(`Custom feature already registered: ${feature.id}`)
        }
        batchIds.add(feature.id)
      }
      assertContributionResources([...entries.values(), ...normalized])
      for (const feature of normalized) entries.set(feature.id, feature)
    },

    unregister(id) {
      return entries.delete(normalizeId(id))
    },

    has(id) {
      return entries.has(normalizeId(id))
    },

    get(id) {
      return entries.get(normalizeId(id))
    },

    features() {
      return ordered()
    },

    routes() {
      return ordered().flatMap((feature) => feature.routes ?? [])
    },

    menuItems() {
      return ordered().flatMap((feature) => feature.menuItems ?? [])
    },

    settingsTabs() {
      return ordered().flatMap((feature) => feature.settingsTabs ?? [])
    },

    permissions() {
      const permissions = new Set<string>()
      for (const feature of ordered()) {
        for (const permission of feature.permissions ?? []) permissions.add(permission)
      }
      return [...permissions]
    },

    clear() {
      entries.clear()
    }
  }
}

/** Application-wide registry.  Feature packages register during bootstrap. */
export const customFeatureRegistry = createCustomFeatureRegistry()

/**
 * Return all contributions in the shape expected by host surfaces.
 *
 * The optional registry argument makes the adapter easy to test and lets an
 * embedded surface use a scoped registry while application code can omit it.
 */
export function getCustomFeatureContributions(
  registry: CustomFeatureRegistry = customFeatureRegistry
): CustomFeatureContributions {
  return {
    routes: registry.routes(),
    menuItems: registry.menuItems(),
    settingsTabs: registry.settingsTabs(),
    permissions: registry.permissions()
  }
}

/** Convenience adapters for incremental router/sidebar/settings adoption. */
export function getCustomRoutes(
  registry: CustomFeatureRegistry = customFeatureRegistry
): readonly RouteRecordRaw[] {
  return registry.routes()
}

export function getCustomMenuItems(
  registry: CustomFeatureRegistry = customFeatureRegistry
): readonly CustomMenuItem[] {
  return registry.menuItems()
}

export function getCustomSettingsTabs(
  registry: CustomFeatureRegistry = customFeatureRegistry
): readonly CustomSettingsTab[] {
  return registry.settingsTabs()
}

export function getCustomPermissions(
  registry: CustomFeatureRegistry = customFeatureRegistry
): readonly string[] {
  return registry.permissions()
}
