import { computed, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'

export interface DiscoverPageData<T> {
  items: T[]
  page: number
  totalPages: number
}

export function usePagedDiscover<T>(
  fetcher: (page: number, signal: AbortSignal) => Promise<DiscoverPageData<T> | undefined>,
  keyOf: (item: T) => string,
  options: {
    include?: (item: T) => boolean
    ready?: () => boolean
    filterKey?: () => unknown
    infiniteScroll?: boolean
  } = {},
) {
  // Retain the unfiltered pages so toggling never repeats provider requests.
  const allItems = shallowRef<T[]>([])
  const ready = computed(() => options.ready?.() ?? true)
  const items = computed(() => (ready.value ? allItems.value.filter((item) => options.include?.(item) ?? true) : []))
  const hiddenCount = computed(() => (ready.value ? allItems.value.length - items.value.length : 0))
  const page = ref(0)
  const totalPages = ref(1)
  const loading = ref(false)
  const initialLoading = ref(true)
  const loadFailed = ref(false)
  const autoPaused = ref(false)
  const hasMore = computed(() => page.value < totalPages.value)
  const sentinel = ref<HTMLElement | null>(null)
  const seen = new Set<string>()
  let epoch = 0
  let disposed = false
  let controller: AbortController | undefined
  let observer: IntersectionObserver | undefined

  async function fetchPage() {
    if (disposed || !ready.value || loading.value || !hasMore.value) return
    loading.value = true
    loadFailed.value = false
    autoPaused.value = false
    const requestEpoch = epoch
    controller = new AbortController()
    const { signal } = controller
    try {
      // Skip fully hidden/duplicate pages, but never scan an entire catalogue
      // automatically. The user can explicitly continue after five such pages.
      for (let attempts = 0; attempts < 5; attempts += 1) {
        const nextPage = page.value + 1
        const data = await fetcher(nextPage, signal)
        if (requestEpoch !== epoch) return
        if (!data || data.page !== nextPage || !Number.isInteger(data.totalPages) || data.totalPages < 0) {
          throw new Error('Invalid discover page')
        }
        const fresh: T[] = []
        for (const item of data.items) {
          const key = keyOf(item)
          if (seen.has(key)) continue
          seen.add(key)
          fresh.push(item)
        }
        allItems.value = [...allItems.value, ...fresh]
        page.value = nextPage
        // TMDB exposes at most 500 pages, even when total_pages is larger.
        totalPages.value = data.items.length ? Math.min(data.totalPages, 500) : nextPage
        if (!hasMore.value || !ready.value || fresh.some((item) => options.include?.(item) ?? true)) break
        if (attempts === 4) autoPaused.value = true
      }
    } catch {
      if (requestEpoch === epoch) loadFailed.value = true
    } finally {
      if (requestEpoch === epoch) {
        loading.value = false
        initialLoading.value = false
        await nextTick()
        if (requestEpoch === epoch) rearmObserver()
      }
    }
  }

  function autoFetch() {
    if (!loadFailed.value && !autoPaused.value) void fetchPage()
  }

  function rearmObserver() {
    if (!disposed && sentinel.value && observer) {
      observer.unobserve(sentinel.value)
      observer.observe(sentinel.value)
    }
  }

  function reset() {
    epoch += 1
    controller?.abort()
    seen.clear()
    allItems.value = []
    page.value = 0
    totalPages.value = 1
    loading.value = false
    initialLoading.value = true
    loadFailed.value = false
    autoPaused.value = false
    void fetchPage()
  }

  watch(
    [ready, () => options.filterKey?.()],
    async () => {
      autoPaused.value = false
      await nextTick()
      if (disposed) return
      if (!items.value.length) autoFetch()
      rearmObserver()
    },
    { flush: 'post' },
  )

  watch(
    sentinel,
    (element, previous) => {
      if (previous) observer?.unobserve(previous)
      if (element) observer?.observe(element)
    },
    { flush: 'post' },
  )

  onMounted(() => {
    if (options.infiniteScroll !== false && typeof IntersectionObserver !== 'undefined') {
      observer = new IntersectionObserver(
        (entries) => {
          if (entries.some((entry) => entry.isIntersecting)) autoFetch()
        },
        { rootMargin: '200px' },
      )
      rearmObserver()
    }
    void fetchPage()
  })

  onUnmounted(() => {
    disposed = true
    epoch += 1
    controller?.abort()
    observer?.disconnect()
  })

  return {
    items,
    hiddenCount,
    page,
    totalPages,
    loading,
    initialLoading,
    loadFailed,
    hasMore,
    sentinel,
    fetchPage,
    reset,
  }
}
