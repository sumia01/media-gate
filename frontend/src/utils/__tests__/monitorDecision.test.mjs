import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'
import { monitorDecisionFreshness } from '../monitorDecision.ts'

const inputUpdatedAt = '2026-09-01T10:00:00Z'
const changedDuringSearch = '2026-09-01T11:00:00Z'
const snapshot = {
  inputUpdatedAt,
  checkedAt: '2026-09-01T12:00:00Z',
  summary: 'No results.',
  outcome: 'no_results',
  details: [],
  truncated: false,
}

test('freshness follows input version, never completion time', () => {
  assert.equal(monitorDecisionFreshness(snapshot, inputUpdatedAt), 'unchanged')
  for (const changed of [changedDuringSearch, '2026-09-01T13:00:00Z']) {
    assert.equal(monitorDecisionFreshness(snapshot, changed), 'stale')
  }
  assert.equal(monitorDecisionFreshness(null, inputUpdatedAt), null)
  for (const input of [undefined, null, '', 'invalid']) {
    assert.equal(monitorDecisionFreshness({ ...snapshot, inputUpdatedAt: input }, inputUpdatedAt), 'unknown')
  }
})

const { descriptor } = parse(
  readFileSync(new URL('../../components/media/MonitorDecisionPanel.vue', import.meta.url), 'utf8'),
)
const compiled = compileScript(descriptor, {
  id: 'monitor-decision-test',
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

function findNode(root, predicate) {
  if (predicate(root)) return root
  for (const child of root.children) {
    const found = findNode(child, predicate)
    if (found) return found
  }
}

async function settle() {
  await setImmediate()
  await Vue.nextTick()
}

async function mountPanel(t, props = {}) {
  const exports = {}
  const listeners = new Map()
  const requests = []
  const modules = {
    vue: Vue,
    'lucide-vue-next': { ChevronRight: () => null },
    '@/api/client': {
      default: {
        GET: (path, options) => new Promise((resolve) => requests.push({ path, ...options, resolve })),
      },
    },
    '@/composables/useEventStream': {
      useEventStream: () => ({
        on: (event, callback) => listeners.set(event, callback),
        off: (event, callback) => {
          assert.equal(listeners.get(event), callback)
          listeners.delete(event)
        },
      }),
    },
    '@/utils/monitorDecision': { monitorDecisionFreshness },
  }
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected component import: ${name}`)
    return modules[name]
  }, exports)
  const currentProps = Vue.reactive({
    mediaItemId: 1,
    monitored: true,
    updatedAt: inputUpdatedAt,
    active: true,
    ...props,
  })
  const root = node('root')
  const app = renderer.createApp({ setup: () => () => Vue.h(exports.default, currentProps) })
  app.mount(root)
  t.after(() => app.unmount())
  await settle()
  return {
    root,
    props: currentProps,
    requests,
    listeners,
    clickDisclosure() {
      const button = findNode(root, (entry) => entry.type === 'button' && entry.props['aria-controls'])
      assert.ok(button)
      button.props.onClick()
    },
    emit(type, data) {
      listeners.get(type)?.(data)
    },
    resolve(index, decision = snapshot) {
      requests[index].resolve({ data: { decision } })
    },
    fail(index) {
      requests[index].resolve({ error: { code: 500 } })
    },
  }
}

test('saved check stays idle while hidden/collapsed, aborts exits and reloads retained data on reopen', async (t) => {
  const panel = await mountPanel(t, { active: false })
  assert.equal(panel.requests.length, 0)
  panel.props.active = true
  await Vue.nextTick()
  assert.equal(panel.requests.length, 0)

  panel.clickDisclosure()
  await Vue.nextTick()
  assert.equal(panel.requests.length, 1)
  assert.equal(panel.requests[0].params.path.id, 1)
  panel.clickDisclosure()
  assert.equal(panel.requests[0].signal.aborted, true)
  panel.resolve(0, { ...snapshot, summary: 'Stale response' })
  await settle()
  assert.doesNotMatch(textContent(panel.root), /Stale response/)

  panel.clickDisclosure()
  panel.resolve(1)
  await settle()
  assert.match(textContent(panel.root), /No results\./)
  panel.props.active = false
  await Vue.nextTick()
  panel.props.active = true
  await Vue.nextTick()
  assert.equal(panel.requests.length, 3, 'returning while expanded reloads the saved snapshot')
  panel.props.active = false
  await Vue.nextTick()
  assert.equal(panel.requests[2].signal.aborted, true, 'leaving Activity aborts a saved-check request')
  panel.resolve(2, { ...snapshot, summary: 'Hidden response' })
  await settle()
  assert.doesNotMatch(textContent(panel.root), /Hidden response/)
  panel.props.active = true
  await Vue.nextTick()
  panel.resolve(3, { ...snapshot, summary: 'Reloaded snapshot' })
  await settle()
  assert.match(textContent(panel.root), /Reloaded snapshot/)

  panel.clickDisclosure()
  panel.emit('worker.finished', { name: 'monitor' })
  assert.equal(panel.requests.length, 4, 'a collapsed panel only becomes dirty')
  panel.clickDisclosure()
  assert.equal(panel.requests.length, 5)
  panel.fail(4)
  await settle()
  const text = textContent(panel.root)
  assert.match(text, /Close and reopen this section/)
  assert.match(text, /Reloaded snapshot/, 'a later failure retains the successful snapshot')
  assert.doesNotMatch(text, /Refresh saved check|Use Retry/)
})

test('worker completion refreshes only an active expanded check and preserves freshness semantics', async (t) => {
  const panel = await mountPanel(t)
  panel.clickDisclosure()
  panel.resolve(0)
  await settle()
  panel.props.updatedAt = changedDuringSearch
  await Vue.nextTick()
  assert.match(textContent(panel.root), /monitoring settings changed since the inputs/)

  panel.emit('worker.finished', { name: 'monitor' })
  assert.equal(panel.requests.length, 2)
  panel.resolve(1, { ...snapshot, inputUpdatedAt: changedDuringSearch })
  await settle()
  assert.doesNotMatch(textContent(panel.root), /may not reflect|freshness is unknown/)

  panel.props.active = false
  await Vue.nextTick()
  panel.emit('worker.finished', { name: 'monitor' })
  assert.equal(panel.requests.length, 2)
})

test('route changes clear and collapse the check and reject late old-item responses', async (t) => {
  const panel = await mountPanel(t)
  panel.clickDisclosure()
  assert.equal(panel.requests.length, 1)
  panel.props.mediaItemId = 2
  await Vue.nextTick()
  assert.equal(panel.requests[0].signal.aborted, true)
  panel.resolve(0, { ...snapshot, summary: 'Old item' })
  await settle()
  assert.doesNotMatch(textContent(panel.root), /Old item/)
  assert.match(textContent(panel.root), /Open to load the latest saved check/)
  panel.clickDisclosure()
  assert.equal(panel.requests[1].params.path.id, 2)
})

test('legacy input freshness remains unknown after a successful lazy load', async (t) => {
  const panel = await mountPanel(t, { monitored: false })
  panel.clickDisclosure()
  panel.resolve(0, { ...snapshot, inputUpdatedAt: null })
  await settle()
  assert.match(textContent(panel.root), /Input freshness is unknown/)
  assert.match(textContent(panel.root), /Auto-download is disabled/)
  assert.match(textContent(panel.root), /Last check completed:/)
})
