<script setup lang="ts">
import { ArrowLeft } from 'lucide-vue-next'
import { computed, onMounted, watch } from 'vue'
import client from '@/api/client'
import DiscoverCard from '@/components/media/DiscoverCard.vue'
import DiscoverFilter from '@/components/media/DiscoverFilter.vue'
import DiscoverResultsStatus from '@/components/media/DiscoverResultsStatus.vue'
import { discoverKey, useDiscoverFilter } from '@/composables/useDiscoverFilter'
import { usePagedDiscover } from '@/composables/usePagedDiscover'
import { useWatchedLibrary } from '@/composables/useWatchedLibrary'
import type { DiscoverItem } from '@/types/api'

const props = defineProps<{
  category: 'trending' | 'popular-movies' | 'popular-series'
}>()

const title = computed(() => {
  switch (props.category) {
    case 'trending':
      return 'Trending This Week'
    case 'popular-movies':
      return 'Popular Movies'
    case 'popular-series':
      return 'Popular Series'
  }
})

const endpoint = computed(() => {
  switch (props.category) {
    case 'trending':
      return '/discover/trending' as const
    case 'popular-movies':
      return '/discover/popular-movies' as const
    case 'popular-series':
      return '/discover/popular-series' as const
  }
})

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

const { items, hiddenCount, loading, initialLoading, loadFailed, hasMore, sentinel, fetchPage, reset } =
  usePagedDiscover<DiscoverItem>(
    async (page, signal) => {
      const { data } = await client.GET(endpoint.value, {
        params: { query: { page } },
        signal,
      })
      return data
    },
    discoverKey,
    { include: includeItem, ready: () => filterReady.value, filterKey: () => hideInLibrary.value },
  )

watch(() => props.category, reset, { flush: 'post' })

onMounted(() => {
  fetchWatched()
  fetchLibraryItems()
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">{{ title }}</h1>
      <router-link :to="{ name: 'home' }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">
        <ArrowLeft class="w-4 h-4 inline" /> Back
      </router-link>
    </div>

    <DiscoverFilter
      v-model:hide-in-library="hideInLibrary"
      :library-loading="libraryLoading"
      :library-failed="libraryFailed"
      @retry="fetchLibraryItems"
    />

    <!-- Skeleton grid on initial load -->
    <div v-if="(initialLoading && filterReady) || (hideInLibrary && libraryLoading)" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
      <div v-for="n in 20" :key="n" class="animate-pulse">
        <div class="aspect-[2/3] rounded-lg bg-white/5" />
        <div class="mt-2 h-4 w-3/4 rounded bg-white/5" />
        <div class="mt-1 h-3 w-1/3 rounded bg-white/5" />
      </div>
    </div>

    <!-- Items grid -->
    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
      <DiscoverCard
        v-for="item in items"
        :key="discoverKey(item)"
        :item="item"
        :in-library="isInLibrary(item)"
        :watched="isWatched(item)"
        @click="goToPreview(item)"
      />
    </div>

    <div ref="sentinel">
      <DiscoverResultsStatus
        :ready="filterReady"
        :loading="loading"
        :initial-loading="initialLoading"
        :load-failed="loadFailed"
        :has-more="hasMore"
        :item-count="items.length"
        :hidden-count="hiddenCount"
        @load-more="fetchPage"
      />
    </div>
  </div>
</template>
