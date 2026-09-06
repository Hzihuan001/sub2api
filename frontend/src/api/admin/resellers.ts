import { apiClient } from '../client'

export interface ResellerTenant {
  id: number
  user_id: number
  name: string
  status: 'active' | 'suspended' | 'disabled'
  protocol_version: string
  instance_id?: string
  allowed_cidrs: string[]
  created_at: string
}

export interface ResellerProduct {
  id: number
  reseller_id: number
  moshu_group_id: number
  product_code: string
  display_name: string
  platform: string
  enabled: boolean
  cost_rate_multiplier: number
  price_catalog_version: number
  models: string[]
  credential_configured: boolean
}

export interface ResellerSettlement {
  id: number
  request_id: string
  product_code: string
  requested_model?: string
  standard_cost: number
  cost_rate_multiplier: number
  actual_cost: number
  status: string
  created_at: string
}

export default {
  async listTenants(): Promise<ResellerTenant[]> {
    const response = await apiClient.get<ResellerTenant[]>('/admin/resellers')
    return response.data
  },
  async createTenant(payload: { user_id: number; name: string; allowed_cidrs: string[] }): Promise<ResellerTenant> {
    const response = await apiClient.post<ResellerTenant>('/admin/resellers', payload)
    return response.data
  },
  async updateTenant(id: number, payload: { status: string; allowed_cidrs: string[] }): Promise<ResellerTenant> {
    const response = await apiClient.patch<ResellerTenant>(`/admin/resellers/${id}`, payload)
    return response.data
  },
  async listProducts(id: number): Promise<ResellerProduct[]> {
    const response = await apiClient.get<ResellerProduct[]>(`/admin/resellers/${id}/products`)
    return response.data
  },
  async upsertProduct(id: number, payload: { moshu_group_id: number; product_code: string; display_name: string; enabled: boolean }): Promise<ResellerProduct> {
    const response = await apiClient.post<ResellerProduct>(`/admin/resellers/${id}/products`, payload)
    return response.data
  },
  async createEnrollment(id: number, productIDs: number[]): Promise<{ enrollment_code: string; expires_at: string }> {
    const response = await apiClient.post<{ enrollment_code: string; expires_at: string }>(`/admin/resellers/${id}/enrollments`, { product_ids: productIDs, ttl_minutes: 30 })
    return response.data
  },
  async rotate(id: number, productID: number): Promise<{ api_key: string }> {
    const response = await apiClient.post<{ api_key: string }>(`/admin/resellers/${id}/products/${productID}/credentials/rotate`)
    return response.data
  },
  async settlements(id: number): Promise<{ items: ResellerSettlement[]; next_cursor?: number }> {
    const response = await apiClient.get<{ items: ResellerSettlement[]; next_cursor?: number }>(`/admin/resellers/${id}/settlements`)
    return response.data
  }
}
