<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import type { Library, MediaItem, MediaFile, MediaProfile } from '@/types/api'
import { useJobQueue } from '@/composables/useJobQueue'
import { parseGenres, profileImageUrl, posterUrl, formatBytes } from '@/utils/media'
import ErrorBanner from '@/components/ErrorBanner.vue'
import MatchPanel from '@/components/media/MatchPanel.vue'
import EpisodeGrid from '@/components/media/EpisodeGrid.vue'
import IndexerSearchModal from '@/components/media/IndexerSearchModal.vue'
import DownloadList from '@/components/media/DownloadList.vue'

const route = useRoute()
const router = useRouter()
const { onJobDone } = useJobQueue()

const item = ref<MediaItem | null>(null)
const library = ref<Library | null>(null)
const files = ref<MediaFile[]>([])
const profiles = ref<MediaProfile[]>([])
const loading = ref(false)
const error = ref('')
const showMatchPanel = ref(false)
const resyncing = ref(false)
const filesExpanded = ref(false)
const showIndexerSearch = ref(false)
const indexerSearchSeason = ref<number | undefined>()
const indexerSearchEpisode = ref<number | undefined>()
const indexerSearchEpisodeId = ref<number | undefined>()
const episodeRefreshKey = ref(0)
const downloadRefreshKey = ref(0)
const replacingDownloadId = ref<number | null>(null)

const metadata = computed(() => item.value?.metadata ?? null)

const genres = computed(() => parseGenres(metadata.value?.genres))

const externalUrl = computed(() => {
  if (!metadata.value) return null
  if (metadata.value.source === 'tmdb') {
    const type = item.value?.mediaType === 'movie' ? 'movie' : 'tv'
    return `https://www.themoviedb.org/${type}/${metadata.value.externalId}`
  }
  if (metadata.value.source === 'tvdb') {
    return `https://thetvdb.com/?id=${metadata.value.externalId}&tab=series`
  }
  return null
})

const imdbUrl = computed(() => {
  if (!metadata.value?.imdbId) return null
  return `https://www.imdb.com/title/${metadata.value.imdbId}/`
})

const credits = computed(() => metadata.value?.credits ?? [])
const cast = computed(() => credits.value.filter(c => c.type === 'cast'))
const crew = computed(() => credits.value.filter(c => c.type === 'crew'))

async function fetchItem(id: number) {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/media/{id}', {
    params: { path: { id } },
  })
  loading.value = false
  if (err) {
    error.value = 'Failed to load media item'
    return
  }
  if (data) {
    item.value = data
    fetchLibrary(data.libraryId)
    fetchFiles(data.id)
  }
}

async function fetchLibrary(id: number) {
  const { data } = await client.GET('/libraries/{id}', {
    params: { path: { id } },
  })
  if (data) library.value = data
}

async function fetchFiles(id: number) {
  const { data } = await client.GET('/media/{id}/files', {
    params: { path: { id } },
  })
  files.value = data?.files ?? []
}

async function fetchProfiles() {
  const { data } = await client.GET('/media-profiles')
  profiles.value = data?.profiles ?? []
}

async function updateMediaItem(update: { mediaProfileId?: number; monitorNewSeasons?: boolean }) {
  if (!item.value) return
  const { data } = await client.PATCH('/media/{id}', {
    params: { path: { id: item.value.id } },
    body: update,
  })
  if (data) item.value = data
}

async function onProfileChange(event: Event) {
  const value = (event.target as HTMLSelectElement).value
  await updateMediaItem({ mediaProfileId: value ? Number(value) : undefined })
}

async function onMonitorToggle(event: Event) {
  const checked = (event.target as HTMLInputElement).checked
  await updateMediaItem({ monitorNewSeasons: checked })
}

async function handleResync() {
  if (!item.value) return
  resyncing.value = true
  await client.POST('/media/{id}/resync', {
    params: { path: { id: item.value.id } },
  })
  await fetchFiles(item.value.id)
  resyncing.value = false
}

async function handleUnmatch() {
  if (!item.value) return
  const { error: err } = await client.DELETE('/media/{id}/match', {
    params: { path: { id: item.value.id } },
  })
  if (!err) {
    await fetchItem(item.value.id)
  }
}

async function handleDelete() {
  if (!item.value) return
  if (!confirm(`Delete "${item.value.title}"? This will remove all files from the library, torrents from the download client, and all associated data.`)) return
  const { error: err } = await client.DELETE('/media/{id}', {
    params: { path: { id: item.value.id } },
  })
  if (!err && library.value) {
    router.replace({ name: 'library-detail', params: { id: library.value.id } })
  }
}

function openMatchPanel() {
  showMatchPanel.value = true
}

function closeMatchPanel() {
  showMatchPanel.value = false
}

async function onMatchDone() {
  showMatchPanel.value = false
  if (item.value) {
    await fetchItem(item.value.id)
  }
}

