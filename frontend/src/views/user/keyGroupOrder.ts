import { GROUP_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { Group } from '@/types'

const platformOrder = new Map(
  GROUP_PLATFORM_OPTIONS.map((option, index) => [option.value, index])
)

const groupNameCollator = new Intl.Collator(['zh-CN', 'en'], {
  numeric: true,
  sensitivity: 'base'
})

/**
 * Keep API-key group selectors stable and easy to scan: platforms stay
 * together in the shared catalog order, then group names are sorted within
 * each platform. The source array is never mutated.
 */
export function sortGroupsForKeySelection(groups: readonly Group[]): Group[] {
  return [...groups].sort((left, right) => {
    const leftPlatform = platformOrder.get(left.platform) ?? Number.MAX_SAFE_INTEGER
    const rightPlatform = platformOrder.get(right.platform) ?? Number.MAX_SAFE_INTEGER
    if (leftPlatform !== rightPlatform) return leftPlatform - rightPlatform

    const byName = groupNameCollator.compare(left.name, right.name)
    if (byName !== 0) return byName
    return left.id - right.id
  })
}
