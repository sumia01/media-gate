import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { setImmediate } from 'node:timers/promises'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'
import * as keywords from '../releaseKeywords.ts'

const { descriptor } = parse(
  readFileSync(new URL('../../components/media/ReleaseKeywordsInput.vue', import.meta.url), 'utf8'),
)
const compiled = compileScript(descriptor, { id: 'release-keywords-input-test' })
const { outputText } = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})
const renderer = Vue.createRenderer({
  createComment: () => ({}),
  insert() {},
  remove() {},
  parentNode: () => null,
  nextSibling: () => null,
})

async function settle() {
  await setImmediate()
  await Vue.nextTick()
}

function mountInput(t, value = '', initialBodyHeight = 600) {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const oldWindow = globalThis.window
  const oldResizeObserver = globalThis.ResizeObserver
  const scrolls = []
  const observers = []
  const listeners = new Map()
  const bodyHeight = initialBodyHeight
  globalThis.window = {
    innerWidth: 600,
    innerHeight: 320,
    addEventListener(type, callback) {
      if (!listeners.has(type)) listeners.set(type, new Set())
      listeners.get(type).add(callback)
    },
    removeEventListener(type, callback) {
      listeners.get(type)?.delete(callback)
    },
  }
  globalThis.ResizeObserver = class {
    disconnected = false
    constructor(callback) {
      this.callback = callback
      observers.push(this)
    }
    observe() {}
    disconnect() {
      this.disconnected = true
    }
  }
  const requests = []
  const exports = {}
  const api = {
    GET: (path, options) => new Promise((resolve, reject) => requests.push({ path, ...options, resolve, reject })),
  }
  new Function('require', 'exports', outputText)((name) => {
    if (name === 'vue') return Vue
    if (name === '@/api/client') return { default: api }
    if (name === '@/utils/releaseKeywords') return keywords
    assert.fail(`unexpected import: ${name}`)
  }, exports)
  const props = Vue.reactive({
    id: 'release-keywords',
    modelValue: value,
    scrollContainer: { getBoundingClientRect: () => ({ top: 0, bottom: bodyHeight, height: bodyHeight }) },
  })
  let state
  const app = renderer.createApp({
    setup() {
      state = exports.default.setup(props, {
        expose() {},
        emit(_name, value) {
          props.modelValue = value
        },
      })
      return () => null
    },
  })
  app.mount({})
  state.input.value = {
    getBoundingClientRect: () => ({ top: 50, height: 38, bottom: 88, left: 20, width: 300 }),
    scrollIntoView: () => scrolls.push('input'),
    focus() {},
    setSelectionRange() {},
  }
  state.list.value = {
    children: { item: (index) => ({ offsetTop: 4 + index * 36, offsetHeight: 36 }) },
    scrollTop: 0,
    clientHeight: 152,
    scrollIntoView: () => scrolls.push('list'),
  }
  t.after(() => {
    app.unmount()
    globalThis.window = oldWindow
    globalThis.ResizeObserver = oldResizeObserver
  })
  return {
    state,
    props,
    requests,
    scrolls,
    observers,
    listeners,
    unmount: () => app.unmount(),
    async resize(height) {
      window.innerHeight = height
      for (const observer of observers) {
        if (!observer.disconnected) observer.callback()
      }
      await settle()
    },
    type(value, caret = value.length) {
      const target = {
        value,
        selectionStart: caret,
        selectionEnd: caret,
        setSelectionRange(start, end) {
          this.selectionStart = start
          this.selectionEnd = end
        },
      }
      state.onInput({ target })
      return target
    },
    async fetch() {
      t.mock.timers.tick(150)
      await settle()
    },
  }
}

test('input lowercases immediately, preserves the caret, and debounces prefix requests', async (t) => {
  const view = mountInput(t)
  view.state.focused.value = true
  await view.fetch()
  assert.equal(view.requests.length, 0, 'empty input does not query suggestions')
  const target = view.type('EtHeL', 2)
  assert.equal(target.value, 'ethel')
  assert.equal(target.selectionStart, 2)
  assert.equal(view.props.modelValue, 'ethel')
  t.mock.timers.tick(100)
  view.type('ETh')
  t.mock.timers.tick(149)
  assert.equal(view.requests.length, 0)
  t.mock.timers.tick(1)
  await settle()
  assert.equal(view.requests.length, 1)
  assert.equal(view.requests[0].params.query.prefix, 'eth')
})

test('keyboard selection inserts a suggestion without submitting and replaces a middle token', async (t) => {
  const view = mountInput(t, 'et, flux')
  view.state.caret.value = 2
  view.state.focused.value = true
  await view.fetch()
  view.requests[0].resolve({ data: { keywords: ['ethel'] } })
  await settle()
  assert.deepEqual(view.scrolls, [], 'opening suggestions must not scroll the modal or input')
  let prevented = 0
  view.state.onKeydown({
    key: 'ArrowDown',
    preventDefault() {
      prevented++
    },
  })
  await settle()
  assert.equal(view.state.active.value, 0)
  view.state.onKeydown({
    key: 'Enter',
    preventDefault() {
      prevented++
    },
  })
  await settle()
  assert.equal(prevented, 2)
  assert.equal(view.props.modelValue, 'ethel, flux')
  assert.equal(view.state.expanded.value, false)
})

