<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import type { DiscoverItem } from '@/types/api'
import DiscoverCard from '@/components/media/DiscoverCard.vue'

const props = defineProps<{
  category: 'trending' | 'popular-movies' | 'popular-series'
}>()

const router = useRouter()

const items = ref<DiscoverItem[]>([])
const page = ref(0)
const totalPages = ref(1)
const loading = ref(false)
const initialLoading = ref(true)

const watchedSet = ref<Set<string>>(new Set())
const libraryMap = ref<Map<string, number>>(new Map())

const title = computed(() => {
  switch (props.category) {
    case 'trending': return 'Trending This Week'
    case 'popular-movies': return 'Popular Movies'
    case 'popular-series': return 'Popular Series'
  }
})

const endpoint = computed(() => {
  switch (props.category) {
    case 'trending': return '/discover/trending' as const
    case 'popular-movies': return '/discover/popular-movies' as const
    case 'popular-series': return '/discover/popular-series' as const
  }
})

function watchedKey(source: string, externalId: number): string {
  return `${source}:${externalId}`
}

function isWatched(source: string, externalId: number): boolean {
  return watchedSet.value.has(watchedKey(source, externalId))
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

async function fetchPage() {
  if (loading.value || page.value >= totalPages.value) return
  loading.value = true
  const nextPage = page.value + 1
  const { data } = await client.GET(endpoint.value, {
    params: { query: { page: nextPage } },
  })
  if (data) {
    items.value = [...items.value, ...data.items]
    page.value = data.page
    totalPages.value = data.totalPages
  }
  loading.value = false
  initialLoading.value = false
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

// Infinite scroll via IntersectionObserver
const sentinel = ref<HTMLElement | null>(null)
let observer: IntersectionObserver | null = null

onMounted(() => {
  fetchPage()
  fetchWatched()
  fetchLibraryItems()

  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0]?.isIntersecting) {
        fetchPage()
      }
    },
    { rootMargin: '200px' },
  )

  const check = () => {
    if (sentinel.value) {
      observer!.observe(sentinel.value)
    } else {
      requestAnimationFrame(check)
    }
  }
  check()
})

onUnmounted(() => {
  observer?.disconnect()
})

const hasMore = computed(() => page.value < totalPages.value)
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">{{ title }}</h1>
      <router-link :to="{ name: 'home' }" class="text-sm text-violet-400 hover:text-violet-300 transition-colors">
        &larr; Back
      </router-link>
    </div>

    <!-- Skeleton grid on initial load -->
    <div v-if="initialLoading" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
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
        <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
        Loading more...
      </div>
      <p v-else-if="!hasMore && items.length" class="text-gray-500 text-sm">No more items</p>
    </div>
  </div>
</template>
