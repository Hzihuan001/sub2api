import { beforeEach, describe, expect, it, vi } from 'vitest'
import { create, update } from '../keys'

const { post, put } = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }))
vi.mock('../client', () => ({ apiClient: { post, put } }))

describe('API key routing payloads', () => {
  beforeEach(() => {
    post.mockReset().mockResolvedValue({ data: { id: 1 } })
    put.mockReset().mockResolvedValue({ data: { id: 1 } })
  })

  it('creates an ordinary key with no backups by default', async () => {
    await create('Single group', 2)
    expect(post).toHaveBeenCalledWith('/keys', { name: 'Single group', group_id: 2, backup_group_ids: [] })
  })

  it('preserves explicit backup priority order on create', async () => {
    await create('Route', 2, undefined, undefined, undefined, undefined, undefined, undefined, [5, 3])
    expect(post).toHaveBeenCalledWith('/keys', { name: 'Route', group_id: 2, backup_group_ids: [5, 3] })
  })

  it('distinguishes omitted backup updates from an explicit clear', async () => {
    await update(1, { name: 'Rename' })
    await update(1, { backup_group_ids: [] })
    await update(1, { backup_group_ids: [5, 3] })
    expect(put.mock.calls).toEqual([
      ['/keys/1', { name: 'Rename' }],
      ['/keys/1', { backup_group_ids: [] }],
      ['/keys/1', { backup_group_ids: [5, 3] }]
    ])
  })
})
