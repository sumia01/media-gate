export function parseReleaseKeywords(value: string): string[] {
  const keywords = value
    .split(',')
    .map((part) => part.trim().toLowerCase())
    .filter(Boolean)
  return [...new Set(keywords)]
}

export function releaseKeywordToken(value: string, caret: number) {
  const position = Math.max(0, Math.min(caret, value.length))
  const start = position === 0 ? 0 : value.lastIndexOf(',', position - 1) + 1
  const nextComma = value.indexOf(',', position)
  const end = nextComma === -1 ? value.length : nextComma
  return {
    start,
    end,
    prefix: value.slice(start, position).trim().toLowerCase(),
    selected: parseReleaseKeywords(`${value.slice(0, start)},${value.slice(end)}`),
  }
}

export function insertReleaseKeyword(value: string, caret: number, keyword: string) {
  const { start, end } = releaseKeywordToken(value, caret)
  const replacement = `${start > 0 ? ' ' : ''}${keyword.trim().toLowerCase()}`
  const suffix = end < value.length ? value.slice(end) : ', '
  return {
    value: `${value.slice(0, start)}${replacement}${suffix}`.toLowerCase(),
    caret: start + replacement.length + (end === value.length ? 2 : 0),
  }
}

// Fixed coordinates keep the popup out of modal layout. Four fixed-height rows
// fit normally; a small visual viewport can reduce the available scroll area.
export function releaseKeywordPopupPosition(
  anchor: { top: number; bottom: number; left: number; width: number },
  viewport: { top: number; left: number; width: number; height: number },
  count: number,
) {
  if (
    anchor.bottom <= viewport.top ||
    anchor.top >= viewport.top + viewport.height ||
    anchor.left + anchor.width <= viewport.left ||
    anchor.left >= viewport.left + viewport.width
  )
    return null
  const margin = 8
  const gap = 4
  const fourRows = 4 * 36 + 10
  const below = Math.max(0, viewport.top + viewport.height - margin - anchor.bottom - gap)
  const above = Math.max(0, anchor.top - gap - viewport.top - margin)
  const openAbove = below < fourRows && above > below
  const maxHeight = Math.min(fourRows, openAbove ? above : below)
  const width = Math.min(anchor.width, viewport.width - margin * 2)
  if (maxHeight < 46 || width <= 0) return null
  const height = Math.min(maxHeight, Math.max(1, Math.min(4, count)) * 36 + 10)
  const left = Math.max(viewport.left + margin, Math.min(anchor.left, viewport.left + viewport.width - margin - width))
  return {
    left: `${left}px`,
    top: `${openAbove ? anchor.top - gap - height : anchor.bottom + gap}px`,
    width: `${width}px`,
    maxHeight: `${maxHeight}px`,
  }
}
