export const CUSTOM_SITE_NAME = 'Moshu'

/** Only the public site_name display field is branded; retain its existing whitespace behavior. */
export function normalizePublicSiteName(value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return CUSTOM_SITE_NAME
  return value.split('Sub2API').join('Moshu').split('sub2api').join('moshu')
}

/** Route titles honor a configured site name verbatim after trimming, without rebranding it. */
export function resolveTitleSiteName(value: unknown): string {
  return typeof value === 'string' && value.trim() ? value.trim() : CUSTOM_SITE_NAME
}
