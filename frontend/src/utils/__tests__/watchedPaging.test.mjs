import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import createClient from 'openapi-fetch'
import ts from 'typescript'
import * as Vue from 'vue'
import { discoverKey } from '../../composables/useDiscoverFilter.ts'
import { usePagedDiscover } from '../../composables/usePagedDiscover.ts'

function compile(path) {
  const { descriptor } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'))
  const { content } = compileScript(descriptor, {
    id: path,
    inlineTemplate: true,
    templateOptions: { compilerOptions: { hoistStatic: false } },
  })
  return ts.transpileModule(content, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
}

const view = compile('../../views/WatchedView.vue')
const status = compile('../../components/media/DiscoverResultsStatus.vue')

function evaluate(source, modules) {
  const exports = {}
  new Function('require', 'exports', source)((name) => {
    assert.ok(name in modules, `unexpected import: ${name}`)
    return modules[name]
  }, exports)
  return exports.default
}

function node(type, text = '') {
  return { type, text, props: {}, children: [], parent: null }
}

const renderer = Vue.createRenderer({
  createElement: node,
  createText: (text) => node('text', text),
  createComment: () => node('comment'),
  insert(child, parent, anchor = null) {
    child.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, child)
  },
  remove(child) {
    const siblings = child.parent?.children
    if (siblings) siblings.splice(siblings.indexOf(child), 1)
  },
  setText: (child, text) => {
    child.text = text
  },
  setElementText(child, text) {
    child.text = text
    child.children = []
  },
  patchProp: (child, key, _previous, value) => {
    child.props[key] = value
  },
  parentNode: (child) => child.parent,
  nextSibling: (child) => child.parent?.children[child.parent.children.indexOf(child) + 1] ?? null,
})

function findAll(root, predicate) {
  return [...(predicate(root) ? [root] : []), ...root.children.flatMap((child) => findAll(child, predicate))]
}

function text(root) {
  return `${root.text} ${root.children.map(text).join(' ')}`.replace(/\s+/g, ' ').trim()
}

async function settle() {
  await setImmediate()
  await Vue.nextTick()
  await Vue.nextTick()
}

async function mount(t, options = {}) {
  const originalObserver = globalThis.IntersectionObserver
  let observer
  globalThis.IntersectionObserver = class {
    constructor(callback) {
      this.callback = callback
      observer = this
    }
    observes = 0
    disconnected = false
    observe() {
      this.observes++
    }
    unobserve() {}
    disconnect() {
      this.disconnected = true
    }
  }
  let data = Array.from({ length: options.count ?? 65 }, (_, i) => ({
    id: 100 - i,
    source: 'tmdb',
    mediaType: 'movie',
    externalId: 100 - i,
    title: `Watched ${100 - i}`,
    posterPath: `https://artwork.test/${100 - i}.jpg`,
    watchedAt: '2026-11-13T10:00:00Z',
  }))
  const requests = []
  const deletes = []
  const replies = [...(options.replies ?? [])]
  const deleteReplies = []
  const repairs = []
  const pushes = []
  const json = (body, status = 200) =>
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    })
  const api = createClient({
    baseUrl: 'http://watched.test/api/v1',
    async fetch(request) {
      const url = new URL(request.url)
      if (request.method === 'DELETE') {
        const id = Number(url.pathname.split('/').pop())
        deletes.push(id)
        const reply = await deleteReplies.shift()
        if (reply instanceof Error) throw reply
        if (reply?.status) return json({ code: reply.status }, reply.status)
        data = data.filter((item) => item.id !== id)
        return json(undefined, 204)
      }
      assert.equal(url.pathname, '/api/v1/watched')
      assert.equal(url.searchParams.get('limit'), '30', 'the grid must not use the unpaged badge endpoint')
      const before = url.searchParams.get('before')
      requests.push({ before, signal: request.signal })
      const eligible = data.filter((item) => !before || item.id < Number(before.slice(1)))
      const reply = await replies.shift()
      if (reply instanceof Error) throw reply
      if (reply?.status) return json({ code: reply.status }, reply.status)
      if (reply?.body) return json(reply.body)
      const items = eligible.slice(0, 30)
      const hasMore = eligible.length > items.length
      return json({ items, hasMore, nextCursor: hasMore ? `c${items.at(-1).id}` : undefined })
    },
  })
  const icon = Vue.defineComponent({ setup: () => () => Vue.h('i') })
  const icons = { Eye: icon, X: icon, LibraryBig: icon, Loader2: icon, SearchX: icon, TriangleAlert: icon }
  const Status = evaluate(status, { vue: Vue, 'lucide-vue-next': icons })
  const View = evaluate(view, {
    vue: Vue,
    'vue-router': { useRouter: () => ({ push: (route) => pushes.push(route) }) },
    'lucide-vue-next': icons,
    '@/api/client': { default: api },
    '@/components/ErrorBanner.vue': {
      default: Vue.defineComponent({ props: ['message'], setup: (props) => () => Vue.h('div', props.message) }),
    },
    '@/components/media/DiscoverResultsStatus.vue': { default: Status },
    '@/composables/useDiscoverFilter': { discoverKey },
    '@/composables/usePagedDiscover': { usePagedDiscover },
    '@/composables/useWatchedPosters': {
      useWatchedPosters: () => ({
        itemPosterUrl: (item) => item.posterPath,
        loadMissingPosters: (items) => repairs.push(items.map((item) => item.id)),
        onPosterError() {},
      }),
    },
  })
  const root = node('root')
  const app = renderer.createApp(View)
  app.mount(root)
  let mounted = true
  const unmount = () => {
    if (mounted) app.unmount()
    mounted = false
  }
  t.after(() => {
    unmount()
    globalThis.IntersectionObserver = originalObserver
  })
  await settle()
  const images = () => findAll(root, (item) => item.type === 'img')
  return {
    root,
    requests,
    deletes,
    replies,
    deleteReplies,
    repairs,
    pushes,
    observer,
    unmount,
    images,
    intersect: () => observer.callback([{ isIntersecting: true }]),
    button(label) {
      const result = findAll(root, (item) => item.type === 'button' && text(item) === label)[0]
      assert.ok(result, `missing button ${label}`)
      return result
    },
    removeButton: () =>
      findAll(root, (item) => item.type === 'button' && item.props.title === 'Remove from watched')[0],
  }
}

