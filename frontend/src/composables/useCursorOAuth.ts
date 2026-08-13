import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { CursorTokenInfo } from '@/api/admin/cursor'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

/** 深链轮询节奏：每 3 秒一次，最长等待 5 分钟（授权会话有效期内）。 */
const CURSOR_POLL_INTERVAL_MS = 3_000
const CURSOR_POLL_TIMEOUT_MS = 300_000

const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms))

/**
 * Cursor OAuth composable（镜像 useGrokOAuth 的字段结构）。
 * 深链语义：generateAuthUrl 拿到 auth_url/session_id/state → 打开链接让用户在
 * 浏览器确认 → pollForToken 轮询后端直到返回 token；exchangeAuthCode 保留
 * 与 Grok 相同的手动兑换路径（粘贴回调 code 时可用）。
 */
export function useCursorOAuth() {
  const appStore = useAppStore()
  const { t } = useI18n()

  const authUrl = ref('')
  const sessionId = ref('')
  const state = ref('')
  const loading = ref(false)
  const error = ref('')
  /** 深链轮询中（区别于一次性请求的 loading） */
  const polling = ref(false)

  // 轮询代际：resetState/重新生成链接会使旧的轮询循环立刻退出。
  let pollGeneration = 0

  const cancelPolling = () => {
    pollGeneration += 1
    polling.value = false
  }

  const resetState = () => {
    cancelPolling()
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    loading.value = false
    error.value = ''
  }

  const generateAuthUrl = async (proxyId: number | null | undefined): Promise<boolean> => {
    cancelPolling()
    loading.value = true
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    error.value = ''

    try {
      const payload: Record<string, unknown> = {}
      if (proxyId) payload.proxy_id = proxyId

      const response = await adminAPI.cursor.generateAuthUrl(payload)
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      state.value = response.state
      return true
    } catch (err: any) {
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.cursor.failedToGenerateUrl'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const exchangeAuthCode = async (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
  }): Promise<CursorTokenInfo | null> => {
    const code = params.code?.trim()
    if (!code || !params.sessionId || !params.state) {
      error.value = t('admin.accounts.oauth.cursor.missingExchangeParams')
      return null
    }

    loading.value = true
    error.value = ''

    try {
      const payload: Record<string, unknown> = {
        session_id: params.sessionId,
        state: params.state,
        code
      }
      if (params.proxyId) payload.proxy_id = params.proxyId

      return await adminAPI.cursor.exchangeCode(payload as any)
    } catch (err: any) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.cursor.errors',
        t('admin.accounts.oauth.cursor.failedToExchangeCode')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  /**
   * 深链轮询：反复调用 /admin/cursor/oauth/poll 直到用户在浏览器完成确认。
   * 返回 token 信息；超时/取消/出错返回 null（取消不写入 error）。
   */
  const pollForToken = async (params: {
    sessionId: string
    state: string
    proxyId?: number | null
    intervalMs?: number
    timeoutMs?: number
  }): Promise<CursorTokenInfo | null> => {
    if (!params.sessionId || !params.state) {
      error.value = t('admin.accounts.oauth.cursor.missingExchangeParams')
      return null
    }

    const generation = ++pollGeneration
    const intervalMs = params.intervalMs ?? CURSOR_POLL_INTERVAL_MS
    const timeoutMs = params.timeoutMs ?? CURSOR_POLL_TIMEOUT_MS
    const deadline = Date.now() + timeoutMs

    polling.value = true
    error.value = ''

    try {
      while (Date.now() < deadline) {
        if (generation !== pollGeneration) return null

        try {
          const payload: Record<string, unknown> = {
            session_id: params.sessionId,
            state: params.state
          }
          if (params.proxyId) payload.proxy_id = params.proxyId

          const tokenInfo = await adminAPI.cursor.pollAuthorization(payload as any)
          if (generation !== pollGeneration) return null
          if (tokenInfo?.access_token) {
            return tokenInfo
          }
          // status === 'pending'（或缺少 token）→ 继续等待
        } catch (err: any) {
          if (generation !== pollGeneration) return null
          error.value = extractI18nErrorMessage(
            err,
            t,
            'admin.accounts.oauth.cursor.errors',
            t('admin.accounts.oauth.cursor.failedToPoll')
          )
          appStore.showError(error.value)
          return null
        }

        await sleep(intervalMs)
      }

      if (generation === pollGeneration) {
        error.value = t('admin.accounts.oauth.cursor.pollTimeout')
        appStore.showError(error.value)
      }
      return null
    } finally {
      if (generation === pollGeneration) {
        polling.value = false
      }
    }
  }

  const validateRefreshToken = async (
    refreshToken: string,
    proxyId?: number | null
  ): Promise<CursorTokenInfo | null> => {
    if (!refreshToken.trim()) {
      error.value = t('admin.accounts.oauth.cursor.pleaseEnterRefreshToken')
      return null
    }

    loading.value = true
    error.value = ''

    try {
      return await adminAPI.cursor.refreshCursorToken(refreshToken.trim(), proxyId)
    } catch (err: any) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.cursor.errors',
        t('admin.accounts.oauth.cursor.failedToValidateRT')
      )
      return null
    } finally {
      loading.value = false
    }
  }

  // Build account credentials for create/re-auth（与后端契约对齐）：
  //   access_token   Cursor access JWT（或后端归一化后的 WorkosCursorSessionToken）
  //   refresh_token  可选
  //   api_key        可选，crsr_ 开头的 User API Key（后端用它兑换 access token）
  //   base_url       留空由后端选择默认 https://api2.cursor.sh
  // 永不写入 raw session token / password 等一次性登录材料。
  const buildCredentials = (tokenInfo: CursorTokenInfo): Record<string, unknown> => {
    const credentials: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      token_type: tokenInfo.token_type,
      expires_at: tokenInfo.expires_at,
      client_id: tokenInfo.client_id,
      scope: tokenInfo.scope,
      email: tokenInfo.email,
      sub: tokenInfo.sub,
      team_id: tokenInfo.team_id,
      subscription_tier: tokenInfo.subscription_tier,
      entitlement_status: tokenInfo.entitlement_status,
      // Leave base_url unset so the backend defaults to https://api2.cursor.sh.
    }
    if (tokenInfo.refresh_token) credentials.refresh_token = tokenInfo.refresh_token
    if (tokenInfo.api_key) credentials.api_key = tokenInfo.api_key
    if (tokenInfo.id_token) credentials.id_token = tokenInfo.id_token
    const blocked = new Set(['sso_token', 'session_token', 'password', 'sso', 'sso-rw', 'status'])
    return Object.fromEntries(
      Object.entries(credentials).filter(
        ([key, value]) => !blocked.has(key) && value !== undefined && value !== ''
      )
    )
  }

  const buildExtraInfo = (tokenInfo: CursorTokenInfo): Record<string, unknown> => {
    const extra: Record<string, unknown> = {}
    if (tokenInfo.email) extra.email = tokenInfo.email
    if (tokenInfo.subscription_tier) extra.subscription_tier = tokenInfo.subscription_tier
    if (tokenInfo.entitlement_status) extra.entitlement_status = tokenInfo.entitlement_status
    return extra
  }

  const validateSSOToken = async (
    ssoToken: string,
    proxyId?: number | null
  ): Promise<CursorTokenInfo | null> => {
    if (!ssoToken.trim()) {
      error.value = t('admin.accounts.oauth.cursor.pleaseEnterSSOToken', 'Please enter a session token')
      return null
    }
    loading.value = true
    error.value = ''
    try {
      return await adminAPI.cursor.validateSSOToken(ssoToken.trim(), proxyId)
    } catch (err: any) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.cursor.errors',
        t('admin.accounts.oauth.cursor.failedToValidateSSO', 'Failed to validate session token')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const authorizePassword = async (
    emailAndPassword: string,
    proxyId?: number | null
  ): Promise<CursorTokenInfo | null> => {
    if (!emailAndPassword.trim()) {
      error.value = t('admin.accounts.oauth.cursor.pleaseEnterPassword', 'Please enter email----password')
      return null
    }
    loading.value = true
    error.value = ''
    try {
      return await adminAPI.cursor.authorizePassword(emailAndPassword, proxyId)
    } catch (err: any) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.cursor.errors',
        t('admin.accounts.oauth.cursor.failedToAuthorizePassword', 'Password authorization failed')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    polling,
    resetState,
    cancelPolling,
    generateAuthUrl,
    exchangeAuthCode,
    pollForToken,
    validateRefreshToken,
    validateSSOToken,
    authorizePassword,
    buildCredentials,
    buildExtraInfo
  }
}
