import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
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
    expect(document).toContain('<meta http-equiv="Content-Security-Policy"')
    expect(document).toContain('moshu-showcase-preview-style')
    expect(document).not.toContain('narrative')
  })

  it('removes direct SVG-adjacent narrative text from fragments', () => {
    const document = sandboxDocument('<svg viewBox="0 0 10 10"></svg>``` Save this as pelican.html ```')

    expect(document).toContain('<svg')
    expect(document).not.toContain('Save this as')
  })

  it('replaces source CSP and preview-only reduced-motion rules while starting the SVG animation', () => {
    const markup = '<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src \'none\'; script-src \'none\'"><style>@media screen and (prefers-reduced-motion: reduce) { #animated { animation: none; } }</style><style media="(prefers-reduced-motion: reduce)">#animated { animation: none; }</style></head><body><svg><circle id="animated" cx="0" cy="0" r="1"></circle></svg><script>const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)"); let playing = !reducedMotion.matches; function frame(timestamp) { if (playing) document.querySelector("#animated").setAttribute("cx", String(timestamp)); requestAnimationFrame(frame) } requestAnimationFrame(frame); const example = \'<meta http-equiv="Content-Security-Policy">\';</script></body></html>'
    const preview = sandboxDocument(markup)
    const animationFrames: FrameRequestCallback[] = []
    const rendered = new JSDOM(preview, {
      runScripts: 'dangerously',
      pretendToBeVisual: true,
      beforeParse(window) {
        window.matchMedia = (query: string) => ({
          matches: query.includes('prefers-reduced-motion'),
          media: query,
          onchange: null,
          addListener: () => undefined,
          removeListener: () => undefined,
          addEventListener: () => undefined,
          removeEventListener: () => undefined,
          dispatchEvent: () => false
        }) as unknown as MediaQueryList
        window.requestAnimationFrame = callback => {
          animationFrames.push(callback)
          return animationFrames.length
        }
      }
    })

    expect(preview).toContain("script-src 'unsafe-inline'")
    expect(preview).not.toContain("script-src 'none'")
    expect(preview).not.toContain("'unsafe-eval'")
    expect(preview).toContain('prefers-reduced-motion: force-motion')
    expect(preview).not.toContain('overflow-y:auto')
    expect(preview).toContain("const example = '<meta http-equiv=\"Content-Security-Policy\">';")
    expect(rendered.window.matchMedia('(prefers-reduced-motion: reduce)').matches).toBe(false)
    expect(animationFrames).toHaveLength(1)
    animationFrames.shift()?.(1234)
    expect(rendered.window.document.querySelector('#animated')?.getAttribute('cx')).toBe('1234')
    rendered.window.close()
  })
})