test('arrows preserve the active token before debounce and while suggestions are loading', async (t) => {
  const view = mountInput(t, 'eth, flux')
  view.state.caret.value = 3
  view.state.focused.value = true
  for (const key of ['ArrowDown', 'ArrowUp']) {
    let prevented = false
    view.state.onKeydown({
      key,
      preventDefault() {
        prevented = true
      },
    })
    assert.equal(prevented, true, `${key} must not move the native input caret during debounce`)
  }
  await view.fetch()
  assert.equal(view.requests[0].params.query.prefix, 'eth')
  for (const key of ['ArrowDown', 'ArrowUp']) {
    let prevented = false
    view.state.onKeydown({
      key,
      preventDefault() {
        prevented = true
      },
    })
    assert.equal(prevented, true, `${key} must not move the native input caret during the request`)
  }
  assert.equal(view.state.caret.value, 3)
  assert.equal(view.requests[0].signal.aborted, false)
  view.requests[0].resolve({ data: { keywords: ['ethel'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, ['ethel'])
})

test('many suggestions float outside the scroll body and resize without scrolling or leaking listeners', async (t) => {
  const view = mountInput(t, 'e', 132)
  view.state.focused.value = true
  await view.fetch()
  view.requests[0].resolve({
    data: { keywords: ['ebp', 'encounters', 'enigma', 'epsilon', 'escape', 'ethel', 'evo', 'eztv'] },
  })
  await settle()
  assert.equal(view.state.popupPosition.value.maxHeight, '154px', 'four rows need not fit inside the 132px modal body')
  assert.equal(view.state.popupPosition.value.top, '92px')
  assert.deepEqual(view.scrolls, [], 'neither the input nor an ancestor should be scrolled')
  await view.resize(180)
  assert.equal(view.state.popupPosition.value.maxHeight, '80px')
  await view.resize(600)
  assert.equal(view.state.popupPosition.value.maxHeight, '154px')
  for (let i = 0; i < 8; i++) view.state.onKeydown({ key: 'ArrowDown', preventDefault() {} })
  await settle()
  assert.ok(view.state.list.value.scrollTop > 0, 'keyboard navigation scrolls only the popup')
  assert.deepEqual(view.scrolls, [])
  view.unmount()
  assert.ok(view.observers.length > 0)
  assert.ok(view.observers.every((observer) => observer.disconnected))
  assert.ok([...view.listeners.values()].every((callbacks) => callbacks.size === 0))
})

test('pending queries keep the popup steady but cannot select stale options', async (t) => {
  const view = mountInput(t, 'e')
  view.state.focused.value = true
  await view.fetch()
  view.requests[0].resolve({ data: { keywords: ['ethel'] } })
  await settle()
  view.state.list.value.scrollTop = 100
  view.type('fl')
  assert.equal(view.state.expanded.value, true)
  assert.equal(view.state.loading.value, true)
  assert.deepEqual(view.state.suggestions.value, ['ethel'], 'old rows remain visible during debounce/fetch')
  await view.state.choose('ethel')
  assert.equal(view.props.modelValue, 'fl', 'a stale row must not be selectable')
  await view.fetch()
  view.requests[1].resolve({ data: { keywords: ['flux'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, ['flux'])
  assert.equal(view.state.loading.value, false)
  assert.equal(view.state.list.value.scrollTop, 0, 'new ranked results start at the best match')
})

test('the popup and loading/status content cannot change the input normal-flow height', () => {
  const root = descriptor.template.ast.children.find((node) => node.type === 1)
  const children = root.children.filter((node) => node.type === 1)
  assert.ok(
    children.some((node) => node.tag === 'Teleport'),
    'popup is teleported out of the modal',
  )
  assert.equal(
    children.some((node) => node.tag === 'ul'),
    false,
  )
  const status = children.find((node) => node.tag === 'p')
  assert.ok(status.props.some((prop) => prop.name === 'class' && prop.value?.content.includes('sr-only')))
})

test('already selected names are omitted, pointer selection works, and Escape dismisses', async (t) => {
  const view = mountInput(t, 'ethel, e')
  view.state.focused.value = true
  await view.fetch()
  view.requests[0].resolve({ data: { keywords: ['ethel', 'ethell'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, ['ethell'])
  await view.state.choose('ethell')
  assert.equal(view.props.modelValue, 'ethel, ethell, ')
  view.type('ethel, f')
  await view.fetch()
  view.requests.at(-1).resolve({ data: { keywords: ['flux'] } })
  await settle()
  assert.equal(view.state.expanded.value, true)
  view.state.onKeydown({ key: 'Escape' })
  assert.equal(view.state.expanded.value, false)
})

test('changed prefixes and blur abort old requests and reject late responses', async (t) => {
  const view = mountInput(t)
  view.state.focused.value = true
  view.type('e')
  await view.fetch()
  const old = view.requests[0]
  view.type('f')
  assert.equal(old.signal.aborted, true)
  await view.fetch()
  view.requests[1].resolve({ data: { keywords: ['flux'] } })
  await settle()
  old.resolve({ data: { keywords: ['ethel'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, ['flux'])
  view.type('g')
  await view.fetch()
  const pending = view.requests.at(-1)
  view.state.focused.value = false
  assert.equal(pending.signal.aborted, true)
  pending.resolve({ data: { keywords: ['group'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, [])
})

test('failure never blocks manual input and unmount cancels timers and requests', async (t) => {
  const view = mountInput(t)
  view.state.focused.value = true
  view.type('ETHEL')
  await view.fetch()
  view.requests[0].reject(new Error('offline'))
  await settle()
  assert.equal(view.state.failed.value, true)
  assert.equal(view.props.modelValue, 'ethel')
  view.type('FLUX')
  await view.fetch()
  const pending = view.requests.at(-1)
  view.unmount()
  assert.equal(pending.signal.aborted, true)
  pending.resolve({ data: { keywords: ['flux'] } })
  await settle()
  assert.deepEqual(view.state.suggestions.value, [])
})
