import assert from 'node:assert/strict'
import { registerHooks } from 'node:module'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { createRenderer, nextTick, ref } from 'vue'

const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === '@/api/client') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export default { GET: (...args) => globalThis.mediaActivityTestGet(...args) }',
      }
    }
    if (specifier === '@/composables/useEventStream') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export const useEventStream = () => globalThis.mediaActivityTestStream',
      }
    }
    return nextResolve(specifier, context)
  },
})
const { MEDIA_ACTIVITY_CAPACITY, useMediaActivity } = await import('../useMediaActivity.ts')
hooks.deregister()

const renderer = createRenderer({
  createComment: () => ({}),
  insert() {},
  remove() {},
  parentNode: () => null,
  nextSibling: () => null,
})

const entry = (id, recordedAt = '2026-09-01T12:00:00Z') => ({
  id,
  recordedAt,
  actor: { kind: 'user', name: 'Alice' },
  action: 'request.made',
  operationId: `operation-${id}`,
  mediaTitle: 'Example',
  detailsVersion: 1,
  details: { target: { scope: 'media' } },
})

async function settle() {
  await setImmediate()
  await nextTick()
}

async function mountActivity(t, initial = {}) {
  const originalWindow = globalThis.window
  const requests = []
  const subscriptions = new Map()
  const windowListeners = new Map()
  const connected = ref(false)
  globalThis.mediaActivityTestGet = (_path, options) => new Promise((resolve) => requests.push({ ...options, resolve }))
  globalThis.mediaActivityTestStream = {
    connected,
    on(type, callback) {
      subscriptions.set(type, callback)
    },
    off(type, callback) {
      assert.equal(subscriptions.get(type), callback)
      subscriptions.delete(type)
    },
  }
  globalThis.window = {
    addEventListener(type, callback) {
      windowListeners.set(type, callback)
    },
    removeEventListener(type, callback) {
      assert.equal(windowListeners.get(type), callback)
      windowListeners.delete(type)
    },
  }

  const mediaItemId = ref(initial.mediaItemId ?? 1)
  const active = ref(initial.active ?? false)
  const expanded = ref(initial.expanded ?? false)
  let activity
  const app = renderer.createApp({
    setup() {
      activity = useMediaActivity(mediaItemId, active, expanded)
      return () => null
    },
  })
  app.mount({})
  let mounted = true
  const unmount = () => {
    if (mounted) app.unmount()
    mounted = false
  }
  t.after(() => {
    unmount()
    globalThis.window = originalWindow
    delete globalThis.mediaActivityTestGet
    delete globalThis.mediaActivityTestStream
  })
  await settle()
  return {
    activity,
    mediaItemId,
    active,
    expanded,
    connected,
    requests,
    subscriptions,
    windowListeners,
    unmount,
    emit(type, data) {
      subscriptions.get(type)?.(data)
    },
    focus() {
      windowListeners.get('focus')?.()
    },
    resolve(index, items, hasMore = false, nextCursor) {
      requests[index].resolve({ data: { items, hasMore, nextCursor } })
    },
    fail(index) {
      requests[index].resolve({ error: { code: 500 } })
    },
  }
}

