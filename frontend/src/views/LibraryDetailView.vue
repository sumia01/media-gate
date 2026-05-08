<script setup lang="ts">
import { Eye, Plus, RefreshCw, Sparkles } from 'lucide-vue-next'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import client from '@/api/client'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import { useEventStream } from '@/composables/useEventStream'
import { useGlobalSearch } from '@/composables/useGlobalSearch'
import { useJobQueue } from '@/composables/useJobQueue'
import type { Library, MediaItem, MediaProfile } from '@/types/api'
import { posterUrl } from '@/utils/media'

const route = useRoute()
const router = useRouter()
const { jobs, triggerSync, triggerMatch, hasActiveJob, onJobDone } = useJobQueue()
const { openSearch, setActiveLibrary } = useGlobalSearch()
const { on, off } = useEventStream()

const library = ref<Library | null>(null)
const profiles = ref<MediaProfile[]>([])
const items = ref<MediaItem[]>([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const showMatchModal = ref(false)

const watchedSet = ref<Set<string>>(new Set())

function watchedKey(source: string, externalId: number): string {
  return `${source}:${externalId}`
}

function isItemWatched(item: MediaItem): boolean {
  if (!item.metadata?.source || !item.metadata?.externalId) return false
  return watchedSet.value.has(watchedKey(item.metadata.source, item.metadata.externalId))
}

async function fetchWatched() {
  const { data } = await client.GET('/watched')
  const set = new Set<string>()
  for (const w of data?.items ?? []) {
    set.add(watchedKey(w.source, w.externalId))
  }
  watchedSet.value = set
}

function openAddSearch() {
  if (library.value) {
    setActiveLibrary(library.value.id, library.value.mediaType as 'movie' | 'series')
  }
  openSearch()
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
    (j) => j.libraryId === library.value!.id && j.type === 'match_library' && j.status === 'running' && j.progress,
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

async function onProfileChange(event: Event) {
  if (!library.value) return
  const val = (event.target as HTMLSelectElement).value
  const profileId = val ? Number(val) : undefined
  const { data } = await client.PUT('/libraries/{id}', {
    params: { path: { id: library.value.id } },
    body: {
      name: library.value.name,
      path: library.value.path,
      mediaType: library.value.mediaType,
      ...(profileId ? { mediaProfileId: profileId } : {}),
    },
  })
  if (data) library.value = data
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

async function loadAll() {
  const id = Number(route.params.id)
  const profileRes = await client.GET('/media-profiles')
  profiles.value = profileRes.data?.profiles ?? []
  await fetchLibrary(id)
  await Promise.all([fetchMedia(id), fetchWatched()])
}

function navigateToMedia(item: MediaItem) {
  router.push({ name: 'media-detail', params: { id: item.id } })
}

// SSE: real-time refresh when items are synced or matched in this library
function handleLibraryEvent(data: any) {
  if (library.value && data.libraryId === library.value.id) {
    fetchMedia(library.value.id)
  }
}

const libraryEvents = [
  'library.sync_completed',
  'library.match_completed',
  'media.item_synced',
  'media.item_matched',
  'media.item_deleted',
]

onMounted(() => {
  loadAll()
  for (const type of libraryEvents) {
    on(type, handleLibraryEvent)
  }
})
onUnmounted(() => {
  for (const type of libraryEvents) {
    off(type, handleLibraryEvent)
  }
})
watch(() => route.params.id, loadAll)
</script>

<template>
  <div>
    <!-- Header -->
    <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between mb-6">
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
      <div class="flex items-center gap-2 flex-wrap">
        <span v-if="library" class="text-xs text-gray-500 font-mono hidden md:inline">{{ library.path }}</span>
        <!-- Default profile select -->
        <select
          v-if="library && profiles.length"
          class="text-xs bg-[#161b2e] border border-violet-900/20 rounded-lg px-2.5 py-2 text-gray-300 focus:outline-none focus:border-violet-500/50 transition-colors duration-200"
          :value="library.mediaProfileId ?? ''"
          @change="onProfileChange"
        >
          <option value="">No profile</option>
          <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
        </select>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
          @click="openAddSearch"
        >
          <Plus class="w-4 h-4" />
          Add
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="isSyncingThisLibrary"
          @click="handleSync"
        >
          <RefreshCw class="w-4 h-4" :class="isSyncingThisLibrary ? 'animate-spin' : ''" />
          {{ isSyncingThisLibrary ? 'Syncing...' : 'Sync' }}
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg border border-violet-600/50 text-violet-300 hover:bg-violet-600/10 text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="isMatchingThisLibrary"
          @click="handleMatch"
        >
          <Sparkles class="w-4 h-4" :class="isMatchingThisLibrary ? 'animate-pulse' : ''" />
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

    <ErrorBanner :message="error" />

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
              v-if="item.status !== 'new'"
              :src="posterUrl(item)"
              :alt="item.title"
              class="w-full h-full object-cover"
              @error="($event.target as HTMLImageElement).style.display = 'none'"
            />
            <span v-if="item.status === 'new'" class="text-3xl text-gray-600">{{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}</span>
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
                  'bg-amber-600/20 text-amber-300': item.status === 'partial',
                }"
              >
                {{ item.status }}
              </span>
              <span v-if="isItemWatched(item)" class="inline-flex items-center gap-0.5 text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300">
                <Eye class="w-2.5 h-2.5" />
                seen
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Match Mode Modal -->
    <BaseModal
      v-if="showMatchModal"
      @close="showMatchModal = false"
    >
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
    </BaseModal>
  </div>
</template>
