<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import client from '@/api/client'
import type { Indexer, Library, TorrentResult } from '@/types/api'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import { formatSize } from '@/utils/media'
import {
  parseTitleSeasonEpisode,
  classifyMatch,
  matchLevelOrder,
  parseTorrentQuality,
  matchesProfile,
  type MatchLevel,
  type ProfileMatchInput,
} from '@/utils/torrent'

const props = defineProps<{
  mediaItemId?: number
  imdbId: string
  mediaType: 'movie' | 'series'
  title: string
  seasonNumber?: number
  episodeNumber?: number
  episodeId?: number
  mediaProfile?: ProfileMatchInput
  // For "Add & Download" flow (preview page — no mediaItemId yet)
  source?: 'tmdb' | 'tvdb'
  externalId?: number
}>()

const emit = defineEmits<{
  close: []
  added: [mediaItemId: number]
}>()

// --- State ---
const indexers = ref<Indexer[]>([])
const selectedIndexerId = ref('')
const season = ref(props.seasonNumber?.toString() ?? '')
const episode = ref(props.episodeNumber?.toString() ?? '')
const results = ref<TorrentResult[]>([])
const loading = ref(false)
const error = ref('')
const downloadingIdx = ref<Set<number>>(new Set())
const downloadedIdx = ref<Set<number>>(new Set())

// --- Add & Download flow (preview context) ---
const canAddAndDownload = computed(() => props.mediaItemId == null && props.source && props.externalId != null)
const libraries = ref<Library[]>([])
const compatibleLibraries = computed(() => libraries.value.filter(l => l.mediaType === props.mediaType))
const addDownloadIdx = ref<number | null>(null) // which result row triggered the picker
const selectedLibraryId = ref<number | null>(null)
const addingDownload = ref(false)

// --- Season/episode matching ---
const sortedResults = computed(() => {
  const userSeason = season.value ? parseInt(season.value, 10) : null
  const userEpisode = episode.value ? parseInt(episode.value, 10) : null
  const profile = props.mediaProfile

  const classify = (r: TorrentResult, i: number) => ({
    result: r,
    matchLevel: (props.mediaType === 'series' && userSeason !== null)
      ? classifyMatch(parseTitleSeasonEpisode(r.title), userSeason, userEpisode)
      : 'none' as MatchLevel,
    profileMatch: profile ? matchesProfile(parseTorrentQuality(r.title), profile) : false,
    originalIndex: i,
  })

  const classified = results.value.map(classify)

  if (props.mediaType === 'series' && userSeason !== null) {
    classified.sort((a, b) => {
      const diff = matchLevelOrder(a.matchLevel) - matchLevelOrder(b.matchLevel)
      return diff !== 0 ? diff : a.originalIndex - b.originalIndex
    })
  }

  return classified
})

function matchRowClass(level: MatchLevel): string {
  switch (level) {
    case 'full': return 'bg-emerald-500/15'
    case 'season': return 'bg-amber-500/15'
    default: return ''
  }
}

const hasMatches = computed(() =>
  sortedResults.value.some((i) => i.matchLevel !== 'none')
)

const hasProfileMatches = computed(() =>
  sortedResults.value.some((i) => i.profileMatch)
)

