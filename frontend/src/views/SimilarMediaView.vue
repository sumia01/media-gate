<script setup lang="ts">
import { ArrowLeft, Loader2 } from 'lucide-vue-next'
import { computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import DiscoverCard from '@/components/media/DiscoverCard.vue'
import { usePagedDiscover } from '@/composables/usePagedDiscover'
import { useWatchedLibrary } from '@/composables/useWatchedLibrary'
import type { DiscoverItem } from '@/types/api'

const props = defineProps<{
  source: string
  externalId: string
}>()

const route = useRoute()
const router = useRouter()

const sourceParam = computed(() => (props.source === 'tvdb' ? 'tvdb' : 'tmdb') as 'tmdb' | 'tvdb')
const mediaType = computed(() => (route.query.mediaType === 'series' ? 'series' : 'movie') as 'movie' | 'series')

const title = computed(() => {
  const t = route.query.title
  return typeof t === 'string' && t ? `Similar to ${t}` : 'Similar Titles'
})

const { isWatched, isInLibrary, fetchWatched, fetchLibraryItems, goToPreview } = useWatchedLibrary()

const { items, loading, initialLoading, loadFailed, hasMore, sentinel, fetchPage, reset } =
  usePagedDiscover<DiscoverItem>(
    async (page) => {
      const { data } = await client.GET('/discover/similar/{source}/{externalId}', {
        params: {
          path: { source: sourceParam.value, externalId: Number(props.externalId) },
          query: { mediaType: mediaType.value, page },
        },
      })
      return data
    },
    (item) => `${item.source}:${item.externalId}`,
  )

// Refetch when the route points at a different origin title while this
// component instance is reused. The route-name guard keeps the watcher from
// firing on the way OUT of the page, when route.query already belongs to the
// next route but this component is still mounted.
watch(
  () => `${props.source}:${props.externalId}:${mediaType.value}`,
  () => {
    if (route.name !== 'discover-similar') return
    reset()
  },
)

onMounted(() => {
  fetchWatched()
  fetchLibraryItems()
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">{{ title }}</h1>
      <button
        class="text-sm text-violet-400 hover:text-violet-300 transition-colors"
        @click="router.back()"
      >
        <ArrowLeft class="w-4 h-4 inline" /> Back
      </button>
    </div>

    <!-- Skeleton grid on initial load -->
    <div v-if="initialLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
      <div v-for="n in 20" :key="n" class="animate-pulse">
        <div class="aspect-[2/3] rounded-lg bg-white/5" />
        <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
        <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
      </div>
    </div>

    <!-- Empty state -->
    <div v-else-if="!items.length && !loadFailed" class="text-center py-16">
      <p class="text-gray-400 text-sm">No similar titles found</p>
      <p class="text-gray-600 text-xs mt-1">TMDB returned no suggestions for this title — this also happens when no TMDB API key is configured</p>
    </div>

    <!-- Items grid -->
    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
      <DiscoverCard
        v-for="item in items"
        :key="`${item.source}-${item.externalId}`"
        :item="item"
        :in-library="isInLibrary(item.source, item.externalId)"
        :watched="isWatched(item.source, item.externalId)"
        @click="goToPreview(item)"
      />
    </div>

    <!-- Sentinel + loading spinner for infinite scroll -->
    <div ref="sentinel" class="flex justify-center py-8">
      <div v-if="loading && !initialLoading" class="flex items-center gap-2 text-gray-400 text-sm">
        <Loader2 class="w-5 h-5 animate-spin" />
        Loading more...
      </div>
      <button
        v-else-if="loadFailed"
        class="text-sm text-red-400 hover:text-red-300 transition-colors"
        @click="fetchPage()"
      >
        Failed to load — click to retry
      </button>
      <p v-else-if="!hasMore && items.length" class="text-gray-500 text-sm">No more items</p>
    </div>
  </div>
</template>
