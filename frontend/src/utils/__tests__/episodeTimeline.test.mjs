import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  createTimelineCache,
  DAY_WIDTH,
  formatTimelineDay,
  localToday,
  RAIL_DAYS,
  recenterTimelineRail,
  timelineAvailability,
  timelineDate,
  timelineDay,
  timelineViewport,
  timelineWindow,
  WINDOW_DAYS,
} from '../episodeTimeline.ts'

test('calendar arithmetic handles leap years, year boundaries, DST and negative days', () => {
  for (const [date, next] of [
    ['2024-02-28', '2024-02-29'],
    ['2024-02-29', '2024-03-01'],
    ['2026-12-31', '2027-01-01'],
    ['1969-12-31', '1970-01-01'],
    ['2026-03-08', '2026-03-09'],
    ['2026-11-01', '2026-11-02'],
  ]) {
    assert.equal(timelineDate(timelineDay(date) + 1), next)
  }
  for (const value of ['2026-02-29', '2026-04-31', '', '2026-1-01', '2026-01-01T00:00:00Z'])
    assert.throws(() => timelineDay(value))
  assert.equal(timelineWindow(-1), -14)
})

test('dates and today stay correct in time zones east and west of UTC', () => {
  const previous = process.env.TZ
  try {
    for (const zone of ['America/Los_Angeles', 'Pacific/Kiritimati', 'Europe/Budapest']) {
      process.env.TZ = zone
      assert.equal(timelineDate(localToday(new Date(2026, 8, 5, 23, 30))), '2026-09-05')
      assert.equal(formatTimelineDay(timelineDay('2026-09-05'), { month: 'short', day: 'numeric' }), 'Sep 5')
    }
  } finally {
    if (previous === undefined) delete process.env.TZ
    else process.env.TZ = previous
  }
})

test('recenter preserves the exact visible date and fractional offset in both directions', () => {
  let start = timelineDay('2026-09-01')
  for (let i = 0; i < 2000; i++) {
    const left = i % 2 ? DAY_WIDTH * 60 + 17 : 21
    const oldPosition = start + left / DAY_WIDTH
    const next = recenterTimelineRail(start, left, 900)
    assert.equal(next.start + next.left / DAY_WIDTH, oldPosition)
    assert.ok(next.left >= WINDOW_DAYS * DAY_WIDTH)
    assert.ok(next.left < (RAIL_DAYS - WINDOW_DAYS) * DAY_WIDTH)
    const view = timelineViewport(next.start, next.left, 900)
    assert.ok(view.end - view.first <= 10, 'DOM is bounded to visible days and overscan')
    start = next.start
  }
})

test('availability never hides available or unmonitored episodes', () => {
  const today = timelineDay('2026-09-05')
  assert.equal(
    timelineAvailability({ episode: { hasFile: true, monitored: false, airDate: '2026-09-10' } }, today),
    'Available',
  )
  assert.equal(timelineAvailability({ episode: { monitored: false, airDate: '2026-09-10' } }, today), 'Upcoming')
  assert.equal(
    timelineAvailability({ episode: { airDate: '2026-09-05', downloadStatus: 'failed' } }, today),
    'Not available',
  )
  assert.equal(
    timelineAvailability({ episode: { airDate: '2026-09-05', downloadStatus: 'downloading' } }, today),
    'Downloading',
  )
})

function deferredLoader() {
  const calls = []
  const load = (from, to, signal) => new Promise((resolve, reject) => calls.push({ from, to, signal, resolve, reject }))
  return { calls, load }
}

test('empty windows are cached, deduplicated, and do not end traversal in either direction', async () => {
  const { calls, load } = deferredLoader()
  const cache = createTimelineCache(load, () => {})
  const start = timelineWindow(timelineDay('2026-09-05'))
  const pending = cache.ensure(start)
  await cache.ensure(start)
  assert.equal(calls.length, 1)
  assert.equal(timelineDay(calls[0].to) - timelineDay(calls[0].from), WINDOW_DAYS)
  calls[0].resolve([])
  await pending
  await cache.ensure(start)
  assert.equal(calls.length, 1)
  const earlier = cache.ensure(start - WINDOW_DAYS)
  const later = cache.ensure(start + WINDOW_DAYS)
  assert.equal(calls.length, 3)
  calls[1].resolve([])
  calls[2].resolve([])
  await Promise.all([earlier, later])
  assert.equal(cache.windows.size, 3)
  cache.dispose()
})

