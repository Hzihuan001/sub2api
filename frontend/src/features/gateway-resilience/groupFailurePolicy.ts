import { apiClient } from '@/api/client'

/** One status-code rule shared by every code listed in status_codes. */
export interface GroupFailureRule {
  status_codes: number[]
  threshold: number
  window_seconds: number
  cooldown_seconds: number
}

export interface GroupFailurePolicy {
  rules: GroupFailureRule[]
}

/** Values used when an administrator adds a new rule. */
export const ruleDefaults = {
  threshold: 3,
  window_seconds: 60,
  cooldown_seconds: 600
} as const

export const ruleLimits = {
  status_code: { min: 100, max: 599 },
  status_codes: { max: 20 },
  threshold: { min: 1, max: 100 },
  window_seconds: { min: 1, max: 3600 },
  cooldown_seconds: { min: 1, max: 7200 }
} as const

export function isValidStatusCode(value: unknown): value is number {
  return Number.isInteger(value) && Number(value) >= ruleLimits.status_code.min && Number(value) <= ruleLimits.status_code.max
}

export function isValidRule(rule: GroupFailureRule): boolean {
  return Array.isArray(rule.status_codes)
    && rule.status_codes.length > 0
    && rule.status_codes.length <= ruleLimits.status_codes.max
    && new Set(rule.status_codes).size === rule.status_codes.length
    && rule.status_codes.every(isValidStatusCode)
    && Number.isInteger(rule.threshold) && rule.threshold >= ruleLimits.threshold.min && rule.threshold <= ruleLimits.threshold.max
    && Number.isInteger(rule.window_seconds) && rule.window_seconds >= ruleLimits.window_seconds.min && rule.window_seconds <= ruleLimits.window_seconds.max
    && Number.isInteger(rule.cooldown_seconds) && rule.cooldown_seconds >= ruleLimits.cooldown_seconds.min && rule.cooldown_seconds <= ruleLimits.cooldown_seconds.max
}

/** Validate the response before it reaches the editor. Duplicate codes are rejected. */
export function parseGroupFailurePolicy(value: unknown): GroupFailurePolicy {
  if (!value || typeof value !== 'object' || !('rules' in value)) throw new Error('Invalid failure policy')
  const rawRules = (value as { rules?: unknown }).rules
  if (!Array.isArray(rawRules)) throw new Error('Invalid failure rules')
  const seen = new Set<number>()
  const result: GroupFailureRule[] = []
  for (const candidate of rawRules) {
    if (!candidate || typeof candidate !== 'object') throw new Error('Invalid failure rule')
    const raw = candidate as Partial<GroupFailureRule>
    const rule: GroupFailureRule = {
      status_codes: Array.isArray(raw.status_codes) ? raw.status_codes.map(Number) : [],
      threshold: Number(raw.threshold),
      window_seconds: Number(raw.window_seconds),
      cooldown_seconds: Number(raw.cooldown_seconds)
    }
    if (!isValidRule(rule)) throw new Error('Invalid failure rule values')
    for (const code of rule.status_codes) {
      if (seen.has(code)) throw new Error('Duplicate failure status code')
      seen.add(code)
    }
    result.push(rule)
  }
  return { rules: result }
}

export async function getGroupFailurePolicy(groupId: number, signal: AbortSignal): Promise<GroupFailurePolicy> {
  const { data } = await apiClient.get(`/admin/groups/${groupId}/failure-policy`, { signal })
  return parseGroupFailurePolicy(data)
}

export async function putGroupFailurePolicy(groupId: number, policy: GroupFailurePolicy, signal: AbortSignal): Promise<void> {
  await apiClient.put(`/admin/groups/${groupId}/failure-policy`, policy, { signal })
}
