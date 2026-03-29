<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, nextTick } from 'vue'
import client from '@/api/client'
import type { MatchCandidate, TorrentResult } from '@/types/api'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'

const props = defineProps<{
  indexerId: number
  indexerName: string
}>()

const emit = defineEmits<{
  close: []
}>()

// --- Step state ---
const step = ref<'search' | 'results'>('search')

// --- Step 1: Meta search ---
const searchMediaType = ref<'movie' | 'series'>('movie')
const query = ref('')
const candidates = ref<MatchCandidate[]>([])
const searching = ref(false)
const searchError = ref('')
const hasSearched = ref(false)
const searchInput = ref<HTMLInputElement | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

// --- Step 2: Indexer results ---
const selectedTitle = ref('')
const results = ref<TorrentResult[]>([])
const loadingResults = ref(false)
const resultsError = ref('')
const fetchingDetail = ref(false)

// --- Lifecycle ---
onMounted(() => {
  nextTick(() => searchInput.value?.focus())
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
  if (debounceTimer) clearTimeout(debounceTimer)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

// --- Meta search ---
watch(query, (val) => {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!val.trim()) {
    candidates.value = []
    hasSearched.value = false
    return
  }
  debounceTimer = setTimeout(() => searchMeta(val.trim()), 300)
})

watch(searchMediaType, () => {
  if (query.value.trim()) {
    if (debounceTimer) clearTimeout(debounceTimer)
    searchMeta(query.value.trim())
  }
})

async function searchMeta(q: string) {
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

// --- Candidate selection → indexer search ---
async function selectCandidate(candidate: MatchCandidate) {
  fetchingDetail.value = true
  searchError.value = ''
  selectedTitle.value = `${candidate.title}${candidate.year ? ` (${candidate.year})` : ''}`

  // Fetch detail to get IMDb ID
  const { data, error: err } = await client.GET('/search/{source}/{externalId}', {
    params: {
      path: {
        source: candidate.source as 'tmdb' | 'tvdb',
        externalId: candidate.externalId!,
      },
      query: { mediaType: searchMediaType.value },
    },
  })
  fetchingDetail.value = false

  if (err || !data) {
    searchError.value = 'Failed to load details for this title'
    return
  }
  if (!data.imdbId) {
    searchError.value = 'No IMDb ID available for this title. Try another result.'
    return
  }

  await searchIndexer(data.imdbId)
}

async function searchIndexer(imdbId: string) {
  step.value = 'results'
  loadingResults.value = true
  resultsError.value = ''

  const searchType = searchMediaType.value === 'movie' ? 'movie-search' : 'tv-search'

  const { data, error: err } = await client.GET('/indexers/search', {
    params: {
      query: {
        imdbId,
        type: searchType,
        indexerIds: String(props.indexerId),
        limit: 100,
      } as any,
    },
  })
  loadingResults.value = false
  if (err) {
    resultsError.value = 'Indexer search failed'
    return
  }
  results.value = data?.results ?? []
}

function backToSearch() {
  step.value = 'search'
  results.value = []
  resultsError.value = ''
}

function dummyDownload(title: string) {
  alert(`Download added: ${title}`)
}

// --- Helpers ---
function formatSize(size: string): string {
  const bytes = parseFloat(size)
  if (isNaN(bytes) || bytes === 0) return size
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
}

function formatDate(unix: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleDateString('hu-HU', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function volumeLabel(dl: number | undefined, ul: number | undefined): string {
  const parts: string[] = []
  if (dl !== undefined && dl !== 1) {
    if (dl === 0) parts.push('Freeleech')
    else parts.push(`DL: ${dl}x`)
  }
  if (ul !== undefined && ul !== 1) {
    parts.push(`UL: ${ul}x`)
  }
  return parts.join(' / ')
}
</script>

<template>
  <BaseModal max-width="max-w-7xl" @close="emit('close')">
    <!-- Header -->
    <div class="flex items-center justify-between mb-5">
      <h2 class="text-lg font-semibold text-gray-100">
        Try it out &mdash; {{ indexerName }}
      </h2>
      <button
        class="text-gray-500 hover:text-gray-300 text-lg transition-colors"
        @click="emit('close')"
      >
        &#x2715;
      </button>
    </div>

    <!-- Step 1: Meta search -->
    <div v-if="step === 'search'">
      <!-- Search bar -->
      <div class="flex items-center gap-3 px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 mb-4">
        <!-- Media type toggle -->
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

        <input
          ref="searchInput"
          v-model="query"
          type="text"
          :placeholder="searchMediaType === 'movie' ? 'Search for movies...' : 'Search for series...'"
          class="flex-1 bg-transparent text-gray-100 text-sm placeholder-gray-600 outline-none"
        />
      </div>

      <ErrorBanner :message="searchError" />

      <!-- Candidate results area -->
      <div class="rounded-lg">
        <!-- Loading -->
        <div v-if="searching || fetchingDetail" class="flex items-center justify-center py-8">
          <span class="text-gray-500 text-sm animate-pulse">
            {{ fetchingDetail ? 'Loading details...' : 'Searching...' }}
          </span>
        </div>

        <!-- Empty prompt -->
        <div v-else-if="!hasSearched" class="px-4 py-8 text-center text-gray-600 text-sm">
          Start typing to search for {{ searchMediaType === 'movie' ? 'movies' : 'series' }}...
        </div>

        <!-- No results -->
        <div v-else-if="!candidates.length" class="px-4 py-8 text-center text-gray-500 text-sm">
          No results found for "{{ query }}"
        </div>

        <!-- Candidates -->
        <div v-else>
          <div
            v-for="candidate in candidates"
            :key="`${candidate.source}-${candidate.externalId}`"
            class="flex items-start gap-3 px-4 py-3 border-b border-gray-800/40 last:border-0 hover:bg-violet-900/10 transition-colors duration-150 cursor-pointer rounded-lg"
            @click="selectCandidate(candidate)"
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

            <!-- Arrow -->
            <div class="flex-shrink-0 self-center text-gray-600">&#x203A;</div>
          </div>
        </div>
      </div>
    </div>

    <!-- Step 2: Indexer results -->
    <div v-else>
      <!-- Back button + selected title -->
      <div class="flex items-center gap-3 mb-4">
        <button
          class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-violet-300 transition-colors duration-200"
          @click="backToSearch"
        >
          <span class="text-base leading-none">&larr;</span>
          Back
        </button>
        <span class="text-sm text-gray-500">|</span>
        <span class="text-sm text-gray-300">{{ selectedTitle }}</span>
      </div>

      <ErrorBanner :message="resultsError" />

      <!-- Loading -->
      <div v-if="loadingResults" class="flex items-center justify-center py-12">
        <span class="text-gray-500 text-sm animate-pulse">Searching {{ indexerName }}...</span>
      </div>

      <!-- No results -->
      <div
        v-else-if="!results.length"
        class="flex flex-col items-center justify-center py-12 text-gray-500"
      >
        <p class="text-sm">No results found on {{ indexerName }}.</p>
      </div>

      <!-- Results table -->
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm">
          <thead class="sticky top-0 bg-[#0f1225]">
            <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-500 border-b border-violet-900/20">
              <th class="px-3 py-2.5">Title</th>
              <th class="px-3 py-2.5 whitespace-nowrap">Size</th>
              <th class="px-3 py-2.5 text-center whitespace-nowrap">S</th>
              <th class="px-3 py-2.5 text-center whitespace-nowrap">L</th>
              <th class="px-3 py-2.5 whitespace-nowrap">Category</th>
              <th class="px-3 py-2.5 whitespace-nowrap">Date</th>
              <th class="px-3 py-2.5"></th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(result, idx) in results"
              :key="idx"
              class="border-b border-violet-900/10 hover:bg-violet-600/5 transition-colors duration-200"
            >
              <!-- Title -->
              <td class="px-3 py-2.5">
                <div class="flex items-center gap-2">
                  <a
                    v-if="result.detailsUrl"
                    :href="result.detailsUrl"
                    target="_blank"
                    rel="noopener"
                    class="text-gray-200 hover:text-violet-300 transition-colors duration-200 truncate max-w-md"
                    :title="result.title"
                  >
                    {{ result.title }}
                  </a>
                  <span v-else class="text-gray-200 truncate max-w-md" :title="result.title">
                    {{ result.title }}
                  </span>
                  <span
                    v-if="volumeLabel(result.downloadVolumeFactor, result.uploadVolumeFactor)"
                    class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300 whitespace-nowrap flex-shrink-0"
                  >
                    {{ volumeLabel(result.downloadVolumeFactor, result.uploadVolumeFactor) }}
                  </span>
                </div>
              </td>

              <!-- Size -->
              <td class="px-3 py-2.5 text-gray-400 whitespace-nowrap">
                {{ formatSize(result.size) }}
              </td>

              <!-- Seeders -->
              <td class="px-3 py-2.5 text-center text-green-400">
                {{ result.seeders }}
              </td>

              <!-- Leechers -->
              <td class="px-3 py-2.5 text-center text-red-400">
                {{ result.leechers }}
              </td>

              <!-- Category -->
              <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
                {{ result.categoryDesc || result.category || '' }}
              </td>

              <!-- Date -->
              <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
                {{ formatDate(result.date) }}
              </td>

              <!-- Actions -->
              <td class="px-3 py-2.5">
                <div class="flex items-center gap-1">
                  <a
                    v-if="result.detailsUrl"
                    :href="result.detailsUrl"
                    target="_blank"
                    rel="noopener"
                    class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
                    title="Open on tracker"
                  >
                    Open
                  </a>
                  <button
                    class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-emerald-300 hover:bg-emerald-600/10 transition-colors duration-200"
                    @click="dummyDownload(result.title)"
                  >
                    Download
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>

        <p class="mt-3 text-xs text-gray-600">{{ results.length }} results</p>
      </div>
    </div>
  </BaseModal>
</template>
