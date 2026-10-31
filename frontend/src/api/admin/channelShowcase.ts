import { apiClient } from '../client'

export interface ChannelShowcaseOrderResponse {
  group_ids: number[]
}

export async function getOrder(): Promise<ChannelShowcaseOrderResponse> {
  const { data } = await apiClient.get<ChannelShowcaseOrderResponse>('/admin/pelican-showcase/order')
  return data || { group_ids: [] }
}

export async function updateOrder(groupIds: number[]): Promise<ChannelShowcaseOrderResponse> {
  const { data } = await apiClient.put<ChannelShowcaseOrderResponse>('/admin/pelican-showcase/order', { group_ids: groupIds })
  return data || { group_ids: groupIds }
}

const channelShowcaseAPI = { getOrder, updateOrder }

export default channelShowcaseAPI
