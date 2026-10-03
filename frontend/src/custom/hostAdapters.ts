import type { RouteRecordRaw } from 'vue-router'

import {
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

/**
 * Return settings contributions in host display order without exposing the
 * registry implementation to SettingsView. A fresh array is returned so the
 * host may filter or sort it without mutating feature manifests.
 */
export function getHostCustomSettingsTabs(
  registry: CustomFeatureRegistry = customFeatureRegistry
): CustomSettingsTab[] {
  return [...getCustomSettingsTabs(registry)].sort((left, right) => {
    const orderDiff = (left.order ?? Number.POSITIVE_INFINITY) - (right.order ?? Number.POSITIVE_INFINITY)
    return orderDiff || left.id.localeCompare(right.id)
  })
}
