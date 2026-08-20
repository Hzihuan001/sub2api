/**
 * Admin Cursor API endpoints
 * Handles Cursor OAuth (deep-link) flows for administrators.
 *
 * 端点集整体镜像 Grok（frontend/src/api/admin/grok.ts）：路径把 grok→cursor，
 * 请求/响应结构保持一致。差异仅在于 Cursor 使用「深链登录 + 轮询」语义：
 * auth-url 返回授权链接与会话标识（后端内部持有 uuid/PKCE verifier），
 * 前端打开链接让用户在浏览器确认，然后轮询 poll 端点直到拿到 token。
 */

import { apiClient } from '../client'

export interface CursorAuthUrlResponse {
  auth_url: string
  session_id: string
  state: string
}

export interface CursorAuthUrlRequest {
  proxy_id?: number
  redirect_uri?: string
}

export interface CursorOAuthCapabilities {
  password_auth_enabled: boolean
}

const CURSOR_AUTHORIZATION_TIMEOUT_MS = 120_000

export async function getCapabilities(): Promise<CursorOAuthCapabilities> {
  const { data } = await apiClient.get<CursorOAuthCapabilities>('/admin/cursor/oauth/capabilities')
  return data
}

export interface CursorExchangeCodeRequest {
  session_id: string
  state: string
  code: string
  proxy_id?: number
  redirect_uri?: string
}

export interface CursorPollRequest {
  session_id: string
  state: string
  proxy_id?: number
}

export interface CursorTokenInfo {
  access_token?: string
  refresh_token?: string
  /** crsr_ 开头的 User API Key（后端换取 access token 时可选返回） */
  api_key?: string
  token_type?: string
  id_token?: string
  expires_at?: number | string
  expires_in?: number
  scope?: string
  client_id?: string
  email?: string
  sub?: string
  team_id?: string
  subscription_tier?: string
  entitlement_status?: string
  /** 深链轮询状态：'pending' 表示用户尚未在浏览器完成确认 */
  status?: string
  [key: string]: unknown
}

export interface CursorSSOToOAuthRequest {
  sso_tokens: string[]
  name?: string
  notes?: string | null
  proxy_id?: number | null
  group_ids?: number[]
  credentials?: Record<string, unknown>
  extra?: Record<string, unknown>
  concurrency?: number
  load_factor?: number
  priority?: number
  rate_multiplier?: number
  expires_at?: number | null
  auto_pause_on_expired?: boolean
}

export interface CursorSSOToOAuthItemResult {
  index: number
  name?: string
  email?: string
  account?: unknown
  error?: string
}

export interface CursorSSOToOAuthResponse {
  created: CursorSSOToOAuthItemResult[]
  failed: CursorSSOToOAuthItemResult[]
}

const CURSOR_SSO_IMPORT_CONCURRENCY = 3
const CURSOR_SSO_IMPORT_TIMEOUT_PER_BATCH_MS = 90_000
const CURSOR_SSO_IMPORT_TIMEOUT_BUFFER_MS = 90_000

export function getCursorSSOImportTimeout(keyCount: number): number {
  const batches = Math.ceil(Math.max(1, keyCount) / CURSOR_SSO_IMPORT_CONCURRENCY)
  return batches * CURSOR_SSO_IMPORT_TIMEOUT_PER_BATCH_MS + CURSOR_SSO_IMPORT_TIMEOUT_BUFFER_MS
}

export async function generateAuthUrl(
  payload: CursorAuthUrlRequest
): Promise<CursorAuthUrlResponse> {
  const { data } = await apiClient.post<CursorAuthUrlResponse>(
    '/admin/cursor/oauth/auth-url',
    payload
  )
  return data
}

export async function exchangeCode(payload: CursorExchangeCodeRequest): Promise<CursorTokenInfo> {
  const { data } = await apiClient.post<CursorTokenInfo>(
    '/admin/cursor/oauth/exchange-code',
    payload
  )
  return data
}

/**
 * 深链轮询：用户在浏览器确认授权后，后端向 Cursor 兑换 token。
 * 未完成时返回 { status: 'pending' }（HTTP 200），完成后返回 token 字段。
 */
export async function pollAuthorization(payload: CursorPollRequest): Promise<CursorTokenInfo> {
  const { data } = await apiClient.post<CursorTokenInfo>(
    '/admin/cursor/oauth/poll',
    payload
  )
  return data
}

export async function refreshCursorToken(
  refreshToken: string,
  proxyId?: number | null
): Promise<CursorTokenInfo> {
  const payload: Record<string, unknown> = { refresh_token: refreshToken }
  if (proxyId) payload.proxy_id = proxyId

  const { data } = await apiClient.post<CursorTokenInfo>(
    '/admin/cursor/oauth/refresh-token',
    payload
  )
  return data
}

export async function createFromSSO(payload: CursorSSOToOAuthRequest): Promise<CursorSSOToOAuthResponse> {
  const { data } = await apiClient.post<CursorSSOToOAuthResponse>(
    '/admin/cursor/sso-to-oauth',
    payload,
    { timeout: getCursorSSOImportTimeout(payload.sso_tokens.length) }
  )
  return data
}

/**
 * Validate a pasted WorkosCursorSessionToken (userId::JWT) and convert to
 * account credentials (raw session token is normalized server-side).
 */
export async function validateSSOToken(
  ssoToken: string,
  proxyId?: number | null
): Promise<CursorTokenInfo> {
  const payload: Record<string, unknown> = { sso_token: ssoToken }
  if (proxyId) payload.proxy_id = proxyId
  const { data } = await apiClient.post<CursorTokenInfo>('/admin/cursor/oauth/sso-token', payload, {
    timeout: CURSOR_AUTHORIZATION_TIMEOUT_MS
  })
  return data
}

/**
 * Password login (structural mirror of Grok; gated by capabilities and
 * disabled unless the backend enables it).
 * Password is only sent over the wire for this call; never persist it in credentials.
 */
export async function authorizePassword(
  emailAndPassword: string,
  proxyId?: number | null
): Promise<CursorTokenInfo> {
  // Format: email----password (password may contain dashes).
  const sep = '----'
  const idx = emailAndPassword.indexOf(sep)
  const email = (idx >= 0 ? emailAndPassword.slice(0, idx) : emailAndPassword).trim()
  const password = idx >= 0 ? emailAndPassword.slice(idx + sep.length) : ''
  const payload: Record<string, unknown> = { email, password }
  if (proxyId) payload.proxy_id = proxyId
  const { data } = await apiClient.post<CursorTokenInfo>('/admin/cursor/oauth/password', payload, {
    timeout: CURSOR_AUTHORIZATION_TIMEOUT_MS
  })
  return data
}

export default {
  generateAuthUrl,
  getCapabilities,
  exchangeCode,
  pollAuthorization,
  refreshCursorToken,
  createFromSSO,
  validateSSOToken,
  authorizePassword,
}
