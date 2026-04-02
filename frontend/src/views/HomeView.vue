<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import type { MediaItem, DiscoverItem } from '@/types/api'
import { posterUrl } from '@/utils/media'

const router = useRouter()

const recentItems = ref<MediaItem[]>([])
const trendingItems = ref<DiscoverItem[]>([])
const popularMovies = ref<DiscoverItem[]>([])
const popularSeries = ref<DiscoverItem[]>([])

const recentLoading = ref(true)
const trendingLoading = ref(true)
const moviesLoading = ref(true)
const seriesLoading = ref(true)

onMounted(() => {
  fetchRecent()
  fetchTrending()
  fetchPopularMovies()
  fetchPopularSeries()
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
          <div class="aspect-[2/3] rounded-xl bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in recentItems"
          :key="item.id"
          class="group relative cursor-pointer transition-all duration-300 hover:shadow-[0_0_24px_rgba(139,92,246,0.3)] hover:scale-[1.03]"
          @click="goToMedia(item)"
        >
          <div class="aspect-[2/3] w-full rounded-xl overflow-hidden relative bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
            <img
              v-if="getRecentPoster(item)"
              :src="getRecentPoster(item)!"
              :alt="item.title"
              class="absolute inset-0 w-full h-full object-cover"
              loading="lazy"
            />
            <span class="absolute top-2 left-2 z-10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded"
              :class="item.mediaType === 'movie' ? 'bg-violet-600/90 text-violet-100' : 'bg-fuchsia-600/90 text-fuchsia-100'"
            >
              {{ item.mediaType }}
            </span>
            <div v-if="item.metadata?.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
              &#9733; {{ item.metadata.rating.toFixed(1) }}
            </div>
            <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/70 to-transparent rounded-b-xl" />
          </div>
          <div class="mt-2 px-0.5">
            <p class="text-sm font-medium text-gray-100 truncate">{{ item.title }}</p>
            <p class="text-xs text-gray-500">{{ item.year }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Trending -->
    <section v-if="trendingLoading || trendingItems.length" class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Trending This Week</h2>
      <div v-if="trendingLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-xl bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in trendingItems"
          :key="`${item.source}-${item.externalId}`"
          class="group relative cursor-pointer transition-all duration-300 hover:shadow-[0_0_24px_rgba(139,92,246,0.3)] hover:scale-[1.03]"
          @click="goToPreview(item)"
        >
          <div class="aspect-[2/3] w-full rounded-xl overflow-hidden relative bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
            <img
              v-if="item.posterUrl"
              :src="item.posterUrl"
              :alt="item.title"
              class="absolute inset-0 w-full h-full object-cover"
              loading="lazy"
            />
            <span class="absolute top-2 left-2 z-10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded"
              :class="item.mediaType === 'movie' ? 'bg-violet-600/90 text-violet-100' : 'bg-fuchsia-600/90 text-fuchsia-100'"
            >
              {{ item.mediaType }}
            </span>
            <div v-if="item.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
              &#9733; {{ item.rating.toFixed(1) }}
            </div>
            <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/70 to-transparent rounded-b-xl" />
          </div>
          <div class="mt-2 px-0.5">
            <p class="text-sm font-medium text-gray-100 truncate">{{ item.title }}</p>
            <p class="text-xs text-gray-500">{{ item.year }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Popular Movies -->
    <section v-if="moviesLoading || popularMovies.length" class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Popular Movies</h2>
      <div v-if="moviesLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-xl bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in popularMovies"
          :key="`${item.source}-${item.externalId}`"
          class="group relative cursor-pointer transition-all duration-300 hover:shadow-[0_0_24px_rgba(139,92,246,0.3)] hover:scale-[1.03]"
          @click="goToPreview(item)"
        >
          <div class="aspect-[2/3] w-full rounded-xl overflow-hidden relative bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
            <img
              v-if="item.posterUrl"
              :src="item.posterUrl"
              :alt="item.title"
              class="absolute inset-0 w-full h-full object-cover"
              loading="lazy"
            />
            <span class="absolute top-2 left-2 z-10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded bg-violet-600/90 text-violet-100">
              movie
            </span>
            <div v-if="item.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
              &#9733; {{ item.rating.toFixed(1) }}
            </div>
            <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/70 to-transparent rounded-b-xl" />
          </div>
          <div class="mt-2 px-0.5">
            <p class="text-sm font-medium text-gray-100 truncate">{{ item.title }}</p>
            <p class="text-xs text-gray-500">{{ item.year }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Popular Series -->
    <section v-if="seriesLoading || popularSeries.length" class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Popular Series</h2>
      <div v-if="seriesLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-xl bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in popularSeries"
          :key="`${item.source}-${item.externalId}`"
          class="group relative cursor-pointer transition-all duration-300 hover:shadow-[0_0_24px_rgba(139,92,246,0.3)] hover:scale-[1.03]"
          @click="goToPreview(item)"
        >
          <div class="aspect-[2/3] w-full rounded-xl overflow-hidden relative bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
            <img
              v-if="item.posterUrl"
              :src="item.posterUrl"
              :alt="item.title"
              class="absolute inset-0 w-full h-full object-cover"
              loading="lazy"
            />
            <span class="absolute top-2 left-2 z-10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded bg-fuchsia-600/90 text-fuchsia-100">
              series
            </span>
            <div v-if="item.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
              &#9733; {{ item.rating.toFixed(1) }}
            </div>
            <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/70 to-transparent rounded-b-xl" />
          </div>
          <div class="mt-2 px-0.5">
            <p class="text-sm font-medium text-gray-100 truncate">{{ item.title }}</p>
            <p class="text-xs text-gray-500">{{ item.year }}</p>
          </div>
        </div>
      </div>
    </section>

    <!-- Empty state when nothing loads -->
    <div v-if="!recentLoading && !trendingLoading && !moviesLoading && !seriesLoading && !recentItems.length && !trendingItems.length && !popularMovies.length && !popularSeries.length" class="flex flex-col items-center justify-center py-20 text-gray-500">
      <p class="text-lg">Nothing to show yet</p>
      <p class="text-sm mt-1">Add media to your libraries or configure your TMDB API key in settings</p>
    </div>
  </div>
</template>
