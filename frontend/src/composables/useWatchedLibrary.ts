import { onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import { type DiscoverIdentity, discoverKey } from '@/composables/useDiscoverFilter'
import type { DiscoverItem } from '@/types/api'

// Shared watched/in-library badge state and preview navigation for discover
// grids (HomeView, DiscoverCategoryView, SimilarMediaView).
export function useWatchedLibrary() {
  const router = useRouter()

  const watchedSet = ref<Set<string>>(new Set())
  const libraryMap = ref<Map<string, number>>(new Map())
  const libraryReady = ref(false)
  const libraryLoading = ref(true)
  const libraryFailed = ref(false)
  const controller = new AbortController()
  onUnmounted(() => controller.abort())

  function isWatched(item: DiscoverIdentity): boolean {
    return watchedSet.value.has(discoverKey(item))
  }

  function isInLibrary(item: DiscoverIdentity): boolean {
    return libraryMap.value.has(discoverKey(item))
  }

  function libraryMediaId(item: DiscoverIdentity): number | undefined {
    return libraryMap.value.get(discoverKey(item))
  }

  async function fetchWatched() {
    // Badges are decorative — a failed fetch must not become an unhandled
    // rejection or break the page; the previous state is kept.
    try {
      const { data } = await client.GET('/watched', { signal: controller.signal })
      if (!data || controller.signal.aborted) return
      const set = new Set<string>()
      for (const item of data.items) {
        set.add(discoverKey(item))
      }
      watchedSet.value = set
    } catch {
      // keep the previous state
    }
  }

  async function fetchLibraryItems() {
    libraryLoading.value = true
    libraryFailed.value = false
    try {
      const { data } = await client.GET('/media/external-ids', { signal: controller.signal })
      if (controller.signal.aborted) return
      if (!data) throw new Error('Library membership unavailable')
      const map = new Map<string, number>()
      for (const item of data.items) {
        map.set(discoverKey(item), item.mediaItemId)
      }
      libraryMap.value = map
      libraryReady.value = true
    } catch {
      if (!controller.signal.aborted) libraryFailed.value = true
    } finally {
      if (!controller.signal.aborted) libraryLoading.value = false
    }
  }

  function goToPreview(item: DiscoverItem) {
    const mediaId = libraryMediaId(item)
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

  return {
    isWatched,
    isInLibrary,
    libraryMediaId,
    libraryReady,
    libraryLoading,
    libraryFailed,
    fetchWatched,
    fetchLibraryItems,
    goToPreview,
  }
}
