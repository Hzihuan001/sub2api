import { describe, expect, it } from 'vitest'
import { normalizePublicBranding } from '../publicBranding'

describe('normalizePublicBranding', () => {
  it('replaces public product-name variants without changing surrounding copy', () => {
    expect(normalizePublicBranding('Sub2API / sub2api / SUB2API')).toBe('Moshu / moshu / MOSHU')
  })

  it('leaves unrelated text unchanged', () => {
    expect(normalizePublicBranding('Deployment and operation commitment')).toBe(
      'Deployment and operation commitment'
    )
  })
})
