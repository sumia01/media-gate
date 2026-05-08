<script setup lang="ts">
import { Eye, X } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import ErrorBanner from '@/components/ErrorBanner.vue'
import type { WatchedItem } from '@/types/api'

const router = useRouter()
const items = ref<WatchedItem[]>([])
const loading = ref(false)
const error = ref('')

function itemPosterUrl(item: WatchedItem): string | null {
  // Library item with cached poster — use the media poster endpoint
  if (item.mediaItemId) {
    return `/api/v1/media/${item.mediaItemId}/poster`
  }
  // Fallback to external poster URL
  return externalPosterUrl(item.posterPath)
}

function externalPosterUrl(posterPath: string | undefined): string | null {
  if (!posterPath) return null
  if (posterPath.startsWith('http')) return posterPath
  if (posterPath.startsWith('/')) return `https://image.tmdb.org/t/p/w342${posterPath}`
  return null
}

async function fetchWatched() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/watched')
  loading.value = false
  if (err) {
    error.value = 'Failed to load watched list'
    return
  }
  items.value = data?.items ?? []
}

async function unmark(item: WatchedItem) {
  await client.DELETE('/watched/{id}', { params: { path: { id: item.id } } })
  items.value = items.value.filter((i) => i.id !== item.id)
}

function navigate(item: WatchedItem) {
  router.push({
    name: 'media-preview',
    params: { source: item.source, externalId: String(item.externalId) },
    query: { mediaType: item.mediaType },
  })
}

onMounted(fetchWatched)
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold text-gray-100 tracking-tight mb-6">Watched</h1>

    <ErrorBanner :message="error" />

    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <div v-else-if="items.length === 0" class="text-gray-500 text-sm">
      No watched items yet. Mark media as watched from the media detail or preview pages.
    </div>

    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
      <div
        v-for="item in items"
        :key="item.id"
        class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 cursor-pointer hover:border-violet-500/40 transition-all duration-200"
        @click="navigate(item)"
      >
        <!-- Poster -->
        <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center">
          <img
            v-if="itemPosterUrl(item)"
            :src="itemPosterUrl(item)!"
            :alt="item.title"
            class="w-full h-full object-cover"
            @error="(e) => {
              const img = e.target as HTMLImageElement
              const fallback = externalPosterUrl(item.posterPath)
              if (fallback && img.src !== fallback) { img.src = fallback }
              else { img.style.display = 'none' }
            }"
          />
          <span v-else class="text-4xl text-gray-600">
            {{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}
          </span>
        </div>

        <!-- Info -->
        <div class="p-2.5">
          <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
          <div class="flex items-center gap-2 mt-1">
            <span v-if="item.year" class="text-xs text-gray-500">{{ item.year }}</span>
            <span
              class="text-[9px] font-bold uppercase px-1.5 py-0.5 rounded-full"
              :class="item.mediaType === 'movie'
                ? 'bg-violet-600/20 text-violet-300'
                : 'bg-fuchsia-600/20 text-fuchsia-300'"
            >
              {{ item.mediaType }}
            </span>
          </div>
        </div>

        <!-- Unmark button (hover overlay) -->
        <button
          class="absolute top-2 right-2 p-1.5 rounded-lg bg-black/60 text-gray-400 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity duration-200"
          title="Remove from watched"
          @click.stop="unmark(item)"
        >
          <X class="w-4 h-4" />
        </button>

        <!-- Watched badge -->
        <div class="absolute top-2 left-2 flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-emerald-600/80 text-white text-[9px] font-medium">
          <Eye class="w-3 h-3" />
          Watched
        </div>
      </div>
    </div>
  </div>
</template>
