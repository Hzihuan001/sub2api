import { describe, expect, it, vi } from 'vitest'
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

  it('retains and executes animation scripts after a standalone SVG', () => {
    const response = '```html\n<svg><circle id="animated" cx="0"></circle></svg><script>requestAnimationFrame(timestamp => document.querySelector("#animated").setAttribute("cx", String(timestamp)))</script>\n```'
    const markup = extractMarkup(response)
    const animationFrames: FrameRequestCallback[] = []
    const rendered = new JSDOM(sandboxDocument(markup), {
      runScripts: 'dangerously',
      beforeParse(window) {
        window.requestAnimationFrame = callback => {
          animationFrames.push(callback)
          return animationFrames.length
        }
      }
    })

    expect(markup).toContain('<script>requestAnimationFrame')
    expect(isMarkup(markup)).toBe(true)
    expect(animationFrames).toHaveLength(1)
    animationFrames.shift()?.(2468)
    expect(rendered.window.document.querySelector('#animated')?.getAttribute('cx')).toBe('2468')
    expect(response).toContain('```html')
    rendered.window.close()
  })

  it('preserves fragment styles and scripts parsed into head alongside the SVG', () => {
    const response = 'Here is the artwork:\n```html\n<script>window.previewOffset = 5;</script><style>#animated { fill: red; }</style><svg><circle id="animated" cx="0"></circle></svg><script>requestAnimationFrame(timestamp => document.querySelector("#animated").setAttribute("cx", String(timestamp + window.previewOffset)))</script>\n``` Save this as pelican.html'
    const markup = extractMarkup(response)
    const preview = sandboxDocument(markup)
    const animationFrames: FrameRequestCallback[] = []
    const rendered = new JSDOM(preview, {
      runScripts: 'dangerously',
      beforeParse(window) {
        window.requestAnimationFrame = callback => {
          animationFrames.push(callback)
          return animationFrames.length
        }
      }
    })

    expect(isMarkup(markup)).toBe(true)
    expect(rendered.window.document.head.querySelector('style')?.textContent).toBe('#animated { fill: red; }')
    expect(rendered.window.document.head.querySelector('script:not([id])')?.textContent).toBe('window.previewOffset = 5;')
    expect(preview).toContain("default-src 'none'")
    expect(preview).not.toContain('Save this as')
    expect(animationFrames).toHaveLength(1)
    animationFrames.shift()?.(100)
    expect(rendered.window.document.querySelector('#animated')?.getAttribute('cx')).toBe('105')
    rendered.window.close()
  })

  it.each([
    '<!doctype html><html><head><script nonce="provider-nonce">window.seed = 1;</script></head><body><svg></svg><script nonce="untrusted-nonce">window.started = true;</script></body></html>',
    '<script nonce="provider-nonce">window.seed = 1;</script><svg></svg><script nonce="untrusted-nonce">window.started = true;</script>',
    '<svg><script nonce="provider-nonce">window.seed = 1;</script></svg><script nonce="untrusted-nonce">window.started = true;</script>'
  ])('authorizes every preview script with the host nonce while retaining the sandbox CSP', markup => {
    const hostScript = document.createElement('script')
    const hostNonce = 'current-host-nonce-"<&'
    hostScript.nonce = hostNonce
    document.head.appendChild(hostScript)
    try {
      const preview = sandboxDocument(markup)
      const parsed = new DOMParser().parseFromString(preview, 'text/html')
      const scripts = Array.from(parsed.querySelectorAll('script'))

      expect(scripts).toHaveLength(3)
      expect(scripts.every(script => script.getAttribute('nonce') === hostNonce)).toBe(true)
      expect(parsed.querySelector('#moshu-showcase-animation-bootstrap')?.getAttribute('nonce')).toBe(hostNonce)
      expect(preview).not.toContain('nonce="provider-nonce"')
      expect(preview).not.toContain('nonce="untrusted-nonce"')
      expect(preview).toContain('nonce="current-host-nonce-&quot;')
      const policy = parsed.querySelector('meta[http-equiv="Content-Security-Policy"]')?.getAttribute('content')
      expect(policy).toContain("default-src 'none'")
      expect(policy).toContain("connect-src 'none'")
      expect(policy).not.toContain("'unsafe-eval'")
      expect(markup).toContain('nonce="provider-nonce"')
    } finally {
      hostScript.remove()
    }
  })

  it('retains nonce authorization through the browser document fallback', () => {
    const BrowserDOMParser = DOMParser
    const hostScript = document.createElement('script')
    hostScript.nonce = 'fallback-host-nonce'
    document.head.appendChild(hostScript)
    vi.stubGlobal('DOMParser', undefined)
    try {
      const preview = sandboxDocument('<svg></svg><script nonce="provider-nonce">window.started = true;</script>')
      const parsed = new BrowserDOMParser().parseFromString(preview, 'text/html')

      expect(parsed.querySelector('svg')).not.toBeNull()
      expect(Array.from(parsed.querySelectorAll('script'))).toHaveLength(2)
      expect(Array.from(parsed.querySelectorAll('script')).every(script => script.getAttribute('nonce') === hostScript.nonce)).toBe(true)
      expect(preview).toContain("connect-src 'none'")
    } finally {
      vi.unstubAllGlobals()
      hostScript.remove()
    }
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
