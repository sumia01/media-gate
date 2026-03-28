<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useJobQueue } from '@/composables/useJobQueue'
import MatchPanel from '@/components/media/MatchPanel.vue'

type Library = components['schemas']['Library']
type MediaItem = components['schemas']['MediaItem']

const route = useRoute()
const router = useRouter()
const { onJobDone } = useJobQueue()

const item = ref<MediaItem | null>(null)
const library = ref<Library | null>(null)
const loading = ref(false)
const error = ref('')
const showMatchPanel = ref(false)

const metadata = computed(() => item.value?.metadata ?? null)

const genres = computed<string[]>(() => {
  if (!metadata.value?.genres) return []
  try {
    const parsed = JSON.parse(metadata.value.genres)
    if (Array.isArray(parsed)) return parsed
  } catch {
    // Fall back to comma-separated
    return metadata.value.genres.split(',').map((g: string) => g.trim()).filter(Boolean)
  }
  return []
})

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

function posterUrl(itemId: number): string {
  return `/api/v1/media/${itemId}/poster`
}

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
  }
}

async function fetchLibrary(id: number) {
  const { data } = await client.GET('/libraries/{id}', {
    params: { path: { id } },
  })
  if (data) library.value = data
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

const removeJobDoneListener = onJobDone((libraryId) => {
  if (item.value && item.value.libraryId === libraryId) {
    fetchItem(item.value.id)
  }
})

function loadAll() {
  const id = Number(route.params.id)
  fetchItem(id)
}

onMounted(loadAll)
onUnmounted(removeJobDoneListener)
watch(() => route.params.id, loadAll)
</script>

<template>
  <div>
    <!-- Back nav -->
    <router-link
      v-if="library"
      :to="{ name: 'library-detail', params: { id: library.id } }"
      class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-violet-300 transition-colors duration-200 mb-6"
    >
      <span class="text-base leading-none">&larr;</span>
      Back to {{ library.name }}
    </router-link>

    <!-- Error -->
    <div
      v-if="error"
      class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
    >
      {{ error }}
    </div>

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
              v-if="item.status === 'matched' || item.status === 'requested'"
              :src="posterUrl(item.id)"
              :alt="item.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
            <span v-if="item.status !== 'matched' && item.status !== 'requested'" class="text-6xl text-gray-600">
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
                'bg-emerald-600/20 text-emerald-300': item.status === 'matched',
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

          <!-- Match source info -->
          <div v-if="metadata" class="px-4 py-3 rounded-lg bg-emerald-500/5 border border-emerald-500/20 mb-6">
            <div class="flex items-center gap-3">
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
                class="text-xs text-violet-400 hover:text-violet-300 transition-colors duration-200 ml-auto"
              >
                View on {{ metadata.source.toUpperCase() }} &nearr;
              </a>
            </div>
          </div>

          <!-- Action buttons -->
          <div class="flex items-center gap-3">
            <button
              class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
              @click="openMatchPanel"
            >
              {{ item.status === 'matched' ? 'Re-match' : 'Match' }}
            </button>
            <button
              v-if="item.status === 'matched'"
              class="px-4 py-2 rounded-lg border border-red-500/30 text-red-400 hover:bg-red-500/10 text-sm font-medium transition-colors duration-200"
              @click="handleUnmatch"
            >
              Unmatch
            </button>
            <button
              v-if="item.source === 'request'"
              class="px-4 py-2 rounded-lg border border-red-500/30 text-red-400 hover:bg-red-500/10 text-sm font-medium transition-colors duration-200"
              @click="handleDelete"
            >
              Delete
            </button>
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
  </div>
</template>
