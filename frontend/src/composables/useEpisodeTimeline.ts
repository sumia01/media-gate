import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import client from '@/api/client'
import { useEventStream } from '@/composables/useEventStream'
import {
  createTimelineCache,
  DAY_WIDTH,
  localToday,
  RAIL_DAYS,
  recenterTimelineRail,
  timelineDate,
  timelineViewport,
  timelineWindow,
  WINDOW_DAYS,
} from '@/utils/episodeTimeline'

export function useEpisodeTimeline() {
  const { on, off } = useEventStream()
  const rail = ref<HTMLElement | null>(null)
  const today = ref(localToday())
  const start = ref(today.value - WINDOW_DAYS * 2)
  const left = ref(WINDOW_DAYS * 2 * DAY_WIDTH)
  const width = ref(DAY_WIDTH * 3)
  const revision = ref(0)
  const dragging = ref(false)
  let disposed = false
  let shifting = false
  let frame = 0
  let observer: ResizeObserver | undefined
  let gesture: { id: number; x: number; left: number } | undefined
  let suppressClick = false
  let refreshTimer: ReturnType<typeof setTimeout> | undefined

  const cache = createTimelineCache(
    async (from, to, signal) => {
      const { data, error } = await client.GET('/media/episode-timeline', { params: { query: { from, to } }, signal })
      if (!data) throw new Error(error?.message || 'Could not load episodes. Please try again.')
      return data.items
    },
    () => {
      revision.value++
    },
  )

  const viewport = computed(() => timelineViewport(start.value, left.value, width.value))
  const days = computed(() => {
    void revision.value
    const result = []
    for (let day = viewport.value.first; day < viewport.value.end; day++) {
      const date = timelineDate(day)
      const window = cache.windows.get(timelineWindow(day))
      result.push({ day, date, window, items: window?.items.filter((item) => item.episode.airDate === date) ?? [] })
    }
    return result
  })
  const visibleRange = computed(() => ({
    first: start.value + Math.floor(left.value / DAY_WIDTH),
    last: start.value + Math.ceil((left.value + width.value) / DAY_WIDTH) - 1,
  }))
  const loading = computed(() => days.value.some((day) => !day.window || day.window.status === 'loading'))
  const failedWindows = computed(() => {
    void revision.value
    return [...cache.windows.values()].filter(
      (window) =>
        window.status === 'error' &&
        window.from < viewport.value.end &&
        window.from + WINDOW_DAYS > viewport.value.first,
    )
  })
  const empty = computed(
    () => !loading.value && !failedWindows.value.length && days.value.every((day) => !day.items.length),
  )

  function loadViewport() {
    const { first, end } = viewport.value
    // Keep at most one spare window on either side of the rendered days.
    cache.retain(timelineWindow(first) - WINDOW_DAYS, timelineWindow(end - 1) + WINDOW_DAYS * 2)
    if (refreshTimer !== undefined) return
    for (let from = timelineWindow(first); from < end; from += WINDOW_DAYS) void cache.ensure(from)
  }

  function queueRefresh() {
    if (disposed || refreshTimer !== undefined) return
    refreshTimer = setTimeout(() => {
      refreshTimer = undefined
      if (!disposed) loadViewport()
    }, 150)
  }

  function handleItemChange(data: { mediaItemId: number }) {
    if (disposed) return
    const invalidated = cache.invalidate(
      (window) =>
        window.status === 'loading' || window.items.some((item) => item.episode.mediaItemId === data.mediaItemId),
    )
    if (invalidated) queueRefresh()
  }

  function handleCatalogChange() {
    if (disposed) return
    // A changed date can enter an empty window. Discard both sides immediately
    // so an old response cannot duplicate the episode at its previous date.
    cache.invalidate(() => true)
    queueRefresh()
  }

  const itemEvents = [
    'download.created',
    'download.sent_to_client',
    'download.failed',
    'download.completed',
    'download.deleted',
    'download.import_completed',
    'download.import_failed',
    'download.seeding_completed',
    'media.resync_completed',
    'media.item_deleted',
  ]
  const catalogEvents = [
    'media.item_matched',
    'media.metadata_refreshed',
    'library.sync_completed',
    'library.sync_failed',
    'library.match_completed',
    'library.match_failed',
  ]

  async function measure() {
    const el = rail.value
    if (!el || disposed || shifting) return
    width.value = el.clientWidth
    const position = recenterTimelineRail(start.value, el.scrollLeft, width.value)
    left.value = position.left
    if (position.start !== start.value) {
      shifting = true
      if (gesture) gesture.left += position.left - el.scrollLeft
      start.value = position.start
      await nextTick()
      if (disposed || !rail.value) return
      el.scrollLeft = position.left
      shifting = false
    }
    loadViewport()
  }

  function onScroll() {
    if (frame || disposed) return
    frame = requestAnimationFrame(() => {
      frame = 0
      void measure()
    })
  }

  function move(direction: number) {
    const el = rail.value
    if (!el) return
    el.scrollLeft += direction * Math.max(DAY_WIDTH, Math.floor(el.clientWidth / DAY_WIDTH) * DAY_WIDTH)
    onScroll()
  }

  async function goToday() {
    const el = rail.value
    if (shifting || disposed || !el) return
    shifting = true
    stopDrag()
    clearTimeout(refreshTimer)
    refreshTimer = undefined
    today.value = localToday()
    cache.reset()
    revision.value++
    start.value = today.value - WINDOW_DAYS * 2
    left.value = WINDOW_DAYS * 2 * DAY_WIDTH + DAY_WIDTH / 2 - el.clientWidth / 2
    await nextTick()
    if (disposed || rail.value !== el) return
    el.scrollLeft = left.value
    shifting = false
    await measure()
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.target !== rail.value || event.altKey || event.ctrlKey || event.metaKey) return
    if (event.key === 'Home') {
      event.preventDefault()
      void goToday()
    } else if (['ArrowLeft', 'ArrowRight', 'PageUp', 'PageDown'].includes(event.key)) {
      event.preventDefault()
      const direction = event.key === 'ArrowLeft' || event.key === 'PageUp' ? -1 : 1
      if (event.key.startsWith('Arrow') && rail.value) {
        rail.value.scrollLeft += direction * DAY_WIDTH
        onScroll()
      } else move(direction)
    }
  }

  function onPointerDown(event: PointerEvent) {
    suppressClick = false
    if (event.pointerType === 'touch' || event.button !== 0 || !event.isPrimary || !rail.value) return
    if ((event.target as HTMLElement).closest('button')) return
    gesture = { id: event.pointerId, x: event.clientX, left: rail.value.scrollLeft }
  }

  function onPointerMove(event: PointerEvent) {
    const el = rail.value
    if (!gesture || gesture.id !== event.pointerId || !el) return
    const delta = event.clientX - gesture.x
    if (!dragging.value && Math.abs(delta) < 6) return
    if (!dragging.value) {
      dragging.value = true
      suppressClick = true
      el.setPointerCapture(event.pointerId)
    }
    event.preventDefault()
    el.scrollLeft = gesture.left - delta
    onScroll()
  }

  function stopDrag() {
    const pointer = gesture?.id
    gesture = undefined
    dragging.value = false
    if (pointer !== undefined && rail.value?.hasPointerCapture(pointer)) rail.value.releasePointerCapture(pointer)
  }

  function onClick(event: MouseEvent) {
    if (suppressClick && event.detail !== 0) {
      event.preventDefault()
      event.stopPropagation()
      suppressClick = false
    }
  }

  onMounted(async () => {
    for (const type of itemEvents) on(type, handleItemChange)
    for (const type of catalogEvents) on(type, handleCatalogChange)
    await goToday()
    if (!rail.value || disposed) return
    observer = new ResizeObserver(onScroll)
    observer.observe(rail.value)
    // Pointerup outside the rail still ends a click-sized gesture.
    window.addEventListener('pointerup', stopDrag)
    window.addEventListener('pointercancel', stopDrag)
  })

  onUnmounted(() => {
    disposed = true
    clearTimeout(refreshTimer)
    for (const type of itemEvents) off(type, handleItemChange)
    for (const type of catalogEvents) off(type, handleCatalogChange)
    stopDrag()
    observer?.disconnect()
    cancelAnimationFrame(frame)
    window.removeEventListener('pointerup', stopDrag)
    window.removeEventListener('pointercancel', stopDrag)
    cache.dispose()
  })

  return {
    rail,
    today,
    start,
    days,
    visibleRange,
    loading,
    failedWindows,
    empty,
    dragging,
    railWidth: RAIL_DAYS * DAY_WIDTH,
    dayWidth: DAY_WIDTH,
    goToday,
    move,
    onScroll,
    onKeydown,
    onPointerDown,
    onPointerMove,
    stopDrag,
    onClick,
    retry: (from: number) => cache.ensure(from, true),
  }
}