test('Watched reuses Discover scrolling and status, fetching/rendering/repairing only loaded pages', async (t) => {
  const h = await mount(t)
  assert.equal(h.requests.length, 1)
  assert.equal(h.images().length, 30)
  assert.deepEqual(
    h.repairs.map((ids) => ids.length),
    [30],
  )
  assert.ok(h.images().every((img) => img.props.loading === 'lazy'))
  const firstImage = h.images()[0]
  h.intersect()
  await settle()
  assert.equal(h.requests[1].before, 'c71')
  assert.equal(h.images().length, 60)
  assert.equal(h.images()[0], firstImage, 'appending must not remount existing cards')
  assert.ok(h.observer.observes >= 3, 'sentinel is rearmed after each page')
  h.button('Load more').props.onClick()
  await settle()
  assert.equal(h.images().length, 65)
  assert.match(text(h.root), /No more titles/)
  h.intersect()
  await settle()
  assert.equal(h.requests.length, 3)
  assert.deepEqual(
    h.repairs.map((ids) => ids.length),
    [30, 60, 65],
  )
})

for (const failure of [{ status: 502 }, new Error('offline')]) {
  test(`initial failure stays retryable without claiming empty success: ${String(failure.status ?? failure)}`, async (t) => {
    const h = await mount(t, { replies: [failure] })
    assert.equal(h.images().length, 0)
    assert.match(text(h.root), /Could not load titles/)
    assert.doesNotMatch(text(h.root), /No watched items yet/)
    h.intersect()
    await settle()
    assert.equal(h.requests.length, 1, 'observer must not spin on a failed request')
    h.button('Retry').props.onClick()
    await settle()
    assert.equal(h.images().length, 30)
    assert.deepEqual(
      h.requests.map((r) => r.before),
      [null, null],
    )
  })
}

test('append failures preserve cards and retry the same cursor; overlapping triggers share the request', async (t) => {
  const h = await mount(t)
  const firstImage = h.images()[0]
  let finish
  h.replies.push(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  h.intersect()
  h.intersect()
  await settle()
  assert.equal(h.requests.length, 2)
  assert.equal(h.images().length, 30)
  finish({ status: 502 })
  await settle()
  assert.equal(h.images()[0], firstImage)
  h.button('Retry').props.onClick()
  await settle()
  assert.deepEqual(
    h.requests.map((r) => r.before),
    [null, 'c71', 'c71'],
  )
  assert.equal(h.images().length, 60)
})

test('successful removal filters locally without resetting the cursor; failed removal retains its card', async (t) => {
  const h = await mount(t)
  const remove = h.removeButton()
  h.deleteReplies.push({ status: 500 })
  await remove.props.onClick({ stopPropagation() {} })
  await settle()
  assert.equal(h.images().length, 30)
  assert.match(text(h.root), /Failed to remove watched item/)
  await remove.props.onClick({ stopPropagation() {} })
  await settle()
  assert.equal(h.images().length, 29)
  assert.doesNotMatch(text(h.root), /Failed to remove watched item/)
  h.intersect()
  await settle()
  assert.equal(h.requests[1].before, 'c71')
  assert.equal(h.images().length, 59)
  assert.equal(h.images()[29].props.alt, 'Watched 70', 'deletion cannot skip the next unseen title')
  assert.equal(new Set(h.images().map((img) => img.props.alt)).size, 59)
})

test('navigation disconnects the shared observer and aborts the page; late pages cannot trigger poster repair', async (t) => {
  const h = await mount(t)
  let finish
  h.replies.push(
    new Promise((resolve) => {
      finish = resolve
    }),
  )
  h.intersect()
  await settle()
  h.unmount()
  assert.equal(h.requests[1].signal.aborted, true)
  assert.equal(h.observer.disconnected, true)
  finish()
  await settle()
  assert.equal(h.repairs.length, 1)
  h.intersect()
  await settle()
  assert.equal(h.requests.length, 2)
})

test('malformed cursor pages stay retryable and do not replace the last good cursor', async (t) => {
  const h = await mount(t)
  h.replies.push({ body: { items: [], hasMore: true, nextCursor: 'c71' } })
  h.intersect()
  await settle()
  assert.equal(h.images().length, 30)
  assert.match(text(h.root), /Could not load titles/)
  h.button('Retry').props.onClick()
  await settle()
  assert.equal(h.images().length, 60)
  assert.deepEqual(
    h.requests.map((r) => r.before),
    [null, 'c71', 'c71'],
  )
})

test('an empty Watched list explains marking titles, not provider-key settings', async (t) => {
  const h = await mount(t, { count: 0 })
  assert.match(text(h.root), /No watched items yet/)
  assert.match(text(h.root), /Mark media as watched from the media detail or preview pages/)
  assert.doesNotMatch(text(h.root), /TMDB API key/)
  h.intersect()
  await settle()
  assert.equal(h.requests.length, 1)
})
