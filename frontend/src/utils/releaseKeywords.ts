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
