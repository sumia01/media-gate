import { computed, type MaybeRefOrGetter, onMounted, onUnmounted, ref, toValue, watch } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useEventStream } from '@/composables/useEventStream'

type MediaActivity = components['schemas']['MediaActivity']
type MediaActivityPage = components['schemas']['MediaActivityPage']
type RequestKind = 'initial' | 'refresh' | 'older' | 'continue' | 'back'

interface FailedRequest {
  kind: RequestKind
  before?: string
  limit: number
}

const INITIAL_LIMIT = 30
const PAGE_LIMIT = 100
export const MEDIA_ACTIVITY_CAPACITY = 500

function dedupe(items: MediaActivity[], existing: ReadonlySet<number> = new Set()): MediaActivity[] {
  const seen = new Set(existing)
  return items.filter((item) => {
    if (seen.has(item.id)) return false
    seen.add(item.id)
    return true
  })
}

function requestError(kind: RequestKind): string {
  if (kind === 'initial') return 'Could not load activity.'
  if (kind === 'older' || kind === 'continue') return 'Could not load older activity.'
  return 'Could not refresh the latest activity.'
}

export function useMediaActivity(
  mediaItemId: MaybeRefOrGetter<number>,
  active: MaybeRefOrGetter<boolean>,
  expanded: MaybeRefOrGetter<boolean>,
) {
  const items = ref<MediaActivity[]>([])
  const loaded = ref(false)
  const loading = ref(false)
  const loadingKind = ref<RequestKind | null>(null)
  const error = ref('')
  const errorKind = ref<RequestKind | null>(null)
  const dirty = ref(false)
  const hasMore = ref(false)
  const nextCursor = ref<string>()
  const windowMode = ref<'latest' | 'older'>('latest')
  const { connected, on, off } = useEventStream()

  let controller: AbortController | undefined
  let generation = 0
  let revision = 0
  let stopped = false
  let failedRequest: FailedRequest | null = null
  let sawConnection = connected.value
  let sawDisconnect = false

  const visible = () => !stopped && toValue(active) && toValue(expanded)

  function abort() {
    generation++
    controller?.abort()
    controller = undefined
    loading.value = false
    loadingKind.value = null
  }

  function reset() {
    abort()
    items.value = []
    loaded.value = false
    error.value = ''
    errorKind.value = null
    dirty.value = false
    hasMore.value = false
    nextCursor.value = undefined
    windowMode.value = 'latest'
    failedRequest = null
    revision = 0
  }

  function markDirty() {
    if (!loaded.value && !loading.value) return
    revision++
    dirty.value = true
  }

  async function requestPage(kind: RequestKind, before: string | undefined, limit: number) {
    if (!visible() || loading.value) return false

    const capturedId = toValue(mediaItemId)
    const capturedRevision = revision
    const wasDirty = dirty.value
    const requestGeneration = ++generation
    const current = new AbortController()
    controller = current
    loading.value = true
    loadingKind.value = kind
    error.value = ''
    errorKind.value = null

    try {
      const { data, error: failure } = await client.GET('/media/{id}/activity', {
        params: {
          path: { id: capturedId },
          query: { limit, before },
        },
        signal: current.signal,
      })
      if (
        current.signal.aborted ||
        generation !== requestGeneration ||
        capturedId !== toValue(mediaItemId) ||
        !visible()
      ) {
        return false
      }
      if (
        failure ||
        !data ||
        data.items.length > limit ||
        (data.hasMore && (!data.nextCursor || data.items.length === 0))
      ) {
        throw new Error('invalid activity page')
      }

      const page: MediaActivityPage = data
      if (kind === 'older') {
        const existing = new Set(items.value.map((item) => item.id))
        items.value = [...items.value, ...dedupe(page.items, existing)]
      } else {
        items.value = dedupe(page.items)
        if (kind === 'continue') windowMode.value = 'older'
        if (kind === 'initial' || kind === 'refresh' || kind === 'back') windowMode.value = 'latest'
      }
      loaded.value = true
      hasMore.value = page.hasMore
      nextCursor.value = page.nextCursor
      dirty.value =
        kind === 'initial' || kind === 'refresh' || kind === 'back'
          ? revision !== capturedRevision
          : wasDirty || revision !== capturedRevision
      failedRequest = null
      return true
    } catch {
      if (
        current.signal.aborted ||
        generation !== requestGeneration ||
        capturedId !== toValue(mediaItemId) ||
        !visible()
      ) {
        return false
      }
      error.value = requestError(kind)
      errorKind.value = kind
      failedRequest = { kind, before, limit }
      return false
    } finally {
      if (generation === requestGeneration) {
        controller = undefined
        loading.value = false
        loadingKind.value = null
      }
    }
  }

  function loadInitial() {
    return requestPage('initial', undefined, INITIAL_LIMIT)
  }

  function refreshLatest() {
    return requestPage('refresh', undefined, INITIAL_LIMIT)
  }

  function loadOlder() {
    const remaining = MEDIA_ACTIVITY_CAPACITY - items.value.length
    if (!loaded.value || !hasMore.value || !nextCursor.value || remaining <= 0) return Promise.resolve(false)
    return requestPage('older', nextCursor.value, Math.min(PAGE_LIMIT, remaining))
  }

  function continueOlder() {
    if (!loaded.value || !hasMore.value || !nextCursor.value) return Promise.resolve(false)
    return requestPage('continue', nextCursor.value, INITIAL_LIMIT)
  }

  function backToLatest() {
    return requestPage('back', undefined, INITIAL_LIMIT)
  }

  function retry() {
    if (!failedRequest) return Promise.resolve(false)
    return requestPage(failedRequest.kind, failedRequest.before, failedRequest.limit)
  }

  function handleActivityAdded(data: { mediaItemId?: number }) {
    if (data.mediaItemId === toValue(mediaItemId)) markDirty()
  }

  function handleFocus() {
    markDirty()
  }

  watch(
    [() => toValue(mediaItemId), () => toValue(active), () => toValue(expanded)],
    ([id, isActive, isExpanded], previous) => {
      if (previous && id !== previous[0]) reset()
      if (!isActive || !isExpanded) {
        abort()
        return
      }
      if (!loaded.value) void loadInitial()
      else if (dirty.value) void refreshLatest()
    },
    { immediate: true },
  )

  watch(
    connected,
    (isConnected) => {
      if (isConnected) {
        if ((sawConnection && sawDisconnect) || loading.value) markDirty()
        sawConnection = true
        sawDisconnect = false
      } else if (sawConnection) {
        sawDisconnect = true
      }
    },
    { flush: 'sync' },
  )

  onMounted(() => {
    on('media.activity_added', handleActivityAdded)
    window.addEventListener('focus', handleFocus)
  })

  onUnmounted(() => {
    stopped = true
    abort()
    off('media.activity_added', handleActivityAdded)
    window.removeEventListener('focus', handleFocus)
  })

  return {
    items,
    loaded,
    loading,
    loadingKind,
    error,
    errorKind,
    dirty,
    hasMore,
    nextCursor,
    windowMode,
    atCapacity: computed(() => items.value.length >= MEDIA_ACTIVITY_CAPACITY && hasMore.value),
    markDirty,
    refreshLatest,
    loadOlder,
    continueOlder,
    backToLatest,
    retry,
  }
}
