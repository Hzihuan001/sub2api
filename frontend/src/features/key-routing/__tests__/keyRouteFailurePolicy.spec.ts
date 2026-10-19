import { describe, expect, it } from 'vitest'
import type { KeyRouteFailureRule } from '@/types'
import { isValidKeyRouteFailureRule, validateKeyRouteFailurePolicy } from '../keyRouting'

const statusRule = (overrides: Partial<KeyRouteFailureRule> = {}): KeyRouteFailureRule => ({ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600, ...overrides })
const sampleRule = (overrides: Partial<KeyRouteFailureRule> = {}): KeyRouteFailureRule => ({ type: 'timeout', threshold: 3, sample_size: 20, cooldown_seconds: 120, ...overrides })

describe('key route failure policy validation', () => {
  it('accepts legacy status rules, inheritance, and explicit disable', () => {
    expect(validateKeyRouteFailurePolicy(null)).toBeNull()
    expect(validateKeyRouteFailurePolicy({ rules: [] })).toBeNull()
    expect(isValidKeyRouteFailureRule(statusRule())).toBe(true)
    expect(isValidKeyRouteFailureRule(statusRule({ type: 'status_code' }))).toBe(true)
  })

  it('validates semantic rule field requirements independently', () => {
    expect(isValidKeyRouteFailureRule({ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120 })).toBe(true)
    expect(isValidKeyRouteFailureRule(sampleRule())).toBe(true)
    expect(isValidKeyRouteFailureRule(sampleRule({ type: 'rate_limit' }))).toBe(true)
    expect(isValidKeyRouteFailureRule(sampleRule({ sample_size: undefined }))).toBe(false)
    expect(isValidKeyRouteFailureRule(sampleRule({ sample_size: 2 }))).toBe(false)
    expect(isValidKeyRouteFailureRule(statusRule({ window_seconds: undefined }))).toBe(false)
    expect(isValidKeyRouteFailureRule(sampleRule({ type: 'unknown' as KeyRouteFailureRule['type'] }))).toBe(false)
  })

  it.each([0, 101, 1.5, Number.NaN])('rejects invalid threshold %s', (threshold) => {
    expect(isValidKeyRouteFailureRule({ type: 'consecutive_failures', threshold, cooldown_seconds: 120 })).toBe(false)
  })

  it.each([0, 101, 1.5, Number.NaN])('rejects invalid sample size %s', (sample_size) => {
    expect(isValidKeyRouteFailureRule(sampleRule({ threshold: 1, sample_size }))).toBe(false)
  })

  it('enforces numeric ranges and accepts boundaries', () => {
    expect(isValidKeyRouteFailureRule(sampleRule({ threshold: 100, sample_size: 100, cooldown_seconds: 7200 }))).toBe(true)
    expect(isValidKeyRouteFailureRule(sampleRule({ threshold: 1, sample_size: 1, cooldown_seconds: 1 }))).toBe(true)
    expect(isValidKeyRouteFailureRule(statusRule({ status_codes: [100, 599], threshold: 100, window_seconds: 3600, cooldown_seconds: 7200 }))).toBe(true)
    expect(isValidKeyRouteFailureRule(statusRule({ window_seconds: 3601 }))).toBe(false)
    expect(isValidKeyRouteFailureRule(sampleRule({ cooldown_seconds: 7201 }))).toBe(false)
    expect(isValidKeyRouteFailureRule(statusRule({ status_codes: [99, 600] }))).toBe(false)
  })

  it('forbids duplicate semantic types but allows distinct semantic and status rules', () => {
    expect(validateKeyRouteFailurePolicy({ rules: [sampleRule(), sampleRule()] })).toBe('duplicate')
    expect(validateKeyRouteFailurePolicy({ rules: [sampleRule(), sampleRule({ type: 'rate_limit' }), statusRule(), statusRule({ status_codes: [503] })] })).toBeNull()
    expect(validateKeyRouteFailurePolicy({ rules: [statusRule(), statusRule({ type: 'status_code', status_codes: [502, 503] })] })).toBe('duplicate')
  })

  it('limits the policy to twenty rules', () => {
    const rules = Array.from({ length: 20 }, (_, index) => statusRule({ status_codes: [400 + index] }))
    expect(validateKeyRouteFailurePolicy({ rules })).toBeNull()
    expect(validateKeyRouteFailurePolicy({ rules: [...rules, statusRule({ status_codes: [500] })] })).toBe('invalid')
  })

  it('validates TTFT wait and SLA percentages without mixing protocol fields', () => {
    const ttft = sampleRule({ type: 'ttft', first_content_seconds: 15 })
    const sla = sampleRule({ type: 'sla_success_rate', threshold: 95 })
    expect(isValidKeyRouteFailureRule(ttft)).toBe(true)
    expect(isValidKeyRouteFailureRule({ ...ttft, first_content_seconds: 5 })).toBe(true)
    expect(isValidKeyRouteFailureRule({ ...ttft, first_content_seconds: 3600 })).toBe(true)
    for (const first_content_seconds of [undefined, 0, 4, 3601, 5.5, Number.NaN]) {
      expect(isValidKeyRouteFailureRule({ ...ttft, first_content_seconds })).toBe(false)
    }
    expect(isValidKeyRouteFailureRule(sla)).toBe(true)
    expect(isValidKeyRouteFailureRule({ ...sla, threshold: 101 })).toBe(false)
    expect(isValidKeyRouteFailureRule({ ...sla, first_content_seconds: 15 })).toBe(false)
    expect(isValidKeyRouteFailureRule({ ...sla, window_seconds: 60 })).toBe(false)
    expect(isValidKeyRouteFailureRule(statusRule({ sample_size: 20 }))).toBe(false)
    expect(isValidKeyRouteFailureRule({ type: 'consecutive_failures', threshold: 3, cooldown_seconds: 120, sample_size: 20 })).toBe(false)
    expect(validateKeyRouteFailurePolicy({ rules: [ttft, sla] })).toBeNull()
    expect(validateKeyRouteFailurePolicy({ rules: [ttft, { ...ttft }] })).toBe('duplicate')
  })
})
