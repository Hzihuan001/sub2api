import type { RouteRecordRaw } from 'vue-router'

import {
  assertCustomRouteResources,
  customFeatureRegistry,
  getCustomMenuItems,
  getCustomRoutes,
  getCustomSettingsTabs,
  type CustomFeatureRegistry,
  type CustomMenuItem
} from './registry'
import type { CustomSettingsTab } from './registry'

/** Minimal view model consumed by host navigation surfaces. */
export interface HostCustomNavItem {
  path: string
  label: string
  parentPath?: string
  beforePath?: string
  afterPath?: string
  icon: unknown
  iconSvg?: string
  featureFlag?: () => boolean | undefined
  permission?: string
  adminOnly?: boolean
  hideInSimpleMode?: boolean
  children?: HostCustomNavItem[]
}

/**
 * Inject custom routes immediately before the host catch-all route. Returning
 * the original array for an empty registry preserves existing route behavior.
 */
export function mergeCustomRoutes(
  baseRoutes: readonly RouteRecordRaw[],
  registry: CustomFeatureRegistry = customFeatureRegistry
): readonly RouteRecordRaw[] {
  const customRoutes = getCustomRoutes(registry)
  if (customRoutes.length === 0) return baseRoutes
  assertCustomRouteResources(customRoutes, baseRoutes)

  const catchAllIndex = baseRoutes.findIndex(
    (route) => route.name === 'NotFound' || route.path === '/:pathMatch(.*)*'
  )
  const insertionIndex = catchAllIndex >= 0 ? catchAllIndex : baseRoutes.length
  return [
    ...baseRoutes.slice(0, insertionIndex),
    ...customRoutes,
    ...baseRoutes.slice(insertionIndex)
  ]
}

function sortMenuItems(items: readonly CustomMenuItem[]): CustomMenuItem[] {
  return [...items].sort((left, right) => {
    const leftOrder = left.order ?? Number.POSITIVE_INFINITY
    const rightOrder = right.order ?? Number.POSITIVE_INFINITY
    if (leftOrder !== rightOrder) return leftOrder - rightOrder
    return left.id.localeCompare(right.id)
  })
}

/** Convert registry menu declarations using the host's translation function. */
export function mapCustomMenuItems(
  items: readonly CustomMenuItem[],
  translate: (labelKey: string) => string
): HostCustomNavItem[] {
  const mapItem = (item: CustomMenuItem): HostCustomNavItem => ({
    path: item.path,
    label: translate(item.labelKey),
    parentPath: item.parentPath,
    beforePath: item.beforePath,
    afterPath: item.afterPath,
    icon: item.icon ?? null,
    iconSvg: item.iconSvg,
    featureFlag: item.featureFlag,
    permission: item.permission,
    adminOnly: item.adminOnly,
    hideInSimpleMode: item.hideInSimpleMode,
    children: item.children ? mapCustomMenuItems(item.children, translate) : undefined
  })

  return sortMenuItems(items).map(mapItem)
}

/** Convenience wrapper for host surfaces that only need registry menus. */
export function getHostCustomMenuItems(
  translate: (labelKey: string) => string,
  registry: CustomFeatureRegistry = customFeatureRegistry
): HostCustomNavItem[] {
  return mapCustomMenuItems(getCustomMenuItems(registry), translate)
}

interface MenuPlacement<T> {
  path: string
  parentPath?: string
  beforePath?: string
  afterPath?: string
  children?: T[]
}

/** Place feature menus without teaching the host individual feature paths. */
export function placeCustomMenuItems<T extends MenuPlacement<T>>(
  baseItems: readonly T[], contributions: readonly T[]
): T[] {
  const copy = (items: readonly T[]): T[] => items.map((item) => ({
    ...item, ...(item.children ? { children: copy(item.children) } : {})
  }))
  const result = copy(baseItems)
  const find = (items: T[], path: string): T | undefined => {
    for (const item of items) {
      if (item.path === path) return item
      const nested = item.children && find(item.children, path)
      if (nested) return nested
    }
    return undefined
  }
  const afterTails = new Map<string, string>()
  for (const item of copy(contributions)) {
    if (find(result, item.path)) throw new Error(`Custom menu conflicts with host: ${item.path}`)
    const parent = item.parentPath ? find(result, item.parentPath) : undefined
    if (item.parentPath && !parent) throw new Error(`Custom menu parent not found: ${item.parentPath}`)
    const siblings = parent ? (parent.children ??= []) : result
    const anchorKey = `${item.parentPath ?? ''}:${item.afterPath ?? ''}`
    const anchor = item.beforePath ?? (item.afterPath && (afterTails.get(anchorKey) ?? item.afterPath))
    const anchorIndex = anchor ? siblings.findIndex((sibling) => sibling.path === anchor) : -1
    if (anchor && anchorIndex < 0) throw new Error(`Custom menu anchor not found: ${anchor}`)
    const insertionIndex = anchorIndex < 0 ? siblings.length : anchorIndex + (item.afterPath ? 1 : 0)
    siblings.splice(insertionIndex, 0, item)
    if (item.afterPath) afterTails.set(anchorKey, item.path)
  }
  return result
}

/**
 * Return settings contributions in host display order without exposing the
 * registry implementation to SettingsView. A fresh array is returned so the
 * host may filter or sort it without mutating feature manifests.
 */
export function getHostCustomSettingsTabs(
  registry: CustomFeatureRegistry = customFeatureRegistry,
  reservedIds: readonly string[] = []
): CustomSettingsTab[] {
  const tabs = [...getCustomSettingsTabs(registry)]
  for (const tab of tabs) {
    if (reservedIds.includes(tab.id.trim())) throw new Error(`Custom settings tab conflicts with host: ${tab.id}`)
  }
  return tabs.sort((left, right) => {
    const orderDiff = (left.order ?? Number.POSITIVE_INFINITY) - (right.order ?? Number.POSITIVE_INFINITY)
    return orderDiff || left.id.localeCompare(right.id)
  })
}
