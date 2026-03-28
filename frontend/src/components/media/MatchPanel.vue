<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type MediaItem = components['schemas']['MediaItem']
type MatchCandidate = components['schemas']['MatchCandidate']

const props = defineProps<{
  item: MediaItem
}>()

const emit = defineEmits<{
  close: []
  matched: []
}>()

const query = ref(props.item.title)
const source = ref<'tmdb' | 'tvdb'>('tmdb')
const candidates = ref<MatchCandidate[]>([])
const searching = ref(false)
const matching = ref(false)
const searchError = ref('')

watch(() => props.item, (newItem) => {
  query.value = newItem.title
  candidates.value = []
  searchError.value = ''
  search()
})

async function search() {
  searching.value = true
  searchError.value = ''
  const { data, error: err } = await client.GET('/media/{id}/search', {
    params: {
      path: { id: props.item.id },
      query: { query: query.value, source: source.value },
    },
  })
  searching.value = false
  if (err) {
    searchError.value = 'Search failed'
    return
  }
  candidates.value = data?.candidates ?? []
}

async function selectCandidate(c: MatchCandidate) {
  matching.value = true
  const { error: err } = await client.POST('/media/{id}/match', {
    params: { path: { id: props.item.id } },
    body: { source: c.source, externalId: c.externalId },
  })
  matching.value = false
  if (!err) {
    emit('matched')
  }
}

async function unmatch() {
  matching.value = true
  const { error: err } = await client.DELETE('/media/{id}/match', {
    params: { path: { id: props.item.id } },
  })
  matching.value = false
  if (!err) {
    emit('matched')
  }
}

function confidenceColor(confidence: number): string {
  if (confidence >= 0.8) return 'bg-emerald-500/20 text-emerald-300 border-emerald-500/30'
  if (confidence >= 0.5) return 'bg-yellow-500/20 text-yellow-300 border-yellow-500/30'
  return 'bg-red-500/20 text-red-300 border-red-500/30'
}

onMounted(search)
</script>

<template>
  <!-- Backdrop -->
  <div class="fixed inset-0 z-40 bg-black/60" @click="emit('close')"></div>

  <!-- Panel -->
  <div class="fixed inset-y-0 right-0 z-50 w-full max-w-lg bg-[#0c0f1a] border-l border-violet-900/30 overflow-y-auto">
    <div class="p-6">
      <!-- Header -->
      <div class="flex items-center justify-between mb-6">
        <div>
          <h2 class="text-lg font-semibold text-gray-100">{{ item.title }}</h2>
          <p class="text-xs text-gray-500 mt-0.5">
            {{ item.mediaType }} <span v-if="item.year">&#183; {{ item.year }}</span>
          </p>
        </div>
        <button
          class="text-gray-500 hover:text-gray-300 transition-colors duration-200"
          @click="emit('close')"
        >
          &#x2715;
        </button>
      </div>

      <!-- Unmatch button if has metadata -->
      <div v-if="item.metadata" class="mb-6">
        <div class="px-4 py-3 rounded-lg bg-emerald-500/5 border border-emerald-500/20">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm text-emerald-300 font-medium">{{ item.metadata.title }}</p>
              <p class="text-xs text-gray-500 mt-0.5">
                Matched via {{ item.metadata.source.toUpperCase() }}
                &#183; {{ Math.round(item.metadata.confidence * 100) }}% confidence
              </p>
            </div>
            <button
              class="px-3 py-1.5 rounded-lg border border-red-500/30 text-xs text-red-400 hover:bg-red-500/10 transition-colors duration-200"
              :disabled="matching"
              @click="unmatch"
            >
              Unmatch
            </button>
          </div>
        </div>
      </div>

      <!-- Search -->
      <div class="space-y-3 mb-6">
        <div class="flex gap-2">
          <select
            v-model="source"
            class="px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          >
            <option value="tmdb">TMDB</option>
            <option value="tvdb">TVDB</option>
          </select>
          <input
            v-model="query"
            placeholder="Search..."
            class="flex-1 px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @keyup.enter="search"
          />
          <button
            class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50"
            :disabled="searching"
            @click="search"
          >
            {{ searching ? '...' : 'Search' }}
          </button>
        </div>
      </div>

      <!-- Error -->
      <div
        v-if="searchError"
        class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
      >
        {{ searchError }}
      </div>

      <!-- Loading -->
      <div v-if="searching" class="text-gray-500 text-sm py-8 text-center">Searching...</div>

      <!-- Results -->
      <div v-else-if="candidates.length" class="space-y-3">
        <div
          v-for="c in candidates"
          :key="`${c.source}-${c.externalId}`"
          class="flex gap-3 p-3 rounded-lg bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/30 transition-colors duration-200"
        >
          <!-- Poster thumbnail -->
          <div class="w-16 h-24 flex-shrink-0 rounded overflow-hidden bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20">
            <img
              v-if="c.posterUrl"
              :src="c.posterUrl"
              :alt="c.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
          </div>

          <!-- Info -->
          <div class="flex-1 min-w-0">
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-200 truncate">{{ c.title }}</p>
                <div class="flex items-center gap-2 mt-0.5">
                  <span v-if="c.year" class="text-xs text-gray-500">{{ c.year }}</span>
                  <span
                    class="text-[10px] font-bold px-1.5 py-0.5 rounded-full border"
                    :class="confidenceColor(c.confidence)"
                  >
                    {{ Math.round(c.confidence * 100) }}%
                  </span>
                </div>
              </div>
              <button
                class="flex-shrink-0 px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-xs font-medium transition-colors duration-200 disabled:opacity-50"
                :disabled="matching"
                @click="selectCandidate(c)"
              >
                Select
              </button>
            </div>
            <p v-if="c.overview" class="text-xs text-gray-500 mt-1.5 line-clamp-2">{{ c.overview }}</p>
          </div>
        </div>
      </div>

      <!-- No results -->
      <div
        v-else-if="!searching"
        class="text-gray-500 text-sm py-8 text-center"
      >
        No results found. Try a different search.
      </div>
    </div>
  </div>
</template>
