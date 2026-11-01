import { describe, expect, it } from 'vitest'
import { extractMarkup, isMarkup, sandboxDocument } from '../pelicanMarkup'

describe('pelican showcase markup', () => {
  it('removes markdown fences and trailing prose from a full HTML response', () => {
    const response = '```html\n<!doctype html><html><head></head><body><svg viewBox="0 0 10 10"></svg>Save this as pelican.html and open it in a browser.</body></html>\n```'
    const markup = extractMarkup(response)

    expect(markup).toMatch(/^<!doctype html>/i)
    expect(markup).not.toContain('```')
    expect(isMarkup(markup)).toBe(true)
  })

  it('adds CSP inside a full document instead of nesting the document in body', () => {
    const markup = '<!doctype html><html><head><title>test</title></head><body><svg viewBox="0 0 10 10"></svg>narrative</body></html>'
    const document = sandboxDocument(markup)

    expect((document.match(/<html\b/gi) || []).length).toBe(1)
    expect(document).toContain('<head><meta http-equiv="Content-Security-Policy"')
    expect(document).not.toContain('narrative')
  })

  it('removes direct SVG-adjacent narrative text from fragments', () => {
    const document = sandboxDocument('<svg viewBox="0 0 10 10"></svg>``` Save this as pelican.html ```')

    expect(document).toContain('<svg')
    expect(document).not.toContain('Save this as')
  })
})
