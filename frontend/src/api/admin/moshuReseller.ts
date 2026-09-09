import { apiClient } from '../client'

export interface MoshuResellerConnection {
  base_url: string
  instance_id: string
  reseller_id?: number
  reseller_name: string
  protocol_version: string
  status: string
  catalog_version: number
  last_catalog_sync_at?: string
  last_settlement_sync_at?: string
  last_error?: string
}

export interface MoshuProduct {
  id: number
  remote_product_id: number
  product_code: string
  display_name: string
  platform: string
  moshu_group_id: number
  authorized: boolean
  selected: boolean
  cost_rate_multiplier: number
  cost_rate_override?: number
  sales_rate_multiplier?: number
  price_catalog_version: number
  models: string[]
  credential_available: boolean
  local_group_id?: number
  local_account_id?: number
  capacity: number
  effective_at: string
}

export interface MoshuResellerStatus {
  enabled: boolean
  connected: boolean
  connection?: MoshuResellerConnection
  products: MoshuProduct[]
}

export interface MoshuResellerBalance {
  balance: number
  frozen_balance: number
  warning: boolean
}

export default {
  async status(): Promise<MoshuResellerStatus> {
    const response = await apiClient.get<MoshuResellerStatus>('/admin/moshu-reseller/status')
    return response.data
  },

  async balance(): Promise<MoshuResellerBalance> {
    const response = await apiClient.get<MoshuResellerBalance>('/admin/moshu-reseller/balance')
    return response.data
  },

  async enroll(payload: { base_url?: string; enrollment_code: string }): Promise<MoshuResellerStatus> {
    const response = await apiClient.post<MoshuResellerStatus>('/admin/moshu-reseller/enroll', payload)
    return response.data
  },

  async syncCatalog(): Promise<MoshuResellerStatus> {
    const response = await apiClient.post<MoshuResellerStatus>('/admin/moshu-reseller/catalog/sync')
    return response.data
  },

  async configureProduct(id: number, payload: { selected: boolean; sales_name: string; sales_multiplier: number; capacity: number }): Promise<MoshuProduct> {
    const response = await apiClient.put<MoshuProduct>(`/admin/moshu-reseller/products/${id}`, payload)
    return response.data
  },

  async rotateCredential(id: number): Promise<MoshuProduct> {
    const response = await apiClient.post<MoshuProduct>(`/admin/moshu-reseller/products/${id}/credentials/rotate`)
    return response.data
  },

  async setProductCost(id: number, payload: { cost_rate_multiplier: number } | { follow_upstream: true }): Promise<MoshuProduct> {
    const response = await apiClient.put<MoshuProduct>(`/admin/moshu-reseller/products/${id}/cost`, payload)
    return response.data
  },

  async ensureTestAccount(id: number): Promise<{ account_id: number }> {
    const response = await apiClient.post<{ account_id: number }>(`/admin/moshu-reseller/products/${id}/test-account`)
    return response.data
  }
}
