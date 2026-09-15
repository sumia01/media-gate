import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import ts from 'typescript'
import * as Vue from 'vue'

const source = readFileSync(new URL('../useWatchedLibrary.ts', import.meta.url), 'utf8')
const { outputText } = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})

const renderer = Vue.createRenderer({
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

function loadComposable(api, router) {
  const exports = {}
  const modules = {
    vue: Vue,
    'vue-router': { useRouter: () => router },
    '@/api/client': { default: api },
    '@/composables/useDiscoverFilter': {
      discoverKey: (item) => `${item.source}:${item.mediaType}:${item.externalId}`,
    },
  }
  new Function('require', 'exports', outputText)((name) => modules[name], exports)
  return exports.useWatchedLibrary
}

function mount(t, setup) {
  let state
  const app = renderer.createApp({
    setup() {
      state = setup()
      return () => null
    },
  })
  app.mount({})
  t.after(() => app.unmount())
  return state
}

test('card navigation waits for the shared library lookup before choosing its route', async (t) => {
  let resolveMembership
  let membershipRequests = 0
  const pushes = []
  const api = {
    GET(path) {
      assert.equal(path, '/media/external-ids')
      membershipRequests++
      return new Promise((resolve) => {
        resolveMembership = resolve
      })
    },
  }
  const useWatchedLibrary = loadComposable(api, { push: (route) => pushes.push(route) })
  const state = mount(t, useWatchedLibrary)
  const item = { source: 'tmdb', mediaType: 'movie', externalId: 42 }

  const lookup = state.fetchLibraryItems()
  const navigation = state.goToPreview(item)
  await setImmediate()
  assert.equal(membershipRequests, 1, 'the in-flight lookup is shared')
  assert.deepEqual(pushes, [], 'navigation waits for membership')

  resolveMembership({ data: { items: [{ ...item, mediaItemId: 7 }] } })
  await lookup
  await navigation
  assert.deepEqual(pushes, [{ name: 'media-detail', params: { id: 7 } }])
})
