import assert from 'node:assert/strict'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { createRenderer, nextTick, ref } from 'vue'
import { discoverKey, useDiscoverFilter } from '../useDiscoverFilter.ts'
import { usePagedDiscover } from '../usePagedDiscover.ts'

// Exercise the actual composables and lifecycle without a DOM or a test framework.
const renderer = createRenderer({
  createElement: () => ({}),
  createText: () => ({}),
  createComment: () => ({}),
  insert() {},
  remove() {},
  setText() {},
  setElementText() {},
  patchProp() {},
  parentNode: () => null,
  nextSibling: () => null,
})

function mount(t, setup) {
  let state
  const app = renderer.createApp({
    setup() {
      state = setup()
      return () => null
    },
  })
  app.mount({})
  let mounted = true
  function unmount() {
    if (mounted) app.unmount()
    mounted = false
  }
  t.after(unmount)
  return { state, unmount }
}

function stubGlobal(t, name, value) {
  const original = Object.getOwnPropertyDescriptor(globalThis, name)
  Object.defineProperty(globalThis, name, { configurable: true, writable: true, value })
  t.after(() => {
    if (original) Object.defineProperty(globalThis, name, original)
    else delete globalThis[name]
  })
}

async function settle() {
  await setImmediate()
  await nextTick()
}

const movie = (id) => ({ source: 'tmdb', mediaType: 'movie', externalId: id })
const pageData = (page, items, totalPages = 10) => ({ page, items, totalPages })

test('identity includes provider AND media type; watched alone is never filtered', () => {
  const owned = movie(42)
  const series = { ...owned, mediaType: 'series' }
  const tvdb = { ...series, source: 'tvdb' }
  assert.equal(new Set([owned, series, tvdb].map(discoverKey)).size, 3)
  const membership = new Set([discoverKey(owned)])
  const filter = useDiscoverFilter((item) => membership.has(discoverKey(item)), ref(true))
  filter.hideInLibrary.value = true
  assert.equal(filter.includeItem(owned), false)
  assert.equal(filter.includeItem(series), true)
  assert.equal(filter.includeItem(tvdb), true)
  assert.equal(filter.includeItem({ ...movie(99), watched: true }), true)
  filter.hideInLibrary.value = false
  assert.equal(filter.includeItem(owned), true)
})

test('preference is restored, shared across views, and persists safely', async (t) => {
  const writes = []
  stubGlobal(t, 'localStorage', {
    getItem: () => 'true',
    setItem: (...args) => writes.push(args),
  })
  const { useDiscoverFilter: useFreshFilter } = await import('../useDiscoverFilter.ts?storage')
  const first = useFreshFilter(() => false, ref(true))
  const second = useFreshFilter(() => false, ref(true))
  assert.equal(first.hideInLibrary.value, true)
  first.hideInLibrary.value = false
  assert.equal(second.hideInLibrary.value, false)
  assert.deepEqual(writes, [['discover.hideInLibrary', 'false']])
})

test('blocked storage reads and writes do not break filtering', async (t) => {
  stubGlobal(t, 'localStorage', {
    getItem() {
      throw new Error('blocked')
    },
    setItem() {
      throw new Error('quota')
    },
  })
  const { useDiscoverFilter: useFreshFilter } = await import('../useDiscoverFilter.ts?blocked')
  const filter = useFreshFilter(() => true, ref(true))
  assert.equal(filter.hideInLibrary.value, false)
  assert.doesNotThrow(() => {
    filter.hideInLibrary.value = true
  })
  assert.equal(filter.includeItem(movie(1)), false)
})

test('waits for membership, then continues through entirely hidden pages', async (t) => {
  const membershipReady = ref(false)
  const filter = useDiscoverFilter((item) => item.externalId < 3, membershipReady)
  filter.hideInLibrary.value = true
  const requests = []
  const { state } = mount(t, () =>
    usePagedDiscover(
      async (page) => {
        requests.push(page)
        return pageData(page, [movie(page)], 3)
      },
      discoverKey,
      { include: filter.includeItem, ready: () => filter.filterReady.value },
    ),
  )
  await settle()
  assert.deepEqual(requests, [])
  assert.deepEqual(state.items.value, [])
  membershipReady.value = true
  await settle()
  assert.deepEqual(requests, [1, 2, 3])
  assert.deepEqual(state.items.value, [movie(3)])
  assert.equal(state.hiddenCount.value, 2)
  assert.equal(state.hasMore.value, false)
})

