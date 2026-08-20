import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { post },
}))

import {
  createFromSSO,
  generateAuthUrl,
  getCursorSSOImportTimeout,
  pollAuthorization,
  refreshCursorToken,
} from '@/api/admin/cursor'

describe('admin Cursor OAuth API', () => {
  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { created: [], failed: [] } })
  })

  it.each([
    [1, 180_000],
    [3, 180_000],
    [4, 270_000],
    [7, 360_000],
  ])('uses a timeout sized for %i session tokens', async (keyCount, expectedTimeout) => {
    expect(getCursorSSOImportTimeout(keyCount)).toBe(expectedTimeout)

    await createFromSSO({
      sso_tokens: Array.from({ length: keyCount }, (_, index) => `user-${index + 1}::jwt`),
    })

    expect(post).toHaveBeenCalledWith(
      '/admin/cursor/sso-to-oauth',
      expect.objectContaining({ sso_tokens: expect.any(Array) }),
      { timeout: expectedTimeout },
    )
  })

  it('generates deep-link auth URLs against the mirrored cursor endpoint', async () => {
    post.mockResolvedValueOnce({
      data: { auth_url: 'https://cursor.com/loginDeepControl?...', session_id: 'sess', state: 'st' },
    })

    const response = await generateAuthUrl({ proxy_id: 7 })

    expect(post).toHaveBeenCalledWith('/admin/cursor/oauth/auth-url', { proxy_id: 7 })
    expect(response.auth_url).toContain('cursor.com')
    expect(response.session_id).toBe('sess')
    expect(response.state).toBe('st')
  })

  it('polls the authorization endpoint with the session identifiers', async () => {
    post.mockResolvedValueOnce({ data: { status: 'pending' } })

    const pending = await pollAuthorization({ session_id: 'sess', state: 'st' })

    expect(post).toHaveBeenCalledWith('/admin/cursor/oauth/poll', {
      session_id: 'sess',
      state: 'st',
    })
    expect(pending.status).toBe('pending')
    expect(pending.access_token).toBeUndefined()
  })

  it('refreshes tokens through the mirrored refresh-token endpoint', async () => {
    post.mockResolvedValueOnce({ data: { access_token: 'jwt' } })

    await refreshCursorToken('rt-1', 3)

    expect(post).toHaveBeenCalledWith('/admin/cursor/oauth/refresh-token', {
      refresh_token: 'rt-1',
      proxy_id: 3,
    })
  })
})
