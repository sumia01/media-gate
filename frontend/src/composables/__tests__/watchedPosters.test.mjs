import assert from 'node:assert/strict'
import { registerHooks } from 'node:module'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { createRenderer, nextTick } from 'vue'

const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === '@/api/client') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export default { GET: (...args) => globalThis.watchedPosterGet(...args) }',
      }
    }
    if (specifier === '@/utils/media') {
      return { shortCircuit: true, url: new URL('../../utils/media.ts', import.meta.url).href }
    }
    if (specifier === '@/composables/useDiscoverFilter') {
      return { shortCircuit: true, url: new URL('../useDiscoverFilter.ts', import.meta.url).href }
    }
    return nextResolve(specifier, context)
  },
})
const { useWatchedPosters } = await import('../useWatchedPosters.ts')
hooks.deregister()

const renderer = createRenderer({
  createComment: () => ({}),
  insert() {},
  remove() {},
  parentNode: () => null,
  nextSibling: () => null,
})

const item = (id, extra = {}) => ({
  id,
  source: 'tmdb',
  mediaType: 'movie',
  externalId: id,
  title: `Movie ${id}`,
  ...extra,
})

async function settle() {
  await setImmediate()
  await nextTick()
}

function mount(t) {
  const requests = []
  globalThis.watchedPosterGet = (path, options) =>
    new Promise((resolve, reject) => requests.push({ path, ...options, resolve, reject }))
  let posters
  const app = renderer.createApp({
    setup() {
      posters = useWatchedPosters()
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
    delete globalThis.watchedPosterGet
  })
  return { ...posters, requests, unmount }
}

test('deleted legacy library posters resolve by exact provider identity without changing watched data', async (t) => {
  const h = mount(t)
  const legacy = item(1, { externalId: 278927, posterPath: '52.jpg' })
  const original = { ...legacy }
  assert.equal(h.itemPosterUrl(legacy), null)
  h.loadMissingPosters([legacy])
  assert.equal(h.requests.length, 1)
  assert.equal(h.requests[0].path, '/search/{source}/{externalId}')
  assert.deepEqual(h.requests[0].params, {
    path: { source: 'tmdb', externalId: 278927 },
    query: { mediaType: 'movie' },
  })
  h.requests[0].resolve({ data: { posterUrl: 'https://image.tmdb.org/t/p/w500/jungle.jpg' } })
  await settle()
  assert.equal(h.itemPosterUrl(legacy), 'https://image.tmdb.org/t/p/w500/jungle.jpg')
  assert.deepEqual(legacy, original)
})

test('cached and external posters avoid metadata requests; cached failures try the saved external URL first', (t) => {
  const h = mount(t)
  const cached = item(1, { mediaItemId: 52, posterPath: '/jungle.jpg' })
  const tmdb = item(2, { posterPath: '/dinosaur.jpg' })
  const tvdb = item(3, { source: 'tvdb', posterPath: 'https://artworks.thetvdb.com/poster.jpg' })
  h.loadMissingPosters([cached, tmdb, tvdb])
  assert.equal(h.requests.length, 0)
  assert.equal(h.itemPosterUrl(cached), '/api/v1/media/52/poster')
  assert.equal(h.itemPosterUrl(tmdb), 'https://image.tmdb.org/t/p/w342/dinosaur.jpg')
  assert.equal(h.itemPosterUrl(tvdb), tvdb.posterPath)
  h.onPosterError(cached)
  assert.equal(h.itemPosterUrl(cached), 'https://image.tmdb.org/t/p/w342/jungle.jpg')
  assert.equal(h.requests.length, 0)
})

test('failed cached and external images recover once and never cycle through broken URLs', async (t) => {
  const h = mount(t)
  const cached = item(1, { mediaItemId: 52, posterPath: '/old.jpg' })
  h.onPosterError(cached)
  h.onPosterError(cached)
  assert.equal(h.itemPosterUrl(cached), null)
  assert.equal(h.requests.length, 1)
  h.requests[0].resolve({ data: { posterUrl: 'https://image.tmdb.org/t/p/w500/new.jpg' } })
  await settle()
  assert.equal(h.itemPosterUrl(cached), 'https://image.tmdb.org/t/p/w500/new.jpg')
  h.onPosterError(cached)
  h.onPosterError(cached)
  assert.equal(h.itemPosterUrl(cached), null)
  assert.equal(h.requests.length, 1)
})

test('recovery shares exact identities but keeps provider and media type separate', async (t) => {
  const h = mount(t)
  const items = [
    item(1),
    item(2, { externalId: 1 }),
    item(3, { externalId: 1, mediaType: 'series' }),
    item(4, { externalId: 1, source: 'tvdb' }),
  ]
  h.loadMissingPosters(items)
  h.loadMissingPosters(items)
  assert.equal(h.requests.length, 3)
  for (const [i, req] of h.requests.entries()) req.resolve({ data: { posterUrl: `https://example.com/${i}.jpg` } })
  await settle()
  assert.equal(h.itemPosterUrl(items[0]), h.itemPosterUrl(items[1]))
  assert.notEqual(h.itemPosterUrl(items[0]), h.itemPosterUrl(items[2]))
  assert.notEqual(h.itemPosterUrl(items[0]), h.itemPosterUrl(items[3]))
})

test('metadata failures, absent artwork and previously failed URLs keep a stable placeholder', async (t) => {
  const h = mount(t)
  const items = [item(1), item(2), item(3), item(4, { posterPath: '/broken.jpg' })]
  h.loadMissingPosters(items)
  h.onPosterError(items[3])
  h.requests[0].resolve({ error: { code: 502 } })
  h.requests[1].reject(new Error('network failure'))
  h.requests[2].resolve({ data: {} })
  h.requests[3].resolve({ data: { posterUrl: 'https://image.tmdb.org/t/p/w342/broken.jpg' } })
  await settle()
  for (const entry of items) assert.equal(h.itemPosterUrl(entry), null)
  h.loadMissingPosters(items)
  assert.equal(h.requests.length, 4)
})

test('legacy lookups are bounded and navigation aborts work and ignores late responses', async (t) => {
  const h = mount(t)
  const items = Array.from({ length: 10 }, (_, i) => item(i + 1, { posterPath: `${i + 1}.jpg` }))
  h.loadMissingPosters(items)
  assert.equal(h.requests.length, 4)
  h.requests[0].resolve({ data: { posterUrl: 'https://example.com/1.jpg' } })
  await settle()
  assert.equal(h.requests.length, 5)
  h.unmount()
  for (const req of h.requests) assert.equal(req.signal.aborted, true)
  for (const req of h.requests.slice(1)) req.resolve({ data: { posterUrl: 'https://example.com/late.jpg' } })
  await settle()
  assert.equal(h.requests.length, 5, 'queued requests must not start after unmount')
  assert.equal(h.itemPosterUrl(items[1]), null, 'late results must not update state')
})
