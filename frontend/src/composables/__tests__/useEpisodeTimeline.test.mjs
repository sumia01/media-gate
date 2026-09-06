import assert from 'node:assert/strict'
import { registerHooks } from 'node:module'
import { test } from 'node:test'
import { createRenderer, markRaw, nextTick } from 'vue'
import {
  DAY_WIDTH,
  timelineAvailability,
  timelineDate,
  timelineDay,
  timelineWindow,
  WINDOW_DAYS,
} from '../../utils/episodeTimeline.ts'

// Load the real composable without a browser or additional test dependencies.
const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === '@/api/client') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export default { GET: (...args) => globalThis.timelineTestGet(...args) }',
      }
    }
    if (specifier === '@/composables/useEventStream') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export const useEventStream = () => globalThis.timelineTestStream',
      }
    }
    if (specifier === '@/utils/episodeTimeline') {
      return { shortCircuit: true, url: new URL('../../utils/episodeTimeline.ts', import.meta.url).href }
    }
    return nextResolve(specifier, context)
  },
})
const { useEpisodeTimeline } = await import('../useEpisodeTimeline.ts')
hooks.deregister()

async function mountTimeline(t) {
  const previous = new Map(
    ['window', 'ResizeObserver', 'requestAnimationFrame', 'cancelAnimationFrame', 'setTimeout', 'clearTimeout'].map(
      (key) => [key, globalThis[key]],
    ),
  )
  const requests = []
  const subscriptions = new Map()
  const listeners = new Map()
  const frames = new Map()
  const timers = new Map()
  let nextID = 0
  let disconnected = false
  globalThis.timelineTestGet = (_path, options) =>
    new Promise((resolve, reject) => requests.push({ ...options, resolve, reject }))
  globalThis.timelineTestStream = {
    on(type, handler) {
      subscriptions.set(type, handler)
    },
    off(type, handler) {
      assert.equal(subscriptions.get(type), handler)
      subscriptions.delete(type)
    },
  }
  globalThis.window = {
    addEventListener(type, handler) {
      listeners.set(type, handler)
    },
    removeEventListener(type) {
      listeners.delete(type)
    },
  }
  globalThis.ResizeObserver = class {
    observe() {}
    disconnect() {
      disconnected = true
    }
  }
  globalThis.requestAnimationFrame = (callback) => {
    frames.set(++nextID, callback)
    return nextID
  }
  globalThis.cancelAnimationFrame = (id) => frames.delete(id)
  globalThis.setTimeout = (callback, delay) => {
    timers.set(++nextID, { callback, delay })
    return nextID
  }
  globalThis.clearTimeout = (id) => timers.delete(id)
  const el = markRaw({
    scrollLeft: 0,
    clientWidth: 320,
    captured: new Set(),
    setPointerCapture(id) {
      this.captured.add(id)
    },
    hasPointerCapture(id) {
      return this.captured.has(id)
    },
    releasePointerCapture(id) {
      this.captured.delete(id)
    },
  })
  const renderer = createRenderer({
    createComment: () => ({}),
    insert() {},
    remove() {},
    parentNode: () => null,
    nextSibling: () => null,
  })
  let timeline
  const app = renderer.createApp({
    setup() {
      timeline = useEpisodeTimeline()
      timeline.rail.value = el
      return () => null
    },
  })
  let mounted = true
  function unmount() {
    if (mounted) {
      mounted = false
      app.unmount()
    }
  }
  async function flush() {
    for (const [id, callback] of frames) {
      frames.delete(id)
      callback()
    }
    await nextTick()
    await nextTick()
    await nextTick()
  }
  async function flushRefresh() {
    const pending = [...timers.values()]
    timers.clear()
    for (const { callback, delay } of pending) {
      assert.equal(delay, 150)
      callback()
    }
    await flush()
  }
  t.after(() => {
    unmount()
    for (const [key, value] of previous) {
      if (value === undefined) delete globalThis[key]
      else globalThis[key] = value
    }
    delete globalThis.timelineTestGet
    delete globalThis.timelineTestStream
  })
  app.mount({})
  await flush()
  return {
    timeline,
    el,
    requests,
    listeners,
    subscriptions,
    frames,
    timers,
    flush,
    flushRefresh,
    unmount,
    get disconnected() {
      return disconnected
    },
    emit(type, data) {
      subscriptions.get(type)?.(data)
    },
  }
}

