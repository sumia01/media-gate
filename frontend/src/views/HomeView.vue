<script setup lang="ts">
import { ArrowRight, Eye } from 'lucide-vue-next'
import { onMounted, onUnmounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import DiscoverCard from '@/components/media/DiscoverCard.vue'
import DiscoverFilter from '@/components/media/DiscoverFilter.vue'
import DiscoverResultsStatus from '@/components/media/DiscoverResultsStatus.vue'
import EpisodeTimeline from '@/components/media/EpisodeTimeline.vue'
import { discoverKey, useDiscoverFilter } from '@/composables/useDiscoverFilter'
import { usePagedDiscover } from '@/composables/usePagedDiscover'
import { useWatchedLibrary } from '@/composables/useWatchedLibrary'
import type { DiscoverItem, MediaItem } from '@/types/api'
import { posterUrl } from '@/utils/media'

const router = useRouter()

const recentItems = ref<MediaItem[]>([])
const recentLoading = ref(true)
const recentFailed = ref(false)
const controller = new AbortController()
onUnmounted(() => controller.abort())

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

const sections = [
  { title: 'Trending This Week', route: 'discover-trending', endpoint: '/discover/trending' },
  { title: 'Popular Movies', route: 'discover-popular-movies', endpoint: '/discover/popular-movies' },
  { title: 'Popular Series', route: 'discover-popular-series', endpoint: '/discover/popular-series' },
] as const
const feeds = sections.map((section) =>
  reactive({
    ...section,
    ...usePagedDiscover<DiscoverItem>(
      async (page, signal) => {
        const { data } = await client.GET(section.endpoint, { params: { query: { page } }, signal })
        return data
      },
      discoverKey,
      {
        include: includeItem,
        ready: () => filterReady.value,
        filterKey: () => hideInLibrary.value,
        infiniteScroll: false,
      },
    ),
  }),
)

function isRecentWatched(item: MediaItem): boolean {
  if (!item.metadata?.source || !item.metadata?.externalId) return false
  return isWatched({ source: item.metadata.source, externalId: item.metadata.externalId, mediaType: item.mediaType })
}

onMounted(() => {
  fetchRecent()
  fetchWatched()
  fetchLibraryItems()
})

async function fetchRecent() {
  recentLoading.value = true
  recentFailed.value = false
  try {
    const { data } = await client.GET('/discover/recently-added', { signal: controller.signal })
    if (controller.signal.aborted) return
    if (!data) throw new Error('Recently added unavailable')
    recentItems.value = data.items
  } catch {
    if (!controller.signal.aborted) recentFailed.value = true
  } finally {
    if (!controller.signal.aborted) recentLoading.value = false
  }
}

function goToMedia(item: MediaItem) {
  router.push({ name: 'media-detail', params: { id: item.id } })
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
    <EpisodeTimeline class="mb-8" />

    <!-- Recently Added -->
    <section v-if="recentLoading || recentItems.length || recentFailed" class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Recently Added</h2>
      <div v-if="recentLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else-if="recentFailed" role="alert" class="text-sm text-amber-300">
        Could not load recently added titles.
        <button type="button" class="ml-2 underline underline-offset-2 hover:text-amber-200" @click="fetchRecent">Retry</button>
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

    <DiscoverFilter
      v-model:hide-in-library="hideInLibrary"
      :library-loading="libraryLoading"
      :library-failed="libraryFailed"
      @retry="fetchLibraryItems"
    />

    <section v-for="feed in feeds" :key="feed.route" class="mb-10">
      <div class="flex items-center justify-between mb-4">
        <h2 class="text-lg font-semibold text-gray-100 tracking-tight">{{ feed.title }}</h2>
        <router-link :to="{ name: feed.route }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">See more <ArrowRight class="w-3 h-3 inline-block ml-1" /></router-link>
      </div>
      <div v-if="(feed.initialLoading && filterReady) || (hideInLibrary && libraryLoading)" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div v-for="n in 7" :key="n" class="animate-pulse">
          <div class="aspect-[2/3] rounded-lg bg-white/5" />
          <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
          <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
        </div>
      </div>
      <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <DiscoverCard
          v-for="item in feed.items"
          :key="discoverKey(item)"
          :item="item"
          :in-library="isInLibrary(item)"
          :watched="isWatched(item)"
          @click="goToPreview(item)"
        />
      </div>
      <DiscoverResultsStatus
        :ready="filterReady"
        :loading="feed.loading"
        :initial-loading="feed.initialLoading"
        :load-failed="feed.loadFailed"
        :has-more="feed.hasMore"
        :item-count="feed.items.length"
        :hidden-count="feed.hiddenCount"
        @load-more="feed.fetchPage"
      />
    </section>
  </div>
</template>
