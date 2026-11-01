const SANDBOX_CSP = "<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src data: blob:; connect-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none';\">"

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
  const start = text.search(/<!doctype\s+html|<html\b|<svg\b/i)
  if (start < 0) return text
  const markup = text.slice(start).trim()
  const htmlEnd = markup.search(/<\/html\s*>/i)
  if (htmlEnd >= 0) {
    const closeEnd = markup.slice(htmlEnd).search(/>/)
    return markup.slice(0, htmlEnd + closeEnd + 1).trim()
  }
  const svgEnd = markup.search(/<\/svg\s*>/i)
  if (svgEnd >= 0) {
    const closeEnd = markup.slice(svgEnd).search(/>/)
    return markup.slice(0, svgEnd + closeEnd + 1).trim()
  }
  return markup
}

export function isMarkup(value?: string) {
  const text = extractMarkup(value || '').toLowerCase()
  return /^(?:<!doctype\s+html|<html\b|<svg\b)/i.test(text)
}

/**
 * Build an isolated preview document. Full HTML documents must not be nested
 * inside another body: browsers then move text nodes and trailing explanations
 * outside the SVG, which is the rendering issue seen in the showcase.
 */
export function sandboxDocument(value: string) {
  const markup = value.trim()
  if (/<meta[^>]+content-security-policy/i.test(markup)) return markup

  const fullDocument = /^(?:<!doctype\s+html[^>]*>\s*)?<html\b/i.test(markup)
  if (fullDocument) {
    if (typeof DOMParser !== 'undefined') {
      const parsed = new DOMParser().parseFromString(markup, 'text/html')
      removeSvgAdjacentNarrative(parsed)
      const head = parsed.head || parsed.documentElement.insertBefore(parsed.createElement('head'), parsed.body)
      head.insertAdjacentHTML('afterbegin', SANDBOX_CSP)
      return `<!doctype html>${parsed.documentElement.outerHTML}`
    }
    const headOpen = /<head\b[^>]*>/i.exec(markup)
    if (headOpen?.index != null) {
      const insertAt = headOpen.index + headOpen[0].length
      return `${markup.slice(0, insertAt)}${SANDBOX_CSP}${markup.slice(insertAt)}`
    }
  }

  // A fragment is parsed here only for the visible preview. Remove direct
  // narrative text around the SVG while retaining scripts/styles that drive
  // the animation. This does not alter the source used by Download.
  if (typeof DOMParser !== 'undefined') {
    const parsed = new DOMParser().parseFromString(markup, 'text/html')
    if (parsed.querySelector('svg')) {
      removeSvgAdjacentNarrative(parsed)
      return `<!doctype html><html><head>${SANDBOX_CSP}</head><body>${parsed.body.innerHTML}</body></html>`
    }
  }

  return `<!doctype html><html><head>${SANDBOX_CSP}</head><body>${markup}</body></html>`
}
