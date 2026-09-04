import { apiClient } from '../client'
import type { OperatorRolePolicy } from '@/authz/permissions'

export async function getOperatorPermissions(): Promise<OperatorRolePolicy> {
  const { data } = await apiClient.get<OperatorRolePolicy>('/admin/roles/operator/permissions')
  return data
}

export async function updateOperatorPermissions(
  policy: OperatorRolePolicy
): Promise<OperatorRolePolicy> {
  const { data } = await apiClient.put<OperatorRolePolicy>('/admin/roles/operator/permissions', policy)
  return data
}

export default { getOperatorPermissions, updateOperatorPermissions }
