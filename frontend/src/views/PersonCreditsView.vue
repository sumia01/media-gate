<script setup lang="ts">
import { ArrowLeft, Film, Tv, UserRound } from 'lucide-vue-next'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import DiscoverCard from '@/components/media/DiscoverCard.vue'
import DiscoverFilter from '@/components/media/DiscoverFilter.vue'
import { useDiscoverFilter } from '@/composables/useDiscoverFilter'
import { useWatchedLibrary } from '@/composables/useWatchedLibrary'
import type { PersonCredits } from '@/types/api'
import { profileImageUrl } from '@/utils/media'

const props = defineProps<{
  source: string
  personId: string
}>()

const route = useRoute()
const router = useRouter()
const credits = ref<PersonCredits | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const movieLimit = ref(35)
const seriesLimit = ref(35)
const activeTab = ref<'movie' | 'series'>('movie')
const movieTab = ref<HTMLButtonElement | null>(null)
const seriesTab = ref<HTMLButtonElement | null>(null)
let request = 0
let controller: AbortController | undefined

const {
  isWatched,
  isInLibrary,
  libraryReady,
  libraryLoading,
  libraryFailed,
  fetchWatched,
  fetchLibraryItems,
  goToPreview,
} = useWatchedLibrary()
const { hideInLibrary, filterReady, includeItem } = useDiscoverFilter(isInLibrary, libraryReady)

const queryName = computed(() => (typeof route.query.name === 'string' ? route.query.name : ''))
const queryImage = computed(() => (typeof route.query.image === 'string' ? route.query.image : ''))
const sourceParam = computed(() => (props.source === 'tvdb' ? 'tvdb' : 'tmdb') as 'tmdb' | 'tvdb')
const personName = computed(() => credits.value?.name || queryName.value || 'Cast member')
const headerImage = computed(() => credits.value?.profileUrl ?? profileImageUrl({ image: queryImage.value }))
const movies = computed(() => credits.value?.movies ?? [])
const series = computed(() => credits.value?.series ?? [])
const filteredMovies = computed(() => (filterReady.value ? movies.value.filter(includeItem) : []))
const filteredSeries = computed(() => (filterReady.value ? series.value.filter(includeItem) : []))
const visibleMovies = computed(() => filteredMovies.value.slice(0, movieLimit.value))
const visibleSeries = computed(() => filteredSeries.value.slice(0, seriesLimit.value))
const hiddenMovies = computed(() => movies.value.length - filteredMovies.value.length)
const hiddenSeries = computed(() => series.value.length - filteredSeries.value.length)

function emptyCreditsMessage(kind: 'movie' | 'series', hidden: number): string {
  if (!filterReady.value) {
    return libraryFailed.value ? 'Library membership unavailable.' : 'Checking your library...'
  }
  if (hidden > 0) return `All ${kind} credits are already in your library.`
  return `No ${kind} credits found.`
}

const emptyMoviesMessage = computed(() => emptyCreditsMessage('movie', hiddenMovies.value))
const emptySeriesMessage = computed(() => emptyCreditsMessage('series', hiddenSeries.value))

function selectTab(tab: 'movie' | 'series', focus = false) {
  activeTab.value = tab
  if (!focus) return
  void nextTick(() => (tab === 'movie' ? movieTab.value : seriesTab.value)?.focus())
}

function onTabKeydown(event: KeyboardEvent) {
  let tab: 'movie' | 'series' | undefined
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
    tab = activeTab.value === 'movie' ? 'series' : 'movie'
  } else if (event.key === 'Home') {
    tab = 'movie'
  } else if (event.key === 'End') {
    tab = 'series'
  }
  if (!tab) return
  event.preventDefault()
  selectTab(tab, true)
}

