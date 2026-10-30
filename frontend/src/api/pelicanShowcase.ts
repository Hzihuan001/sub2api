import { apiClient } from './client'

export interface PelicanShowcaseResult {
  id: number
  account_name: string
  model_id: string
  thinking_effort?: string
  status: string
  response_text?: string
  error_message?: string
  latency_ms: number
  started_at: string
  finished_at: string
}

export interface PelicanShowcaseGroup {
  id: number
  name: string
  platform: string
  results: PelicanShowcaseResult[]
  latest?: PelicanShowcaseResult
  latest_failure?: PelicanShowcaseResult
  status: 'healthy' | 'degraded' | 'unknown' | string
  last_test_at?: string
  success_count: number
  failure_count: number
}

export interface PelicanShowcaseResponse {
  groups: PelicanShowcaseGroup[]
  description?: string
}

export async function getPelicanShowcase(options?: { signal?: AbortSignal }) {
  const { data } = await apiClient.get<PelicanShowcaseResponse>('/pelican-showcase', {
    signal: options?.signal,
  })
  return data
}

export const pelicanShowcaseAPI = { getPelicanShowcase }
export default pelicanShowcaseAPI
