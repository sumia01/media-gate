import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'

const { descriptor } = parse(
  readFileSync(new URL('../../components/media/MediaActivityPanel.vue', import.meta.url), 'utf8'),
)
const compiled = compileScript(descriptor, {
  id: 'media-activity-panel-test',
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

function findButton(root, label) {
  if (root.type === 'button' && textContent(root).includes(label)) return root
  for (const child of root.children) {
    const found = findButton(child, label)
    if (found) return found
  }
}

test('pagination replacements move focus to the next useful control', async (t) => {
  const activity = {
    items: Vue.ref([]),
    loaded: Vue.ref(true),
    loading: Vue.ref(false),
    loadingKind: Vue.ref(null),
    error: Vue.ref(''),
    errorKind: Vue.ref(null),
    dirty: Vue.ref(false),
    hasMore: Vue.ref(true),
    nextCursor: Vue.ref('cursor'),
    windowMode: Vue.ref('latest'),
    atCapacity: Vue.ref(false),
    markDirty() {},
    refreshLatest: async () => true,
    retry: async () => true,
  }
  let olderLoads = 0
  activity.loadOlder = async () => {
    olderLoads++
    if (olderLoads === 1) activity.atCapacity.value = true
    else activity.hasMore.value = false
    return true
  }
  activity.continueOlder = async () => {
    activity.atCapacity.value = false
    activity.hasMore.value = true
    activity.windowMode.value = 'older'
    return true
  }
  activity.backToLatest = async () => {
    activity.hasMore.value = false
    activity.windowMode.value = 'latest'
    return true
  }

  const exports = {}
  const icons = {
    Activity: () => null,
    CheckCircle2: () => null,
    ChevronRight: () => null,
    Download: () => null,
    Eye: () => null,
    FileClock: () => null,
    RefreshCw: () => null,
    Settings2: () => null,
    Subtitles: () => null,
  }
  const modules = {
    vue: Vue,
    'lucide-vue-next': icons,
    '@/composables/useMediaActivity': { useMediaActivity: () => activity },
    '@/utils/mediaActivity': {
      activityAbsoluteTime: () => '',
      activityActionLabel: () => '',
      activityDetailLines: () => [],
      activityRelativeTime: () => '',
      activityTargetSummary: () => '',
    },
  }
  new Function('require', 'exports', outputText)((name) => {
    assert.ok(name in modules, `unexpected component import: ${name}`)
    return modules[name]
  }, exports)

  const root = node('root')
  const app = renderer.createApp({ setup: () => () => Vue.h(exports.default, { mediaItemId: 1, active: true }) })
  app.mount(root)
  t.after(() => app.unmount())

  const disclosure = findButton(root, 'Activity log')
  assert.ok(disclosure)
  await disclosure.props.onClick()
  await Vue.nextTick()

  await findButton(root, 'Load older').props.onClick()
  await Vue.nextTick()
  assert.equal(findButton(root, 'Continue with older activity').focused, true)

  await findButton(root, 'Continue with older activity').props.onClick()
  await Vue.nextTick()
  assert.equal(findButton(root, 'Load older').focused, true)

  await findButton(root, 'Load older').props.onClick()
  await Vue.nextTick()
  assert.equal(findButton(root, 'Back to latest').focused, true)

  await findButton(root, 'Back to latest').props.onClick()
  await Vue.nextTick()
  assert.equal(disclosure.focused, true)
})
