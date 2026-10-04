/**
 * Normalize upstream product names before rendering customer-facing content.
 *
 * Technical identifiers and storage keys intentionally keep their original
 * values elsewhere in the application. This helper is for text that is about
 * to be rendered as public copy (for example, Markdown legal documents).
 */
export function normalizePublicBranding(value: string): string {
  return value
    .replace(/Sub2API/g, 'Moshu')
    .replace(/sub2api/g, 'moshu')
    .replace(/SUB2API/g, 'MOSHU')
}
