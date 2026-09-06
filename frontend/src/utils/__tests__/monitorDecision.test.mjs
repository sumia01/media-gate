import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'
import { monitorDecisionFreshness } from '../monitorDecision.ts'

const inputUpdatedAt = '2026-09-01T10:00:00Z'
const checkedAt = '2026-09-01T12:00:00Z'
const changedDuringSearch = '2026-09-01T11:00:00Z'
const snapshot = {
  inputUpdatedAt,
  checkedAt,
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

// Compile the real SFC and render with Vue's DOM-free renderer, using existing dependencies.
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

async function mountPanel(t, props, saved = snapshot) {
  const exports = {}
  const listeners = new Map()
  const modules = {
    vue: Vue,
    'lucide-vue-next': { ChevronRight: () => null, RefreshCw: () => null },
    '@/api/client': { default: { GET: async () => ({ data: { decision: saved } }) } },
    '@/composables/useEventStream': {
      useEventStream: () => ({
        on: (event, callback) => listeners.set(event, callback),
        off: (event) => listeners.delete(event),
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
    ...props,
  })
  const root = node('root')
  const app = renderer.createApp({ setup: () => () => Vue.h(exports.default, currentProps) })
  app.mount(root)
  let mounted = true
  const unmount = () => {
    if (mounted) app.unmount()
    mounted = false
  }
  t.after(unmount)
  await setImmediate()
  await Vue.nextTick()
  root.children[0].children[0].props.onClick()
  await Vue.nextTick()
  return {
    root,
    props: currentProps,
    unmount,
    async updateSnapshot(next) {
      saved = next
      listeners.get('worker.finished')?.({ name: 'monitor' })
      await setImmediate()
      await Vue.nextTick()
    },
  }
}

test('real panel warns after acknowledged edit during search and after remount', async (t) => {
  const first = await mountPanel(t, {})
  assert.doesNotMatch(textContent(first.root), /may not reflect|freshness is unknown/)
  first.props.updatedAt = changedDuringSearch
  await Vue.nextTick()
  assert.match(textContent(first.root), /monitoring settings changed since the inputs/)
  first.unmount()
  const reloaded = await mountPanel(t, { updatedAt: changedDuringSearch })
  assert.match(textContent(reloaded.root), /monitoring settings changed since the inputs/)
  assert.match(textContent(reloaded.root), /Last check completed:/)
})

test('a fresh worker snapshot clears the warning without remounting after a local edit', async (t) => {
  const panel = await mountPanel(t, {})
  // Only the acknowledged server version matters, not when the browser receives it.
  panel.props.updatedAt = changedDuringSearch
  await Vue.nextTick()
  assert.match(textContent(panel.root), /monitoring settings changed since the inputs/)

  const fresh = { ...snapshot, inputUpdatedAt: changedDuringSearch }
  await panel.updateSnapshot(fresh)
  assert.doesNotMatch(textContent(panel.root), /may not reflect|freshness is unknown/)

  // A still newer edit cannot be cleared by another response for the older input.
  panel.props.updatedAt = '2026-09-01T13:00:00Z'
  await panel.updateSnapshot(fresh)
  assert.match(textContent(panel.root), /monitoring settings changed since the inputs/)
  await panel.updateSnapshot({ ...fresh, inputUpdatedAt: panel.props.updatedAt })
  assert.doesNotMatch(textContent(panel.root), /may not reflect|freshness is unknown/)
})

test('real panel warns for old cycle inputs and changes after completion', async (t) => {
  for (const updatedAt of [changedDuringSearch, '2026-09-01T13:00:00Z']) {
    const panel = await mountPanel(t, { updatedAt })
    assert.match(textContent(panel.root), /saved result may not reflect the current state/)
  }
})

test('legacy snapshot freshness stays explicitly unknown, including disabled items', async (t) => {
  for (const input of [undefined, null]) {
    const panel = await mountPanel(t, { monitored: false }, { ...snapshot, inputUpdatedAt: input })
    assert.match(textContent(panel.root), /Input freshness is unknown/)
    assert.match(textContent(panel.root), /Auto-download is disabled/)
    assert.match(textContent(panel.root), /Last check completed:/)
  }
})

test('unchanged item stays free of stale warnings on remount after search marker updates', async (t) => {
  for (const outcome of ['no_results', 'grabbed']) {
    const panel = await mountPanel(t, {}, { ...snapshot, outcome })
    assert.doesNotMatch(textContent(panel.root), /may not reflect|freshness is unknown/)
    panel.unmount()
  }
})
