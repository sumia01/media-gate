<script setup lang="ts">
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import client from '@/api/client'
import type { MatchCandidate, TorrentResult } from '@/types/api'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import { formatSize } from '@/utils/media'

const props = defineProps<{
  profileId: number
  profileName: string
}>()

const emit = defineEmits<{
  close: []
}>()

// --- State ---
const step = ref<1 | 2 | 3>(1)
const mediaType = ref<'movie' | 'series'>('movie')
const query = ref('')
const candidates = ref<MatchCandidate[]>([])
const searching = ref(false)
const hasSearched = ref(false)

const selectedTitle = ref('')
const seasonInput = ref('1')

const results = ref<TorrentResult[]>([])
const totalResults = ref(0)
const filteredCount = ref(0)
const testLoading = ref(false)
const error = ref('')

const searchInput = ref<HTMLInputElement | null>(null)

let debounceTimer: ReturnType<typeof setTimeout> | null = null

// --- Lifecycle ---
onMounted(() => {
  nextTick(() => searchInput.value?.focus())
  document.addEventListener('keydown', onKeydown)
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

// --- Step 1: Media search ---
function onQueryInput() {
  if (debounceTimer) clearTimeout(debounceTimer)
  if (!query.value.trim()) {
    candidates.value = []
    hasSearched.value = false
    return
  }
  debounceTimer = setTimeout(() => searchMedia(query.value.trim()), 300)
}

function onMediaTypeChange(type: 'movie' | 'series') {
  mediaType.value = type
  if (query.value.trim()) {
    if (debounceTimer) clearTimeout(debounceTimer)
    searchMedia(query.value.trim())
  }
}

async function searchMedia(q: string) {
  searching.value = true
  hasSearched.value = true
  const { data, error: err } = await client.GET('/search', {
    params: { query: { query: q, mediaType: mediaType.value } },
  })
  searching.value = false
  if (err) {
    error.value = 'Search failed'
    return
  }
  candidates.value = data?.candidates ?? []
}

function selectCandidate(candidate: MatchCandidate) {
  selectedTitle.value = candidate.title
  if (mediaType.value === 'series') {
    step.value = 2
  } else {
    runTestSearch()
  }
}

// --- Step 2: Season selection ---
function submitSeason() {
  runTestSearch()
}

// --- Step 3: Test search ---
async function runTestSearch() {
  testLoading.value = true
  error.value = ''
  results.value = []
  step.value = 3

  const queryParams: { query: string; mediaType: 'movie' | 'series'; season?: string } = {
    query: selectedTitle.value,
    mediaType: mediaType.value,
  }
  if (mediaType.value === 'series' && seasonInput.value) {
    queryParams.season = seasonInput.value
  }

  const { data, error: err } = await client.GET('/media-profiles/{id}/test-search', {
    params: {
      path: { id: props.profileId },
      query: queryParams,
    },
  })
  testLoading.value = false
  if (err) {
    error.value = 'Test search failed'
    return
  }
  results.value = data?.results ?? []
  totalResults.value = data?.totalResults ?? 0
  filteredCount.value = data?.filteredCount ?? 0
}

function goBack() {
  if (step.value === 3 && mediaType.value === 'series') {
    step.value = 2
  } else {
    step.value = 1
    results.value = []
    error.value = ''
  }
}

function goToStart() {
  step.value = 1
  results.value = []
  error.value = ''
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
        Test Profile &mdash; {{ profileName }}
      </h2>
      <button
        class="text-gray-500 hover:text-gray-300 text-lg transition-colors"
        @click="emit('close')"
      >
        &#x2715;
      </button>
    </div>

    <!-- Step 1: Search media -->
    <template v-if="step === 1">
      <div class="flex items-center gap-3 px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 mb-4">
        <!-- Media type toggle -->
        <div class="flex-shrink-0 flex rounded-lg overflow-hidden border border-gray-700/50">
          <button
            class="px-2.5 py-1 text-xs font-medium transition-colors duration-150"
            :class="mediaType === 'movie'
              ? 'bg-violet-600 text-white'
              : 'bg-transparent text-gray-400 hover:text-gray-200'"
            @click="onMediaTypeChange('movie')"
          >
            Movies
          </button>
          <button
            class="px-2.5 py-1 text-xs font-medium transition-colors duration-150"
            :class="mediaType === 'series'
              ? 'bg-violet-600 text-white'
              : 'bg-transparent text-gray-400 hover:text-gray-200'"
            @click="onMediaTypeChange('series')"
          >
            Series
          </button>
        </div>

        <input
          ref="searchInput"
          v-model="query"
          type="text"
          :placeholder="mediaType === 'movie' ? 'Search for movies...' : 'Search for series...'"
          class="flex-1 px-3 py-1.5 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          @input="onQueryInput"
        />
      </div>

      <ErrorBanner :message="error" />

      <div class="max-h-[28rem] overflow-y-auto">
        <!-- Loading -->
        <div v-if="searching" class="flex items-center justify-center py-8">
          <span class="text-gray-500 text-sm animate-pulse">Searching...</span>
        </div>

        <!-- Empty prompt -->
        <div v-else-if="!hasSearched" class="px-4 py-8 text-center text-gray-600 text-sm">
          Search for a {{ mediaType === 'movie' ? 'movie' : 'series' }} to test against this profile
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
            @click="selectCandidate(candidate)"
          >
            <div class="w-11 h-16 flex-shrink-0 rounded overflow-hidden bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
              <img
                v-if="candidate.posterUrl"
                :src="candidate.posterUrl"
                :alt="candidate.title"
                class="w-full h-full object-cover"
                @error="($event.target as HTMLImageElement).style.display = 'none'"
              />
            </div>
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2">
                <p class="text-sm font-medium text-gray-200 truncate">{{ candidate.title }}</p>
                <span v-if="candidate.year" class="text-xs text-gray-500 flex-shrink-0">{{ candidate.year }}</span>
              </div>
              <p v-if="candidate.overview" class="text-xs text-gray-500 mt-1 line-clamp-2 leading-relaxed">
                {{ candidate.overview }}
              </p>
            </div>
            <div class="flex-shrink-0 self-center text-gray-600">&#x203A;</div>
          </div>
        </div>
      </div>
    </template>

    <!-- Step 2: Season selection (series only) -->
    <template v-if="step === 2">
      <div class="px-4 py-6">
        <p class="text-sm text-gray-400 mb-1">Selected:</p>
        <p class="text-base font-medium text-gray-200 mb-6">{{ selectedTitle }}</p>

        <label class="block text-xs font-medium text-gray-400 mb-1.5">Season number</label>
        <div class="flex items-center gap-3">
          <input
            v-model="seasonInput"
            type="number"
            min="1"
            class="w-20 px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 text-center focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
          <button
            class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
            @click="submitSeason"
          >
            Search Indexers
          </button>
        </div>

        <button
          class="mt-4 text-xs text-gray-500 hover:text-gray-300 transition-colors"
          @click="goToStart"
        >
          &larr; Back to search
        </button>
      </div>
    </template>

    <!-- Step 3: Results -->
    <template v-if="step === 3">
      <ErrorBanner :message="error" />

      <!-- Loading -->
      <div v-if="testLoading" class="flex items-center justify-center py-12">
        <span class="text-gray-500 text-sm animate-pulse">Searching indexers...</span>
      </div>

      <template v-else>
        <!-- Summary banner -->
        <div class="flex items-center justify-between px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 mb-4">
          <div class="text-sm text-gray-300">
            <span class="font-semibold text-emerald-400">{{ filteredCount }}</span>
            <span class="text-gray-500"> of </span>
            <span class="font-semibold text-gray-300">{{ totalResults }}</span>
            <span class="text-gray-500"> results match </span>
            <span class="font-semibold text-violet-300">{{ profileName }}</span>
          </div>
          <button
            class="text-xs text-gray-500 hover:text-gray-300 transition-colors"
            @click="goBack"
          >
            &larr; Back
          </button>
        </div>

        <!-- Empty state -->
        <div v-if="!results.length" class="flex flex-col items-center justify-center py-12 text-gray-500">
          <p class="text-sm">No indexer results match this profile's criteria.</p>
          <button
            class="mt-3 text-xs text-violet-400 hover:text-violet-300 transition-colors"
            @click="goToStart"
          >
            Try a different search
          </button>
        </div>

        <!-- Results table -->
        <div v-else class="overflow-auto min-h-0">
          <!-- Auto-grab legend -->
          <div class="flex items-center gap-1.5 mb-3 text-xs text-gray-500">
            <span class="inline-block w-3 h-3 rounded-sm bg-emerald-500/40"></span>
            <span>Auto-grab pick (highest seeders)</span>
          </div>

          <table class="w-full text-sm">
            <thead class="sticky top-0 bg-[#0f1225]">
              <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-500 border-b border-violet-900/20">
                <th class="px-3 py-2.5">Title</th>
                <th class="px-3 py-2.5 whitespace-nowrap">Size</th>
                <th class="px-3 py-2.5 text-center whitespace-nowrap">S</th>
                <th class="px-3 py-2.5 text-center whitespace-nowrap">L</th>
                <th class="px-3 py-2.5 whitespace-nowrap">Indexer</th>
                <th class="px-3 py-2.5 whitespace-nowrap">Category</th>
                <th class="px-3 py-2.5 whitespace-nowrap">Date</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(result, idx) in results"
                :key="idx"
                :class="[
                  'border-b border-violet-900/10 hover:bg-violet-600/5 transition-colors duration-200',
                  idx === 0 ? 'bg-emerald-500/15' : '',
                ]"
              >
                <!-- Title -->
                <td class="px-3 py-2.5">
                  <div class="flex items-center gap-2">
                    <span
                      v-if="idx === 0"
                      class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300 whitespace-nowrap flex-shrink-0"
                    >
                      Auto-grab
                    </span>
                    <a
                      v-if="result.detailsUrl"
                      :href="result.detailsUrl"
                      target="_blank"
                      rel="noopener"
                      class="text-gray-200 hover:text-violet-300 transition-colors duration-200 truncate max-w-lg"
                      :title="result.title"
                    >
                      {{ result.title }}
                    </a>
                    <span v-else class="text-gray-200 truncate max-w-lg" :title="result.title">
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

                <!-- Indexer -->
                <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
                  {{ result.indexerName }}
                </td>

                <!-- Category -->
                <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
                  {{ result.categoryDesc || result.category || '' }}
                </td>

                <!-- Date -->
                <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
                  {{ formatDate(result.date) }}
                </td>
              </tr>
            </tbody>
          </table>

          <p class="mt-3 text-xs text-gray-600">{{ results.length }} results</p>
        </div>
      </template>
    </template>
  </BaseModal>
</template>