test('independent failures require explicit retry, with no automatic fetch loop', async () => {
  const { calls, load } = deferredLoader()
  const cache = createTimelineCache(load, () => {})
  const failed = cache.ensure(0)
  const other = cache.ensure(14)
  calls[0].reject(new Error('Offline'))
  calls[1].resolve([{ episode: { id: 2 } }])
  await Promise.all([failed, other])
  for (let i = 0; i < 50; i++) await cache.ensure(0)
  assert.equal(calls.length, 2)
  assert.equal(cache.windows.get(0).error, 'Offline')
  assert.equal(cache.windows.get(14).status, 'ready')
  const retry = cache.ensure(0, true)
  assert.equal(calls.length, 3)
  calls[2].resolve([])
  await retry
  assert.equal(cache.windows.get(0).status, 'ready')
  cache.dispose()
})

test('reset, eviction and disposal reject late responses even when transport ignores abort', async () => {
  const { calls, load } = deferredLoader()
  let notifications = 0
  const cache = createTimelineCache(load, () => notifications++)
  const stale = cache.ensure(0)
  cache.reset()
  const current = cache.ensure(0)
  assert.ok(calls[0].signal.aborted)
  calls[1].resolve([{ episode: { id: 2 } }])
  await current
  calls[0].resolve([{ episode: { id: 1 } }])
  await stale
  assert.equal(cache.windows.get(0).items[0].episode.id, 2)
  const evicted = cache.ensure(14)
  cache.retain(28, 70)
  assert.ok(calls[2].signal.aborted)
  calls[2].reject(new Error('Late error'))
  await evicted
  assert.equal(cache.windows.size, 0)
  const last = cache.ensure(28)
  cache.dispose()
  const before = notifications
  calls[3].resolve([])
  await last
  assert.equal(cache.windows.size, 0)
  assert.equal(notifications, before)
  await cache.ensure(0)
  assert.equal(calls.length, 4)
})

test('years of panning keep only nearby windows, and evicted dates can be revisited', async () => {
  let calls = 0
  const cache = createTimelineCache(
    async () => {
      calls++
      return []
    },
    () => {},
  )
  for (let day = 0; day < 3650; day++) {
    const first = timelineWindow(day)
    cache.retain(first - WINDOW_DAYS, first + WINDOW_DAYS * 3)
    await cache.ensure(first)
    await cache.ensure(first + WINDOW_DAYS)
    assert.ok(cache.windows.size <= 4)
  }
  const before = calls
  cache.retain(0, 28)
  await cache.ensure(0)
  assert.equal(calls, before + 1)
  assert.equal(cache.windows.size, 1)
  cache.dispose()
})

test('selective invalidation aborts stale requests without dropping unrelated windows', async () => {
  const { calls, load } = deferredLoader()
  const cache = createTimelineCache(load, () => {})
  const stale = cache.ensure(0)
  const other = cache.ensure(14)
  calls[1].resolve([{ episode: { id: 2 } }])
  await other
  assert.equal(
    cache.invalidate((window) => window.from === 0),
    true,
  )
  assert.ok(calls[0].signal.aborted)
  assert.equal(cache.windows.get(14).status, 'ready')
  assert.equal(
    cache.invalidate((window) => window.from === 0),
    false,
  )
  const fresh = cache.ensure(0)
  calls[2].resolve([{ episode: { id: 3 } }])
  await fresh
  calls[0].resolve([{ episode: { id: 1 } }])
  await stale
  assert.equal(cache.windows.get(0).items[0].episode.id, 3)
  assert.equal(cache.windows.get(14).items[0].episode.id, 2)
  cache.dispose()
})
