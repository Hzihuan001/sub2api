import type { Group } from '@/types'

export const MAX_BACKUP_GROUPS = 4

export type KeyRoutingGroup = Pick<Group, 'id' | 'name' | 'status' | 'subscription_type' | 'claude_code_only'>
export type KeyRoutingError = 'tooMany' | 'primaryRequired' | 'groupUnavailable' | 'standardOnly' | 'claudeCodeOnly' | 'duplicate'

/** A single group retains the existing subscription and Claude Code-only behavior. */
export function validateKeyRouting(
  primaryGroupId: number | null,
  backupGroupIds: readonly number[],
  groups: readonly KeyRoutingGroup[]
): KeyRoutingError | null {
  if (backupGroupIds.length === 0) return null
  if (backupGroupIds.length > MAX_BACKUP_GROUPS) return 'tooMany'
  if (primaryGroupId === null) return 'primaryRequired'
  const routeIds = [primaryGroupId, ...backupGroupIds]
  if (new Set(routeIds).size !== routeIds.length) return 'duplicate'
  for (const id of routeIds) {
    const group = groups.find((item) => item.id === id)
    if (!group || group.status !== 'active') return 'groupUnavailable'
    if (group.subscription_type !== 'standard') return 'standardOnly'
    if (group.claude_code_only) return 'claudeCodeOnly'
  }
  return null
}

export function availableBackupGroups<T extends KeyRoutingGroup>(
  groups: readonly T[],
  primaryGroupId: number | null,
  backupGroupIds: readonly number[]
): T[] {
  return groups.filter((group) => group.status === 'active' &&
    group.subscription_type === 'standard' &&
    !group.claude_code_only &&
    group.id !== primaryGroupId && !backupGroupIds.includes(group.id))
}

export function moveBackupGroup(ids: readonly number[], index: number, direction: -1 | 1): number[] {
  const next = [...ids]
  const destination = index + direction
  if (index < 0 || index >= next.length || destination < 0 || destination >= next.length) return next
  ;[next[index], next[destination]] = [next[destination], next[index]]
  return next
}
