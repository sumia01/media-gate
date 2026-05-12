/**
 * Sanitize HTML to prevent XSS by allowing only a safe subset of tags and attributes.
 * Uses the browser's DOMParser which does not execute scripts during parsing.
 */

const ALLOWED_TAGS = new Set(['a', 'b', 'i', 'em', 'strong', 'br', 'p', 'span', 'code'])
const ALLOWED_ATTRS: Record<string, Set<string>> = {
  a: new Set(['href', 'target', 'rel']),
}

function sanitizeNode(node: Node): string {
  if (node.nodeType === Node.TEXT_NODE) {
    return node.textContent ?? ''
  }

  if (node.nodeType !== Node.ELEMENT_NODE) {
    return ''
  }

  const el = node as Element
  const tag = el.tagName.toLowerCase()

  if (!ALLOWED_TAGS.has(tag)) {
    // Strip the tag but keep safe child content.
    let inner = ''
    for (const child of el.childNodes) {
      inner += sanitizeNode(child)
    }
    return inner
  }

  // Build sanitized attributes.
  let attrs = ''
  const allowedAttrs = ALLOWED_ATTRS[tag]
  if (allowedAttrs) {
    for (const attr of el.attributes) {
      if (!allowedAttrs.has(attr.name.toLowerCase())) continue
      const value = attr.value
      // Block javascript: URLs in href.
      if (attr.name === 'href' && /^\s*javascript:/i.test(value)) continue
      attrs += ` ${attr.name}="${value.replace(/"/g, '&quot;')}"`
    }
  }

  // Force external links to open safely.
  if (tag === 'a') {
    if (!attrs.includes('target=')) attrs += ' target="_blank"'
    if (!attrs.includes('rel=')) attrs += ' rel="noopener noreferrer"'
  }

  let inner = ''
  for (const child of el.childNodes) {
    inner += sanitizeNode(child)
  }

  if (tag === 'br') return '<br>'
  return `<${tag}${attrs}>${inner}</${tag}>`
}

export function sanitizeHtml(html: string): string {
  if (!html) return ''
  const doc = new DOMParser().parseFromString(html, 'text/html')
  let result = ''
  for (const child of doc.body.childNodes) {
    result += sanitizeNode(child)
  }
  return result
}
