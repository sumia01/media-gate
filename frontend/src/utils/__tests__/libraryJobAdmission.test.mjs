import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import createClient from 'openapi-fetch'
import ts from 'typescript'
import * as Vue from 'vue'

function compileView(path, id) {
  const { descriptor } = parse(readFileSync(new URL(path, import.meta.url), 'utf8'))
  return compileScript(descriptor, {
    id,
    inlineTemplate: true,
    templateOptions: { compilerOptions: { hoistStatic: false } },
  }).content
}

const viewSource = compileView('../../views/LibraryDetailView.vue', 'library-admission-test')
const bannerSource = compileView('../../components/ErrorBanner.vue', 'library-admission-banner-test')
const queueSource = readFileSync(new URL('../../composables/useJobQueue.ts', import.meta.url), 'utf8')

function evaluate(source, modules) {
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  })
  const exports = {}
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected import: ${name}`)
    return modules[name]
  }, exports)
  return exports
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

function textContent(root) {
  return `${root.text || ''} ${root.children.map(textContent).join(' ')}`.replace(/\s+/g, ' ').trim()
}

function findAll(root, predicate, result = []) {
  if (predicate(root)) result.push(root)
  for (const child of root.children) findAll(child, predicate, result)
  return result
}

async function settle() {
  await setImmediate()
  await Vue.nextTick()
  await Vue.nextTick()
}

async function mountLibrary(t) {
  const route = Vue.reactive({ params: { id: '1' } })
  const posts = []
  const replies = []
  const jobs = []
  const listeners = new Map()
  const vueErrors = []
  let jobReads = 0
  let failNextJobRead = false
  const json = (body, status = 200) =>
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    })
  const api = createClient({
    baseUrl: 'http://library-admission.test/api/v1',
    async fetch(request) {
      const url = new URL(request.url)
      const path = url.pathname.replace('/api/v1', '')
      if (request.method === 'POST') {
        posts.push({ path, query: url.searchParams })
        const reply = await (replies.shift() ?? { status: 202 })
        if (reply instanceof Error) throw reply
        if (reply.status !== 202) return json(reply.body, reply.status)
        const libraryId = Number(path.split('/')[2])
        const job = {
          id: `job_${jobs.length + 1}`,
          libraryId,
          status: 'pending',
          type: path.endsWith('/sync') ? 'sync_library' : 'match_library',
          createdAt: '2026-10-21T12:00:00Z',
        }
        jobs.push(job)
        return json(job, 202)
      }
      if (path === '/jobs') {
        jobReads++
        if (failNextJobRead) {
          failNextJobRead = false
          throw new TypeError('status refresh disconnected')
        }
        return json({ jobs })
      }
      if (path === '/media-profiles') return json({ profiles: [] })
      if (path === '/watched') return json({ items: [] })
      if (/^\/libraries\/\d+\/media$/.test(path)) return json({ items: [], total: 0 })
      if (/^\/libraries\/\d+$/.test(path)) {
        const id = Number(path.split('/')[2])
        return json({ id, name: `Library ${id}`, path: `/library/${id}`, mediaType: 'movie' })
      }
      assert.fail(`unexpected request: ${request.method} ${path}`)
    },
  })
  const stream = {
    on(type, callback) {
      if (!listeners.has(type)) listeners.set(type, new Set())
      listeners.get(type).add(callback)
    },
    off(type, callback) {
      listeners.get(type)?.delete(callback)
    },
  }
  const queueModule = evaluate(queueSource, {
    vue: Vue,
    '@/api/client': { default: api },
    './useEventStream': { useEventStream: () => stream },
  })
  let queue
  const empty = Vue.defineComponent({ setup: () => () => Vue.h('span') })
  const modal = Vue.defineComponent({
    setup:
      (_props, { slots }) =>
      () =>
        Vue.h('section', slots.default?.()),
  })
  const viewModule = evaluate(viewSource, {
    vue: Vue,
    'vue-router': { useRoute: () => route, useRouter: () => ({ push() {} }), onBeforeRouteLeave() {} },
    'lucide-vue-next': { Eye: empty, Loader2: empty, Plus: empty, RefreshCw: empty, Sparkles: empty },
    '@/api/client': { default: api },
    '@/components/BaseModal.vue': { default: modal },
    '@/components/ErrorBanner.vue': { default: evaluate(bannerSource, { vue: Vue }).default },
    '@/components/media/LibraryFilters.vue': { default: empty },
    '@/composables/useEventStream': { useEventStream: () => stream },
    '@/composables/useGlobalSearch': { useGlobalSearch: () => ({ openSearch() {}, setActiveLibrary() {} }) },
    '@/composables/useJobQueue': {
      useJobQueue() {
        queue = queueModule.useJobQueue()
        return queue
      },
    },
    '@/utils/media': { filterLibraryItems: (items) => items, libraryGenres: () => [], posterUrl: () => '' },
  })
  const root = node('root')
  const app = renderer.createApp(viewModule.default)
  app.config.errorHandler = (error) => vueErrors.push(error)
  app.mount(root)
  t.after(() => {
    app.unmount()
    assert.deepEqual(vueErrors, [], 'request errors must not escape Vue handlers')
    assert.equal(
      [...listeners.values()].reduce((sum, callbacks) => sum + callbacks.size, 0),
      0,
    )
  })
  await settle()
  function button(label) {
    const found = findAll(
      root,
      (entry) =>
        entry.type === 'button' && (textContent(entry) === label || textContent(entry).startsWith(`${label} `)),
    )
    assert.equal(found.length, 1, `missing unique button: ${label}`)
    return found[0]
  }
  return {
    root,
    route,
    posts,
    jobs,
    queue,
    get jobReads() {
      return jobReads
    },
    banner() {
      return findAll(root, (entry) => entry.type === 'div' && entry.props.class?.includes('bg-red-500/10'))
        .map(textContent)
        .join(' ')
    },
    reply(response) {
      replies.push(response)
    },
    defer() {
      let resolve
      replies.push(
        new Promise((done) => {
          resolve = done
        }),
      )
      return resolve
    },
    failRefresh() {
      failNextJobRead = true
    },
    async submit(operation, fullRematch = false) {
      if (operation === 'match') {
        button('Match').props.onClick()
        await Vue.nextTick()
        await button(fullRematch ? 'Full re-match' : 'Unmatched only').props.onClick()
      } else {
        await button('Sync').props.onClick()
      }
      await settle()
    },
  }
}

for (const operation of ['sync', 'match']) {
  for (const [status, message] of [
    [503, 'job queue is full; try again later'],
    [503, 'job queue is shutting down'],
    [409, 'library already has an active job'],
    [404, 'library not found'],
  ]) {
    test(`${operation} displays HTTP ${status}: ${message}`, async (t) => {
      const view = await mountLibrary(t)
      view.reply({ status, body: { code: status, message } })
      await view.submit(operation)
      assert.equal(view.banner(), message)
      assert.equal(view.jobs.length, 0)
      assert.equal(view.jobReads, 1, 'rejected jobs must not refresh or appear accepted')
      assert.equal(view.posts.length, 1)
      assert.doesNotMatch(textContent(view.root), /Match Mode/, 'page error remains visible outside the closed modal')
    })
  }

  test(`${operation} shows a fallback for empty HTTP errors and network failures`, async (t) => {
    const view = await mountLibrary(t)
    for (const reply of [{ status: 503 }, new TypeError('private transport diagnostic')]) {
      view.reply(reply)
      await view.submit(operation)
      assert.match(view.banner(), /Could not start .*Please try again\./)
      assert.doesNotMatch(view.banner(), /private transport/)
    }
    assert.equal(view.jobReads, 1)
  })

  test(`${operation} preserves accepted-job refresh and full-rematch options`, async (t) => {
    const view = await mountLibrary(t)
    await view.submit(operation, true)
    assert.equal(view.banner(), '')
    assert.equal(view.jobReads, 2)
    assert.equal(view.queue.jobs.value.length, 1)
    assert.equal(view.posts[0].path, `/libraries/1/${operation}`)
    assert.equal(view.posts[0].query.get('fullRematch'), operation === 'match' ? 'true' : null)
    assert.match(textContent(view.root), operation === 'match' ? /Matching\.\.\./ : /Syncing\.\.\./)
  })

  test(`${operation} remains accepted when the subsequent status refresh disconnects`, async (t) => {
    const view = await mountLibrary(t)
    view.failRefresh()
    await view.submit(operation)
    assert.equal(view.banner(), '')
    assert.equal(view.jobs.length, 1, 'the POST was accepted before the GET failed')
    assert.equal(view.queue.jobs.value.length, 0, 'keep the previous snapshot after a failed read')
    await view.queue.fetchJobs()
    assert.equal(view.queue.jobs.value.length, 1, 'a later refresh can recover normally')
  })

  test(`${operation} clears a rejection when retrying without automatically resubmitting`, async (t) => {
    const view = await mountLibrary(t)
    view.reply({ status: 503, body: { code: 503, message: 'job queue is full; try again later' } })
    await view.submit(operation)
    assert.match(view.banner(), /queue is full/)
    const resolve = view.defer()
    const retry = view.submit(operation)
    await settle()
    assert.equal(view.banner(), '')
    assert.equal(view.posts.length, 2)
    resolve({ status: 202 })
    await retry
    assert.equal(view.banner(), '')
    assert.equal(view.jobs.length, 1)
  })

  test(`${operation} ignores a late rejection after navigating to another library`, async (t) => {
    const view = await mountLibrary(t)
    const resolve = view.defer()
    const pending = view.submit(operation)
    await settle()
    view.route.params.id = '2'
    await settle()
    resolve({ status: 503, body: { code: 503, message: 'outgoing library failure' } })
    await pending
    assert.match(textContent(view.root), /Library 2/)
    assert.equal(view.banner(), '')
  })
}

test('an older submission cannot overwrite a successful retry with a late rejection', async (t) => {
  const view = await mountLibrary(t)
  const resolve = view.defer()
  const first = view.submit('sync')
  await settle()
  await view.submit('sync')
  resolve({ status: 503, body: { code: 503, message: 'older attempt rejected' } })
  await first
  assert.equal(view.banner(), '')
  assert.equal(view.jobs.length, 1)
})

test('leaving and returning to the same library invalidates the outgoing request', async (t) => {
  const view = await mountLibrary(t)
  const resolve = view.defer()
  const pending = view.submit('sync')
  await settle()
  view.route.params.id = '2'
  await settle()
  view.route.params.id = '1'
  await settle()
  resolve({ status: 503, body: { code: 503, message: 'stale original request' } })
  await pending
  assert.equal(view.banner(), '')
})
