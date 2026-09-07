import type { MediaRequestAttribution } from '@/types/api'

type RequestScope = MediaRequestAttribution['scope']

export function mediaRequesterNames(
  requests: MediaRequestAttribution[],
  scope?: RequestScope,
  seasonNumber?: number,
  episodeNumber?: number,
): string[] {
  const requesters = new Map<string, string>()
  for (const request of requests) {
    if (
      (scope && request.scope !== scope) ||
      (seasonNumber !== undefined && request.seasonNumber !== seasonNumber) ||
      (episodeNumber !== undefined && request.episodeNumber !== episodeNumber)
    ) {
      continue
    }
    const key = request.requester.id ? `id:${request.requester.id}` : `name:${request.requester.name}`
    requesters.set(key, request.requester.name)
  }
  return [...requesters.values()]
}
