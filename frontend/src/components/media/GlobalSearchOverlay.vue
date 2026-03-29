<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useGlobalSearch } from '@/composables/useGlobalSearch'

type MatchCandidate = components['schemas']['MatchCandidate']

const router = useRouter()
const { searchMediaType, closeSearch } = useGlobalSearch()

const query = ref('')
const candidates = ref<MatchCandidate[]>([])
const searching = ref(false)
const searchError = ref('')
const searchInput = ref<HTMLInputElement | null>(null)
const hasSearched = ref(false)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

onMounted(() => {
  nextTick(() => searchInput.value?.focus())
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    closeSearch()
  }
}

watch(query, (val) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!val.trim()) {
    candidates.value = []
    hasSearched.value = false
    return
  }
  debounceTimer = setTimeout(() => search(val.trim()), 300)
})

// Re-search when media type toggles
watch(searchMediaType, () => {
  if (query.value.trim()) {
    if (debounceTimer) clearTimeout(debounceTimer)
    search(query.value.trim())
  }
})

async function search(q: string) {
  searching.value = true
  searchError.value = ''
  hasSearched.value = true
  const { data, error: err } = await client.GET('/search', {
    params: { query: { query: q, mediaType: searchMediaType.value } },
  })
  searching.value = false
  if (err) {
    searchError.value = 'Search failed'
    return
  }
  candidates.value = data?.candidates ?? []
}

function navigateToPreview(candidate: MatchCandidate) {
  closeSearch()
  router.push({
    name: 'media-preview',
    params: { source: candidate.source, externalId: candidate.externalId },
    query: { mediaType: searchMediaType.value },
  })
}

function handleBackdropClick(e: MouseEvent) {
  if (e.target === e.currentTarget) {
    closeSearch()
  }
}
</script>

<template>
  <!-- Backdrop -->
  <div class="fixed inset-0 bg-black/60 z-40" @click="handleBackdropClick">
    <!-- Dropdown panel -->
    <div class="fixed top-16 left-1/2 -translate-x-1/2 w-full max-w-2xl z-50" @click.stop>
      <div class="bg-[#0f1219] border border-violet-900/30 rounded-xl shadow-2xl shadow-black/50 overflow-hidden">
        <!-- Media type toggle + search input -->
        <div class="flex items-center gap-3 px-4 py-3 border-b border-gray-800/60">
          <div class="flex-shrink-0 flex rounded-lg overflow-hidden border border-gray-700/50">
            <button
              class="px-2.5 py-1 text-xs font-medium transition-colors duration-150"
              :class="searchMediaType === 'movie'
                ? 'bg-violet-600 text-white'
                : 'bg-transparent text-gray-400 hover:text-gray-200'"
              @click="searchMediaType = 'movie'"
            >
              Movies
            </button>
            <button
              class="px-2.5 py-1 text-xs font-medium transition-colors duration-150"
              :class="searchMediaType === 'series'
                ? 'bg-violet-600 text-white'
                : 'bg-transparent text-gray-400 hover:text-gray-200'"
              @click="searchMediaType = 'series'"
            >
              Series
            </button>
          </div>

          <span class="text-gray-500 text-lg">&#128269;</span>
          <input
            ref="searchInput"
            v-model="query"
            type="text"
            :placeholder="searchMediaType === 'movie' ? 'Search for movies...' : 'Search for series...'"
            class="flex-1 bg-transparent text-gray-100 text-sm placeholder-gray-600 outline-none"
          />
          <button
            class="text-gray-500 hover:text-gray-300 text-lg transition-colors"
            @click="closeSearch()"
          >
            &#x2715;
          </button>
        </div>

        <!-- Results area -->
        <div class="max-h-[28rem] overflow-y-auto">
          <!-- Loading -->
          <div v-if="searching" class="flex items-center justify-center py-8">
            <span class="text-gray-500 text-sm animate-pulse">Searching...</span>
          </div>

          <!-- Error -->
          <div v-else-if="searchError" class="px-4 py-6 text-center text-red-400 text-sm">
            {{ searchError }}
          </div>

          <!-- Empty prompt -->
          <div v-else-if="!hasSearched" class="px-4 py-8 text-center text-gray-600 text-sm">
            Start typing to search for {{ searchMediaType === 'movie' ? 'movies' : 'series' }}...
          </div>

          <!-- No results -->
          <div v-else-if="!candidates.length" class="px-4 py-8 text-center text-gray-500 text-sm">
            No results found for "{{ query }}"
          </div>

          <!-- Results -->
          <div v-else>
            <div
              v-for="candidate in candidates"
              :key="`${candidate.source}-${candidate.externalId}`"
              class="flex items-start gap-3 px-4 py-3 border-b border-gray-800/40 last:border-0 hover:bg-violet-900/10 transition-colors duration-150 cursor-pointer"
              @click="navigateToPreview(candidate)"
            >
              <!-- Poster thumbnail -->
              <div class="w-11 h-16 flex-shrink-0 rounded overflow-hidden bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
                <img
                  v-if="candidate.posterUrl"
                  :src="candidate.posterUrl"
                  :alt="candidate.title"
                  class="w-full h-full object-cover"
                  @error="($event.target as HTMLImageElement).style.display = 'none'"
                />
                <div v-else class="w-full h-full flex items-center justify-center">
                  <span class="text-gray-600 text-xs">{{ searchMediaType === 'movie' ? '&#127910;' : '&#128250;' }}</span>
                </div>
              </div>

              <!-- Info -->
              <div class="flex-1 min-w-0">
                <div class="flex items-center gap-2">
                  <p class="text-sm font-medium text-gray-200 truncate">{{ candidate.title }}</p>
                  <span v-if="candidate.year" class="text-xs text-gray-500 flex-shrink-0">{{ candidate.year }}</span>
                </div>
                <p v-if="candidate.overview" class="text-xs text-gray-500 mt-1 line-clamp-2 leading-relaxed">
                  {{ candidate.overview }}
                </p>
              </div>

              <!-- Arrow icon -->
              <div class="flex-shrink-0 self-center text-gray-600">
                &#x203A;
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
