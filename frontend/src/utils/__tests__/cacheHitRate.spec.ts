import { describe, expect, it } from 'vitest'

import { cacheHitRatePercent, formatCacheHitRate } from '../cacheHitRate'

describe('cacheHitRate', () => {
  it('uses the complete normalized input context as the denominator', () => {
    const usage = {
      input_tokens: 1_107,
      cache_creation_tokens: 0,
      cache_read_tokens: 205_440,
    }

    expect(cacheHitRatePercent(usage)).toBeCloseTo(99.464, 3)
    expect(formatCacheHitRate(usage)).toBe('99.46%')
  })

  it('includes cache creation tokens in the complete input context', () => {
    expect(formatCacheHitRate({
      input_tokens: 100,
      cache_creation_tokens: 40,
      cache_read_tokens: 60,
    })).toBe('30.00%')
  })

  it('shows a reported cache creation with no read as a zero-percent hit', () => {
    expect(formatCacheHitRate({
      input_tokens: 100,
      cache_creation_tokens: 20,
      cache_read_tokens: 0,
    })).toBe('0.00%')
  })

  it('distinguishes historical rows without cache evidence from zero hits', () => {
    expect(cacheHitRatePercent({
      input_tokens: 100,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
    })).toBeNull()
    expect(formatCacheHitRate({
      input_tokens: 100,
      cache_creation_tokens: 0,
      cache_read_tokens: 0,
    })).toBe('--')
  })

  it('normalizes invalid token counts without producing NaN or infinity', () => {
    expect(formatCacheHitRate({
      input_tokens: Number.NaN,
      cache_creation_tokens: -1,
      cache_read_tokens: 10,
    })).toBe('100.00%')
  })
})
