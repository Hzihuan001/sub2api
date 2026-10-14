/** Older saved menus omit this field and retain the historical enabled value. */
export function normalizeCustomMenuAuthContext(value: boolean | undefined): boolean {
  return value !== false
}

export function supportsCustomMenuAuthContext(url: string | undefined): boolean {
  return !url?.startsWith('md:')
}
