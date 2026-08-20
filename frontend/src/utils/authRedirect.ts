export function resolvePostLoginRedirect(
  requestedRedirect: unknown,
  isManagement: boolean
): string {
  if (typeof requestedRedirect === 'string' && requestedRedirect.trim()) {
    return requestedRedirect
  }

  return isManagement ? '/admin/dashboard' : '/dashboard'
}