function openIndexerSearch(season?: number, episode?: number, episodeId?: number) {
  indexerSearchSeason.value = season
  indexerSearchEpisode.value = episode
  indexerSearchEpisodeId.value = episodeId
  showIndexerSearch.value = true
}

async function closeIndexerSearch() {
  const oldId = replacingDownloadId.value
  showIndexerSearch.value = false
  indexerSearchSeason.value = undefined
  indexerSearchEpisode.value = undefined
  indexerSearchEpisodeId.value = undefined
  replacingDownloadId.value = null
  episodeRefreshKey.value++

  // If replacing, delete old download after modal closes (new download was already created in modal)
  if (oldId) {
    await client.DELETE('/downloads/{id}', {
      params: { path: { id: oldId } },
    })
  }
  downloadRefreshKey.value++
}

function onDownloadReplace(downloadId: number, seasonNumber?: number, episodeNumber?: number, episodeId?: number) {
  replacingDownloadId.value = downloadId
  openIndexerSearch(seasonNumber, episodeNumber, episodeId)
}

const removeJobDoneListener = onJobDone((libraryId) => {
  if (item.value && item.value.libraryId === libraryId) {
    fetchItem(item.value.id)
  }
})

function loadAll() {
  const id = Number(route.params.id)
  fetchItem(id)
  fetchProfiles()
}

onMounted(loadAll)
onUnmounted(removeJobDoneListener)
watch(() => route.params.id, loadAll)
</script>

