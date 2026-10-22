import type { KeyModelPolicy, KeyRouteFailurePolicy, KeyRouteScope } from '@/types'

/** UI draft shared by self-service and admin editors; persistence stays with each host. */
export interface ApiKeyEditorForm {
  name: string
  group_id: number | null
  backup_group_ids: number[]
  key_route_scope: KeyRouteScope
  key_route_failure_policy: KeyRouteFailurePolicy | null
  key_model_policy: KeyModelPolicy | null
  status: 'active' | 'inactive'
  use_custom_key: boolean
  custom_key: string
  enable_ip_restriction: boolean
  ip_whitelist: string
  ip_blacklist: string
  enable_quota: boolean
  quota: number | null
  enable_rate_limit: boolean
  rate_limit_5h: number | null
  rate_limit_1d: number | null
  rate_limit_7d: number | null
  enable_expiration: boolean
  expiration_preset: '7' | '30' | '90' | 'custom'
  expiration_date: string
}

