<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import type { ExternalMediaDetail, ExternalSeasonSummary } from '@/types/api'
import { parseGenres, profileImageUrl } from '@/utils/media'
import ErrorBanner from '@/components/ErrorBanner.vue'
import AddToLibraryModal from '@/components/media/AddToLibraryModal.vue'
import IndexerSearchModal from '@/components/media/IndexerSearchModal.vue'

const route = useRoute()
const router = useRouter()

const detail = ref<ExternalMediaDetail | null>(null)
const externalSeasons = ref<ExternalSeasonSummary[]>([])
const loading = ref(false)
const error = ref('')
const showAddModal = ref(false)
const showIndexerSearch = ref(false)

const isWatched = ref(false)
const watchedId = ref<number | null>(null)
const watchedLoading = ref(false)

const genres = computed(() => parseGenres(detail.value?.genres))

const externalUrl = computed(() => {
  if (!detail.value) return null
  if (detail.value.source === 'tmdb') {
    const type = detail.value.mediaType === 'movie' ? 'movie' : 'tv'
    return `https://www.themoviedb.org/${type}/${detail.value.externalId}`
  }
  if (detail.value.source === 'tvdb') {
    return `https://thetvdb.com/?id=${detail.value.externalId}&tab=series`
  }
  return null
})

const imdbUrl = computed(() => {
  if (!detail.value?.imdbId) return null
  return `https://www.imdb.com/title/${detail.value.imdbId}/`
})

const credits = computed(() => detail.value?.credits ?? [])
const cast = computed(() => credits.value.filter(c => c.type === 'cast'))
const crew = computed(() => credits.value.filter(c => c.type === 'crew'))

async function checkWatched() {
  const d = detail.value
  if (!d) return
  const { data } = await client.GET('/watched/check', {
    params: { query: { source: d.source as 'tmdb' | 'tvdb', externalId: d.externalId } },
  })
  if (data) {
    isWatched.value = data.watched
    watchedId.value = data.id ?? null
  }
}

async function toggleWatched() {
  const d = detail.value
  if (!d) return
  watchedLoading.value = true
  if (isWatched.value && watchedId.value) {
    await client.DELETE('/watched/{id}', { params: { path: { id: watchedId.value } } })
    isWatched.value = false
    watchedId.value = null
  } else {
    const { data } = await client.POST('/watched', {
      body: {
        source: d.source as 'tmdb' | 'tvdb',
        externalId: d.externalId,
        imdbId: d.imdbId ?? undefined,
        title: d.title,
        mediaType: (d.mediaType ?? 'movie') as 'movie' | 'series',
        year: d.year ?? undefined,
        posterPath: d.posterUrl ?? undefined,
      },
    })
    if (data) {
      isWatched.value = true
      watchedId.value = data.id
    }
  }
  watchedLoading.value = false
}

async function fetchDetail() {
  const source = route.params.source as string
  const externalId = Number(route.params.externalId)
  const mediaType = (route.query.mediaType as string) || 'movie'

  loading.value = true
  error.value = ''

  const { data, error: err } = await client.GET('/search/{source}/{externalId}', {
    params: {
      path: { source: source as 'tmdb' | 'tvdb', externalId },
      query: { mediaType: mediaType as 'movie' | 'series' },
    },
  })
  loading.value = false
  if (err) {
    error.value = 'Failed to load media details'
    return
  }
  if (data) {
    detail.value = data
    checkWatched()
    // Prefetch episodes for series (used by AddToLibraryModal)
    if (data.mediaType === 'series' && data.seasons && data.seasons > 0) {
      client.GET('/search/{source}/{externalId}/episodes', {
        params: {
          path: { source: source as 'tmdb' | 'tvdb', externalId },
          query: { seasonCount: data.seasons },
        },
      }).then(({ data: epData }) => {
        externalSeasons.value = epData?.seasons ?? []
      })
    }
  }
}

function handleAdded(mediaItemId: number) {
  showAddModal.value = false
  router.push({ name: 'media-detail', params: { id: mediaItemId } })
}

onMounted(fetchDetail)
watch(() => [route.params.source, route.params.externalId, route.query.mediaType], fetchDetail)
</script>

