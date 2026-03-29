<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import AddToLibraryModal from '@/components/media/AddToLibraryModal.vue'

type ExternalMediaDetail = components['schemas']['ExternalMediaDetail']

const route = useRoute()
const router = useRouter()

const detail = ref<ExternalMediaDetail | null>(null)
const loading = ref(false)
const error = ref('')
const showAddModal = ref(false)

const genres = computed<string[]>(() => {
  if (!detail.value?.genres) return []
  try {
    const parsed = JSON.parse(detail.value.genres)
    if (Array.isArray(parsed)) return parsed
  } catch {
    return detail.value.genres.split(',').map((g: string) => g.trim()).filter(Boolean)
  }
  return []
})

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

function profileImageUrl(person: { image?: string }): string | null {
  if (!person.image) return null
  if (person.image.startsWith('/')) {
    return `https://image.tmdb.org/t/p/w185${person.image}`
  }
  if (person.image.startsWith('http')) {
    return person.image
  }
  return null
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
  if (data) detail.value = data
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
    <div class="flex items-center justify-between mb-6 gap-4">
      <button
        class="inline-flex items-center gap-1.5 text-sm text-gray-400 hover:text-violet-300 transition-colors duration-200 flex-shrink-0"
        @click="router.back()"
      >
        <span class="text-base leading-none">&larr;</span>
        Back
      </button>

      <button
        v-if="detail"
        class="flex items-center gap-2 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
        @click="showAddModal = true"
      >
        <span class="text-base leading-none">+</span>
        Add to Library
      </button>
    </div>

    <!-- Error -->
    <div
      v-if="error"
      class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
    >
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading && !detail" class="text-gray-500 text-sm">Loading...</div>

    <!-- Content -->
    <div v-else-if="detail">
      <!-- Hero section -->
      <div class="flex gap-8">
        <!-- Poster -->
        <div class="flex-shrink-0 w-[300px]">
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
          <div v-if="cast.length" class="mb-6">
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
          <div v-if="crew.length" class="mb-6">
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
          <div class="flex flex-wrap gap-3 mb-6">
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
        @added="handleAdded"
        @close="showAddModal = false"
      />
    </Teleport>
  </div>
</template>