test('mounted timeline loads lazily, preserves drag/recenter anchors and cleans up on unmount', async (t) => {
  const harness = await mountTimeline(t)
  const { timeline, el, requests, flush } = harness
  function pointer(x, type = 'mouse') {
    return {
      pointerType: type,
      button: 0,
      isPrimary: true,
      pointerId: 1,
      clientX: x,
      target: { closest: () => null },
      preventDefault() {},
    }
  }
  function click() {
    let prevented = false
    timeline.onClick({
      detail: 1,
      preventDefault() {
        prevented = true
      },
      stopPropagation() {},
    })
    return prevented
  }
  assert.ok(requests.length >= 1 && requests.length <= 2, 'only windows covering nearby days are loaded')
  assert.equal(
    (timeline.today.value - timeline.start.value) * DAY_WIDTH + DAY_WIDTH / 2 - el.scrollLeft,
    el.clientWidth / 2,
    'today starts centered in the viewport',
  )
  assert.ok(timeline.days.value.length <= 7, 'narrow screens only render nearby dates')
  for (const request of requests) {
    assert.equal(timelineDay(request.params.query.to) - timelineDay(request.params.query.from), 14)
    request.resolve({ data: { items: [] } })
  }
  await flush()
  const initialRequests = requests.length
  assert.ok(timeline.empty.value)
  for (let i = 0; i < 10; i++) {
    timeline.onScroll()
    await flush()
  }
  assert.equal(requests.length, initialRequests, 'empty windows never cause a fetch loop')

  timeline.onPointerDown(pointer(100))
  timeline.onPointerMove(pointer(103))
  timeline.stopDrag()
  assert.equal(click(), false, 'ordinary card clicks navigate')
  timeline.onPointerDown(pointer(100))
  timeline.onPointerMove(pointer(70))
  assert.ok(timeline.dragging.value)
  assert.ok(el.hasPointerCapture(1))
  timeline.stopDrag()
  assert.equal(click(), true, 'a drag does not activate a card')
  assert.equal(el.captured.size, 0)
  timeline.onPointerDown(pointer(100))
  timeline.onPointerMove(pointer(70))
  timeline.stopDrag()
  timeline.onPointerDown(pointer(100, 'touch'))
  assert.equal(click(), false, 'a cancelled mouse drag cannot swallow a subsequent touch tap')
  assert.equal(timeline.dragging.value, false, 'touch scrolling stays native')

  for (const position of [20, DAY_WIDTH * 62]) {
    el.scrollLeft = position
    const anchored = timeline.start.value + position / DAY_WIDTH
    timeline.onPointerDown(pointer(100))
    timeline.onScroll()
    await flush()
    assert.equal(timeline.start.value + el.scrollLeft / DAY_WIDTH, anchored)
    timeline.onPointerMove(pointer(90))
    assert.equal(
      timeline.start.value + el.scrollLeft / DAY_WIDTH,
      anchored + 10 / DAY_WIDTH,
      'drag offset survives rebasing',
    )
    timeline.stopDrag()
    await flush()
  }
  const before = timeline.visibleRange.value.first
  timeline.onKeydown({ target: el, key: 'ArrowLeft', preventDefault() {} })
  await flush()
  assert.equal(timeline.visibleRange.value.first, before - 1)
  timeline.move(1)
  await flush()
  assert.equal(timeline.visibleRange.value.first, before)

  const stale = requests.filter((request) => !request.signal.aborted).slice(initialRequests)
  await timeline.goToday()
  await flush()
  assert.equal(
    (timeline.today.value - timeline.start.value) * DAY_WIDTH + DAY_WIDTH / 2 - el.scrollLeft,
    el.clientWidth / 2,
  )
  assert.ok(stale.every((request) => request.signal.aborted))
  for (const request of stale) request.resolve({ data: { items: [{ episode: { id: 999 } }] } })
  await flush()
  assert.ok(timeline.days.value.every((day) => day.items.length === 0))
  assert.equal(el.scrollLeft, WINDOW_DAYS * 2 * DAY_WIDTH + DAY_WIDTH / 2 - el.clientWidth / 2)

  timeline.onScroll()
  harness.unmount()
  assert.ok(harness.disconnected)
  assert.equal(harness.listeners.size, 0)
  assert.equal(harness.subscriptions.size, 0)
  assert.equal(harness.frames.size, 0)
  assert.ok(requests.every((request) => request.signal.aborted || requests.indexOf(request) < initialRequests))
  for (const request of requests) request.resolve({ data: { items: [] } })
  await flush()
})

