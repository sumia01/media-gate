import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as Vue from 'vue'
import * as releaseKeywords from '../releaseKeywords.ts'

const { descriptor } = parse(
  readFileSync(new URL('../../components/media/MonitorSettingsModal.vue', import.meta.url), 'utf8'),
)
const compiled = compileScript(descriptor, { id: 'monitor-settings-test' })
const { outputText } = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
})
const exports = {}
new Function('require', 'exports', outputText)((name) => {
  if (name === 'vue') return Vue
  if (name === '@/components/BaseModal.vue') return { default: {} }
  if (name === '@/components/media/ReleaseKeywordsInput.vue') return { default: {} }
  if (name === '@/utils/releaseKeywords') return releaseKeywords
  assert.fail(`unexpected import: ${name}`)
}, exports)

function setup(item) {
  const events = []
  const state = exports.default.setup(
    { item: { monitored: true, ...item }, profiles: [] },
    {
      expose() {},
      emit: (...args) => events.push(args),
    },
  )
  return { state, events }
}

test('release mode defaults on and preserves an explicitly saved false', () => {
  for (const [saved, expected] of [
    [undefined, true],
    [true, true],
    [false, false],
  ]) {
    const { state, events } = setup({ mediaType: 'movie', forceRelease: saved })
    assert.equal(state.forceRelease.value, expected)
    state.preferredRelease.value = ' ETHEL, FLUX '
    state.onSave()
    assert.equal(events[0][0], 'save')
    assert.equal(events[0][1].forceRelease, expected)
    assert.equal(events[0][1].preferredRelease, 'ethel, flux')
  }
})

test('empty keyword lists hide the mode controls without resetting the saved mode', () => {
  const { state, events } = setup({ mediaType: 'movie', forceRelease: false, preferredRelease: 'ETHEL' })
  assert.equal(state.preferredRelease.value, 'ethel')
  assert.equal(state.hasReleaseKeywords.value, true)
  for (const value of ['', ' ', ', , ']) {
    state.preferredRelease.value = value
    assert.equal(state.hasReleaseKeywords.value, false)
    assert.equal(state.releaseLabel.value, 'Release keywords')
    state.onSave()
    assert.equal(events.at(-1)[1].forceRelease, false)
    assert.equal(events.at(-1)[1].preferredRelease, '')
  }
  state.preferredRelease.value = 'flux'
  assert.equal(state.hasReleaseKeywords.value, true)
  assert.equal(state.releaseLabel.value, 'Preferred releases')
})

test('toggle edits and blank keywords survive the monitored series next step', () => {
  const { state, events } = setup({ mediaType: 'series', preferredRelease: 'ETHEL' })
  state.forceRelease.value = false
  state.preferredRelease.value = ' '
  state.onSave()
  assert.equal(events[0][0], 'next')
  assert.equal(events[0][1].forceRelease, false)
  assert.equal(events[0][1].preferredRelease, '')
})

test('short modals scroll their settings while keeping the heading and actions outside the scroller', () => {
  const elements = (node) => (node.children ?? []).filter((child) => child.type === 1)
  const classes = (node) => node.props.find((prop) => prop.name === 'class')?.value?.content.split(/\s+/) ?? []
  const find = (node, predicate) => {
    if (predicate(node)) return node
    for (const child of elements(node)) {
      const match = find(child, predicate)
      if (match) return match
    }
  }
  const modal = elements(descriptor.template.ast).find((node) => node.tag === 'BaseModal')
  const children = elements(modal)
  const scroller = children.find((node) => classes(node).includes('overflow-y-auto'))
  assert.ok(scroller, 'settings need their own vertically scrollable body')
  assert.ok(classes(scroller).includes('min-h-0'), 'body must shrink below its content height')
  assert.ok(
    find(scroller, (node) => node.tag === 'ReleaseKeywordsInput'),
    'release keywords belong in the scrollable body',
  )
  assert.ok(
    find(scroller, (node) => node.props.some((prop) => prop.name === 'role' && prop.value?.content === 'switch')),
  )
  const modeRegion = find(scroller, (node) => node.props.some((prop) => prop.arg?.content === 'inert'))
  assert.ok(
    modeRegion.props.some((prop) => prop.arg?.content === 'inert' && prop.exp?.content === '!hasReleaseKeywords'),
  )
  assert.ok(modeRegion.props.some((prop) => prop.arg?.content === 'class' && prop.exp?.content.includes('invisible')))
  assert.equal(
    modeRegion.props.some((prop) => prop.name === 'if'),
    false,
    'hidden controls reserve their layout space',
  )

  const heading = children.find((node) => node.tag === 'h3')
  const title = children.find((node) => node.tag === 'p')
  const actions = children.at(-1)
  for (const fixed of [heading, title, actions]) {
    assert.ok(classes(fixed).includes('flex-shrink-0'), 'heading, title and footer must not collapse')
  }
  assert.notEqual(actions, scroller)
  assert.equal(elements(actions).filter((node) => node.tag === 'button').length, 2)
})
