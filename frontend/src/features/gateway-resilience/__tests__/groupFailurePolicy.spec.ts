import { describe, expect, it } from 'vitest'
import {
  isValidRule,
  isValidStatusCode,
  parseGroupFailurePolicy,
  ruleDefaults,
  type GroupFailureRule
} from '../groupFailurePolicy'

const rule = (overrides: Partial<GroupFailureRule> = {}): GroupFailureRule => ({
  status_codes: [502, 503],
  ...ruleDefaults,
  ...overrides
})

describe('group failure status-code policy contract', () => {
  it('accepts a rule with several status codes and the documented defaults', () => {
    expect(isValidRule(rule())).toBe(true)
    expect(ruleDefaults).toEqual({ threshold: 3, window_seconds: 60, cooldown_seconds: 600 })
    expect(isValidStatusCode(100)).toBe(true)
    expect(isValidStatusCode(599)).toBe(true)
  })

  it('rejects codes outside the HTTP status range and malformed rule values', () => {
    expect(isValidStatusCode(99)).toBe(false)
    expect(isValidStatusCode(600)).toBe(false)
    expect(isValidRule(rule({ status_codes: [502, 502] }))).toBe(false)
    expect(isValidRule(rule({ threshold: 0 }))).toBe(false)
    expect(isValidRule(rule({ window_seconds: 3601 }))).toBe(false)
    expect(isValidRule(rule({ cooldown_seconds: 7201 }))).toBe(false)
  })

  it('rejects a status code repeated across two rules', () => {
    expect(() => parseGroupFailurePolicy({ rules: [
      rule({ status_codes: [502, 503] }),
      rule({ status_codes: [504, 502] })
    ] })).toThrow('Duplicate failure status code')
  })

  it('keeps each rule independent when parsing a valid policy', () => {
    const policy = parseGroupFailurePolicy({ rules: [
      rule({ status_codes: [502, 503], threshold: 5, window_seconds: 60, cooldown_seconds: 600 }),
      rule({ status_codes: [429], threshold: 2, window_seconds: 30, cooldown_seconds: 120 })
    ] })
    expect(policy.rules).toHaveLength(2)
    expect(policy.rules[0]).toEqual({ status_codes: [502, 503], threshold: 5, window_seconds: 60, cooldown_seconds: 600 })
    expect(policy.rules[1]).toEqual({ status_codes: [429], threshold: 2, window_seconds: 30, cooldown_seconds: 120 })
  })

  it('fails closed for the legacy kind map and malformed payloads', () => {
    expect(() => parseGroupFailurePolicy({ rules: { overload: { threshold: 3, window_seconds: 60, cooldown_seconds: 600 } } })).toThrow('Invalid failure rules')
    expect(() => parseGroupFailurePolicy({ rules: [{ status_codes: [], ...ruleDefaults }] })).toThrow('Invalid failure rule values')
    expect(() => parseGroupFailurePolicy({ rules: [{ status_codes: [502], threshold: 3, window_seconds: 60, cooldown_seconds: 600, extra: true }] })).not.toThrow()
  })
})