test('hidden and collapsed feeds make zero requests, abort exits and reuse a clean cache', async (t) => {
  const harness = await mountActivity(t)
  harness.focus()
  harness.emit('media.activity_added', { mediaItemId: 1 })
  harness.connected.value = true
  await settle()
  assert.equal(harness.requests.length, 0)
  assert.equal(harness.activity.dirty.value, false, 'never-loaded hidden feeds ignore invalidation hints')
  harness.active.value = true
  await settle()
  assert.equal(harness.requests.length, 0)
  harness.expanded.value = true
  await settle()
  assert.equal(harness.requests.length, 1)
  assert.equal(harness.requests[0].params.query.limit, 30)

  harness.expanded.value = false
  await settle()
  assert.equal(harness.requests[0].signal.aborted, true)
  harness.resolve(0, [entry(99)])
  await settle()
  assert.equal(harness.activity.loaded.value, false)

  harness.expanded.value = true
  await settle()
  harness.resolve(1, [entry(3), entry(2)], true, 'cursor-2')
  await settle()
  harness.expanded.value = false
  await settle()
  harness.expanded.value = true
  await settle()
  assert.equal(harness.requests.length, 2, 'clean successful pages are reused after reopening')

  harness.activity.markDirty()
  const refresh = harness.activity.refreshLatest()
  assert.equal(harness.requests.length, 3)
  harness.active.value = false
  await settle()
  assert.equal(harness.requests[2].signal.aborted, true)
  harness.resolve(2, [entry(4)])
  await refresh
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [3, 2],
  )
  harness.active.value = true
  await settle()
  assert.equal(harness.requests.length, 4, 'a dirty expanded feed refreshes after returning to its tab')
})

test('pagination preserves server order, stable-ID dedupe, tied timestamps and retained cursor on errors', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  const tied = '2026-09-01T12:00:00Z'
  harness.resolve(0, [entry(5, tied), entry(4, tied), entry(3, tied)], true, 'before-3')
  await settle()
  const firstCursor = harness.activity.nextCursor.value
  harness.emit('media.activity_added', { mediaItemId: 1 })
  const older = harness.activity.loadOlder()
  assert.equal(harness.requests[1].params.query.before, 'before-3')
  void harness.activity.refreshLatest()
  assert.equal(harness.requests.length, 2, 'page and refresh operations are serialized')
  harness.fail(1)
  await older
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [5, 4, 3],
  )
  assert.equal(harness.activity.nextCursor.value, firstCursor)
  assert.equal(harness.activity.errorKind.value, 'older')

  const retry = harness.activity.retry()
  harness.resolve(2, [entry(3, tied), entry(2, tied), entry(2, tied), entry(1, tied)], false)
  await retry
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [5, 4, 3, 2, 1],
  )
  assert.equal(harness.activity.hasMore.value, false)
  assert.equal(harness.activity.dirty.value, true, 'an insert between pages does not move or refetch the open history')
})

test('initial failures render as unloaded state and retry the newest page', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.fail(0)
  await settle()
  assert.equal(harness.activity.loaded.value, false)
  assert.equal(harness.activity.errorKind.value, 'initial')
  const retry = harness.activity.retry()
  assert.equal(harness.requests[1].params.query.before, undefined)
  assert.equal(harness.requests[1].params.query.limit, 30)
  harness.resolve(1, [])
  await retry
  await settle()
  assert.equal(harness.activity.loaded.value, true)
  assert.deepEqual(harness.activity.items.value, [])
})

test('an empty page cannot claim older activity that its cursor would skip', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.resolve(0, [], true, 'skipping-cursor')
  await settle()
  assert.equal(harness.activity.loaded.value, false)
  assert.equal(harness.activity.errorKind.value, 'initial')
})

test('activity arriving during the first load survives the initial response', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.emit('media.activity_added', { mediaItemId: 1 })
  assert.equal(harness.activity.dirty.value, true)
  harness.resolve(0, [entry(3)])
  await settle()
  assert.equal(harness.activity.loaded.value, true)
  assert.equal(harness.activity.dirty.value, true)
  assert.equal(harness.requests.length, 1, 'invalidation remains user-controlled rather than auto-fetching')
})

test('the first SSE connection during an initial load leaves the result dirty', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.connected.value = true
  assert.equal(harness.activity.dirty.value, true)
  harness.resolve(0, [entry(3)])
  await settle()
  assert.equal(harness.activity.dirty.value, true)
  assert.equal(harness.requests.length, 1)
})

