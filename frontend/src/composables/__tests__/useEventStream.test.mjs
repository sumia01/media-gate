import assert from 'node:assert/strict'
import { registerHooks } from 'node:module'
import { test } from 'node:test'
import { createRenderer, onUnmounted } from 'vue'

const hooks = registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === './useAuth') {
      return {
        shortCircuit: true,
        url: 'data:text/javascript,export const useAuth = () => ({ getAccessToken: () => null })',
      }
    }
    return nextResolve(specifier, context)
  },
})
const { useEventStream } = await import('../useEventStream.ts')
hooks.deregister()

const renderer = createRenderer({
  createComment: () => ({}),
  insert() {},
  remove() {},
  parentNode: () => null,
  nextSibling: () => null,
})

test('removing the last callback removes its native EventSource listener', (t) => {
  const original = globalThis.EventSource
  const sources = []
  globalThis.EventSource = class {
    listeners = new Map()
    closed = false
    constructor(url) {
      this.url = url
      sources.push(this)
    }
    addEventListener(type, callback) {
      const callbacks = this.listeners.get(type) ?? new Set()
      callbacks.add(callback)
      this.listeners.set(type, callbacks)
    }
    removeEventListener(type, callback) {
      this.listeners.get(type)?.delete(callback)
    }
    close() {
      this.closed = true
    }
    dispatch(type, data) {
      for (const callback of this.listeners.get(type) ?? []) callback({ data: JSON.stringify(data) })
    }
  }
  t.after(() => {
    if (original === undefined) delete globalThis.EventSource
    else globalThis.EventSource = original
  })

  let stream
  let calls = 0
  const callback = () => calls++
  const app = renderer.createApp({
    setup() {
      stream = useEventStream()
      stream.on('media.activity_added', callback)
      onUnmounted(() => stream.off('media.activity_added', callback))
      return () => null
    },
  })
  app.mount({})
  assert.equal(sources.length, 1)
  assert.equal(sources[0].listeners.get('media.activity_added').size, 1)
  stream.off('media.activity_added', callback)
  assert.equal(sources[0].listeners.get('media.activity_added').size, 0)

  stream.on('media.activity_added', callback)
  assert.equal(sources[0].listeners.get('media.activity_added').size, 1)
  sources[0].dispatch('media.activity_added', { payload: { mediaItemId: 1 } })
  assert.equal(calls, 1, 're-registering does not retain a duplicate native callback')
  app.unmount()
  assert.equal(sources[0].closed, true)
})
