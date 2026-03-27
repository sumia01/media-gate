<script setup lang="ts">
import { ref, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useJobQueue } from '@/composables/useJobQueue'

type Library = components['schemas']['Library']
type MediaItem = components['schemas']['MediaItem']

const route = useRoute()
const { triggerSync, hasActiveJob } = useJobQueue()

const library = ref<Library | null>(null)
const items = ref<MediaItem[]>([])
const total = ref(0)
const loading = ref(false)
const syncing = ref(false)
const error = ref('')

async function fetchLibrary(id: number) {
  const { data } = await client.GET('/libraries/{id}', {
    params: { path: { id } },
  })
  if (data) library.value = data
}

async function fetchMedia(id: number) {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/libraries/{id}/media', {
    params: { path: { id } },
  })
  loading.value = false
  if (err) {
    error.value = 'Failed to load media items'
    return
  }
  items.value = data?.items ?? []
  total.value = data?.total ?? 0
}

async function handleSync() {
  if (!library.value) return
  syncing.value = true
  await triggerSync(library.value.id)
  syncing.value = false
}

async function loadAll() {
  const id = Number(route.params.id)
  await fetchLibrary(id)
  await fetchMedia(id)
}

watch(hasActiveJob, (active, wasActive) => {
  if (!active && wasActive && library.value) {
    fetchMedia(library.value.id)
  }
})

onMounted(loadAll)
watch(() => route.params.id, loadAll)
</script>

<template>
  <div>
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <div v-if="library" class="flex items-center gap-3">
        <h1 class="text-xl font-semibold text-gray-100 tracking-tight">{{ library.name }}</h1>
        <span
          class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
          :class="library.mediaType === 'movie'
            ? 'bg-violet-600/20 text-violet-300'
            : 'bg-fuchsia-600/20 text-fuchsia-300'"
        >
          {{ library.mediaType }}
        </span>
      </div>
      <div class="flex items-center gap-3">
        <span v-if="library" class="text-xs text-gray-500 font-mono">{{ library.path }}</span>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="syncing"
          @click="handleSync"
        >
          <span class="text-base leading-none" :class="syncing ? 'animate-spin' : ''">&#x21bb;</span>
          {{ syncing ? 'Syncing...' : 'Sync' }}
        </button>
      </div>
    </div>

    <!-- Error -->
    <div
      v-if="error"
      class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
    >
      {{ error }}
    </div>

    <!-- Loading -->
    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <!-- Empty state -->
    <div
      v-else-if="!items.length"
      class="flex flex-col items-center justify-center py-20 text-gray-500"
    >
      <span class="text-4xl mb-3">&#128218;</span>
      <p class="text-sm">No media items yet. Click Sync to scan the library folder.</p>
    </div>

    <!-- Media grid -->
    <div v-else>
      <p class="text-xs text-gray-500 mb-4">{{ total }} items</p>
      <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <div
          v-for="item in items"
          :key="item.id"
          class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/30 transition-colors duration-200"
        >
          <!-- Poster placeholder -->
          <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center">
            <span class="text-3xl text-gray-600">{{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}</span>
          </div>
          <!-- Info -->
          <div class="p-3">
            <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
            <div class="flex items-center gap-2 mt-1">
              <span v-if="item.year" class="text-xs text-gray-500">{{ item.year }}</span>
              <span
                class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full"
                :class="{
                  'bg-emerald-600/20 text-emerald-300': item.status === 'matched',
                  'bg-yellow-600/20 text-yellow-300': item.status === 'new',
                  'bg-red-600/20 text-red-300': item.status === 'missing',
                }"
              >
                {{ item.status }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
