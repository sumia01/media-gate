import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'

const { descriptor } = parse(readFileSync(new URL('../../views/PersonCreditsView.vue', import.meta.url), 'utf8'))
const compiled = compileScript(descriptor, {
  id: 'person-credits-test',
  inlineTemplate: true,
  templateOptions: { compilerOptions: { hoistStatic: false } },
})
const { outputText } = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})

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
  return `${root.text || ''} ${root.children.map(textContent).join(' ')}`
}

function findAll(root, predicate, found = []) {
  if (predicate(root)) found.push(root)
  for (const child of root.children) findAll(child, predicate, found)
  return found
}

const icon = Vue.defineComponent({ setup: () => () => Vue.h('i') })

async function mountView(t) {
  const exports = {}
  const requests = []
  const route = Vue.reactive({ name: 'discover-person', query: { name: 'Example Actor', image: '/actor.jpg' } })
  const movie = (id) => ({ source: 'tmdb', externalId: id, mediaType: 'movie', title: `Movie ${id}` })
  const series = (id) => ({ source: 'tmdb', externalId: id, mediaType: 'series', title: `Series ${id}` })
  const api = {
    async GET(path, options) {
      requests.push({ path, options })
      return {
        data: {
          name: 'Example Actor',
          movies: Array.from({ length: 40 }, (_, index) => movie(index + 1)),
          series: [series(101), series(102)],
        },
      }
    },
  }
  const discoverCard = Vue.defineComponent({
    props: { item: { type: Object, required: true } },
    setup(props) {
      return () => Vue.h('article', { mediaType: props.item.mediaType }, props.item.title)
    },
  })
  const modules = {
    vue: Vue,
    'vue-router': { useRoute: () => route, useRouter: () => ({ back() {} }) },
    'lucide-vue-next': { ArrowLeft: icon, Film: icon, Tv: icon, UserRound: icon },
    '@/api/client': { default: api },
    '@/components/media/DiscoverCard.vue': { default: discoverCard },
    '@/components/media/DiscoverFilter.vue': { default: Vue.defineComponent({ setup: () => () => Vue.h('div') }) },
    '@/composables/useDiscoverFilter': {
      useDiscoverFilter: () => ({ hideInLibrary: Vue.ref(false), filterReady: Vue.ref(true), includeItem: () => true }),
    },
    '@/composables/useWatchedLibrary': {
      useWatchedLibrary: () => ({
        isWatched: () => false,
        isInLibrary: () => false,
        libraryReady: Vue.ref(true),
        libraryLoading: Vue.ref(false),
        libraryFailed: Vue.ref(false),
        fetchWatched() {},
        fetchLibraryItems() {},
        goToPreview() {},
      }),
    },
    '@/utils/media': { profileImageUrl: () => null },
  }
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected component import: ${name}`)
    return modules[name]
  }, exports)

  const root = node('root')
  const app = renderer.createApp(exports.default, { source: 'unsupported', personId: '287' })
  app.mount(root)
  t.after(() => app.unmount())
  await setImmediate()
  await Vue.nextTick()
  await Vue.nextTick()
  return { root, requests, route }
}

test('person credits render bounded movie and series sections and ignore outgoing route changes', async (t) => {
  const view = await mountView(t)
  assert.equal(view.requests.length, 1)
  assert.deepEqual(view.requests[0].options.params.path, { source: 'tmdb', personId: 287 })

  let cards = findAll(view.root, (entry) => entry.type === 'article')
  assert.equal(cards.filter((entry) => entry.props.mediaType === 'movie').length, 35)
  assert.equal(cards.filter((entry) => entry.props.mediaType === 'series').length, 2)
  const loadMore = findAll(
    view.root,
    (entry) => entry.type === 'button' && textContent(entry).includes('Load more movies'),
  )[0]
  loadMore.props.onClick()
  await Vue.nextTick()
  cards = findAll(view.root, (entry) => entry.type === 'article')
  assert.equal(cards.filter((entry) => entry.props.mediaType === 'movie').length, 40)

  view.route.name = 'media-detail'
  view.route.query.name = 'Outgoing route query'
  await Vue.nextTick()
  assert.equal(view.requests.length, 1)
})