async function fetchCredits() {
  const currentRequest = ++request
  controller?.abort()
  controller = new AbortController()
  loading.value = true
  loadFailed.value = false
  credits.value = null
  movieLimit.value = 35
  seriesLimit.value = 35
  activeTab.value = 'movie'

  const parsedID = Number(props.personId)
  const personID = Number.isInteger(parsedID) && parsedID >= 0 ? parsedID : 0
  try {
    const { data, error } = await client.GET('/discover/person/{source}/{personId}', {
      params: {
        path: { source: sourceParam.value, personId: personID },
        query: { name: queryName.value || undefined, image: queryImage.value || undefined },
      },
      signal: controller.signal,
    })
    if (currentRequest !== request || controller.signal.aborted) return
    if (error || !data) {
      loadFailed.value = true
      return
    }
    credits.value = data
  } catch {
    if (currentRequest === request && !controller.signal.aborted) loadFailed.value = true
  } finally {
    if (currentRequest === request && !controller.signal.aborted) loading.value = false
  }
}

watch(
  () => `${props.source}:${props.personId}:${queryName.value}:${queryImage.value}`,
  () => {
    if (route.name !== 'discover-person') return
    void fetchCredits()
  },
  { flush: 'post' },
)

onMounted(() => {
  void fetchCredits()
  void fetchWatched()
  void fetchLibraryItems()
})

onUnmounted(() => controller?.abort())
</script>

