import { describe, expect, it, vi } from 'vitest'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn()
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => {
      const messages: Record<string, string> = {
        'admin.accounts.oauth.cursor.failedToExchangeCode': 'Cursor 授权码兑换失败',
        'admin.accounts.oauth.cursor.errors.CURSOR_OAUTH_INVALID_STATE':
          'Cursor OAuth state 与当前会话不匹配。请重新生成授权链接后再试。',
        'admin.accounts.oauth.cursor.pollTimeout':
          'Cursor 授权等待超时。请重新生成授权链接后再试。'
      }
      return messages[key] ?? key
    }
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    cursor: {
      generateAuthUrl: vi.fn(),
      exchangeCode: vi.fn(),
      pollAuthorization: vi.fn(),
      refreshCursorToken: vi.fn()
    }
  }
}))

import { useCursorOAuth } from '@/composables/useCursorOAuth'
import { adminAPI } from '@/api/admin'

describe('useCursorOAuth.exchangeAuthCode', () => {
  it('shows a state mismatch recovery hint from structured backend errors', async () => {
    vi.mocked(adminAPI.cursor.exchangeCode).mockRejectedValueOnce({
      status: 400,
      reason: 'CURSOR_OAUTH_INVALID_STATE',
      message: 'invalid oauth state'
    })
    const oauth = useCursorOAuth()

    const tokenInfo = await oauth.exchangeAuthCode({
      code: 'code',
      sessionId: 'session-id',
      state: 'wrong-state'
    })

    expect(tokenInfo).toBeNull()
    expect(oauth.error.value).toBe(
      'Cursor OAuth state 与当前会话不匹配。请重新生成授权链接后再试。'
    )
  })
})

describe('useCursorOAuth.pollForToken', () => {
  it('keeps polling while pending and resolves once the token arrives', async () => {
    vi.mocked(adminAPI.cursor.pollAuthorization)
      .mockResolvedValueOnce({ status: 'pending' })
      .mockResolvedValueOnce({ access_token: 'jwt-token', email: 'dev@example.com' })
    const oauth = useCursorOAuth()

    const tokenInfo = await oauth.pollForToken({
      sessionId: 'session-id',
      state: 'state',
      intervalMs: 1
    })

    expect(adminAPI.cursor.pollAuthorization).toHaveBeenCalledTimes(2)
    expect(tokenInfo?.access_token).toBe('jwt-token')
    expect(oauth.polling.value).toBe(false)
  })

  it('stops silently when the poll is cancelled (e.g. modal reset)', async () => {
    vi.mocked(adminAPI.cursor.pollAuthorization).mockImplementation(async () => {
      // Cancel mid-flight, as resetState does when the user backs out.
      oauth.cancelPolling()
      return { status: 'pending' }
    })
    const oauth = useCursorOAuth()

    const tokenInfo = await oauth.pollForToken({
      sessionId: 'session-id',
      state: 'state',
      intervalMs: 1
    })

    expect(tokenInfo).toBeNull()
    expect(oauth.error.value).toBe('')
  })

  it('reports a timeout when the user never confirms in the browser', async () => {
    vi.mocked(adminAPI.cursor.pollAuthorization).mockResolvedValue({ status: 'pending' })
    const oauth = useCursorOAuth()

    const tokenInfo = await oauth.pollForToken({
      sessionId: 'session-id',
      state: 'state',
      intervalMs: 1,
      timeoutMs: 5
    })

    expect(tokenInfo).toBeNull()
    expect(oauth.error.value).toBe('Cursor 授权等待超时。请重新生成授权链接后再试。')
  })
})

describe('useCursorOAuth.buildCredentials', () => {
  it('builds OAuth credentials without forcing base_url or leaking one-shot login material', () => {
    const oauth = useCursorOAuth()

    const credentials = oauth.buildCredentials({
      access_token: 'access-token',
      refresh_token: 'refresh-token',
      api_key: 'crsr_user_key',
      token_type: 'Bearer',
      expires_at: 1_900_000_000,
      client_id: 'client-id',
      scope: 'openid offline_access',
      email: 'cursor@example.com',
      status: 'complete',
      password: 'super-secret',
      sso_token: 'user-id::jwt',
      session_token: 'user-id::jwt'
    } as any)

    expect(credentials.access_token).toBe('access-token')
    expect(credentials.refresh_token).toBe('refresh-token')
    expect(credentials.api_key).toBe('crsr_user_key')
    expect(credentials.email).toBe('cursor@example.com')
    // Backend defaults to https://api2.cursor.sh; do not pin base_url client-side.
    expect(credentials.base_url).toBeUndefined()
    expect(credentials).not.toHaveProperty('password')
    expect(credentials).not.toHaveProperty('sso_token')
    expect(credentials).not.toHaveProperty('session_token')
    expect(credentials).not.toHaveProperty('status')
  })
})
