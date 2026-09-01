import { computed, onMounted, onUnmounted, ref, shallowRef } from 'vue'

export interface DiscoverPageData<T> {
  items: T[]
  page: number
  totalPages: number
}

// Paged discover grid state: infinite scroll via an IntersectionObserver
// sentinel, append with dedup, error surfacing, and stale-response protection
// for views whose route params can change while a request is in flight.
export function usePagedDiscover<T>(
  fetcher: (page: number) => Promise<DiscoverPageData<T> | undefined>,
  keyOf: (item: T) => string,
) {
  const items = shallowRef<T[]>([])
  const page = ref(0)
  const totalPages = ref(1)
  const loading = ref(false)
  const initialLoading = ref(true)
  const loadFailed = ref(false)

  // Bumped by reset() and on unmount; a response whose epoch no longer
  // matches belongs to a previous target and is discarded, not committed.
  let epoch = 0
  const seen = new Set<string>()

  async function fetchPage() {
    if (loading.value || page.value >= totalPages.value) return
    loading.value = true
    loadFailed.value = false
    const requestEpoch = epoch
    const nextPage = page.value + 1
    try {
      const data = await fetcher(nextPage)
      if (requestEpoch !== epoch) return
      if (!data) {
        loadFailed.value = true
        return
      }
      // TMDB list ordering shifts between page snapshots, so a title can
      // repeat across pages — dedup to keep v-for keys unique.
      const fresh = data.items.filter((item) => !seen.has(keyOf(item)))
      for (const item of fresh) {
        seen.add(keyOf(item))
      }
      items.value = [...items.value, ...fresh]
      if (data.items.length === 0 && nextPage > 1) {
        // An empty non-first page means the upstream list shrank, or the
        // backend swallowed a provider error into an empty result — end
        // pagination here instead of adopting a bogus totalPages like 0.
        totalPages.value = page.value
      } else {
        page.value = data.page
        totalPages.value = data.totalPages
      }
      rearmObserver()
    } catch {
      if (requestEpoch === epoch) {
        loadFailed.value = true
      }
    } finally {
      if (requestEpoch === epoch) {
        loading.value = false
        initialLoading.value = false
      }
    }
  }

  // Restart from page 1 (e.g. the route now targets a different title);
  // discards any in-flight response via the epoch bump.
  function reset() {
    epoch += 1
    seen.clear()
    items.value = []
    page.value = 0
    totalPages.value = 1
    loading.value = false
    initialLoading.value = true
    loadFailed.value = false
    fetchPage()
  }

  const sentinel = ref<HTMLElement | null>(null)
  let observer: IntersectionObserver | null = null
  let rafId = 0

  // observe() always delivers a fresh record with the current intersection
  // state, so re-arming after every append keeps infinite scroll alive even
  // when the initial callback fired while a fetch was in flight, or appended
  // content leaves the sentinel inside the viewport margin (tall viewports)
  // and no further intersection transitions would ever occur.
  function rearmObserver() {
    if (observer && sentinel.value) {
      observer.unobserve(sentinel.value)
      observer.observe(sentinel.value)
    }
  }

  onMounted(() => {
    fetchPage()

    observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          fetchPage()
        }
      },
      { rootMargin: '200px' },
    )

    const check = () => {
      if (sentinel.value) {
        observer!.observe(sentinel.value)
      } else {
        rafId = requestAnimationFrame(check)
      }
    }
    check()
  })

  onUnmounted(() => {
    epoch += 1
    observer?.disconnect()
    cancelAnimationFrame(rafId)
  })

  const hasMore = computed(() => page.value < totalPages.value)

  return { items, page, totalPages, loading, initialLoading, loadFailed, hasMore, sentinel, fetchPage, reset }
}
