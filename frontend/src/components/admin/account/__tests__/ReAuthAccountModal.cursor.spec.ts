import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/admin/account/ReAuthAccountModal.vue'),
  'utf8'
)

describe('ReAuthAccountModal Cursor re-auth paths', () => {
  it('exposes session-token and refresh-token options; password auth stays hidden', () => {
    expect(source).toContain(':show-sso-option="isGrok || isCursor"')
    expect(source).toContain(':show-refresh-token-option="isOpenAI || isAntigravity || isGrok || isCursor"')
    expect(source).toContain(':show-email-password-option="false"')
  })

  it('wires deep-link polling for the Cursor OAuth reauth path', () => {
    expect(source).toContain('handleCursorGenerateUrlAndPoll')
    expect(source).toContain('cursorOAuth.pollForToken')
    expect(source).toContain('data-testid="cursor-reauth-polling"')
    // Manual code exchange must cancel the in-flight poll first.
    expect(source).toContain('cursorOAuth.cancelPolling()')
  })

  it('applies tokens onto the existing account instead of batch create', () => {
    expect(source).toContain('applyCursorReauthTokenInfo')
    expect(source).toContain('cursorOAuth.buildCredentials')
    expect(source).not.toContain('createFromSSO')
    expect(source).toContain('applyOAuthCredentials')
  })

  it('defaults Cursor reauth to refresh_token when present, else deep-link manual flow', () => {
    expect(source).toContain('isCursor.value')
    expect(source).toContain("accountHasRefreshToken.value ? 'refresh_token' : 'manual'")
  })
})