async function showWindowBoundary(harness) {
  const { timeline, el, requests, flush } = harness
  const boundary = timelineWindow(timeline.today.value) + WINDOW_DAYS
  el.scrollLeft = (boundary - 1 - timeline.start.value) * DAY_WIDTH + 17
  timeline.onScroll()
  await flush()
  const item = {
    seriesTitle: 'Followed series',
    episode: {
      id: 71,
      mediaItemId: 7,
      seasonNumber: 1,
      episodeNumber: 2,
      airDate: timelineDate(boundary - 1),
      hasFile: false,
      monitored: true,
      downloadStatus: 'downloading',
    },
  }
  for (const request of requests) {
    const { from, to } = request.params.query
    request.resolve({ data: { items: item.episode.airDate >= from && item.episode.airDate < to ? [item] : [] } })
  }
  await flush()
  return { boundary, item, range: { ...timeline.visibleRange.value }, left: el.scrollLeft, start: timeline.start.value }
}

test('import completion refreshes availability in place, coalesces bursts and ignores unrelated items', async (t) => {
  const harness = await mountTimeline(t)
  const { timeline, el, requests, emit, timers, flush, flushRefresh } = harness
  const { item, range, left, start } = await showWindowBoundary(harness)
  const rendered = () => timeline.days.value.flatMap((day) => day.items)
  assert.equal(timelineAvailability(rendered()[0], timeline.today.value), 'Downloading')
  const before = requests.length
  emit('download.import_completed', { mediaItemId: 999 })
  emit('subtitle.downloaded', { mediaItemId: 7 })
  assert.equal(timers.size, 0)
  assert.equal(requests.length, before)
  emit('download.completed', { mediaItemId: 7 })
  emit('download.import_completed', { mediaItemId: 7 })
  emit('media.resync_completed', { mediaItemId: 7 })
  assert.equal(timers.size, 1)
  timeline.onScroll()
  await flush()
  assert.equal(requests.length, before, 'scroll does not bypass burst coalescing')
  await flushRefresh()
  assert.equal(requests.length, before + 1, 'only the affected visible window reloads')
  requests
    .at(-1)
    .resolve({ data: { items: [{ ...item, episode: { ...item.episode, hasFile: true, downloadStatus: undefined } }] } })
  await flush()
  assert.equal(rendered().length, 1)
  assert.equal(timelineAvailability(rendered()[0], timeline.today.value), 'Available')
  assert.deepEqual(timeline.visibleRange.value, range)
  assert.equal(el.scrollLeft, left)
  assert.equal(timeline.start.value, start)
  assert.equal(timers.size, 0)
})

test('metadata changes move episodes across windows without duplicates or stale responses', async (t) => {
  const harness = await mountTimeline(t)
  const { timeline, el, requests, emit, timers, flush, flushRefresh } = harness
  const { boundary, item, range, left, start } = await showWindowBoundary(harness)
  const rendered = () => timeline.days.value.flatMap((day) => day.items)
  const before = requests.length
  emit('media.metadata_refreshed', { mediaItemId: 7 })
  assert.equal(rendered().length, 0, 'both old and destination snapshots are invalidated immediately')
  await flushRefresh()
  const stale = requests.slice(before)
  assert.equal(stale.length, 2, 'only the currently needed windows reload, not the spare cache')
  // A second metadata event during loading must abort the older snapshot even
  // when that item was not in the cache: it could move into these dates.
  emit('media.item_matched', { mediaItemId: 999 })
  assert.ok(stale.every((request) => request.signal.aborted))
  const next = requests.length
  await flushRefresh()
  const fresh = requests.slice(next)
  assert.equal(fresh.length, 2)
  const destination = fresh.find((request) => request.params.query.from === timelineDate(boundary))
  const origin = fresh.find((request) => request !== destination)
  const moved = { ...item, episode: { ...item.episode, airDate: timelineDate(boundary + 1) } }
  destination.resolve({ data: { items: [moved] } })
  await flush()
  assert.deepEqual(rendered(), [moved], 'destination can finish first without duplicating the old date')
  for (const request of stale) request.resolve({ data: { items: [item] } })
  await flush()
  assert.deepEqual(rendered(), [moved], 'late pre-invalidation responses cannot restore the old date')
  origin.resolve({ data: { items: [] } })
  await flush()
  assert.deepEqual(rendered(), [moved])
  assert.deepEqual(timeline.visibleRange.value, range)
  assert.equal(el.scrollLeft, left)
  assert.equal(timeline.start.value, start)
  assert.ok(timeline.days.value.length <= 7)

  const handler = harness.subscriptions.get('media.metadata_refreshed')
  emit('library.sync_completed', { libraryId: 1 })
  assert.equal(timers.size, 1)
  harness.unmount()
  assert.equal(timers.size, 0)
  assert.equal(harness.subscriptions.size, 0)
  const finished = requests.length
  handler({ mediaItemId: 7 })
  await flushRefresh()
  assert.equal(requests.length, finished, 'unmounted subscribers cannot schedule a refresh')
})
