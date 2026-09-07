import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'

const { descriptor } = parse(readFileSync(new URL('../../views/MediaDetailView.vue', import.meta.url), 'utf8'))
const compiled = compileScript(descriptor, {
  id: 'media-detail-activity-test',
  inlineTemplate: true,
  templateOptions: { compilerOptions: { hoistStatic: false } },
})
const { outputText } = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})

function node(type, text = '') {
  return {
    type,
    text,
    props: {},
    children: [],
    parent: null,
    focused: false,
    focus() {
      this.focused = true
    },
  }
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
  return `${root.text || ''} ${root.children.map(textContent).join(' ')}`
}

function findAll(root, predicate, found = []) {
  if (predicate(root)) found.push(root)
  for (const child of root.children) findAll(child, predicate, found)
  return found
}

async function settle() {
  await setImmediate()
  await Vue.nextTick()
  await Vue.nextTick()
}

function component(text, exposeActivity = false) {
  return Vue.defineComponent({
    setup(_props, { expose }) {
      if (exposeActivity) expose({ markDirty() {} })
      return () => Vue.h('div', text)
    },
  })
}

async function mountView(t) {
  const exports = {}
  const route = Vue.reactive({ params: { id: '1' } })
  const streamListeners = new Map()
  const requests = []
  const mutations = []
  const activityDirtyCalls = []
  const itemOverrides = {}
  const deferredMutations = { POST: [], PATCH: [], DELETE: [] }
  const item = (id) => ({
    id,
    libraryId: 10,
    title: `Example ${id}`,
    year: 2026,
    mediaType: 'series',
    source: 'disk',
    status: 'available',
    monitored: true,
    monitorNewSeasons: true,
    updatedAt: '2026-09-01T12:00:00Z',
    requests: [{ userId: 2, userName: 'Alice', scope: 'media', requestedAt: '2026-09-01T12:00:00Z' }],
    ...itemOverrides,
    metadata: {
      source: 'tmdb',
      externalId: id,
      title: `Example ${id}`,
      confidence: 1,
      overview: 'Overview',
      contentRatings: [],
      credits: [],
      imdbId: 'tt123',
      ...(itemOverrides.metadata ?? {}),
    },
  })
  const mutation = (method, path, options, fallback) => {
    mutations.push({ method, path, options })
    return deferredMutations[method].shift()?.promise ?? fallback
  }
  const api = {
    async GET(path, options = {}) {
      requests.push({ path, options })
      if (path === '/media/{id}') return { data: item(options.params.path.id) }
      if (path === '/libraries/{id}') return { data: { id: 10, name: 'Shows', path: '/shows', mediaType: 'series' } }
      if (path === '/media/{id}/files') return { data: { files: [] } }
      if (path === '/media-profiles') return { data: { profiles: [] } }
      if (path === '/settings') return { data: { settings: {} } }
      if (path === '/watched/check') return { data: { watched: false } }
      return { data: {} }
    },
    POST(path, options = {}) {
      return mutation('POST', path, options, { data: {} })
    },
    async PUT() {
      return { data: {} }
    },
    PATCH(path, options = {}) {
      return mutation('PATCH', path, options, { data: item(options.params.path.id) })
    },
    DELETE(path, options = {}) {
      return mutation('DELETE', path, options, {})
    },
  }
  const activityComponent = Vue.defineComponent({
    props: { mediaItemId: { type: Number, required: true } },
    setup(props, { expose }) {
      expose({ markDirty: () => activityDirtyCalls.push(props.mediaItemId) })
      return () => Vue.h('div', 'Activity log')
    },
  })
  const modules = {
    vue: Vue,
    'vue-router': { useRoute: () => route, useRouter: () => ({ replace() {} }) },
    'lucide-vue-next': {
      ArrowLeft: component('back icon'),
      ChevronRight: component('chevron'),
      ExternalLink: component('external icon'),
      Eye: component('eye'),
      EyeOff: component('eye off'),
      Pencil: component('pencil'),
      Play: component('play'),
    },
    '@/api/client': { default: api },
    '@/components/ContentRatingTile.vue': { default: component('ratings') },
    '@/components/ErrorBanner.vue': { default: component('error') },
    '@/components/media/DownloadList.vue': { default: component('downloads') },
    '@/components/media/EpisodeGrid.vue': { default: component('episodes subtitles') },
    '@/components/media/IndexerSearchModal.vue': { default: component('indexer modal') },
    '@/components/media/MatchPanel.vue': { default: component('match panel') },
    '@/components/media/MediaActivityPanel.vue': { default: activityComponent },
    '@/components/media/MonitorDecisionPanel.vue': { default: component('Latest auto-download check') },
    '@/components/media/MonitorSettingsModal.vue': { default: component('settings modal') },
    '@/components/media/SeasonMonitorModal.vue': { default: component('season modal') },
    '@/components/media/SubtitleList.vue': { default: component('subtitles') },
    '@/components/media/SubtitleSearchModal.vue': { default: component('subtitle modal') },
    '@/composables/useEventStream': {
      useEventStream: () => ({
        on: (type, callback) => streamListeners.set(type, callback),
        off: (type) => streamListeners.delete(type),
      }),
    },
    '@/utils/media': {
      formatBytes: () => '',
      parseGenres: () => [],
      posterUrl: () => '',
      profileImageUrl: () => null,
    },
    '@/utils/mediaRequests': { mediaRequesterNames: () => ['Alice'] },
  }
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected component import: ${name}`)
    return modules[name]
  }, exports)

  const root = node('root')
  const app = renderer.createApp(exports.default)
  app.component('router-link', component('Back to Shows'))
  app.mount(root)
  t.after(() => app.unmount())
  await settle()
  return {
    root,
    route,
    streamListeners,
    requests,
    mutations,
    activityDirtyCalls,
    item,
    setItem(updates) {
      Object.assign(itemOverrides, updates)
    },
    deferNext(method) {
      let resolve
      const promise = new Promise((done) => {
        resolve = done
      })
      deferredMutations[method].push({ promise })
      return resolve
    },
  }
}

test('detail tabs are linked, mounted, native-hidden and keep controls/requesters out of Activity', async (t) => {
  const view = await mountView(t)
  const tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  const panels = findAll(view.root, (entry) => entry.props.role === 'tabpanel')
  assert.equal(tabs.length, 2)
  assert.equal(panels.length, 2)
  assert.equal(tabs[0].props['aria-selected'], true)
  assert.equal(tabs[0].props.tabindex, 0)
  assert.equal(tabs[1].props.tabindex, -1)
  assert.equal(tabs[0].props['aria-controls'], panels[0].props.id)
  assert.equal(panels[0].props['aria-labelledby'], tabs[0].props.id)
  assert.equal(tabs[1].props['aria-controls'], panels[1].props.id)
  assert.equal(panels[1].props['aria-labelledby'], tabs[1].props.id)
  assert.equal(panels[0].props.hidden, false)
  assert.equal(panels[1].props.hidden, true)

  const details = textContent(panels[0])
  const activity = textContent(panels[1])
  assert.match(details, /Requested by/)
  assert.match(details, /Alice/)
  assert.match(details, /Search Indexers/)
  assert.match(details, /Auto-download/)
  assert.match(details, /episodes/)
  assert.match(details, /downloads/)
  assert.match(details, /subtitles/)
  assert.doesNotMatch(details, /Latest auto-download check|Activity log/)
  assert.match(activity, /Latest auto-download check.*Activity log/s)
  assert.doesNotMatch(activity, /Requested by|Search Indexers|Auto-download/)
})

test('Arrow/Home/End select and focus tabs without navigation and ordinary refreshes do not reset', async (t) => {
  const view = await mountView(t)
  const tablist = findAll(view.root, (entry) => entry.props.role === 'tablist')[0]
  let prevented = false
  tablist.props.onKeydown({
    key: 'ArrowRight',
    preventDefault() {
      prevented = true
    },
  })
  await Vue.nextTick()
  let tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(prevented, true)
  assert.equal(tabs[1].props['aria-selected'], true)
  assert.equal(tabs[1].focused, true)

  view.streamListeners.get('media.request_added')?.({ mediaItemId: 1 })
  await settle()
  tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[1].props['aria-selected'], true, 'same-item SSE refresh preserves Activity')

  tablist.props.onKeydown({ key: 'Home', preventDefault() {} })
  await Vue.nextTick()
  tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[0].props['aria-selected'], true)
  tablist.props.onKeydown({ key: 'End', preventDefault() {} })
  await Vue.nextTick()
  tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[1].props['aria-selected'], true)

  view.route.params.id = '2'
  await settle()
  tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[0].props['aria-selected'], true, 'a new media route resets to Details')
  assert.match(tabs[0].props.id, /-2$/)
})

test('activity and metadata events refresh only the current item without opening hidden panels', async (t) => {
  const view = await mountView(t)
  const initialRequestCount = view.requests.length
  view.setItem({ monitored: false, updatedAt: '2026-09-02T12:00:00Z' })

  view.streamListeners.get('media.activity_added')?.({ mediaItemId: 999 })
  await settle()
  assert.equal(view.requests.length, initialRequestCount)

  view.streamListeners.get('media.activity_added')?.({ mediaItemId: 1 })
  await settle()
  let eventRequests = view.requests.slice(initialRequestCount)
  assert.deepEqual(
    eventRequests.map((request) => request.path),
    ['/media/{id}'],
  )
  assert.equal(eventRequests[0].options.params.path.id, 1)
  assert.equal(
    eventRequests.some((request) => request.path.includes('activity') || request.path.includes('monitor-decision')),
    false,
  )
  let tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[0].props['aria-selected'], true)
  let decision = findAll(view.root, (entry) => entry.text === 'Latest auto-download check')[0]
  assert.equal(decision.props.updatedAt, '2026-09-02T12:00:00Z')
  assert.equal(decision.props.monitored, false)

  const beforeMetadata = view.requests.length
  view.setItem({ updatedAt: '2026-09-03T12:00:00Z', metadata: { overview: 'Fresh metadata' } })
  view.streamListeners.get('media.metadata_refreshed')?.({ mediaItemId: 1 })
  await settle()
  eventRequests = view.requests.slice(beforeMetadata)
  assert.deepEqual(
    eventRequests.map((request) => request.path),
    ['/media/{id}'],
  )
  decision = findAll(view.root, (entry) => entry.text === 'Latest auto-download check')[0]
  assert.equal(decision.props.updatedAt, '2026-09-03T12:00:00Z')

  tabs[1].props.onClick()
  await Vue.nextTick()
  view.streamListeners.get('media.activity_added')?.({ mediaItemId: 1 })
  await settle()
  tabs = findAll(view.root, (entry) => entry.props.role === 'tab')
  assert.equal(tabs[1].props['aria-selected'], true, 'ordinary same-item refresh preserves Activity')
})

test('late resync and watched completions cannot mutate or invalidate the next route', async (t) => {
  const view = await mountView(t)
  const resyncButton = findAll(
    view.root,
    (entry) => entry.type === 'button' && textContent(entry).includes('Rescan Files'),
  )[0]
  const resolveResync = view.deferNext('POST')
  const resync = resyncButton.props.onClick()
  assert.equal(view.mutations.at(-1).options.params.path.id, 1)

  view.route.params.id = '2'
  await settle()
  const itemTwoFilesBefore = view.requests.filter(
    (request) => request.path === '/media/{id}/files' && request.options.params.path.id === 2,
  ).length
  resolveResync({})
  await resync
  await settle()
  assert.equal(
    view.requests.filter((request) => request.path === '/media/{id}/files' && request.options.params.path.id === 2)
      .length,
    itemTwoFilesBefore,
  )
  assert.deepEqual(view.activityDirtyCalls, [])

  view.route.params.id = '3'
  await settle()
  const watchedButton = findAll(
    view.root,
    (entry) => entry.type === 'button' && textContent(entry).includes('Unseen'),
  )[0]
  const resolveWatched = view.deferNext('POST')
  const watched = watchedButton.props.onClick()
  assert.equal(view.mutations.at(-1).options.body.mediaItemId, 3)

  view.route.params.id = '4'
  await settle()
  resolveWatched({ data: { id: 44 } })
  await watched
  await settle()
  assert.deepEqual(view.activityDirtyCalls, [])
  assert.ok(
    findAll(view.root, (entry) => entry.type === 'button' && textContent(entry).includes('Unseen')).length,
    'the new route retains its own watched state',
  )
})
