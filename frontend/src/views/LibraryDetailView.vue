<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useJobQueue } from '@/composables/useJobQueue'
import { useGlobalSearch } from '@/composables/useGlobalSearch'
import AddMediaSearch from '@/components/media/AddMediaSearch.vue'

type Library = components['schemas']['Library']
type MediaItem = components['schemas']['MediaItem']

const route = useRoute()
const router = useRouter()
const { jobs, triggerSync, triggerMatch, hasActiveJob, onJobDone } = useJobQueue()
const { searchOpen, closeSearch } = useGlobalSearch()

const library = ref<Library | null>(null)
const items = ref<MediaItem[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const showAddSearch = ref(false)
const showMatchModal = ref(false)

// Sync global search bar with local add-media panel
watch(searchOpen, (open) => {
  if (open) showAddSearch.value = true
})

function closeAddSearch() {
  showAddSearch.value = false
  closeSearch()
}

const isSyncingThisLibrary = computed(() =>
  library.value
    ? jobs.value.some(
        (j) =>
          j.libraryId === library.value!.id &&
          j.type === 'sync_library' &&
          (j.status === 'pending' || j.status === 'running'),
      )
    : false,
)

const isMatchingThisLibrary = computed(() =>
  library.value
    ? jobs.value.some(
        (j) =>
          j.libraryId === library.value!.id &&
          j.type === 'match_library' &&
          (j.status === 'pending' || j.status === 'running'),
      )
    : false,
)

const matchProgress = computed(() => {
  if (!library.value) return null
  const job = jobs.value.find(
    (j) =>
      j.libraryId === library.value!.id &&
      j.type === 'match_library' &&
      j.status === 'running' &&
      j.progress,
  )
  return job?.progress ?? null
})

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
  await triggerSync(library.value.id)
}

async function handleMatch() {
  if (!library.value) return
  showMatchModal.value = true
}

async function handleMatchChoice(fullRematch: boolean) {
  showMatchModal.value = false
  if (!library.value) return
  await triggerMatch(library.value.id, fullRematch)
}

function posterUrl(item: MediaItem): string {
  const ts = new Date(item.updatedAt).getTime()
  return `/api/v1/media/${item.id}/poster?t=${ts}`
}

async function loadAll() {
  const id = Number(route.params.id)
  await fetchLibrary(id)
  await fetchMedia(id)
}

function navigateToMedia(item: MediaItem) {
  router.push({ name: 'media-detail', params: { id: item.id } })
}

function handleMediaAdded() {
  if (library.value) fetchMedia(library.value.id)
}

// Reload media items when this library's jobs finish
const removeJobDoneListener = onJobDone((libraryId, jobType) => {
  if (library.value && library.value.id === libraryId) {
    fetchMedia(library.value.id)
  }
})

onMounted(loadAll)
onUnmounted(removeJobDoneListener)
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
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
          @click="showAddSearch = true"
        >
          <span class="text-base leading-none">+</span>
          Add
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="isSyncingThisLibrary"
          @click="handleSync"
        >
          <span class="text-base leading-none" :class="isSyncingThisLibrary ? 'animate-spin' : ''">&#x21bb;</span>
          {{ isSyncingThisLibrary ? 'Syncing...' : 'Sync' }}
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg border border-violet-600/50 text-violet-300 hover:bg-violet-600/10 text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="isMatchingThisLibrary"
          @click="handleMatch"
        >
          <span class="text-base leading-none" :class="isMatchingThisLibrary ? 'animate-pulse' : ''">&#x2728;</span>
          <template v-if="isMatchingThisLibrary && matchProgress">
            Matching {{ matchProgress.current }}/{{ matchProgress.total }}...
          </template>
          <template v-else-if="isMatchingThisLibrary">
            Matching...
          </template>
          <template v-else>
            Match
          </template>
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
          class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/30 transition-colors duration-200 cursor-pointer"
          @click="navigateToMedia(item)"
        >
          <!-- Poster -->
          <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center overflow-hidden">
            <img
              v-if="item.status === 'available' || item.status === 'requested'"
              :src="posterUrl(item)"
              :alt="item.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
            <span v-if="item.status !== 'available' && item.status !== 'requested'" class="text-3xl text-gray-600">{{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}</span>
          </div>
          <!-- Info -->
          <div class="p-3">
            <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
            <div class="flex items-center gap-2 mt-1">
              <span v-if="item.year" class="text-xs text-gray-500">{{ item.year }}</span>
              <span
                class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full"
                :class="{
                  'bg-emerald-600/20 text-emerald-300': item.status === 'available',
                  'bg-yellow-600/20 text-yellow-300': item.status === 'new',
                  'bg-red-600/20 text-red-300': item.status === 'missing',
                  'bg-sky-600/20 text-sky-300': item.status === 'requested',
                }"
              >
                {{ item.status }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Add Media Search -->
    <Teleport to="body">
      <AddMediaSearch
        v-if="showAddSearch && library"
        :library-id="library.id"
        :media-type="library.mediaType"
        @added="handleMediaAdded"
        @close="closeAddSearch"
      />
    </Teleport>

    <!-- Match Mode Modal -->
    <Teleport to="body">
      <div
        v-if="showMatchModal"
        class="fixed inset-0 z-50 flex items-center justify-center"
      >
        <div class="absolute inset-0 bg-black/60" @click="showMatchModal = false"></div>
        <div class="relative bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 w-full max-w-sm shadow-2xl">
          <h3 class="text-base font-semibold text-gray-100 mb-4">Match Mode</h3>
          <div class="space-y-3">
            <button
              class="w-full text-left px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/40 transition-colors duration-200"
              @click="handleMatchChoice(false)"
            >
              <p class="text-sm font-medium text-gray-200">Unmatched only</p>
              <p class="text-xs text-gray-500 mt-0.5">Match items that don't have metadata yet</p>
            </button>
            <button
              class="w-full text-left px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/40 transition-colors duration-200"
              @click="handleMatchChoice(true)"
            >
              <p class="text-sm font-medium text-gray-200">Full re-match</p>
              <p class="text-xs text-gray-500 mt-0.5">Re-match all items, replacing existing metadata</p>
            </button>
          </div>
          <button
            class="mt-4 w-full text-center text-xs text-gray-500 hover:text-gray-400 transition-colors duration-200"
            @click="showMatchModal = false"
          >
            Cancel
          </button>
        </div>
      </div>
    </Teleport>
  </div>
</template>
