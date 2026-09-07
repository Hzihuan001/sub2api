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
  sales_rate_multiplier?: number
  price_catalog_version: number
  models: string[]
  credential_available: boolean
  local_group_id?: number
  local_account_id?: number
  effective_at: string
}

export interface MoshuResellerStatus {
  enabled: boolean
  connected: boolean
  connection?: MoshuResellerConnection
  products: MoshuProduct[]
}

export interface MoshuProfitRecord {
  id: number
  request_id: string
  product_code: string
  requested_model?: string
  moshu_actual_cost: number
  l1_customer_charge: number
  gross_profit: number
  settlement_status: string
  remote_completed_at?: string
  created_at: string
}

export interface MoshuProfitPage {
  items: MoshuProfitRecord[]
  total: number
  page: number
  page_size: number
  pages: number
}

export default {
  async status(): Promise<MoshuResellerStatus> {
    const response = await apiClient.get<MoshuResellerStatus>('/admin/moshu-reseller/status')
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

  async configureProduct(id: number, payload: { selected: boolean; sales_name: string; sales_multiplier: number }): Promise<MoshuProduct> {
    const response = await apiClient.put<MoshuProduct>(`/admin/moshu-reseller/products/${id}`, payload)
    return response.data
  },

  async rotateCredential(id: number): Promise<MoshuProduct> {
    const response = await apiClient.post<MoshuProduct>(`/admin/moshu-reseller/products/${id}/credentials/rotate`)
    return response.data
  },

  async syncSettlements(): Promise<{ synced: number }> {
    const response = await apiClient.post<{ synced: number }>('/admin/moshu-reseller/settlements/sync')
    return response.data
  },

  async profits(page = 1, pageSize = 20): Promise<MoshuProfitPage> {
    const response = await apiClient.get<MoshuProfitPage>('/admin/moshu-reseller/profits', { params: { page, page_size: pageSize } })
    return response.data
  }
}
