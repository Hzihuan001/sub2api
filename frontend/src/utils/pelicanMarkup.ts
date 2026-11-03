const SANDBOX_CSP = "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none';\">"
const PREVIEW_STYLE = '<style id="moshu-showcase-preview-style">html,body{box-sizing:border-box!important;width:100%!important;min-width:0!important;max-width:100%!important;min-height:100%!important;margin:0!important;overflow-x:hidden!important}body{padding:0!important}main{max-width:100%!important}svg{max-width:100%!important}</style>'

// The showcase is specifically for viewing animated output. Some source pages
// pause themselves when the host OS requests reduced motion; the sandboxed
// preview opts into motion while leaving downloaded source files untouched.
const ANIMATION_BOOTSTRAP = '<script id="moshu-showcase-animation-bootstrap">(()=>{const nativeMatchMedia=window.matchMedia?.bind(window);if(!nativeMatchMedia)return;window.matchMedia=query=>{const result=nativeMatchMedia(query);if(!/prefers-reduced-motion/i.test(query))return result;return new Proxy(result,{get(target,key){if(key==="matches")return false;const value=Reflect.get(target,key,target);return typeof value==="function"?value.bind(target):value}})}})();</script>'

function prepareSourceDocument(document: Document) {
  for (const meta of Array.from(document.querySelectorAll('meta[http-equiv]'))) {
    if ((meta.getAttribute('http-equiv') || '').toLowerCase() === 'content-security-policy') {
      meta.remove()
    }
  }
  for (const style of Array.from(document.querySelectorAll('style'))) {
    style.textContent = (style.textContent || '').replace(
      /prefers-reduced-motion\s*:\s*reduce/gi,
      'prefers-reduced-motion: force-motion'
    )
    if (/prefers-reduced-motion\s*:\s*reduce/i.test(style.media)) {
      style.media = style.media.replace(/prefers-reduced-motion\s*:\s*reduce/gi, 'prefers-reduced-motion: force-motion')
    }
  }
}

function injectPreviewPolicy(head: Element) {
  head.insertAdjacentHTML('afterbegin', `${SANDBOX_CSP}${ANIMATION_BOOTSTRAP}`)
  head.insertAdjacentHTML('beforeend', PREVIEW_STYLE)
}

function isNarrativeElement(element: Element) {
  if (!/^(?:p|pre|code|blockquote|figcaption|div|span)$/i.test(element.tagName)) return false
  return /```|save\s+this\s+as|open\s+it\s+in\s+a\s+browser|short\s+script\s+drives/i.test(element.textContent || '')
}

function removeSvgAdjacentNarrative(document: Document) {
  const svg = document.querySelector('svg')
  const parent = svg?.parentElement
  if (!parent || !svg) return

  for (const node of Array.from(parent.childNodes)) {
    if (node === svg) continue
    if (node.nodeType === Node.TEXT_NODE && node.textContent?.trim()) {
      node.remove()
      continue
    }
    if (node.nodeType === Node.ELEMENT_NODE && isNarrativeElement(node as Element)) {
      node.remove()
    }
  }
}

/**
 * Extract the renderable part of a model response without changing the
 * downloadable source. Kiro frequently wraps HTML in Markdown fences and may
 * append an explanation after the artwork.
 */
export function extractMarkup(value: string) {
  let text = value.replace(/^\uFEFF/, '').trim()
  text = text.replace(/^```(?:html|svg|xml)?(?:\r?\n|\s)*/i, '').trim()
  text = text.replace(/(?:\r?\n|\s)*```\s*$/i, '').trim()
  // A fragment may put its animation styles before the SVG and its animation
  // script after it. Keep those siblings rather than extracting the SVG alone.
  const start = text.search(/<!doctype\s+html|<html\b|<style\b|<script\b|<svg\b/i)
  if (start < 0) return text
  const markup = text.slice(start).trim()
  const htmlEnd = markup.search(/<\/html\s*>/i)
  if (htmlEnd >= 0) {
    const closeEnd = markup.slice(htmlEnd).search(/>/)
    return markup.slice(0, htmlEnd + closeEnd + 1).trim()
  }
  return markup
}

export function isMarkup(value?: string) {
  const text = extractMarkup(value || '').toLowerCase()
  return /^(?:<!doctype\s+html|<html\b|<svg\b)/i.test(text) ||
    (/^<(?:style|script)\b/i.test(text) && /<svg\b/i.test(text))
}

/**
 * Build an isolated preview document. Full HTML documents must not be nested
 * inside another body: browsers then move text nodes and trailing explanations
 * outside the SVG, which is the rendering issue seen in the showcase.
 */
export function sandboxDocument(value: string) {
  const markup = value.trim()

  const fullDocument = /^(?:<!doctype\s+html[^>]*>\s*)?<html\b/i.test(markup)
  if (fullDocument) {
    if (typeof DOMParser !== 'undefined') {
      const parsed = new DOMParser().parseFromString(markup, 'text/html')
      prepareSourceDocument(parsed)
      removeSvgAdjacentNarrative(parsed)
      const head = parsed.head || parsed.documentElement.insertBefore(parsed.createElement('head'), parsed.body)
      injectPreviewPolicy(head)
      return `<!doctype html>${parsed.documentElement.outerHTML}`
    }
    const headOpen = /<head\b[^>]*>/i.exec(markup)
    if (headOpen?.index != null) {
      const insertAt = headOpen.index + headOpen[0].length
      return `${markup.slice(0, insertAt)}${SANDBOX_CSP}${ANIMATION_BOOTSTRAP}${markup.slice(insertAt).replace(/<\/head\s*>/i, `${PREVIEW_STYLE}</head>` )}`
    }
  }

  // A fragment is parsed here only for the visible preview. Remove direct
  // narrative text around the SVG while retaining scripts/styles that drive
  // the animation. This does not alter the source used by Download.
  if (typeof DOMParser !== 'undefined') {
    const parsed = new DOMParser().parseFromString(markup, 'text/html')
    prepareSourceDocument(parsed)
    if (parsed.querySelector('svg')) {
      removeSvgAdjacentNarrative(parsed)
      // DOMParser moves leading style/script elements into head. Serialize the
      // whole parsed document so those animation dependencies survive too.
      injectPreviewPolicy(parsed.head)
      return `<!doctype html>${parsed.documentElement.outerHTML}`
    }
  }

  return `<!doctype html><html><head>${SANDBOX_CSP}${PREVIEW_STYLE}${ANIMATION_BOOTSTRAP}</head><body>${markup}</body></html>`
}
