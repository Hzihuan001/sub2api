import { describe, expect, it } from 'vitest'
import { normalizePublicSiteName, resolveTitleSiteName } from '../branding/display'

describe('narrow display branding contracts', () => {
  it.each([undefined, null, 42, '', '   '])('keeps the default for absent site names: %s', (value) => {
    expect(normalizePublicSiteName(value)).toBe('Moshu')
    expect(resolveTitleSiteName(value)).toBe('Moshu')
  })

  it('brands only the public site-name value and keeps its historical surrounding whitespace', () => {
    expect(normalizePublicSiteName('  Sub2API / sub2api Gateway  ')).toBe('  Moshu / moshu Gateway  ')
    expect(normalizePublicSiteName('  Customer Gateway  ')).toBe('  Customer Gateway  ')
  })

  it('keeps configured route title names authoritative without replacing their contents', () => {
    expect(resolveTitleSiteName('  Sub2API Customer  ')).toBe('Sub2API Customer')
    expect(resolveTitleSiteName('  Customer Gateway  ')).toBe('Customer Gateway')
  })
})
