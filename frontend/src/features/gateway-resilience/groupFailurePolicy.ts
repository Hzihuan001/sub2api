import { apiClient } from '@/api/client'

export const failureKinds = ['overload', 'server_error', 'timeout', 'rate_limit'] as const
export type FailureKind = typeof failureKinds[number]

export interface GroupFailureRule {
  threshold: number
  window_seconds: number
  cooldown_seconds: number
}

export interface GroupFailurePolicy {
  rules: Partial<Record<FailureKind, GroupFailureRule>>
}

// Editor defaults are persisted explicitly on save, never by opening the editor.
// Keep the API compatible with older partial policies; missing kinds are filled
// only in the draft so an unsaved visit does not change runtime behavior.
export const ruleDefaults: Record<FailureKind, GroupFailureRule> = {
  overload: { threshold: 3, window_seconds: 300, cooldown_seconds: 600 },
  server_error: { threshold: 3, window_seconds: 60, cooldown_seconds: 60 },
  timeout: { threshold: 2, window_seconds: 60, cooldown_seconds: 60 },
  rate_limit: { threshold: 3, window_seconds: 60, cooldown_seconds: 60 }
}

export const ruleLimits = { threshold: 100, window_seconds: 3600, cooldown_seconds: 7200 } as const

export function isValidRule(rule: GroupFailureRule): boolean {
  return Object.entries(ruleLimits).every(([field, max]) => {
    const value = rule[field as keyof GroupFailureRule]
    return Number.isInteger(value) && value >= 1 && value <= max
  })
}

/** Refuse incomplete or newer payload shapes rather than silently overwriting them. */
export function parseGroupFailurePolicy(value: unknown): GroupFailurePolicy {
  if (!value || typeof value !== 'object' || !('rules' in value)) throw new Error('Invalid failure policy')
  const rules = value.rules
  if (!rules || typeof rules !== 'object' || Array.isArray(rules)) throw new Error('Invalid failure rules')
  const result: GroupFailurePolicy = { rules: {} }
  for (const [kind, candidate] of Object.entries(rules)) {
    if (!failureKinds.includes(kind as FailureKind) || !candidate || typeof candidate !== 'object') {
      throw new Error('Unsupported failure rule')
    }
    const rule = candidate as GroupFailureRule
    if (!isValidRule(rule)) throw new Error('Invalid failure rule values')
    result.rules[kind as FailureKind] = {
      threshold: rule.threshold,
      window_seconds: rule.window_seconds,
      cooldown_seconds: rule.cooldown_seconds
    }
  }
  return result
}

export async function getGroupFailurePolicy(groupId: number, signal: AbortSignal): Promise<GroupFailurePolicy> {
  const { data } = await apiClient.get(`/admin/groups/${groupId}/failure-policy`, { signal })
  return parseGroupFailurePolicy(data)
}

export async function putGroupFailurePolicy(groupId: number, policy: GroupFailurePolicy, signal: AbortSignal): Promise<void> {
  await apiClient.put(`/admin/groups/${groupId}/failure-policy`, policy, { signal })
}
