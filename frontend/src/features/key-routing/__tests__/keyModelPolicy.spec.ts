import { describe, expect, it } from 'vitest'
import { cloneKeyModelPolicy, normalizeKeyModelPolicy, validateKeyModelPolicy } from '../keyModelPolicy'

describe('exact key model policy', () => {
  it('validates limits without treating empty enabled policy as unrestricted', () => {
    expect(validateKeyModelPolicy(null)).toBe(true)
    expect(validateKeyModelPolicy({ enabled: false, models: [] })).toBe(true)
    for (const models of [[], [''], [' '], ['gpt-*'], ['a\nb'], ['a'.repeat(201)], Array(201).fill('model')]) {
      expect(validateKeyModelPolicy({ enabled: true, models })).toBe(false)
    }
    expect(validateKeyModelPolicy({ enabled: true, models: ['模'.repeat(200)] })).toBe(true)
    expect(validateKeyModelPolicy({ enabled: true, models: Array.from({ length: 200 }, (_, id) => `model-${id}`) })).toBe(true)
  })

  it('trims and deduplicates without case folding, wildcards or alias conversion', () => {
    expect(normalizeKeyModelPolicy({ enabled: true, models: [' model-a ', 'model-a', 'MODEL-A', 'models/model-a'] }))
      .toEqual({ enabled: true, models: ['model-a', 'MODEL-A', 'models/model-a'] })
    expect(normalizeKeyModelPolicy({ enabled: false, models: ['draft'] })).toBeNull()
  })

  it('copies edit drafts and never mutates the original key', () => {
    const original = { enabled: true, models: ['a'] }
    const draft = cloneKeyModelPolicy(original)!
    draft.models[0] = 'b'
    expect(original.models).toEqual(['a'])
    expect(cloneKeyModelPolicy(undefined)).toBeNull()
  })
})
