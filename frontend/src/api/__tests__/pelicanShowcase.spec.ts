import { describe, expect, it, vi } from 'vitest'

const get = vi.hoisted(() => vi.fn())

vi.mock('../client', () => ({
  apiClient: { get },
}))

import { getPelicanShowcase } from '../pelicanShowcase'

describe('pelican showcase API', () => {
  it('loads group-scoped showcase data through the authenticated panel endpoint', async () => {
    const data = { groups: [{ id: 7, name: 'GPT', platform: 'openai', results: [], status: 'unknown', success_count: 0, failure_count: 0 }] }
    get.mockResolvedValueOnce({ data })
    const signal = new AbortController().signal

    await expect(getPelicanShowcase({ signal })).resolves.toEqual(data)
    expect(get).toHaveBeenCalledWith('/pelican-showcase', { signal })
  })
})
