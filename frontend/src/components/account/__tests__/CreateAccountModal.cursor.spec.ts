import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Cursor account types', () => {
  it('offers API-key setup alongside OAuth with the official Cursor default', () => {
    expect(source).toContain('data-testid="cursor-account-type-api-key"')
    expect(source).toContain("@click=\"accountCategory = 'apikey'\"")
    expect(source).toContain("newPlatform === 'cursor'")
    expect(source).toContain("? 'https://api2.cursor.sh'")
    expect(source).toContain("form.platform === 'cursor'")
    expect(source).toContain("? 'crsr_...'")
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

  it('splits pasted apikey input into api_key (crsr_) vs access_token (JWT / session token)', () => {
    expect(source).toContain("apiKeyInput.startsWith('crsr_')")
    expect(source).toContain('credentials.access_token = apiKeyInput')
    expect(source).toContain('credentials.api_key = apiKeyInput')
  })
})
