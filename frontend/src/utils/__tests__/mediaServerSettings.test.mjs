import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import createClient from 'openapi-fetch'
import ts from 'typescript'
import * as Vue from 'vue'

const { descriptor } = parse(
  readFileSync(new URL('../../components/libraries/LibrariesMediaServer.vue', import.meta.url), 'utf8'),
)
const { outputText } = ts.transpileModule(compileScript(descriptor, { id: 'media-server-settings-test' }).content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})
const renderer = Vue.createRenderer({
  createComment: () => ({}),
  insert() {},
  remove() {},
  parentNode: () => null,
  nextSibling: () => null,
})
const settle = async () => {
  await setImmediate()
  await Vue.nextTick()
}

async function mount(t, server) {
  const urlKey = `${server}Url`
  const secretKey = server === 'plex' ? 'plexToken' : 'jellyfinApiKey'
  const enabledKey = `${server}RefreshEnabled`
  const idKey = server === 'plex' ? 'plexSectionId' : 'jellyfinLibraryId'
  const settings = { [urlKey]: 'http://media.test', [secretKey]: '****-key', [enabledKey]: server === 'plex' }
  const requests = []
  let failure = null
  let mapping = 'library-id'
  let serverLibrary = 'library-id'
  let holdMappings = false
  const pendingMappings = []
  const json = (body, status = 200) =>
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'content-type': 'application/json' },
    })
  const api = createClient({
    baseUrl: 'http://app.test',
    async fetch(request) {
      const path = new URL(request.url).pathname
      const body = request.method === 'PUT' || path.includes('test-') ? await request.json() : null
      requests.push({ method: request.method, path, body })
      if (failure) {
        const error = failure
        failure = null
        if (error instanceof Error) throw error
        return json({}, error)
      }
      if (path === '/settings') {
        if (body) Object.assign(settings, body)
        return json({ settings })
      }
      if (path === `/${server}/mappings`) {
        if (body) mapping = body.mappings[0][idKey]
        const response = json({
          mappings: [
            {
              libraryId: 1,
              libraryName: 'Movies',
              libraryPath: '/movies',
              libraryType: 'movie',
              [idKey]: mapping,
              autoMatched: false,
            },
          ],
        })
        return request.method === 'GET' ? mappingReply(request, response) : response
      }
      if (path === `/${server}/${server === 'plex' ? 'sections' : 'libraries'}`)
        return mappingReply(
          request,
          json({
            [server === 'plex' ? 'sections' : 'libraries']: [
              { id: serverLibrary, title: 'Movies', type: 'movie', locations: [] },
            ],
          }),
        )
      if (path.includes('/refresh/')) return json(null, 204)
      if (path === `/settings/test-${server}`) return json({ success: true, message: 'Connected' })
      assert.fail(`unexpected request ${path}`)
    },
  })
  function mappingReply(request, response) {
    if (!holdMappings) return response
    // Ignore abort intentionally to also test transports that deliver late replies.
    return new Promise((resolve, reject) =>
      pendingMappings.push({
        signal: request.signal,
        complete: () => resolve(response),
        fail: (error) => (error instanceof Error ? reject(error) : resolve(json({}, error))),
      }),
    )
  }
  const exports = {}
  new Function('require', 'exports', outputText)((name) => {
    if (name === 'vue') return Vue
    if (name === '@/api/client') return { default: api }
    assert.fail(`unexpected import ${name}`)
  }, exports)
  let state
  const app = renderer.createApp({
    setup() {
      state = exports.default.setup({ server }, { expose() {} })
      return () => null
    },
  })
  app.mount({})
  let mounted = true
  function unmount() {
    if (!mounted) return
    mounted = false
    app.unmount()
  }
  t.after(unmount)
  await settle()
  return {
    state,
    requests,
    settings,
    enabledKey,
    unmount,
    pendingMappings,
    holdMappings(value) {
      holdMappings = value
    },
    setServerLibrary(id) {
      serverLibrary = id
      mapping = id
    },
    fail: (error) => {
      failure = error
    },
  }
}