<template>
  <div>
    <!-- Top bar -->
    <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between mb-6">
      <button
        class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-violet-300 transition-colors duration-200 flex-shrink-0"
        @click="router.back()"
      >
        <span class="text-base leading-none">&larr;</span>
        Back
      </button>

      <div v-if="detail" class="flex items-center gap-2">
        <!-- Watched toggle -->
        <button
          class="flex items-center gap-1.5 px-3 py-2 rounded-lg text-sm font-medium transition-colors duration-200"
          :class="isWatched
            ? 'bg-emerald-600/20 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-600/30'
            : 'text-gray-400 border border-violet-800/30 hover:text-violet-300 hover:bg-violet-600/10'"
          :disabled="watchedLoading"
          @click="toggleWatched"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
            <path v-if="isWatched" stroke-linecap="round" stroke-linejoin="round" d="M2.036 12.322a1.012 1.012 0 010-.639C3.423 7.51 7.36 4.5 12 4.5c4.638 0 8.573 3.007 9.963 7.178.07.207.07.431 0 .639C20.577 16.49 16.64 19.5 12 19.5c-4.638 0-8.573-3.007-9.963-7.178z" />
            <path v-if="isWatched" stroke-linecap="round" stroke-linejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            <path v-if="!isWatched" stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
          </svg>
          {{ isWatched ? 'Watched' : 'Unseen' }}
        </button>
        <button
          v-if="detail.imdbId"
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
          @click="showIndexerSearch = true"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" /></svg>
          Check Indexers
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
          @click="showAddModal = true"
        >
          <span class="text-base leading-none">+</span>
          Add to Library
        </button>
      </div>
    </div>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading && !detail" class="text-gray-500 text-sm">Loading...</div>

    <!-- Content -->
    <div v-else-if="detail">
      <!-- Hero section -->
      <div class="flex flex-col md:flex-row gap-6 md:gap-8">
        <!-- Poster -->
        <div class="flex-shrink-0 w-full max-w-[250px] mx-auto md:w-[300px] md:max-w-none md:mx-0">
          <div class="aspect-[2/3] rounded-lg overflow-hidden bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center">
            <img
              v-if="detail.posterUrl"
              :src="detail.posterUrl"
              :alt="detail.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
            <span v-else class="text-6xl text-gray-600">
              {{ detail.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}
            </span>
          </div>
        </div>

        <!-- Info -->
        <div class="flex-1 min-w-0">
          <!-- Title -->
          <h1 class="text-2xl font-bold text-gray-100 tracking-tight mb-3">{{ detail.title }}</h1>

          <!-- Year + badges -->
          <div class="flex items-center gap-3 mb-4">
            <span v-if="detail.year" class="text-sm text-gray-400">{{ detail.year }}</span>
            <span
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="detail.mediaType === 'movie'
                ? 'bg-violet-600/20 text-violet-300'
                : 'bg-fuchsia-600/20 text-fuchsia-300'"
            >
              {{ detail.mediaType }}
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
          <p v-if="detail.overview" class="text-sm text-gray-400 leading-relaxed mb-6">
            {{ detail.overview }}
          </p>

          <!-- Stats grid -->
          <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
            <div v-if="detail.mediaType === 'movie' && detail.runtime" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Runtime</p>
              <p class="text-lg font-semibold text-gray-200">{{ detail.runtime }}<span class="text-xs text-gray-500 font-normal"> min</span></p>
            </div>
            <div v-if="detail.mediaType === 'series' && detail.seasons" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Seasons</p>
              <p class="text-lg font-semibold text-gray-200">{{ detail.seasons }}</p>
            </div>
            <div v-if="detail.status" class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
              <p class="text-xs text-gray-500 mb-1">Status</p>
              <p class="text-sm font-medium text-gray-200">{{ detail.status }}</p>
            </div>
          </div>

          <!-- Cast -->
          <div v-if="cast.length" class="hidden md:block mb-6">
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
          <div v-if="crew.length" class="hidden md:block mb-6">
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

          <!-- Source + IMDb cards -->
          <div class="hidden md:flex flex-wrap gap-3 mb-6">
            <!-- Source card -->
            <div class="inline-flex items-center gap-3 px-4 py-3 rounded-lg bg-emerald-500/5 border border-emerald-500/20">
              <span class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300">
                {{ detail.source }}
              </span>
              <a
                v-if="externalUrl"
                :href="externalUrl"
                target="_blank"
                rel="noopener noreferrer"
                class="text-xs text-violet-400 hover:text-violet-300 transition-colors duration-200"
              >
                View on {{ detail.source.toUpperCase() }} &nearr;
              </a>
            </div>
            <!-- IMDb card -->
            <div
              v-if="detail.imdbId"
              class="inline-flex items-center gap-3 px-4 py-3 rounded-lg bg-amber-500/5 border border-amber-500/20"
            >
              <span class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-amber-600/20 text-amber-300">
                IMDb
              </span>
              <span class="text-xs text-gray-400 font-mono">{{ detail.imdbId }}</span>
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
    </div>

    <!-- Add to Library Modal -->
    <Teleport to="body">
      <AddToLibraryModal
        v-if="showAddModal && detail"
        :source="detail.source"
        :external-id="detail.externalId"
        :media-type="detail.mediaType"
        :external-seasons="externalSeasons"
        @added="handleAdded"
        @close="showAddModal = false"
      />
    </Teleport>

    <!-- Indexer Search Modal -->
    <Teleport to="body">
      <IndexerSearchModal
        v-if="showIndexerSearch && detail && detail.imdbId"
        :imdb-id="detail.imdbId"
        :media-type="detail.mediaType as 'movie' | 'series'"
        :title="detail.title"
        :source="detail.source as 'tmdb' | 'tvdb'"
        :external-id="detail.externalId"
        @close="showIndexerSearch = false"
        @added="handleAdded"
      />
    </Teleport>
  </div>
</template>