test('automatic empty-page scanning stops after five pages and manual load continues', async (t) => {
  let observer
  stubGlobal(
    t,
    'IntersectionObserver',
    class {
      constructor(callback) {
        this.callback = callback
        observer = this
      }
      observes = 0
      observe() {
        this.observes += 1
      }
      unobserve() {}
      disconnect() {}
    },
  )
  const requests = []
  const { state } = mount(t, () =>
    usePagedDiscover(
      async (page) => {
        requests.push(page)
        return pageData(page, [movie(page)], 12)
      },
      discoverKey,
      { include: () => false },
    ),
  )
  state.sentinel.value = {}
  await settle()
  assert.deepEqual(requests, [1, 2, 3, 4, 5])
  const observations = observer.observes
  assert.ok(observations >= 2, 'observer rearmed after Vue DOM updates')
  observer.callback([{ isIntersecting: true }])
  await settle()
  assert.equal(requests.length, 5)
  assert.equal(state.hasMore.value, true)
  await state.fetchPage()
  assert.deepEqual(requests, [1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
  await state.fetchPage()
  assert.equal(state.page.value, 12)
  assert.equal(state.hasMore.value, false)
  assert.equal(state.loading.value, false)
})

test('deduplicates within and across pages without losing colliding movie/TV IDs', async (t) => {
  const series = { ...movie(1), mediaType: 'series' }
  const { state } = mount(t, () =>
    usePagedDiscover(
      async (page) => pageData(page, page === 1 ? [movie(1), movie(1), series] : [series, movie(2)], 2),
      discoverKey,
    ),
  )
  await settle()
  assert.deepEqual(state.items.value, [movie(1), series])
  await state.fetchPage()
  assert.deepEqual(state.items.value, [movie(1), series, movie(2)])
})

test('repeated provider pages fail rather than looping, and retry requests the failed page', async (t) => {
  const requests = []
  let broken = true
  const { state } = mount(t, () =>
    usePagedDiscover(
      async (page) => {
        requests.push(page)
        return pageData(broken ? 1 : page, [movie(page)], 2)
      },
      discoverKey,
      { include: () => false },
    ),
  )
  await settle()
  assert.deepEqual(requests, [1, 2])
  assert.equal(state.page.value, 1)
  assert.equal(state.loadFailed.value, true)
  broken = false
  await state.fetchPage()
  assert.deepEqual(requests, [1, 2, 2])
  assert.equal(state.loadFailed.value, false)
  assert.equal(state.hasMore.value, false)
})

test('network and HTTP failures preserve pages and allow retry', async (t) => {
  const requests = []
  let failure = 'network'
  const { state } = mount(t, () =>
    usePagedDiscover(async (page) => {
      requests.push(page)
      if (failure === 'network') throw new Error('offline')
      if (failure === 'http') return undefined
      return pageData(page, [movie(1)], 1)
    }, discoverKey),
  )
  await settle()
  assert.equal(state.loadFailed.value, true)
  assert.equal(state.initialLoading.value, false)
  failure = 'http'
  await state.fetchPage()
  assert.equal(state.loading.value, false)
  failure = ''
  await state.fetchPage()
  assert.deepEqual(requests, [1, 1, 1])
  assert.deepEqual(state.items.value, [movie(1)])
  assert.equal(state.loadFailed.value, false)
})

test('filter toggles reuse raw pages and apply current membership to in-flight responses', async (t) => {
  const ready = ref(false)
  const filter = useDiscoverFilter((item) => item.externalId === 1, ready)
  filter.hideInLibrary.value = false
  const requests = []
  let resolveFirst
  const { state } = mount(t, () =>
    usePagedDiscover(
      (page) => {
        requests.push(page)
        return page === 1
          ? new Promise((resolve) => {
              resolveFirst = resolve
            })
          : Promise.resolve(pageData(page, [movie(2)], 2))
      },
      discoverKey,
      {
        include: filter.includeItem,
        ready: () => filter.filterReady.value,
        filterKey: () => filter.hideInLibrary.value,
      },
    ),
  )
  filter.hideInLibrary.value = true
  resolveFirst(pageData(1, [movie(1)], 2))
  await settle()
  assert.deepEqual(state.items.value, [])
  assert.deepEqual(requests, [1])
  ready.value = true
  await settle()
  assert.deepEqual(state.items.value, [movie(2)])
  filter.hideInLibrary.value = false
  await settle()
  assert.deepEqual(state.items.value, [movie(1), movie(2)])
  assert.deepEqual(requests, [1, 2])
})

test('navigation reset aborts old fetches and ignores late responses; unmount aborts too', async (t) => {
  const pending = []
  const { state, unmount } = mount(t, () =>
    usePagedDiscover(
      (page, signal) =>
        new Promise((resolve) => {
          pending.push({ page, signal, resolve })
        }),
      discoverKey,
    ),
  )
  state.reset()
  assert.equal(pending[0].signal.aborted, true)
  pending[1].resolve(pageData(1, [movie(2)], 2))
  await settle()
  pending[0].resolve(pageData(1, [movie(1)], 5))
  await settle()
  assert.deepEqual(state.items.value, [movie(2)])
  assert.equal(state.totalPages.value, 2)
  void state.fetchPage()
  unmount()
  assert.equal(pending[2].signal.aborted, true)
  pending[2].resolve(pageData(2, [movie(3)], 2))
  await settle()
  assert.deepEqual(state.items.value, [movie(2)])
  await state.fetchPage()
  assert.equal(pending.length, 3)
})

test('empty provider results terminate, and TMDB page limits are capped', async (t) => {
  const { state: empty } = mount(t, () => usePagedDiscover(async () => pageData(1, [], 0), discoverKey))
  const { state: capped } = mount(t, () => usePagedDiscover(async () => pageData(1, [movie(1)], 9999), discoverKey))
  await settle()
  assert.equal(empty.hasMore.value, false)
  assert.equal(empty.loadFailed.value, false)
  assert.equal(capped.totalPages.value, 500)
})
