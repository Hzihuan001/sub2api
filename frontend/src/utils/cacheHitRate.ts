export interface CacheTokenUsage {
  input_tokens?: number | null
  cache_creation_tokens?: number | null
  cache_read_tokens?: number | null
}

const normalizedTokenCount = (value: number | null | undefined): number => {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) return 0
  return value
}

/**
 * Returns the cache hit percentage for Sub2API's normalized token fields.
 *
 * Sub2API stores uncached input, cache creation and cache reads separately, so
 * the complete input context is their sum. A null result means the historical
 * row has no cache evidence and must be displayed as unavailable rather than
 * as a zero-percent hit.
 */
export const cacheHitRatePercent = (usage: CacheTokenUsage): number | null => {
  const inputTokens = normalizedTokenCount(usage.input_tokens)
  const cacheCreationTokens = normalizedTokenCount(usage.cache_creation_tokens)
  const cacheReadTokens = normalizedTokenCount(usage.cache_read_tokens)

  if (cacheCreationTokens === 0 && cacheReadTokens === 0) return null

  const totalInputTokens = inputTokens + cacheCreationTokens + cacheReadTokens
  if (totalInputTokens === 0) return 0

  return Math.min(100, (cacheReadTokens / totalInputTokens) * 100)
}

export const formatCacheHitRate = (usage: CacheTokenUsage): string => {
  const percentage = cacheHitRatePercent(usage)
  return percentage == null ? '--' : `${percentage.toFixed(2)}%`
}