for (const server of ['plex', 'jellyfin']) {
  test(`${server}: toggle is independent, persisted immediately, and preserves connection drafts`, async (t) => {
    const { state, requests, settings, enabledKey } = await mount(t, server)
    state.serverUrl.value = 'http://unsaved.test'
    const previous = state.refreshEnabled.value
    await state.toggleRefresh()
    assert.equal(state.refreshEnabled.value, !previous)
    assert.equal(settings[enabledKey], !previous)
    assert.equal(state.serverUrl.value, 'http://unsaved.test')
    assert.deepEqual(requests.at(-1).body, { [enabledKey]: !previous })
    await state.fetchSettings()
    assert.equal(state.refreshEnabled.value, !previous)
  })

  test(`${server}: toggle failures retain saved state and recover`, async (t) => {
    const { state, fail } = await mount(t, server)
    const previous = state.refreshEnabled.value
    for (const error of [500, new TypeError('offline')]) {
      fail(error)
      await state.toggleRefresh()
      assert.equal(state.refreshEnabled.value, previous)
      assert.equal(state.savingToggle.value, false)
      assert.match(state.toggleError.value, /Failed/)
    }
    await state.toggleRefresh()
    assert.equal(state.toggleError.value, '')
    assert.equal(state.refreshEnabled.value, !previous)
  })

  test(`${server}: unmapping, masked credential tests, manual refresh errors`, async (t) => {
    const { state, requests, fail } = await mount(t, server)
    state.selectedSections.value[1] = ''
    await state.saveMappings()
    const field = server === 'plex' ? 'plexSectionId' : 'jellyfinLibraryId'
    assert.deepEqual(requests.at(-1).body, { mappings: [{ libraryId: 1, [field]: '' }] })
    await state.loadMappings()
    assert.equal(state.selectedSections.value[1], '')
    await state.testConnection()
    assert.deepEqual(requests.at(-1).body, {})
    state.secret.value = 'draft-key'
    await state.testConnection()
    assert.deepEqual(requests.at(-1).body, { [server === 'plex' ? 'token' : 'apiKey']: 'draft-key' })
    fail(502)
    await state.refreshSection('library-id')
    assert.match(state.mappingError.value, /Failed to refresh/)
    assert.equal(state.refreshing.value, '')
    await state.refreshSection('library-id')
    assert.equal(state.mappingNotice.value, 'Library refresh requested')
  })

  for (const outcome of ['success', 502, new TypeError('offline')]) {
    test(`${server}: connection save ignores old mapping ${outcome}`, async (t) => {
      const view = await mount(t, server)
      view.holdMappings(true)
      const oldLoad = view.state.loadMappings()
      await settle()
      const oldRequests = view.pendingMappings.splice(0)
      assert.equal(oldRequests.length, 2)
      view.holdMappings(false)
      view.setServerLibrary('new-library')
      view.state.serverUrl.value = 'http://new-server.test'
      await view.state.saveConnection()
      assert.equal(view.state.selectedSections.value[1], 'new-library')
      for (const request of oldRequests) {
        assert.equal(request.signal.aborted, true)
        if (outcome === 'success') request.complete()
        else request.fail(outcome)
      }
      await oldLoad
      assert.equal(view.state.sections.value[0].id, 'new-library')
      assert.equal(view.state.selectedSections.value[1], 'new-library')
      assert.equal(view.state.mappingError.value, '')
      await view.state.saveMappings()
      assert.deepEqual(view.requests.at(-1).body, {
        mappings: [{ libraryId: 1, [server === 'plex' ? 'plexSectionId' : 'jellyfinLibraryId']: 'new-library' }],
      })
    })
  }

  test(`${server}: stale completion cannot clear the new load's busy state`, async (t) => {
    const view = await mount(t, server)
    view.holdMappings(true)
    const oldLoad = view.state.loadMappings()
    await settle()
    const oldRequests = view.pendingMappings.splice(0)
    view.setServerLibrary('new-library')
    view.state.serverUrl.value = 'http://new-server.test'
    const save = view.state.saveConnection()
    await settle()
    assert.equal(view.pendingMappings.length, 2)
    for (const request of oldRequests) request.fail(502)
    await oldLoad
    assert.equal(view.state.loadingMappings.value, true)
    assert.equal(view.state.mappingError.value, '')
    for (const request of view.pendingMappings.splice(0)) request.complete()
    await save
    assert.equal(view.state.loadingMappings.value, false)
    assert.equal(view.state.selectedSections.value[1], 'new-library')
  })

  test(`${server}: disconnect and unmount invalidate pending mapping loads`, async (t) => {
    const view = await mount(t, server)
    view.holdMappings(true)
    const load = view.state.loadMappings()
    await settle()
    await view.state.disconnect()
    view.pendingMappings.splice(0).forEach((r) => {
      assert.equal(r.signal.aborted, true)
      r.complete()
    })
    await load
    assert.equal(view.state.connected.value, false)
    assert.deepEqual(view.state.mappings.value, [])
    assert.deepEqual(view.state.selectedSections.value, {})
    const outgoingLoad = view.state.loadMappings()
    await settle()
    view.unmount()
    view.pendingMappings.splice(0).forEach((r) => {
      assert.equal(r.signal.aborted, true)
      r.fail(502)
    })
    await outgoingLoad
    assert.equal(view.state.mappingError.value, '')
    assert.deepEqual(view.state.mappings.value, [])
  })
}