<template>
  <div>
    <!-- Top bar: back nav + actions -->
    <div class="flex items-center justify-between mb-6 gap-4">
      <router-link
        v-if="library"
        :to="{ name: 'library-detail', params: { id: library.id } }"
        class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-violet-300 transition-colors duration-200 flex-shrink-0"
      >
        <span class="text-base leading-none">&larr;</span>
        Back to {{ library.name }}
      </router-link>

      <div v-if="item" class="flex items-center gap-3 flex-wrap justify-end">
        <!-- Quality profile -->
        <div class="flex items-center gap-2">
          <label for="profile-select" class="text-xs text-gray-500">Quality Profile</label>
          <select
            id="profile-select"
            class="text-sm bg-[#161b2e] border border-violet-900/20 rounded-lg px-3 py-1.5 text-gray-200 focus:outline-none focus:border-violet-500/50"
            :value="item.mediaProfileId ?? ''"
            @change="onProfileChange"
          >
            <option value="">None</option>
            <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
          </select>
        </div>

        <!-- Monitor new seasons -->
        <label v-if="item.mediaType === 'series'" class="flex items-center gap-2 cursor-pointer">
          <input
            type="checkbox"
            class="w-4 h-4 rounded border-violet-900/20 bg-[#161b2e] text-violet-600 focus:ring-violet-500/50 focus:ring-offset-0"
            :checked="item.monitorNewSeasons ?? false"
            @change="onMonitorToggle"
          />
          <span class="text-xs text-gray-500">Monitor new seasons</span>
        </label>

        <!-- Divider -->
        <div class="w-px h-6 bg-violet-900/30"></div>

        <!-- Action buttons -->
        <button
          class="px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-xs font-medium transition-colors duration-200"
          @click="openMatchPanel"
        >
          {{ metadata ? 'Re-match' : 'Match' }}
        </button>
        <button
          v-if="metadata?.imdbId"
          class="px-3 py-1.5 rounded-lg border border-violet-500/30 text-violet-300 hover:bg-violet-500/10 text-xs font-medium transition-colors duration-200"
          @click="openIndexerSearch()"
        >
          Search Indexers
        </button>
        <button
          v-if="item.source === 'disk'"
          class="px-3 py-1.5 rounded-lg border border-violet-500/30 text-violet-300 hover:bg-violet-500/10 text-xs font-medium transition-colors duration-200"
          :disabled="resyncing"
          @click="handleResync"
        >
          {{ resyncing ? 'Rescanning...' : 'Rescan Files' }}
        </button>
        <button
          v-if="metadata"
          class="px-3 py-1.5 rounded-lg border border-red-500/30 text-red-400 hover:bg-red-500/10 text-xs font-medium transition-colors duration-200"
          @click="handleUnmatch"
        >
          Unmatch
        </button>
        <button
          class="px-3 py-1.5 rounded-lg border border-red-500/30 text-red-400 hover:bg-red-500/10 text-xs font-medium transition-colors duration-200"
          @click="handleDelete"
        >
          Delete
        </button>
      </div>
    </div>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading && !item" class="text-gray-500 text-sm">Loading...</div>

    <!-- Content -->
    <div v-else-if="item">
      <!-- Hero section -->
      <div class="flex gap-8">
        <!-- Poster -->
        <div class="flex-shrink-0 w-[300px]">
          <div class="aspect-[2/3] rounded-lg overflow-hidden bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center">
            <img
              v-if="item.status === 'available' || item.status === 'requested'"
              :src="posterUrl(item)"
              :alt="item.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
            <span v-if="item.status !== 'available' && item.status !== 'requested'" class="text-6xl text-gray-600">
              {{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}
            </span>
          </div>
        </div>

        <!-- Info -->
        <div class="flex-1 min-w-0">
          <!-- Title -->
          <h1 class="text-2xl font-bold text-gray-100 tracking-tight mb-3">{{ item.title }}</h1>

          <!-- Year + badges -->
          <div class="flex items-center gap-3 mb-4">
            <span v-if="item.year || metadata?.year" class="text-sm text-gray-400">
              {{ item.year || metadata?.year }}
            </span>
            <span
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="item.mediaType === 'movie'
                ? 'bg-violet-600/20 text-violet-300'
                : 'bg-fuchsia-600/20 text-fuchsia-300'"
            >
              {{ item.mediaType }}
            </span>
            <span
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="{
                'bg-emerald-600/20 text-emerald-300': item.status === 'available',
                'bg-yellow-600/20 text-yellow-300': item.status === 'new',
                'bg-red-600/20 text-red-300': item.status === 'missing',
                'bg-sky-600/20 text-sky-300': item.status === 'requested',
              }"
            >
              {{ item.status }}
            </span>
          </div>

          <!-- Genre pills -->
          <div v-if="genres.length" class="flex flex-wrap gap-2 mb-5">
            <span
              v-for="genre in genres"
              :key="genre"
              class="text-xs px-2.5 py-1 rounded-full bg-[#161b2e] border border-violet-900/20 text-gray-300"
            >
              {{ genre }}
            </span>
          </div>

          <!-- Overview -->
          <p v-if="metadata?.overview" class="text-sm text-gray-400 leading-relaxed mb-6">
            {{ metadata.overview }}
          </p>

          <!-- Stats grid -->
          <div v-if="metadata" class="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
            <div v-if="metadata.rating" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Rating</p>
              <p class="text-lg font-semibold text-gray-200">{{ metadata.rating.toFixed(1) }}<span class="text-xs text-gray-500 font-normal">/10</span></p>
            </div>
            <div v-if="item.mediaType === 'movie' && metadata.runtime" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Runtime</p>
              <p class="text-lg font-semibold text-gray-200">{{ metadata.runtime }}<span class="text-xs text-gray-500 font-normal"> min</span></p>
            </div>
            <div v-if="item.mediaType === 'series' && metadata.seasons" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Seasons</p>
              <p class="text-lg font-semibold text-gray-200">{{ metadata.seasons }}</p>
            </div>
            <div v-if="metadata.status" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Status</p>
              <p class="text-sm font-medium text-gray-200">{{ metadata.status }}</p>
            </div>
          </div>

          <!-- Cast -->
          <div v-if="cast.length" class="mb-6">
            <h3 class="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">Cast</h3>
            <div class="flex flex-wrap gap-3">
              <div
                v-for="(person, i) in cast"
                :key="'cast-' + i"
                class="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-900/20"
              >
                <div class="w-8 h-8 rounded-full bg-violet-900/30 flex-shrink-0 overflow-hidden">
                  <img
                    v-if="profileImageUrl(person)"
                    :src="profileImageUrl(person)!"
                    :alt="person.name"
                    class="w-full h-full object-cover"
                    @error="($event.target as HTMLImageElement).style.display = 'none'"
                  />
                </div>
                <div class="min-w-0">
                  <p class="text-sm text-gray-200 truncate">{{ person.name }}</p>
                  <p class="text-[11px] text-gray-500 truncate">{{ person.role }}</p>
                </div>
              </div>
            </div>
          </div>

          <!-- Crew -->
          <div v-if="crew.length" class="mb-6">
            <h3 class="text-xs font-semibold uppercase tracking-wider text-gray-500 mb-3">Crew</h3>
            <div class="flex flex-wrap gap-3">
              <div
                v-for="(person, i) in crew"
                :key="'crew-' + i"
                class="flex items-center gap-2.5 px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-900/20"
              >
                <div class="w-8 h-8 rounded-full bg-violet-900/30 flex-shrink-0 overflow-hidden">
                  <img
                    v-if="profileImageUrl(person)"
                    :src="profileImageUrl(person)!"
                    :alt="person.name"
                    class="w-full h-full object-cover"
                    @error="($event.target as HTMLImageElement).style.display = 'none'"
                  />
                </div>
                <div class="min-w-0">
                  <p class="text-sm text-gray-200 truncate">{{ person.name }}</p>
                  <p class="text-[11px] text-gray-500 truncate">{{ person.role }}</p>
                </div>
              </div>
            </div>
          </div>

          <!-- Match source + IMDb cards -->
          <div v-if="metadata" class="flex flex-wrap gap-3 mb-6">
            <!-- Source card -->
            <div class="inline-flex items-center gap-3 px-4 py-3 rounded-lg bg-emerald-500/5 border border-emerald-500/20">
              <span
                class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
              >
                {{ metadata.source }}
              </span>
              <span class="text-xs text-gray-400">
                {{ Math.round(metadata.confidence * 100) }}% confidence
              </span>
              <a
                v-if="externalUrl"
                :href="externalUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="text-xs text-violet-400 hover:text-violet-300 transition-colors duration-200"
              >
                View on {{ metadata.source.toUpperCase() }} &nearr;
              </a>
            </div>
            <!-- IMDb card -->
            <div
              v-if="metadata.imdbId"
              class="inline-flex items-center gap-3 px-4 py-3 rounded-lg bg-amber-500/5 border border-amber-500/20"
            >
              <span
                class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-amber-600/20 text-amber-300"
              >
                IMDb
              </span>
              <span class="text-xs text-gray-400 font-mono">{{ metadata.imdbId }}</span>
              <a
                v-if="imdbUrl"
                :href="imdbUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="text-xs text-amber-400 hover:text-amber-300 transition-colors duration-200"
              >
                View on IMDb &nearr;
              </a>
            </div>
          </div>
        </div>
      </div>

      <!-- Episodes section (series only) -->
      <EpisodeGrid
        v-if="item.mediaType === 'series' && metadata"
        :mediaItemId="item.id"
        :refreshKey="episodeRefreshKey"
        @search-season="(sn: number) => openIndexerSearch(sn)"
        @search-episode="(sn: number, en: number, eid: number) => openIndexerSearch(sn, en, eid)"
        class="mt-8"
      />

      <!-- Downloads section -->
      <DownloadList
        v-if="item"
        :mediaItemId="item.id"
        :imdbId="metadata?.imdbId"
        :mediaType="(item.mediaType as 'movie' | 'series')"
        :title="`${item.title}${item.year ? ` (${item.year})` : ''}`"
        :refreshKey="downloadRefreshKey"
        @replace="onDownloadReplace"
        class="mt-8"
      />

      <!-- Files section -->
      <div class="mt-8">
        <button
          class="flex items-center gap-3 group cursor-pointer"
          @click="filesExpanded = !filesExpanded"
        >
          <span
            class="text-gray-500 text-xs transition-transform duration-200"
            :class="{ 'rotate-90': filesExpanded }"
          >&#9654;</span>
          <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 group-hover:text-gray-400 transition-colors duration-200">Files</h2>
          <span
            v-if="files.length"
            class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
          >
            {{ files.length }}
          </span>
        </button>

        <div v-if="filesExpanded" class="mt-4">
          <div v-if="!files.length" class="text-sm text-gray-500">No files found.</div>

          <div v-else class="space-y-2">
            <div
              v-for="file in files"
              :key="file.id"
              class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20"
            >
              <div class="flex items-start justify-between gap-4">
                <div class="min-w-0 flex-1">
                  <p class="text-sm font-medium text-gray-200 truncate">{{ file.fileName }}</p>
                  <p class="text-xs text-gray-500 font-mono truncate mt-0.5">{{ file.path }}</p>
                </div>
                <div class="flex items-center gap-2 flex-shrink-0">
                  <span
                    v-if="file.seasonNumber != null"
                    class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-fuchsia-600/20 text-fuchsia-300"
                  >
                    S{{ String(file.seasonNumber).padStart(2, '0') }}E{{ String(file.episodeNumber ?? 0).padStart(2, '0') }}
                  </span>
                  <span
                    v-if="file.resolution"
                    class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
                  >
                    {{ file.resolution }}
                  </span>
                  <span
                    v-if="file.sourceType"
                    class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-sky-600/20 text-sky-300"
                  >
                    {{ file.sourceType }}
                  </span>
                  <span v-if="file.size" class="text-xs text-gray-500">
                    {{ formatBytes(file.size) }}
                  </span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Match Panel -->
    <MatchPanel
      v-if="showMatchPanel && item"
      :item="item"
      @close="closeMatchPanel"
      @matched="onMatchDone"
    />

    <!-- Indexer Search Modal -->
    <IndexerSearchModal
      v-if="showIndexerSearch && item && metadata?.imdbId"
      :mediaItemId="item.id"
      :imdbId="metadata.imdbId"
      :mediaType="item.mediaType as 'movie' | 'series'"
      :title="`${item.title}${item.year ? ` (${item.year})` : ''}`"
      :seasonNumber="indexerSearchSeason"
      :episodeNumber="indexerSearchEpisode"
      :episodeId="indexerSearchEpisodeId"
      @close="closeIndexerSearch"
    />
  </div>
</template>
