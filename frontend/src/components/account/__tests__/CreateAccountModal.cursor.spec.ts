import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Cursor account types', () => {
  // A crsr_ User API Key is not a bearer for api2/api5 — it must be exchanged
  // for a session token, and only oauth-typed accounts run that exchange. An
  // apikey-typed Cursor account was therefore always dead on arrival, so the
  // tile is gone and the key is pasted into the authorization flow instead.
  it('offers OAuth only, with the official Cursor default upstream', () => {
    expect(source).not.toContain('data-testid="cursor-account-type-api-key"')
    expect(source).toContain('data-testid="cursor-oauth-only-hint"')
    expect(source).toContain("newPlatform === 'cursor'")
    expect(source).toContain("? 'https://api2.cursor.sh'")
    expect(source).toContain("form.platform === 'cursor'")
  })

  it('exposes the custom upstream URL toggle for the OAuth create flow', () => {
    expect(source).toContain('data-testid="cursor-custom-base-url-toggle"')
    expect(source).toContain('data-testid="cursor-custom-base-url-input"')
    expect(source).toContain('form.platform === \'cursor\' && isOAuthFlow')
  })

  it('validates and applies upstream config on Cursor OAuth create paths', () => {
    // 深链轮询 / 手动兑换 / RT 批量 / 会话令牌批量
    expect(source.match(/validateCursorOAuthUpstreamConfig\(\)/g)?.length).toBeGreaterThanOrEqual(4)
    expect(source.match(/applyCursorOAuthUpstreamConfig\(credentials\)/g)?.length).toBeGreaterThanOrEqual(3)
  })

  it('starts deep-link polling after generating the auth URL and shows the waiting state', () => {
    expect(source).toContain('handleCursorGenerateUrlAndPoll')
    expect(source).toContain('cursorOAuth.pollForToken')
    expect(source).toContain('data-testid="cursor-oauth-polling"')
  })

  it('forces the OAuth category when the platform switches to Cursor', () => {
    expect(source).toMatch(/newPlatform === 'cursor'\)\s*\{\s*\n\s*accountCategory\.value = 'oauth-based'/)
  })
})
