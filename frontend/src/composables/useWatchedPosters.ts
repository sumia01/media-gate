import { onUnmounted, ref } from 'vue'
import client from '@/api/client'
import { discoverKey } from '@/composables/useDiscoverFilter'
import type { WatchedItem } from '@/types/api'
import { posterUrl } from '@/utils/media'

function externalPosterUrl(path: string | undefined): string | null {
  if (!path) return null
  if (/^https?:\/\//.test(path)) return path
  if (path.startsWith('/')) return `https://image.tmdb.org/t/p/w342${path}`
  return null
}

export function useWatchedPosters() {
  const resolved = ref(new Map<string, string | null>())
  const failed = ref(new Map<number, Set<string>>())
  const attempted = new Set<string>()
  const queue: WatchedItem[] = []
  const controller = new AbortController()
  let active = 0

  onUnmounted(() => {
    controller.abort()
    queue.length = 0
  })

  function itemPosterUrl(item: WatchedItem): string | null {
    const candidates = [
      item.mediaItemId ? posterUrl({ id: item.mediaItemId }) : null,
      externalPosterUrl(item.posterPath),
      resolved.value.get(discoverKey(item)),
    ]
    return candidates.find((url) => url && !failed.value.get(item.id)?.has(url)) ?? null
  }

  async function resolvePoster(item: WatchedItem) {
    try {
      const { data } = await client.GET('/search/{source}/{externalId}', {
        params: {
          path: { source: item.source, externalId: item.externalId },
          query: { mediaType: item.mediaType },
        },
        signal: controller.signal,
      })
      if (!controller.signal.aborted) {
        resolved.value.set(discoverKey(item), externalPosterUrl(data?.posterUrl))
      }
    } catch {
      // Artwork is decorative; provider failures leave the placeholder visible.
    } finally {
      active--
      drainQueue()
    }
  }

  function drainQueue() {
    // Old watched lists can contain many local filenames. Do not fan out an
    // unbounded batch of provider lookups when those library items are deleted.
    while (!controller.signal.aborted && active < 4 && queue.length) {
      const item = queue.shift()!
      active++
      void resolvePoster(item)
    }
  }

  function loadMissingPosters(items: WatchedItem[]) {
    if (controller.signal.aborted) return
    for (const item of items) {
      const key = discoverKey(item)
      if (itemPosterUrl(item) || attempted.has(key)) continue
      attempted.add(key)
      queue.push(item)
    }
    drainQueue()
  }

  function onPosterError(item: WatchedItem) {
    const url = itemPosterUrl(item)
    if (url) {
      const urls = failed.value.get(item.id) ?? new Set<string>()
      urls.add(url)
      failed.value.set(item.id, urls)
    }
    loadMissingPosters([item])
  }

  return { itemPosterUrl, loadMissingPosters, onPosterError }
}