test('dirty hints never auto-fetch and revision races remain dirty after refresh', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.resolve(0, [entry(3)])
  await settle()
  harness.emit('media.activity_added', { mediaItemId: 999 })
  assert.equal(harness.activity.dirty.value, false)
  harness.emit('media.activity_added', { mediaItemId: 1 })
  assert.equal(harness.activity.dirty.value, true)
  assert.equal(harness.requests.length, 1)

  const refresh = harness.activity.refreshLatest()
  harness.focus()
  harness.resolve(1, [entry(4), entry(3)])
  await refresh
  await settle()
  assert.equal(harness.activity.dirty.value, true, 'focus during refresh advances the revision')
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [4, 3],
  )

  harness.connected.value = true
  await settle()
  harness.activity.dirty.value = false
  harness.connected.value = false
  await settle()
  harness.connected.value = true
  await settle()
  assert.equal(harness.activity.dirty.value, true, 'an SSE reconnect marks loaded history dirty')
  assert.equal(harness.requests.length, 2)
})

test('refresh and page failures retain visible rows and retry replaces only after success', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.resolve(0, [entry(3), entry(2)], true, 'before-2')
  await settle()
  harness.activity.markDirty()
  const refresh = harness.activity.refreshLatest()
  harness.fail(1)
  await refresh
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [3, 2],
  )
  assert.equal(harness.activity.dirty.value, true)
  assert.match(harness.activity.error.value, /refresh/)

  const retry = harness.activity.retry()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [3, 2],
  )
  harness.resolve(2, [entry(6), entry(5)], false)
  await retry
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [6, 5],
  )
  assert.equal(harness.activity.dirty.value, false)
})

test('500-entry windows request the exact remaining capacity and navigate older without skipping rows', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  let highest = 700
  const makePage = (count) => Array.from({ length: count }, () => entry(highest--))
  harness.resolve(0, makePage(30), true, 'cursor-30')
  await settle()

  for (let page = 0; page < 5; page++) {
    const pending = harness.activity.loadOlder()
    const requestIndex = harness.requests.length - 1
    const expected = page === 4 ? 70 : 100
    assert.equal(harness.requests[requestIndex].params.query.limit, expected)
    harness.resolve(requestIndex, makePage(expected), true, `cursor-${harness.activity.items.value.length + expected}`)
    await pending
    await settle()
  }
  assert.equal(harness.activity.items.value.length, MEDIA_ACTIVITY_CAPACITY)
  assert.equal(harness.activity.atCapacity.value, true)

  const previousOldest = harness.activity.items.value.at(-1).id
  const nextWindow = harness.activity.continueOlder()
  const continueIndex = harness.requests.length - 1
  assert.equal(harness.requests[continueIndex].params.query.limit, 30)
  assert.equal(harness.requests[continueIndex].params.query.before, 'cursor-500')
  const olderItems = makePage(30)
  assert.ok(olderItems[0].id < previousOldest)
  harness.resolve(continueIndex, olderItems, true, 'older-cursor')
  await nextWindow
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    olderItems.map((item) => item.id),
  )
  assert.equal(harness.activity.windowMode.value, 'older')

  const back = harness.activity.backToLatest()
  const backIndex = harness.requests.length - 1
  harness.resolve(backIndex, [entry(900), entry(899)], true, 'latest-cursor')
  await back
  await settle()
  assert.deepEqual(
    harness.activity.items.value.map((item) => item.id),
    [900, 899],
  )
  assert.equal(harness.activity.windowMode.value, 'latest')
})

test('route reset and unmount abort requests, clear state and remove listeners', async (t) => {
  const harness = await mountActivity(t, { active: true, expanded: true })
  harness.mediaItemId.value = 2
  await settle()
  assert.equal(harness.requests[0].signal.aborted, true)
  assert.equal(harness.requests.length, 2)
  assert.equal(harness.requests[1].params.path.id, 2)
  harness.resolve(0, [entry(100)])
  await settle()
  assert.deepEqual(harness.activity.items.value, [])

  harness.unmount()
  assert.equal(harness.requests[1].signal.aborted, true)
  assert.equal(harness.subscriptions.size, 0)
  assert.equal(harness.windowListeners.size, 0)
  harness.resolve(1, [entry(200)])
  await settle()
  assert.deepEqual(harness.activity.items.value, [])
})
