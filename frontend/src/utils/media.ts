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
  return genres.split(',').map((g) => g.trim()).filter(Boolean)
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
 * Build a cache-busted poster URL for a media item.
 */
export function posterUrl(mediaItem: { id: number; updatedAt: string }): string {
  const ts = new Date(mediaItem.updatedAt).getTime()
  return `/api/v1/media/${mediaItem.id}/poster?t=${ts}`
}
