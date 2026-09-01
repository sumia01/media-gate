import { ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import type { DiscoverItem } from '@/types/api'

// Shared watched/in-library badge state and preview navigation for discover
// grids (HomeView, DiscoverCategoryView, SimilarMediaView).
export function useWatchedLibrary() {
  const router = useRouter()

  const watchedSet = ref<Set<string>>(new Set())
  const libraryMap = ref<Map<string, number>>(new Map())

  function watchedKey(source: string, externalId: number): string {
    return `${source}:${externalId}`
  }

  function isWatched(source: string, externalId: number): boolean {
    return watchedSet.value.has(watchedKey(source, externalId))
  }

  function isInLibrary(source: string, externalId: number): boolean {
    return libraryMap.value.has(watchedKey(source, externalId))
  }

  function libraryMediaId(source: string, externalId: number): number | undefined {
    return libraryMap.value.get(watchedKey(source, externalId))
  }

  async function fetchWatched() {
    // Badges are decorative — a failed fetch must not become an unhandled
    // rejection or break the page; the previous state is kept.
    try {
      const { data } = await client.GET('/watched')
      const set = new Set<string>()
      for (const item of data?.items ?? []) {
        set.add(watchedKey(item.source, item.externalId))
      }
      watchedSet.value = set
    } catch {
      // keep the previous state
    }
  }

  async function fetchLibraryItems() {
    try {
      const { data } = await client.GET('/media/external-ids')
      const map = new Map<string, number>()
      for (const item of data?.items ?? []) {
        map.set(watchedKey(item.source, item.externalId), item.mediaItemId)
      }
      libraryMap.value = map
    } catch {
      // keep the previous state
    }
  }

  function goToPreview(item: DiscoverItem) {
    const mediaId = libraryMediaId(item.source, item.externalId)
    if (mediaId !== undefined) {
      router.push({ name: 'media-detail', params: { id: mediaId } })
      return
    }
    router.push({
      name: 'media-preview',
      params: { source: item.source, externalId: item.externalId },
      query: { mediaType: item.mediaType },
    })
  }

  return { watchedKey, isWatched, isInLibrary, libraryMediaId, fetchWatched, fetchLibraryItems, goToPreview }
}
