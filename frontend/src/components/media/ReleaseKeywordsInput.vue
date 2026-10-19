<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import client from '@/api/client'
import { insertReleaseKeyword, releaseKeywordPopupPosition, releaseKeywordToken } from '@/utils/releaseKeywords'

const props = defineProps<{ id: string; modelValue: string; scrollContainer?: HTMLElement | null }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const input = ref<HTMLInputElement | null>(null)
const list = ref<HTMLUListElement | null>(null)
const popupPosition = ref<ReturnType<typeof releaseKeywordPopupPosition>>(null)
const caret = ref(props.modelValue.length)
const focused = ref(false)
const dismissed = ref(false)
const suggestions = ref<string[]>([])
const active = ref(-1)
const loading = ref(false)
const failed = ref(false)
const token = computed(() => releaseKeywordToken(props.modelValue, caret.value))
const listId = computed(() => `${props.id}-suggestions`)
const eligible = computed(() => token.value.prefix !== '' && [...token.value.prefix].length <= 256)
const expanded = computed(() => focused.value && !dismissed.value && eligible.value)
const status = computed(() => {
  if (loading.value) return 'Searching saved keywords…'
  if (failed.value) return 'Suggestions unavailable. You can still enter keywords.'
  return suggestions.value.length ? `${suggestions.value.length} saved keywords found.` : 'No saved keywords match.'
})

function updatePopupPosition() {
  if (!expanded.value || !input.value) {
    popupPosition.value = null
    return
  }
  const anchor = input.value.getBoundingClientRect()
  const bounds = props.scrollContainer?.getBoundingClientRect()
  if (bounds && (anchor.bottom <= bounds.top || anchor.top >= bounds.bottom)) {
    popupPosition.value = null
    return
  }
  const viewport = window.visualViewport
  popupPosition.value = releaseKeywordPopupPosition(
    anchor,
    {
      top: viewport?.offsetTop ?? 0,
      left: viewport?.offsetLeft ?? 0,
      width: viewport?.width ?? window.innerWidth,
      height: viewport?.height ?? window.innerHeight,
    },
    suggestions.value.length,
  )
}

watch([expanded, () => suggestions.value.length, loading, failed], updatePopupPosition, { flush: 'post' })

watch(
  [() => props.scrollContainer, input],
  ([container, field], _previous, onCleanup) => {
    if (!field) return
    const observer = new ResizeObserver(updatePopupPosition)
    if (container) observer.observe(container)
    observer.observe(field)
    window.addEventListener('resize', updatePopupPosition)
    window.addEventListener('scroll', updatePopupPosition, true)
    const viewport = window.visualViewport
    viewport?.addEventListener('resize', updatePopupPosition)
    viewport?.addEventListener('scroll', updatePopupPosition)
    onCleanup(() => {
      observer.disconnect()
      window.removeEventListener('resize', updatePopupPosition)
      window.removeEventListener('scroll', updatePopupPosition, true)
      viewport?.removeEventListener('resize', updatePopupPosition)
      viewport?.removeEventListener('scroll', updatePopupPosition)
    })
    updatePopupPosition()
  },
  { flush: 'post', immediate: true },
)

watch(
  [token, focused, dismissed],
  ([current, hasFocus, isDismissed], _previous, onCleanup) => {
    active.value = -1
    failed.value = false
    if (!hasFocus || isDismissed || !eligible.value) {
      suggestions.value = []
      loading.value = false
      return
    }

    // Keep the existing popup steady during debounce/fetch, but do not allow
    // stale options to be selected for the newly typed token.
    loading.value = true
    const controller = new AbortController()
    const timeout = setTimeout(async () => {
      try {
        const { data, error } = await client.GET('/release-keywords', {
          params: { query: { prefix: current.prefix } },
          signal: controller.signal,
        })
        if (controller.signal.aborted) return
        if (error || !data) {
          suggestions.value = []
          failed.value = true
        } else {
          suggestions.value = data.keywords.filter((keyword) => !current.selected.includes(keyword))
          if (list.value) list.value.scrollTop = 0
        }
      } catch {
        if (!controller.signal.aborted) {
          suggestions.value = []
          failed.value = true
        }
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
  if (loading.value || !popupPosition.value || !suggestions.value.includes(keyword)) return
  const result = insertReleaseKeyword(props.modelValue, caret.value, keyword)
  dismissed.value = true
  emit('update:modelValue', result.value)
  caret.value = result.caret
  await nextTick()
  input.value?.focus()
  input.value?.setSelectionRange(result.caret, result.caret)
}

function scrollActiveOption() {
  const popup = list.value
  if (!popup || active.value < 0) return
  const option = popup.children.item(active.value) as HTMLElement | null
  if (!option) return
  if (option.offsetTop < popup.scrollTop) popup.scrollTop = option.offsetTop
  const bottom = option.offsetTop + option.offsetHeight
  if (bottom > popup.scrollTop + popup.clientHeight) popup.scrollTop = bottom - popup.clientHeight
}

function onKeydown(event: KeyboardEvent) {
  if (event.isComposing) return
  if (event.key === 'Escape') {
    dismissed.value = true
    return
  }
  if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
    event.preventDefault()
    if (loading.value || !suggestions.value.length) {
      dismissed.value = false
      return
    }
    if (event.key === 'ArrowDown') {
      active.value = (active.value + 1) % suggestions.value.length
    } else {
      active.value = active.value <= 0 ? suggestions.value.length - 1 : active.value - 1
    }
    void nextTick(scrollActiveOption)
  } else if (event.key === 'Enter' && active.value >= 0 && expanded.value && popupPosition.value) {
    event.preventDefault()
    const keyword = suggestions.value[active.value]
    if (keyword) void choose(keyword)
  }
}
</script>

<template>
  <div>
    <input
      :id="id"
      ref="input"
      :value="modelValue"
      type="text"
      role="combobox"
      aria-autocomplete="list"
      :aria-expanded="expanded && popupPosition !== null"
      :aria-controls="listId"
      :aria-activedescendant="expanded && popupPosition && active >= 0 ? `${listId}-${active}` : undefined"
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
    <p class="sr-only" role="status">{{ focused ? status : '' }}</p>
    <Teleport to="body">
      <ul
        v-if="expanded && popupPosition"
        :id="listId"
        ref="list"
        role="listbox"
        aria-label="Previously saved release keywords"
        :aria-busy="loading"
        class="fixed z-[60] overflow-y-auto rounded-lg border border-violet-500/30 bg-[#161b2e] py-1 shadow-xl"
        :style="popupPosition"
        @mousedown.prevent
      >
        <li
          v-for="(keyword, index) in suggestions"
          :id="`${listId}-${index}`"
          :key="keyword"
          role="option"
          :aria-selected="active === index"
          :aria-disabled="loading"
          class="flex h-9 items-center px-3 text-sm text-gray-200"
          :class="[
            active === index ? 'bg-violet-600/30' : 'hover:bg-violet-600/20',
            loading ? 'cursor-wait' : 'cursor-pointer',
          ]"
          @click="choose(keyword)"
        >
          <span class="truncate" :title="keyword">{{ keyword }}</span>
        </li>
        <li v-if="!suggestions.length" class="flex h-9 items-center px-3 text-xs text-gray-500">
          <span class="truncate" :title="status">{{ status }}</span>
        </li>
      </ul>
    </Teleport>
  </div>
</template>
