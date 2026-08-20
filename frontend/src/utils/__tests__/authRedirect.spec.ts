import { describe, expect, it } from 'vitest'
import { resolvePostLoginRedirect } from '../authRedirect'

describe('resolvePostLoginRedirect', () => {
  it('sends management roles to the management dashboard by default', () => {
    expect(resolvePostLoginRedirect(undefined, true)).toBe('/admin/dashboard')
  })

  it('sends ordinary users to their personal dashboard by default', () => {
    expect(resolvePostLoginRedirect(undefined, false)).toBe('/dashboard')
  })

  it('preserves an explicitly requested internal route', () => {
    expect(resolvePostLoginRedirect('/profile', true)).toBe('/profile')
  })

  it('does not treat empty or non-string query values as a redirect', () => {
    expect(resolvePostLoginRedirect('', true)).toBe('/admin/dashboard')
    expect(resolvePostLoginRedirect(['/profile'], false)).toBe('/dashboard')
  })
})
