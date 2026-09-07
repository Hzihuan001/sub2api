import { describe, expect, it, vi } from 'vitest'
import { runResellerBatch } from '../resellerBatch'

describe('reseller batch writes', () => {
  it('deduplicates, continues after failure and supports failure-only retry', async () => {
    const action = vi.fn(async ({ id }: { id: number }) => { if (id === 2) throw new Error('insufficient price') })
    const result = await runResellerBatch([{ id: 1 }, { id: 2 }, { id: 1 }, { id: 3 }], p => String(p.id), action)
    expect(action.mock.calls.map(([p]) => p.id)).toEqual([1, 2, 3])
    expect(result.map(r => r.success)).toEqual([true, false, true])
    expect(result[1].error).toBe('insufficient price')
    const retry = vi.fn(async () => {})
    await runResellerBatch(result.filter(r => !r.success), p => p.name, retry)
    expect(retry).toHaveBeenCalledTimes(1)
    expect(retry).toHaveBeenCalledWith(expect.objectContaining({ id: 2 }))
  })
  it('never runs concurrent writes', async () => {
    let active = 0
    await runResellerBatch([{ id: 1 }, { id: 2 }], p => String(p.id), async () => {
      active++
      expect(active).toBe(1)
      await Promise.resolve()
      active--
    })
  })
})
