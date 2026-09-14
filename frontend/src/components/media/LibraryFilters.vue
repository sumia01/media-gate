<script setup lang="ts">
import { Search, SlidersHorizontal, X } from 'lucide-vue-next'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'

const props = defineProps<{
  genres: string[]
}>()

const query = defineModel<string>('query', { required: true })
const selectedGenres = defineModel<string[]>('selectedGenres', { required: true })

const root = ref<HTMLElement | null>(null)
const genresButton = ref<HTMLButtonElement | null>(null)
const showGenres = ref(false)
const selectedGenreSet = computed(() => new Set(selectedGenres.value))

function toggleGenre(genre: string) {
  selectedGenres.value = selectedGenreSet.value.has(genre)
    ? selectedGenres.value.filter((selected) => selected !== genre)
    : [...selectedGenres.value, genre]
}

function closeGenres(restoreFocus = false) {
  if (!showGenres.value) return
  showGenres.value = false
  if (restoreFocus) nextTick(() => genresButton.value?.focus())
}

function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !showGenres.value) return
  event.preventDefault()
  closeGenres(true)
}

function onFocusIn(event: FocusEvent) {
  if (showGenres.value && event.target instanceof Node && !root.value?.contains(event.target)) closeGenres()
}

watch(
  () => props.genres,
  (genres) => {
    const available = new Set(genres)
    const validSelections = selectedGenres.value.filter((genre) => available.has(genre))
    if (validSelections.length !== selectedGenres.value.length) selectedGenres.value = validSelections
  },
)

onMounted(() => {
  document.addEventListener('keydown', onKeydown)
  document.addEventListener('focusin', onFocusIn)
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  document.removeEventListener('focusin', onFocusIn)
})
</script>

<template>
  <div ref="root" class="flex w-full min-w-0 items-center gap-2 md:w-72 lg:w-80 xl:w-96">
    <div class="relative min-w-0 flex-1">
      <Search class="pointer-events-none absolute left-3 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-gray-600" />
      <input
        v-model="query"
        type="search"
        aria-label="Filter library by title"
        autocomplete="off"
        placeholder="Filter titles..."
        class="w-full rounded-lg border border-gray-800/80 bg-[#111522] py-2 pl-9 pr-8 text-sm text-gray-300 outline-none placeholder:text-gray-700 hover:border-gray-700 focus:border-gray-600 focus:bg-[#141827] transition-colors duration-200"
      />
      <button
        v-if="query"
        type="button"
        aria-label="Clear title filter"
        class="absolute inset-y-0 right-0 flex w-8 items-center justify-center text-gray-600 hover:text-gray-300 transition-colors"
        @click="query = ''"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>

    <div class="relative shrink-0">
      <button
        ref="genresButton"
        type="button"
        :aria-expanded="showGenres"
        aria-controls="library-genre-filter"
        aria-label="Filter library by genre"
        class="flex h-9 items-center gap-2 rounded-lg border px-3 text-sm transition-colors duration-200"
        :class="selectedGenres.length
          ? 'border-violet-700/40 bg-violet-950/30 text-violet-300 hover:border-violet-600/60'
          : 'border-gray-800/80 bg-[#111522] text-gray-500 hover:border-gray-700 hover:text-gray-300'"
        @click="showGenres = !showGenres"
      >
        <SlidersHorizontal class="h-3.5 w-3.5" />
        <span class="hidden sm:inline">Genres</span>
        <span
          v-if="selectedGenres.length"
          class="rounded-full bg-violet-500/20 px-1.5 text-[10px] font-semibold text-violet-200"
        >
          {{ selectedGenres.length }}
        </span>
      </button>

      <div v-if="showGenres" class="fixed inset-0 z-20" @click="closeGenres(true)" />
      <div
        v-if="showGenres"
        id="library-genre-filter"
        role="group"
        aria-label="Genre filters"
        class="absolute right-0 top-full z-30 mt-2 w-64 max-w-[calc(100vw-2rem)] overflow-hidden rounded-xl border border-gray-800 bg-[#111522] shadow-2xl shadow-black/50"
      >
        <div class="flex items-start justify-between gap-4 border-b border-gray-800 px-4 py-3">
          <div>
            <p class="text-sm font-medium text-gray-200">Genres</p>
            <p class="mt-0.5 text-[11px] text-gray-600">Match any selected genre</p>
          </div>
          <button
            v-if="selectedGenres.length"
            type="button"
            class="text-xs text-gray-500 hover:text-gray-300 transition-colors"
            @click="selectedGenres = []"
          >
            Clear
          </button>
        </div>

        <div v-if="genres.length" class="max-h-64 overflow-y-auto p-2">
          <label
            v-for="genre in genres"
            :key="genre"
            class="flex cursor-pointer items-center gap-2.5 rounded-lg px-2 py-2 text-sm text-gray-300 hover:bg-white/[0.03]"
          >
            <input
              type="checkbox"
              :checked="selectedGenreSet.has(genre)"
              class="h-3.5 w-3.5 accent-violet-500 focus-visible:outline-2 focus-visible:outline-violet-400"
              @change="toggleGenre(genre)"
            />
            <span>{{ genre }}</span>
          </label>
        </div>
        <p v-else class="px-4 py-6 text-center text-xs text-gray-600">No genres in this library yet.</p>
      </div>
    </div>
  </div>
</template>
