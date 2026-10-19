import type { Group, KeyRouteFailurePolicy, KeyRouteFailureRule, KeyRouteFailureRuleType } from '@/types'

export type { KeyRouteFailurePolicy, KeyRouteFailureRule, KeyRouteFailureRuleType }

export const MAX_BACKUP_GROUPS = 4

export const KEY_ROUTE_RULE_DEFAULTS = {
  threshold: 3,
  window_seconds: 60,
  cooldown_seconds: 600
} as const

export const KEY_ROUTE_USER_RULE_DEFAULTS = {
  threshold: 3,
  sample_size: 20,
  cooldown_seconds: 120
} as const

export const KEY_ROUTE_RULE_TYPES: readonly KeyRouteFailureRuleType[] = ['ttft', 'sla_success_rate', 'consecutive_failures', 'timeout', 'rate_limit', 'status_code']

export const KEY_ROUTE_RULE_LIMITS = {
  rules: { max: 20 },
  status_code: { min: 100, max: 599 },
  status_codes: { max: 20 },
  threshold: { min: 1, max: 100 },
  sample_size: { min: 1, max: 100 },
  first_content_seconds: { min: 5, max: 3600 },
  window_seconds: { min: 1, max: 3600 },
  cooldown_seconds: { min: 1, max: 7200 }
} as const

export function parseKeyRouteStatusCodes(value: string): number[] | null {
  const tokens = value.split(/[\s,，]+/).map((token) => token.trim()).filter(Boolean)
  if (!tokens.length || tokens.length > KEY_ROUTE_RULE_LIMITS.status_codes.max) return null
  const codes = tokens.map(Number)
  if (codes.some((code) => !Number.isInteger(code) || code < KEY_ROUTE_RULE_LIMITS.status_code.min || code > KEY_ROUTE_RULE_LIMITS.status_code.max)) return null
  return new Set(codes).size === codes.length ? codes : null
}

export function isValidKeyRouteFailureRule(rule: KeyRouteFailureRule): boolean {
  const type = rule.type ?? 'status_code'
  if (!KEY_ROUTE_RULE_TYPES.includes(type)
    || !Number.isInteger(rule.threshold) || rule.threshold < KEY_ROUTE_RULE_LIMITS.threshold.min || rule.threshold > KEY_ROUTE_RULE_LIMITS.threshold.max
    || !Number.isInteger(rule.cooldown_seconds) || rule.cooldown_seconds < KEY_ROUTE_RULE_LIMITS.cooldown_seconds.min || rule.cooldown_seconds > KEY_ROUTE_RULE_LIMITS.cooldown_seconds.max) return false
  if (type !== 'ttft' && rule.first_content_seconds !== undefined) return false
  if (type === 'consecutive_failures') return rule.sample_size === undefined && rule.window_seconds === undefined && rule.status_codes === undefined
  if (type === 'timeout' || type === 'rate_limit' || type === 'sla_success_rate' || type === 'ttft') {
    return rule.window_seconds === undefined && rule.status_codes === undefined
      && typeof rule.sample_size === 'number'
      && Number.isInteger(rule.sample_size) && rule.sample_size >= KEY_ROUTE_RULE_LIMITS.sample_size.min && rule.sample_size <= KEY_ROUTE_RULE_LIMITS.sample_size.max
      && (type === 'sla_success_rate' || rule.threshold <= rule.sample_size)
      && (type !== 'ttft' || (typeof rule.first_content_seconds === 'number' && Number.isInteger(rule.first_content_seconds) && rule.first_content_seconds >= 5 && rule.first_content_seconds <= 3600))
  }
  return rule.sample_size === undefined && Array.isArray(rule.status_codes)
    && rule.status_codes.length > 0
    && rule.status_codes.length <= KEY_ROUTE_RULE_LIMITS.status_codes.max
    && new Set(rule.status_codes).size === rule.status_codes.length
    && rule.status_codes.every((code) => Number.isInteger(code) && code >= KEY_ROUTE_RULE_LIMITS.status_code.min && code <= KEY_ROUTE_RULE_LIMITS.status_code.max)
    && typeof rule.window_seconds === 'number'
    && Number.isInteger(rule.window_seconds) && rule.window_seconds >= KEY_ROUTE_RULE_LIMITS.window_seconds.min && rule.window_seconds <= KEY_ROUTE_RULE_LIMITS.window_seconds.max
}

export function validateKeyRouteFailurePolicy(policy: KeyRouteFailurePolicy | null): string | null {
  if (policy === null) return null
  if (!Array.isArray(policy.rules) || policy.rules.length > KEY_ROUTE_RULE_LIMITS.rules.max) return 'invalid'
  const seen = new Set<number>()
  const semanticTypes = new Set<KeyRouteFailureRuleType>()
  for (const rule of policy.rules) {
    if (!isValidKeyRouteFailureRule(rule)) return 'invalid'
    const type = rule.type ?? 'status_code'
    if (type !== 'status_code') {
      if (semanticTypes.has(type)) return 'duplicate'
      semanticTypes.add(type)
      continue
    }
    for (const code of rule.status_codes ?? []) {
      if (seen.has(code)) return 'duplicate'
      seen.add(code)
    }
  }
  return null
}

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
