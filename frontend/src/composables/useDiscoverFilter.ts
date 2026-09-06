import { computed, type Ref, ref } from 'vue'

export interface DiscoverIdentity {
  source: string
  externalId: number
  mediaType: 'movie' | 'series'
}

export function discoverKey(item: DiscoverIdentity): string {
  return `${item.source}:${item.mediaType}:${item.externalId}`
}

const storageKey = 'discover.hideInLibrary'
const hidden = ref(false)
let restored = false

export function useDiscoverFilter(isInLibrary: (item: DiscoverIdentity) => boolean, libraryReady: Ref<boolean>) {
  if (!restored) {
    restored = true
    try {
      hidden.value = localStorage.getItem(storageKey) === 'true'
    } catch {
      // Storage may be unavailable in private browsing; the in-memory setting still works.
    }
  }

  const hideInLibrary = computed({
    get: () => hidden.value,
    set: (value: boolean) => {
      hidden.value = value
      try {
        localStorage.setItem(storageKey, String(value))
      } catch {
        // Keep the setting for this session when storage is blocked or full.
      }
    },
  })
  const filterReady = computed(() => !hideInLibrary.value || libraryReady.value)
  const includeItem = (item: DiscoverIdentity) => !hideInLibrary.value || !isInLibrary(item)

  return { hideInLibrary, filterReady, includeItem }
}