// --- Lifecycle ---
onMounted(async () => {
  document.addEventListener('keydown', onKeydown)
  const fetches: Promise<void>[] = [fetchIndexers()]
  if (canAddAndDownload.value) {
    fetches.push(fetchLibraries())
  }
  await Promise.all(fetches)
  search()
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

// --- Fetch indexers ---
async function fetchIndexers() {
  const { data } = await client.GET('/indexers')
  indexers.value = (data?.indexers ?? []).filter((i) => i.enabled)
}

// --- Fetch libraries (for Add & Download) ---
async function fetchLibraries() {
  const { data } = await client.GET('/libraries')
  libraries.value = data ?? []
  if (compatibleLibraries.value.length === 1) {
    selectedLibraryId.value = compatibleLibraries.value[0]!.id
  }
}

// --- Search ---
async function search() {
  loading.value = true
  error.value = ''
  results.value = []

  const searchType = props.mediaType === 'movie' ? 'movie-search' : 'tv-search'

  const { data, error: err } = await client.GET('/indexers/search', {
    params: {
      query: {
        query: props.title,
        imdbId: props.imdbId,
        type: searchType,
        indexerIds: selectedIndexerId.value || undefined,
        season: season.value || undefined,
        episode: episode.value || undefined,
        limit: 100,
      } as any,
    },
  })
  loading.value = false
  if (err) {
    error.value = 'Search failed'
    return
  }
  results.value = data?.results ?? []
}

// --- Download ---
async function download(result: TorrentResult, idx: number) {
  if (downloadingIdx.value.has(idx) || downloadedIdx.value.has(idx)) return

  downloadingIdx.value = new Set([...downloadingIdx.value, idx])

  const { error: err } = await client.POST('/downloads', {
    body: {
      mediaItemId: props.mediaItemId!,
      episodeId: props.episodeId,
      seasonNumber: props.seasonNumber,
      indexerId: result.indexerId,
      indexerName: result.indexerName,
      title: result.title,
      downloadUrl: result.downloadUrl!,
      detailsUrl: result.detailsUrl,
      size: result.size,
      imdbId: result.imdbId || props.imdbId,
    },
  })

  const next = new Set(downloadingIdx.value)
  next.delete(idx)
  downloadingIdx.value = next

  if (err) {
    error.value = 'Failed to add download'
    return
  }

  downloadedIdx.value = new Set([...downloadedIdx.value, idx])
}

// --- Add to Library & Download ---
function openAddAndDownload(idx: number) {
  addDownloadIdx.value = idx
}

async function confirmAddAndDownload() {
  if (addDownloadIdx.value == null || !selectedLibraryId.value || !props.source || props.externalId == null) return

  const result = results.value[addDownloadIdx.value]
  if (!result) return

  addingDownload.value = true
  error.value = ''

  // Step 1: Add to library (creates media item + fetches metadata)
  const { data: mediaItem, error: addErr } = await client.POST('/libraries/{id}/media', {
    params: { path: { id: selectedLibraryId.value } },
    body: {
      source: props.source,
      externalId: props.externalId,
    },
  })

  if (addErr) {
    addingDownload.value = false
    const errBody = addErr as { code?: number }
    error.value = errBody.code === 409
      ? 'This media already exists in the selected library'
      : 'Failed to add media to library'
    addDownloadIdx.value = null
    return
  }

  // Step 2: Create download
  const { error: dlErr } = await client.POST('/downloads', {
    body: {
      mediaItemId: mediaItem!.id,
      indexerId: result.indexerId,
      indexerName: result.indexerName,
      title: result.title,
      downloadUrl: result.downloadUrl!,
      detailsUrl: result.detailsUrl,
      size: result.size,
      imdbId: result.imdbId || props.imdbId,
    },
  })

  addingDownload.value = false

  if (dlErr) {
    error.value = 'Media added but failed to start download'
    addDownloadIdx.value = null
    return
  }

  emit('added', mediaItem!.id)
}

// --- Helpers ---
function formatDate(unix: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleDateString('hu-HU', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}
</script>

<template>
  <BaseModal max-width="max-w-7xl" @close="emit('close')">
    <!-- Header -->
    <div class="flex items-center justify-between mb-5">
      <h2 class="text-lg font-semibold text-gray-100">
        Search &mdash; {{ title }}
      </h2>
      <button
        class="text-gray-500 hover:text-gray-300 text-lg transition-colors"
        @click="emit('close')"
      >
        &#x2715;
      </button>
    </div>

    <!-- Filter bar -->
    <div class="flex items-center gap-3 px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 mb-4">
      <!-- Indexer dropdown -->
      <select
        v-model="selectedIndexerId"
        class="px-3 py-1.5 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
      >
        <option value="">All indexers</option>
        <option v-for="idx in indexers" :key="idx.id" :value="String(idx.id)">
          {{ idx.name }}
        </option>
      </select>

      <!-- Season input -->
      <div v-if="mediaType === 'series'" class="flex items-center gap-1.5">
        <label class="text-xs text-gray-500">S</label>
        <input
          v-model="season"
          type="text"
          placeholder="--"
          class="w-12 px-2 py-1.5 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 text-center focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
        />
      </div>

      <!-- Episode input -->
      <div v-if="mediaType === 'series'" class="flex items-center gap-1.5">
        <label class="text-xs text-gray-500">E</label>
        <input
          v-model="episode"
          type="text"
          placeholder="--"
          class="w-12 px-2 py-1.5 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 text-center focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
        />
      </div>

      <!-- Search button -->
      <button
        class="px-4 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
        :disabled="loading"
        @click="search"
      >
        {{ loading ? 'Searching...' : 'Search' }}
      </button>
    </div>

    <ErrorBanner :message="error" />

    <!-- Match legend -->
    <div v-if="hasMatches || hasProfileMatches" class="flex items-center gap-4 mb-3 text-xs text-gray-500">
      <div v-if="hasMatches" class="flex items-center gap-1.5">
        <span class="inline-block w-3 h-3 rounded-sm bg-emerald-500/40"></span>
        <span>Season + Episode match</span>
      </div>
      <div v-if="hasMatches" class="flex items-center gap-1.5">
        <span class="inline-block w-3 h-3 rounded-sm bg-amber-500/40"></span>
        <span>Season match</span>
      </div>
      <div v-if="hasProfileMatches" class="flex items-center gap-1.5">
        <svg class="w-3 h-3 text-amber-400" viewBox="0 0 20 20" fill="currentColor">
          <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.07 3.292a1 1 0 00.95.69h3.462c.969 0 1.371 1.24.588 1.81l-2.8 2.034a1 1 0 00-.364 1.118l1.07 3.292c.3.921-.755 1.688-1.54 1.118l-2.8-2.034a1 1 0 00-1.175 0l-2.8 2.034c-.784.57-1.838-.197-1.539-1.118l1.07-3.292a1 1 0 00-.364-1.118L2.98 8.72c-.783-.57-.38-1.81.588-1.81h3.461a1 1 0 00.951-.69l1.07-3.292z" />
        </svg>
        <span>Profile match</span>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <span class="text-gray-500 text-sm animate-pulse">Searching indexers...</span>
    </div>

    <!-- No results -->
    <div
      v-else-if="!results.length"
      class="flex flex-col items-center justify-center py-12 text-gray-500"
    >
      <p class="text-sm">No results found.</p>
    </div>

    <!-- Results table (desktop) -->
    <div v-else class="overflow-auto min-h-0">
      <table class="hidden md:table w-full text-sm">
        <thead class="sticky top-0 bg-[#0f1225]">
          <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-500 border-b border-violet-900/20">
            <th v-if="props.mediaProfile" class="w-8 px-0 py-2.5"></th>
            <th class="px-3 py-2.5">Title</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Size</th>
            <th class="px-3 py-2.5 text-center whitespace-nowrap">S</th>
            <th class="px-3 py-2.5 text-center whitespace-nowrap">L</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Indexer</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Category</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Date</th>
            <th class="px-3 py-2.5"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="item in sortedResults"
            :key="item.originalIndex"
            :class="[
              'border-b border-violet-900/10 hover:bg-violet-600/5 transition-colors duration-200',
              matchRowClass(item.matchLevel),
            ]"
          >
            <!-- Profile match star -->
            <td v-if="props.mediaProfile" class="w-8 px-0 py-2.5 text-center">
              <svg
                v-if="item.profileMatch"
                class="w-4 h-4 inline-block text-amber-400"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.07 3.292a1 1 0 00.95.69h3.462c.969 0 1.371 1.24.588 1.81l-2.8 2.034a1 1 0 00-.364 1.118l1.07 3.292c.3.921-.755 1.688-1.54 1.118l-2.8-2.034a1 1 0 00-1.175 0l-2.8 2.034c-.784.57-1.838-.197-1.539-1.118l1.07-3.292a1 1 0 00-.364-1.118L2.98 8.72c-.783-.57-.38-1.81.588-1.81h3.461a1 1 0 00.951-.69l1.07-3.292z" />
              </svg>
            </td>

            <!-- Title -->
            <td class="px-3 py-2.5">
              <div class="flex items-center gap-2">
                <a
                  v-if="item.result.detailsUrl"
                  :href="item.result.detailsUrl"
                  target="_blank"
                  rel="noopener"
                  class="text-gray-200 hover:text-violet-300 transition-colors duration-200 truncate max-w-md"
                  :title="item.result.title"
                >
                  {{ item.result.title }}
                </a>
                <span v-else class="text-gray-200 truncate max-w-md" :title="item.result.title">
                  {{ item.result.title }}
                </span>
              </div>
            </td>

            <!-- Size -->
            <td class="px-3 py-2.5 text-gray-400 whitespace-nowrap">
              {{ formatSize(item.result.size) }}
            </td>

            <!-- Seeders -->
            <td class="px-3 py-2.5 text-center text-green-400">
              {{ item.result.seeders }}
            </td>

            <!-- Leechers -->
            <td class="px-3 py-2.5 text-center text-red-400">
              {{ item.result.leechers }}
            </td>

            <!-- Indexer -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ item.result.indexerName }}
            </td>

            <!-- Category -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ item.result.categoryDesc || item.result.category || '' }}
            </td>

            <!-- Date -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ formatDate(item.result.date) }}
            </td>

            <!-- Actions -->
            <td class="px-3 py-2.5">
              <div class="flex items-center gap-1">
                <a
                  v-if="item.result.detailsUrl"
                  :href="item.result.detailsUrl"
                  target="_blank"
                  rel="noopener"
                  class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
                  title="Open on tracker"
                >
                  Open
                </a>
                <template v-if="props.mediaItemId != null">
                  <button
                    v-if="downloadedIdx.has(item.originalIndex)"
                    class="px-2.5 py-1.5 rounded-md text-xs text-emerald-400"
                    disabled
                  >
                    Added
                  </button>
                  <button
                    v-else
                    class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-emerald-300 hover:bg-emerald-600/10 transition-colors duration-200"
                    :disabled="downloadingIdx.has(item.originalIndex)"
                    @click="download(item.result, item.originalIndex)"
                  >
                    {{ downloadingIdx.has(item.originalIndex) ? 'Adding...' : 'Download' }}
                  </button>
                </template>
                <button
                  v-if="canAddAndDownload"
                  class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-emerald-300 hover:bg-emerald-600/10 transition-colors duration-200"
                  @click="openAddAndDownload(item.originalIndex)"
                >
                  Add &amp; Download
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Results cards (mobile) -->
      <div class="md:hidden space-y-2">
        <div
          v-for="item in sortedResults"
          :key="'m-' + item.originalIndex"
          :class="[
            'px-3 py-2.5 rounded-lg border border-violet-900/20',
            matchRowClass(item.matchLevel) || 'bg-[#161b2e]',
          ]"
        >
          <!-- Row 1: Title -->
          <div class="flex items-center gap-1.5 mb-1.5">
            <svg
              v-if="props.mediaProfile && item.profileMatch"
              class="w-3.5 h-3.5 flex-shrink-0 text-amber-400"
              viewBox="0 0 20 20"
              fill="currentColor"
            >
              <path d="M9.049 2.927c.3-.921 1.603-.921 1.902 0l1.07 3.292a1 1 0 00.95.69h3.462c.969 0 1.371 1.24.588 1.81l-2.8 2.034a1 1 0 00-.364 1.118l1.07 3.292c.3.921-.755 1.688-1.54 1.118l-2.8-2.034a1 1 0 00-1.175 0l-2.8 2.034c-.784.57-1.838-.197-1.539-1.118l1.07-3.292a1 1 0 00-.364-1.118L2.98 8.72c-.783-.57-.38-1.81.588-1.81h3.461a1 1 0 00.951-.69l1.07-3.292z" />
            </svg>
            <span class="text-xs text-gray-200 truncate" :title="item.result.title">{{ item.result.title }}</span>
          </div>
          <!-- Row 2: Size, S, L, Indexer, Open, Download -->
          <div class="flex items-center gap-2.5 text-[11px]">
            <span class="text-gray-400">{{ formatSize(item.result.size) }}</span>
            <span class="text-green-400">S:{{ item.result.seeders }}</span>
            <span class="text-red-400">L:{{ item.result.leechers }}</span>
            <span class="text-gray-500 truncate">{{ item.result.indexerName }}</span>
            <span class="ml-auto flex items-center gap-1.5">
              <a
                v-if="item.result.detailsUrl"
                :href="item.result.detailsUrl"
                target="_blank"
                rel="noopener"
                class="text-gray-400 hover:text-violet-300 transition-colors"
                title="Open on tracker"
              >
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14" /></svg>
              </a>
              <template v-if="props.mediaItemId != null">
                <button
                  v-if="downloadedIdx.has(item.originalIndex)"
                  class="text-emerald-400"
                  disabled
                >
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" /></svg>
                </button>
                <button
                  v-else
                  class="text-gray-400 hover:text-emerald-300 transition-colors"
                  :disabled="downloadingIdx.has(item.originalIndex)"
                  @click="download(item.result, item.originalIndex)"
                >
                  <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4" /></svg>
                </button>
              </template>
              <button
                v-if="canAddAndDownload"
                class="text-gray-400 hover:text-emerald-300 transition-colors"
                title="Add to Library &amp; Download"
                @click="openAddAndDownload(item.originalIndex)"
              >
                <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" /></svg>
              </button>
            </span>
          </div>
        </div>
      </div>

      <p class="mt-3 text-xs text-gray-600">{{ results.length }} results</p>
    </div>

    <!-- Library picker overlay (Add & Download flow) -->
    <div v-if="addDownloadIdx != null" class="absolute inset-0 z-10 flex items-center justify-center bg-black/60 rounded-xl">
      <div class="bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 shadow-2xl w-full max-w-sm">
        <h3 class="text-base font-semibold text-gray-100 mb-4">Add to Library &amp; Download</h3>

        <div v-if="!compatibleLibraries.length" class="py-4 text-center text-gray-500 text-sm">
          No {{ mediaType }} libraries found. Create one first.
        </div>

        <template v-else>
          <div class="space-y-2 mb-4">
            <button
              v-for="lib in compatibleLibraries"
              :key="lib.id"
              class="w-full text-left px-4 py-3 rounded-lg border transition-colors duration-200"
              :class="selectedLibraryId === lib.id
                ? 'bg-violet-600/10 border-violet-500/40'
                : 'bg-[#161b2e] border-violet-900/20 hover:border-violet-500/30'"
              @click="selectedLibraryId = lib.id"
            >
              <div class="flex items-center justify-between">
                <div>
                  <p class="text-sm font-medium text-gray-200">{{ lib.name }}</p>
                  <p class="text-xs text-gray-500 mt-0.5 font-mono">{{ lib.path }}</p>
                </div>
                <div
                  v-if="selectedLibraryId === lib.id"
                  class="w-4 h-4 rounded-full bg-violet-600 flex items-center justify-center flex-shrink-0"
                >
                  <span class="text-white text-xs">&#10003;</span>
                </div>
              </div>
            </button>
          </div>
        </template>

        <div class="flex gap-3">
          <button
            class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="!selectedLibraryId || addingDownload"
            @click="confirmAddAndDownload"
          >
            {{ addingDownload ? 'Adding...' : 'Add & Download' }}
          </button>
          <button
            class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
            :disabled="addingDownload"
            @click="addDownloadIdx = null"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  </BaseModal>
</template>
