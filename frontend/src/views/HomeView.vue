<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import DiscoverCard from '@/components/media/DiscoverCard.vue'
import type { DiscoverItem, MediaItem } from '@/types/api'
import { posterUrl } from '@/utils/media'
import { Eye, ArrowRight } from 'lucide-vue-next'

const router = useRouter()

const recentItems = ref<MediaItem[]>([])
const trendingItems = ref<DiscoverItem[]>([])
const popularMovies = ref<DiscoverItem[]>([])
const popularSeries = ref<DiscoverItem[]>([])

const recentLoading = ref(true)
const trendingLoading = ref(true)
const moviesLoading = ref(true)
const seriesLoading = ref(true)

const watchedSet = ref<Set<string>>(new Set())
const libraryMap = ref<Map<string, number>>(new Map())

function watchedKey(source: string, externalId: number): string {
  return `${source}:${externalId}`
}

function isWatched(source: string, externalId: number): boolean {
  return watchedSet.value.has(watchedKey(source, externalId))
}

function isRecentWatched(item: MediaItem): boolean {
  if (!item.metadata?.source || !item.metadata?.externalId) return false
  return isWatched(item.metadata.source, item.metadata.externalId)
}

function isInLibrary(source: string, externalId: number): boolean {
  return libraryMap.value.has(watchedKey(source, externalId))
}

function libraryMediaId(source: string, externalId: number): number | undefined {
  return libraryMap.value.get(watchedKey(source, externalId))
}

async function fetchWatched() {
  const { data } = await client.GET('/watched')
  const set = new Set<string>()
  for (const item of data?.items ?? []) {
    set.add(watchedKey(item.source, item.externalId))
  }
  watchedSet.value = set
}

async function fetchLibraryItems() {
  const { data } = await client.GET('/media/external-ids')
  const map = new Map<string, number>()
  for (const item of data?.items ?? []) {
    map.set(watchedKey(item.source, item.externalId), item.mediaItemId)
  }
  libraryMap.value = map
}

onMounted(() => {
  fetchRecent()
  fetchTrending()
  fetchPopularMovies()
  fetchPopularSeries()
  fetchWatched()
  fetchLibraryItems()
})

async function fetchRecent() {
  const { data } = await client.GET('/discover/recently-added')
  recentItems.value = data?.items ?? []
  recentLoading.value = false
}

async function fetchTrending() {
  const { data } = await client.GET('/discover/trending')
  trendingItems.value = data?.items ?? []
  trendingLoading.value = false
}

async function fetchPopularMovies() {
  const { data } = await client.GET('/discover/popular-movies')
  popularMovies.value = data?.items ?? []
  moviesLoading.value = false
}

async function fetchPopularSeries() {
  const { data } = await client.GET('/discover/popular-series')
  popularSeries.value = data?.items ?? []
  seriesLoading.value = false
}

function goToMedia(item: MediaItem) {
  router.push({ name: 'media-detail', params: { id: item.id } })
}

function goToPreview(item: DiscoverItem) {
  const mediaId = libraryMediaId(item.source, item.externalId)
  if (mediaId !== undefined) {
    router.push({ name: 'media-detail', params: { id: mediaId } })
    return
  }
  router.push({
    name: 'media-preview',
    params: { source: item.source, externalId: item.externalId },
    query: { mediaType: item.mediaType },
  })
}

function getRecentPoster(item: MediaItem): string | null {
  if (item.metadata?.posterPath) {
    return posterUrl(item)
  }
  return null
}
</script>

<template>
  <div>
    <!-- Recently Added -->
    <section v-if="recentLoading || recentItems.length" class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Recently Added</h2>
      <div v-if="recentLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in recentItems"
          :key="item.id"
          class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/40 transition-colors duration-200 cursor-pointer"
          @click="goToMedia(item)"
        >
          <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center overflow-hidden relative">
            <img
              v-if="getRecentPoster(item)"
              :src="getRecentPoster(item)!"
              :alt="item.title"
              class="w-full h-full object-cover"
              loading="lazy"
            />
            <div class="absolute top-2 left-2 z-10 flex items-center gap-1">
              <span class="px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded"
                :class="item.mediaType === 'movie' ? 'bg-violet-600/90 text-violet-100' : 'bg-fuchsia-600/90 text-fuchsia-100'"
              >
                {{ item.mediaType }}
              </span>
              <span v-if="isRecentWatched(item)" class="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded bg-emerald-600/90 text-emerald-100">
                <Eye class="w-2.5 h-2.5" />
                seen
              </span>
            </div>
            <div v-if="item.metadata?.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
              &#9733; {{ item.metadata.rating.toFixed(1) }}
            </div>
          </div>
          <div class="p-3">
            <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
            <p class="text-xs text-gray-500 mt-1">{{ item.year }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Trending -->
    <section v-if="trendingLoading || trendingItems.length" class="mb-10">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold text-gray-100 tracking-tight">Trending This Week</h2>
        <router-link :to="{ name: 'discover-trending' }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">See more <ArrowRight class="w-3 h-3 inline-block ml-1" /></router-link>
      </div>
      <div v-if="trendingLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <DiscoverCard
          v-for="item in trendingItems"
          :key="`${item.source}-${item.externalId}`"
          :item="item"
          :in-library="isInLibrary(item.source, item.externalId)"
          :watched="isWatched(item.source, item.externalId)"
          @click="goToPreview(item)"
        />
      </div>
    </section>

    <!-- Popular Movies -->
    <section v-if="moviesLoading || popularMovies.length" class="mb-10">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold text-gray-100 tracking-tight">Popular Movies</h2>
        <router-link :to="{ name: 'discover-popular-movies' }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">See more <ArrowRight class="w-3 h-3 inline-block ml-1" /></router-link>
      </div>
      <div v-if="moviesLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <DiscoverCard
          v-for="item in popularMovies"
          :key="`${item.source}-${item.externalId}`"
          :item="item"
          :in-library="isInLibrary(item.source, item.externalId)"
          :watched="isWatched(item.source, item.externalId)"
          @click="goToPreview(item)"
        />
      </div>
    </section>

    <!-- Popular Series -->
    <section v-if="seriesLoading || popularSeries.length" class="mb-10">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold text-gray-100 tracking-tight">Popular Series</h2>
        <router-link :to="{ name: 'discover-popular-series' }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">See more <ArrowRight class="w-3 h-3 inline-block ml-1" /></router-link>
      </div>
      <div v-if="seriesLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <DiscoverCard
          v-for="item in popularSeries"
          :key="`${item.source}-${item.externalId}`"
          :item="item"
          :in-library="isInLibrary(item.source, item.externalId)"
          :watched="isWatched(item.source, item.externalId)"
          @click="goToPreview(item)"
        />
      </div>
    </section>

    <!-- Empty state when nothing loads -->
    <div v-if="!recentLoading && !trendingLoading && !moviesLoading && !seriesLoading && !recentItems.length && !trendingItems.length && !popularMovies.length && !popularSeries.length" class="flex flex-col items-center justify-center py-20 text-gray-500">
      <p class="text-lg">Nothing to show yet</p>
      <p class="text-sm mt-1">Add media to your libraries or configure your TMDB API key in settings</p>
    </div>
  </div>
</template>