<template>
  <div>
    <button
      type="button"
      class="mb-6 inline-flex items-center gap-1.5 text-sm text-violet-400 transition-colors hover:text-violet-300 focus-visible:outline-2 focus-visible:outline-violet-400"
      @click="router.back()"
    >
      <ArrowLeft class="h-4 w-4" /> Back
    </button>

    <div v-if="loading" role="status" class="space-y-10">
      <span class="sr-only">Loading cast member credits...</span>
      <div class="flex animate-pulse items-center gap-5">
        <div class="h-28 w-24 rounded-xl bg-white/5" />
        <div class="space-y-3">
          <div class="h-7 w-52 rounded bg-white/5" />
          <div class="h-4 w-28 rounded bg-white/5" />
        </div>
      </div>
      <div v-for="section in 2" :key="section">
        <div class="mb-4 h-6 w-32 rounded bg-white/5" />
        <div class="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          <div v-for="card in 7" :key="card" class="aspect-[2/3] animate-pulse rounded-lg bg-white/5" />
        </div>
      </div>
    </div>

    <div v-else-if="loadFailed" role="alert" class="rounded-xl border border-amber-500/20 bg-amber-500/5 p-8 text-center">
      <p class="text-sm text-amber-300">Could not load this cast member's credits.</p>
      <button
        type="button"
        class="mt-4 rounded-lg border border-violet-500/30 bg-violet-600/10 px-4 py-2 text-sm text-violet-300 transition-colors hover:bg-violet-600/20"
        @click="fetchCredits"
      >
        Retry
      </button>
    </div>

    <template v-else-if="credits">
      <header class="mb-8 flex items-center gap-5 rounded-xl border border-violet-900/25 bg-gradient-to-r from-[#161b2e] to-violet-950/20 p-5 sm:p-6">
        <div class="relative flex h-28 w-24 flex-shrink-0 items-center justify-center overflow-hidden rounded-xl bg-violet-900/30 sm:h-36 sm:w-28">
          <UserRound class="h-10 w-10 text-violet-400/60" />
          <img
            v-if="headerImage"
            :src="headerImage"
            :alt="personName"
            class="absolute inset-0 h-full w-full object-cover"
            @error="($event.target as HTMLImageElement).style.display = 'none'"
            @load="($event.target as HTMLImageElement).style.display = ''"
          />
        </div>
        <div class="min-w-0">
          <p class="mb-1 text-xs font-semibold uppercase tracking-widest text-violet-400">
            {{ credits.knownForDepartment || 'Cast member' }}
          </p>
          <h1 class="text-2xl font-semibold tracking-tight text-gray-100 sm:text-3xl">{{ personName }}</h1>
          <p class="mt-2 text-sm text-gray-400">{{ movies.length }} movies &middot; {{ series.length }} series</p>
          <p v-if="credits.biography" class="mt-3 hidden max-w-3xl line-clamp-3 text-sm leading-relaxed text-gray-500 md:block">
            {{ credits.biography }}
          </p>
        </div>
      </header>

      <DiscoverFilter
        v-model:hide-in-library="hideInLibrary"
        :library-loading="libraryLoading"
        :library-failed="libraryFailed"
        @retry="fetchLibraryItems"
      />

      <div
        role="tablist"
        aria-label="Credits by media type"
        class="mb-4 flex border-b border-violet-900/30"
        @keydown="onTabKeydown"
      >
        <button
          id="person-credits-movies-tab"
          ref="movieTab"
          type="button"
          role="tab"
          :aria-selected="activeTab === 'movie'"
          aria-controls="person-credits-movies-panel"
          :tabindex="activeTab === 'movie' ? 0 : -1"
          class="-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-2.5 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 sm:px-4"
          :class="activeTab === 'movie' ? 'border-violet-400 text-gray-100' : 'border-transparent text-gray-500 hover:text-gray-300'"
          @click="selectTab('movie')"
        >
          <Film class="h-5 w-5 text-violet-400" />
          <span class="text-lg font-semibold tracking-tight">Movies</span>
          <span class="text-sm text-gray-500">{{ filteredMovies.length }}</span>
        </button>
        <button
          id="person-credits-series-tab"
          ref="seriesTab"
          type="button"
          role="tab"
          :aria-selected="activeTab === 'series'"
          aria-controls="person-credits-series-panel"
          :tabindex="activeTab === 'series' ? 0 : -1"
          class="-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-2.5 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 sm:px-4"
          :class="activeTab === 'series' ? 'border-fuchsia-400 text-gray-100' : 'border-transparent text-gray-500 hover:text-gray-300'"
          @click="selectTab('series')"
        >
          <Tv class="h-5 w-5 text-fuchsia-400" />
          <span class="text-lg font-semibold tracking-tight">Series</span>
          <span class="text-sm text-gray-500">{{ filteredSeries.length }}</span>
        </button>
      </div>

      <section
        v-if="activeTab === 'movie'"
        id="person-credits-movies-panel"
        role="tabpanel"
        aria-labelledby="person-credits-movies-tab"
        class="mb-8"
      >
        <div v-if="visibleMovies.length" class="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          <DiscoverCard
            v-for="item in visibleMovies"
            :key="`movie:${item.externalId}`"
            :item="item"
            :in-library="isInLibrary(item)"
            :watched="isWatched(item)"
            @click="goToPreview(item)"
          />
        </div>
        <p v-else class="rounded-xl border border-violet-900/20 bg-violet-950/10 px-6 py-8 text-center text-sm text-gray-500">
          {{ emptyMoviesMessage }}
        </p>
        <button
          v-if="visibleMovies.length < filteredMovies.length"
          type="button"
          class="mx-auto mt-6 block rounded-lg border border-violet-500/30 bg-violet-600/10 px-4 py-2 text-sm text-violet-300 transition-colors hover:bg-violet-600/20"
          @click="movieLimit += 35"
        >
          Load more movies
        </button>
      </section>

      <section
        v-else
        id="person-credits-series-panel"
        role="tabpanel"
        aria-labelledby="person-credits-series-tab"
        class="mb-8"
      >
        <div v-if="visibleSeries.length" class="grid grid-cols-2 gap-5 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7">
          <DiscoverCard
            v-for="item in visibleSeries"
            :key="`series:${item.externalId}`"
            :item="item"
            :in-library="isInLibrary(item)"
            :watched="isWatched(item)"
            @click="goToPreview(item)"
          />
        </div>
        <p v-else class="rounded-xl border border-violet-900/20 bg-violet-950/10 px-6 py-8 text-center text-sm text-gray-500">
          {{ emptySeriesMessage }}
        </p>
        <button
          v-if="visibleSeries.length < filteredSeries.length"
          type="button"
          class="mx-auto mt-6 block rounded-lg border border-fuchsia-500/30 bg-fuchsia-600/10 px-4 py-2 text-sm text-fuchsia-300 transition-colors hover:bg-fuchsia-600/20"
          @click="seriesLimit += 35"
        >
          Load more series
        </button>
      </section>
    </template>
  </div>
</template>
