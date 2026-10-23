import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import createClient from 'openapi-fetch'
import ts from 'typescript'
import * as Vue from 'vue'

test('search modal uses safe indexer options and preserves the selected indexer/profile', async (t) => {
  const previousDocument = globalThis.document
  globalThis.document = { addEventListener() {}, removeEventListener() {} }
  const requests = []
  const api = createClient({
    baseUrl: 'http://selection.test',
    async fetch(request) {
      const url = new URL(request.url)
      requests.push(url)
      let body
      if (url.pathname === '/indexers/options') body = { indexers: [{ id: 7, name: 'Enabled tracker' }] }
      else if (url.pathname === '/indexers/search') body = { results: [] }
      else assert.fail(`unexpected privileged lookup: ${url.pathname}`)
      return new Response(JSON.stringify(body), { headers: { 'content-type': 'application/json' } })
    },
  })
  const { descriptor } = parse(
    readFileSync(new URL('../../components/media/IndexerSearchModal.vue', import.meta.url), 'utf8'),
  )
  const { outputText } = ts.transpileModule(compileScript(descriptor, { id: 'indexer-selection-test' }).content, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  })
  const exports = {}
  const modules = {
    vue: Vue,
    '@/api/client': { default: api },
    'lucide-vue-next': {},
    '@/components/BaseModal.vue': {},
    '@/components/ErrorBanner.vue': {},
    '@/utils/media': {},
    '@/utils/torrent': {},
  }
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected import ${name}`)
    return modules[name]
  }, exports)
  const renderer = Vue.createRenderer({
    createComment: () => ({}),
    insert() {},
    remove() {},
    parentNode: () => null,
    nextSibling: () => null,
  })
  let state
  const app = renderer.createApp({
    setup() {
      state = exports.default.setup(
        { mediaItemId: 1, mediaType: 'movie', title: 'Movie', profileId: 3 },
        { expose() {}, emit() {} },
      )
      return () => null
    },
  })
  app.mount({})
  t.after(() => {
    app.unmount()
    globalThis.document = previousDocument
  })
  await setImmediate()
  await Vue.nextTick()
  assert.deepEqual(state.indexers.value, [{ id: 7, name: 'Enabled tracker' }])
  assert.equal(requests[0].pathname, '/indexers/options')
  state.selectedIndexerId.value = '7'
  await state.search()
  assert.equal(requests.at(-1).searchParams.get('indexerIds'), '7')
  assert.equal(requests.at(-1).searchParams.get('profileId'), '3')
})
