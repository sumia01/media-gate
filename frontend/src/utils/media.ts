/**
 * Parse a genres string that may be JSON array or comma-separated.
 */
export function parseGenres(genres: string | undefined | null): string[] {
  if (!genres) return []
  try {
    const parsed = JSON.parse(genres)
    if (Array.isArray(parsed)) return parsed
  } catch {
    // Fall back to comma-separated
  }
  return genres
    .split(',')
    .map((g) => g.trim())
    .filter(Boolean)
}

/**
 * Build a profile image URL from a TMDB path or TVDB full URL.
 */
export function profileImageUrl(person: { image?: string }): string | null {
  if (!person.image) return null
  if (person.image.startsWith('/')) {
    return `https://image.tmdb.org/t/p/w185${person.image}`
  }
  if (person.image.startsWith('http')) {
    return person.image
  }
  return null
}

/**
 * Build the cached-poster URL for a media item.
 *
 * Correctness comes from the endpoint, not from here: it serves
 * `Cache-Control: no-cache` with an ETag, so the browser revalidates on every
 * use and can never pin a stale or partially-served image. Callers that omit
 * `updatedAt` are still correct.
 *
 * `updatedAt` is a liveness hint. An open tab that re-matches an item keeps
 * rendering the same `<img src>`, and Vue does not patch an attribute whose
 * value is unchanged — so without a differing URL the browser never re-asks and
 * the old artwork stays on screen until a reload. Feeding the timestamp in
 * changes the URL exactly when the item changed, forcing that re-request. It is
 * free for media items, which already carry the field.
 */
export function posterUrl(mediaItem: { id: number; updatedAt?: string }): string {
  const base = `/api/v1/media/${mediaItem.id}/poster`
  if (!mediaItem.updatedAt) return base
  const ts = new Date(mediaItem.updatedAt).getTime()
  return Number.isNaN(ts) ? base : `${base}?t=${ts}`
}

/**
 * Format a size string for display.
 * Handles both human-readable strings ("1.37 GiB") and pure byte numbers ("1471492").
 * If the string already contains a unit suffix, returns it as-is.
 */
export function formatSize(size: string | undefined): string {
  if (!size) return ''
  const bytes = parseFloat(size)
  if (Number.isNaN(bytes)) return size
  // If parsing consumed the entire string, it's a raw number → format it
  if (size.trim() === String(bytes) || /^\d+$/.test(size.trim())) {
    return formatBytes(bytes)
  }
  // Already has a unit (e.g. "1.37 GiB") — return as-is
  return size
}

/**
 * Format a numeric byte count for display.
 */
export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  const kb = bytes / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB`
  const mb = kb / 1024
  if (mb < 1024) return `${mb.toFixed(1)} MB`
  const gb = mb / 1024
  if (gb < 1024) return `${gb.toFixed(1)} GB`
  return `${(gb / 1024).toFixed(1)} TB`
}
