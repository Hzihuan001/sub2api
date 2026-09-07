export interface BatchResult { id: number; name: string; success: boolean; error?: string }

// Bounded, sequential writes: preserve successful items and retry only failures.
export async function runResellerBatch<T extends { id: number }>(
  items: readonly T[], name: (item: T) => string, action: (item: T) => Promise<unknown>
): Promise<BatchResult[]> {
  const results: BatchResult[] = []
  const seen = new Set<number>()
  for (const item of [...items]) {
    if (seen.has(item.id)) continue
    seen.add(item.id)
    try {
      await action(item)
      results.push({ id: item.id, name: name(item), success: true })
    } catch (error) {
      results.push({ id: item.id, name: name(item), success: false, error: (error as { message?: string })?.message || '操作失败' })
    }
  }
  return results
}
