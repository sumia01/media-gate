<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import client from '@/api/client'
import { insertReleaseKeyword, releaseKeywordToken } from '@/utils/releaseKeywords'

const props = defineProps<{ id: string; modelValue: string; scrollContainer?: HTMLElement | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const control = ref<HTMLDivElement | null>(null)
const input = ref<HTMLInputElement | null>(null)
const list = ref<HTMLUListElement | null>(null)
const listMaxHeight = ref<string>()
const caret = ref(props.modelValue.length)
const focused = ref(false)
const dismissed = ref(false)
const suggestions = ref<string[]>([])
const active = ref(-1)
const loading = ref(false)
const failed = ref(false)
const token = computed(() => releaseKeywordToken(props.modelValue, caret.value))
const listId = computed(() => `${props.id}-suggestions`)
const expanded = computed(() => focused.value && !dismissed.value && suggestions.value.length > 0)

async function revealSuggestions() {
  await nextTick()
  const container = props.scrollContainer
  if (!expanded.value || !container || !input.value || !list.value) return
  const inputBounds = input.value.getBoundingClientRect()
  const gap = Math.max(0, list.value.getBoundingClientRect().top - inputBounds.bottom)
  const available = Math.max(0, Math.floor(container.getBoundingClientRect().height - inputBounds.height - gap))
  listMaxHeight.value = `min(9rem, ${available}px)`
  await nextTick()
  // Reveal the input and its fitted list together, never the list alone.
  if (expanded.value) control.value?.scrollIntoView({ block: 'nearest' })
}

watch(expanded, (isExpanded) => {
  if (isExpanded) void revealSuggestions()
})

watch(
  [() => props.scrollContainer, input],
  ([container, field], _previous, onCleanup) => {
    if (!container || !field) return
    const observer = new ResizeObserver(() => {
      if (expanded.value) void revealSuggestions()
    })
    observer.observe(container)
    observer.observe(field)
    onCleanup(() => observer.disconnect())
    if (expanded.value) void revealSuggestions()
  },
  { flush: 'post', immediate: true },
)

watch(
  [token, focused, dismissed],
  ([current, hasFocus, isDismissed], _previous, onCleanup) => {
    suggestions.value = []
    active.value = -1
    loading.value = false
    failed.value = false
    if (!hasFocus || isDismissed || !current.prefix || [...current.prefix].length > 256) return

    const controller = new AbortController()
    const timeout = setTimeout(async () => {
      loading.value = true
      try {
        const { data, error } = await client.GET('/release-keywords', {
          params: { query: { prefix: current.prefix } },
          signal: controller.signal,
        })
        if (controller.signal.aborted) return
        if (error || !data) {
          failed.value = true
        } else {
          suggestions.value = data.keywords.filter((keyword) => !current.selected.includes(keyword))
        }
      } catch {
        if (!controller.signal.aborted) failed.value = true
      } finally {
        if (!controller.signal.aborted) loading.value = false
      }
    }, 150)
    onCleanup(() => {
      clearTimeout(timeout)
      controller.abort()
    })
  },
  { flush: 'sync' },
)

function updateCaret(event: Event) {
  caret.value = (event.target as HTMLInputElement).selectionStart ?? props.modelValue.length
}

function onInput(event: Event) {
  if ((event as InputEvent).isComposing) return
  const target = event.target as HTMLInputElement
  const value = target.value
  const start = value.slice(0, target.selectionStart ?? value.length).toLowerCase().length
  const end = value.slice(0, target.selectionEnd ?? value.length).toLowerCase().length
  const lower = value.toLowerCase()
  if (value !== lower) {
    target.value = lower
    target.setSelectionRange(start, end)
  }
  caret.value = start
  dismissed.value = false
  emit('update:modelValue', lower)
}

async function choose(keyword: string) {
  const result = insertReleaseKeyword(props.modelValue, caret.value, keyword)
  dismissed.value = true
  emit('update:modelValue', result.value)
  caret.value = result.caret
  await nextTick()
  input.value?.focus()
  input.value?.setSelectionRange(result.caret, result.caret)
}

function onKeydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (event.key === 'Escape') {
    dismissed.value = true
    return
  }
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (!suggestions.value.length) {
      dismissed.value = false
      return
    }
    if (event.key === 'ArrowDown') {
      active.value = (active.value + 1) % suggestions.value.length
    } else {
      active.value = active.value <= 0 ? suggestions.value.length - 1 : active.value - 1
    }
    void nextTick(() =>
      document.getElementById(`${listId.value}-${active.value}`)?.scrollIntoView({ block: 'nearest' }),
    )
  } else if (event.key === 'Enter' && active.value >= 0 && expanded.value) {
    event.preventDefault()
    const keyword = suggestions.value[active.value]
    if (keyword) void choose(keyword)
  }
}
</script>

<template>
  <div ref="control">
    <input
      :id="id"
      ref="input"
      :value="modelValue"
      type="text"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="expanded"
      :aria-controls="listId"
      :aria-activedescendant="expanded && active >= 0 ? `${listId}-${active}` : undefined"
      autocomplete="off"
      autocapitalize="none"
      :spellcheck="false"
      placeholder="e.g. ethel, flux"
      class="w-full text-sm bg-[#161b2e] border border-violet-900/20 rounded-lg px-3 py-2 text-gray-200 placeholder-gray-600 focus:outline-none focus:border-violet-500/50"
      @input="onInput"
      @compositionend="onInput"
      @focus="focused = true; updateCaret($event)"
      @blur="focused = false"
      @click="updateCaret"
      @keyup="updateCaret"
      @select="updateCaret"
      @keydown="onKeydown"
    />
    <ul
      v-if="expanded"
      :id="listId"
      ref="list"
      role="listbox"
      aria-label="Previously saved release keywords"
      class="mt-1 max-h-36 overflow-y-auto rounded-lg border border-violet-500/30 bg-[#161b2e] py-1"
      :style="{ maxHeight: listMaxHeight }"
    >
      <li
        v-for="(keyword, index) in suggestions"
        :id="`${listId}-${index}`"
        :key="keyword"
        role="option"
        :aria-selected="active === index"
        class="cursor-pointer px-3 py-2 text-sm text-gray-200"
        :class="active === index ? 'bg-violet-600/30' : 'hover:bg-violet-600/20'"
        @mousedown.prevent
        @click="choose(keyword)"
      >
        {{ keyword }}
      </li>
    </ul>
    <p v-if="loading || failed" class="mt-1 text-xs text-gray-500" role="status">
      {{ failed ? 'Suggestions unavailable. You can still enter keywords.' : 'Loading suggestions…' }}
    </p>
  </div>
</template>
