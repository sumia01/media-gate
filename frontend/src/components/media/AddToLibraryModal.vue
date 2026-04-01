<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue'
import client from '@/api/client'
import type { Library, MediaProfile, ExternalSeasonSummary } from '@/types/api'
import { useGlobalSearch } from '@/composables/useGlobalSearch'

const props = defineProps<{
  source: string
  externalId: number
  mediaType: string
  externalSeasons?: ExternalSeasonSummary[]
}>()

const emit = defineEmits<{
  added: [mediaItemId: number]
  close: []
}>()

const { activeLibraryId } = useGlobalSearch()

// Step: 'configure' = library + monitor + profile, 'seasons' = season/episode toggles
const step = ref<'configure' | 'seasons'>('configure')

const libraries = ref<Library[]>([])
const profiles = ref<MediaProfile[]>([])
const selectedLibraryId = ref<number | null>(null)
const monitored = ref(true)
const selectedProfileId = ref<number | null>(null)
const adding = ref(false)
const error = ref('')
const loadingLibraries = ref(false)

// Step 2 state (series only) — all client-side, no DB
const expandedSeasons = ref<Set<number>>(new Set())
const seasonMonitored = ref<Map<number, boolean>>(new Map())
const episodeMonitored = ref<Map<string, boolean>>(new Map())

const compatibleLibraries = computed(() =>
  libraries.value.filter((lib) => lib.mediaType === props.mediaType),
)

const isSeries = computed(() => props.mediaType === 'series')
const hasSeasons = computed(() => (props.externalSeasons?.length ?? 0) > 0)

// Initialize season/episode monitored maps when externalSeasons prop arrives
watch(() => props.externalSeasons, (seasons) => {
  if (!seasons?.length) return
  const sm = new Map<number, boolean>()
  const em = new Map<string, boolean>()
  for (const s of seasons) {
    sm.set(s.seasonNumber, true)
    for (const ep of s.episodes) {
      em.set(`${s.seasonNumber}-${ep.episodeNumber}`, true)
    }
  }
  seasonMonitored.value = sm
  episodeMonitored.value = em
}, { immediate: true })

onMounted(async () => {
  loadingLibraries.value = true
  const [libRes, profileRes] = await Promise.all([
    client.GET('/libraries'),
    client.GET('/media-profiles'),
  ])
  loadingLibraries.value = false
  libraries.value = libRes.data ?? []
  profiles.value = profileRes.data?.profiles ?? []

  if (activeLibraryId.value) {
    const match = compatibleLibraries.value.find((lib) => lib.id === activeLibraryId.value)
    if (match) selectedLibraryId.value = match.id
  }
  if (!selectedLibraryId.value && compatibleLibraries.value.length === 1) {
    selectedLibraryId.value = compatibleLibraries.value[0]!.id
  }
})

function goToSeasons() {
  step.value = 'seasons'
}

async function handleAdd() {
  if (!selectedLibraryId.value) return

  adding.value = true
  error.value = ''

  const body: {
    source: 'tmdb' | 'tvdb'
    externalId: number
    monitored?: boolean
    mediaProfileId?: number
    seasonMonitors?: { seasonNumber: number; monitored: boolean }[]
  } = {
    source: props.source as 'tmdb' | 'tvdb',
    externalId: props.externalId,
  }

  if (monitored.value) {
    body.monitored = true
    if (selectedProfileId.value) {
      body.mediaProfileId = selectedProfileId.value
    }
  }

  // For series: include season monitor selections
  if (isSeries.value && hasSeasons.value) {
    body.seasonMonitors = props.externalSeasons!.map(s => ({
      seasonNumber: s.seasonNumber,
      monitored: seasonMonitored.value.get(s.seasonNumber) ?? true,
    }))
  }

  const { data, error: err } = await client.POST('/libraries/{id}/media', {
    params: { path: { id: selectedLibraryId.value } },
    body,
  })

  adding.value = false

  if (err) {
    const errBody = err as { code?: number; message?: string }
    if (errBody.code === 409) {
      error.value = 'This media already exists in the selected library'
    } else {
      error.value = 'Failed to add media'
    }
    // Go back to configure step on error so user can retry
    if (step.value === 'seasons') step.value = 'configure'
    return
  }

  if (data) {
    emit('added', data.id)
  }
}

// Season toggle helpers

function toggleSeason(seasonNumber: number) {
  const s = new Set(expandedSeasons.value)
  if (s.has(seasonNumber)) s.delete(seasonNumber)
  else s.add(seasonNumber)
  expandedSeasons.value = s
}

function toggleSeasonMonitor(seasonNumber: number) {
  const current = seasonMonitored.value.get(seasonNumber) ?? true
  const newVal = !current
  seasonMonitored.value = new Map(seasonMonitored.value.set(seasonNumber, newVal))

  const season = props.externalSeasons?.find(s => s.seasonNumber === seasonNumber)
  if (season?.episodes) {
    for (const ep of season.episodes) {
      episodeMonitored.value.set(`${seasonNumber}-${ep.episodeNumber}`, newVal)
    }
    episodeMonitored.value = new Map(episodeMonitored.value)
  }
}

function toggleEpisodeMonitor(seasonNumber: number, episodeNumber: number) {
  const key = `${seasonNumber}-${episodeNumber}`
  const current = episodeMonitored.value.get(key) ?? true
  episodeMonitored.value = new Map(episodeMonitored.value.set(key, !current))
}

const allSeasonsMonitored = computed(() =>
  (props.externalSeasons?.length ?? 0) > 0 &&
  props.externalSeasons!.every(s => seasonMonitored.value.get(s.seasonNumber)),
)

function toggleAllSeasons() {
  const newVal = !allSeasonsMonitored.value
  const sm = new Map<number, boolean>()
  const em = new Map<string, boolean>()
  for (const s of props.externalSeasons ?? []) {
    sm.set(s.seasonNumber, newVal)
    for (const ep of s.episodes) {
      em.set(`${s.seasonNumber}-${ep.episodeNumber}`, newVal)
    }
  }
  seasonMonitored.value = sm
  episodeMonitored.value = em
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <div class="absolute inset-0 bg-black/60" @click="emit('close')"></div>
    <div
      class="relative bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 shadow-2xl"
      :class="step === 'seasons' ? 'w-full max-w-lg max-h-[80vh] flex flex-col' : 'w-full max-w-sm'"
    >
      <!-- Step 1: Configure -->
      <template v-if="step === 'configure'">
        <h3 class="text-base font-semibold text-gray-100 mb-4">Add to Library</h3>

        <!-- Loading -->
        <div v-if="loadingLibraries" class="py-6 text-center text-gray-500 text-sm">
          Loading libraries...
        </div>

        <!-- No compatible libraries -->
        <div v-else-if="!compatibleLibraries.length" class="py-6 text-center text-gray-500 text-sm">
          No {{ mediaType }} libraries found. Create one first.
        </div>

        <template v-else>
          <!-- Library list -->
          <div class="space-y-2 mb-4">
            <button
              v-for="lib in compatibleLibraries"
              :key="lib.id"
              class="w-full text-left px-4 py-3 rounded-lg border transition-colors duration-200"
              :class="selectedLibraryId === lib.id
                ? 'bg-violet-600/10 border-violet-500/40'
                : 'bg-[#161b2e] border-violet-900/20 hover:border-violet-500/30'"
              @click="selectedLibraryId = lib.id"
            >
              <div class="flex items-center justify-between">
                <div>
                  <p class="text-sm font-medium text-gray-200">{{ lib.name }}</p>
                  <p class="text-xs text-gray-500 mt-0.5 font-mono">{{ lib.path }}</p>
                </div>
                <div
                  v-if="selectedLibraryId === lib.id"
                  class="w-4 h-4 rounded-full bg-violet-600 flex items-center justify-center flex-shrink-0"
                >
                  <span class="text-white text-xs">&#10003;</span>
                </div>
              </div>
            </button>
          </div>

          <!-- Monitor toggle -->
          <div class="mb-3 px-1">
            <label class="flex items-center gap-3 cursor-pointer">
              <button
                class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0"
                :class="monitored ? 'bg-emerald-600' : 'bg-gray-600'"
                @click="monitored = !monitored"
              >
                <span
                  class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
                  :class="monitored ? 'translate-x-4' : ''"
                />
              </button>
              <span class="text-sm text-gray-300">Monitor</span>
            </label>
          </div>

          <!-- Quality profile (shown when monitored) -->
          <div v-if="monitored && profiles.length" class="mb-4 px-1">
            <label class="block text-xs text-gray-500 mb-1.5">Quality Profile</label>
            <select
              class="w-full text-sm bg-[#161b2e] border border-violet-900/20 rounded-lg px-3 py-2 text-gray-200 focus:outline-none focus:border-violet-500/50"
              :value="selectedProfileId ?? ''"
              @change="selectedProfileId = ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null"
            >
              <option value="">None</option>
              <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
          </div>
        </template>

        <!-- Error -->
        <div
          v-if="error"
          class="mb-3 px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-xs"
        >
          {{ error }}
        </div>

        <!-- Actions -->
        <div class="flex gap-3">
          <button
            v-if="isSeries && hasSeasons"
            class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="!selectedLibraryId"
            @click="goToSeasons"
          >
            Next
          </button>
          <button
            v-else
            class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="!selectedLibraryId || adding"
            @click="handleAdd"
          >
            {{ adding ? 'Adding...' : 'Add' }}
          </button>
          <button
            class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
            @click="emit('close')"
          >
            Cancel
          </button>
        </div>
      </template>

      <!-- Step 2: Season/Episode Toggles (series only) -->
      <template v-if="step === 'seasons'">
        <div class="flex items-center justify-between mb-4 flex-shrink-0">
          <h3 class="text-base font-semibold text-gray-100">Season Monitoring</h3>
          <button
            class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0"
            :class="allSeasonsMonitored ? 'bg-emerald-600' : 'bg-gray-600'"
            title="Toggle all seasons"
            @click="toggleAllSeasons"
          >
            <span
              class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
              :class="allSeasonsMonitored ? 'translate-x-4' : ''"
            />
          </button>
        </div>

        <div v-if="!externalSeasons?.length" class="py-6 text-center text-gray-500 text-sm">
          No season data available.
        </div>

        <div v-else class="flex-1 overflow-y-auto min-h-0 space-y-2 mb-4">
          <div
            v-for="season in externalSeasons"
            :key="season.seasonNumber"
            class="rounded-lg border border-violet-900/20 overflow-hidden"
          >
            <!-- Season header -->
            <div class="flex items-center gap-3 px-4 py-2.5 bg-[#161b2e]">
              <button
                class="flex-1 flex items-center gap-3 text-left"
                @click="toggleSeason(season.seasonNumber)"
              >
                <span class="text-gray-500 text-xs transition-transform duration-200" :class="expandedSeasons.has(season.seasonNumber) ? 'rotate-180' : ''">
                  &#9660;
                </span>
                <span class="text-sm font-medium text-gray-200">Season {{ season.seasonNumber }}</span>
              </button>
              <button
                class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0"
                :class="seasonMonitored.get(season.seasonNumber) ? 'bg-emerald-600' : 'bg-gray-600'"
                @click="toggleSeasonMonitor(season.seasonNumber)"
              >
                <span
                  class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
                  :class="seasonMonitored.get(season.seasonNumber) ? 'translate-x-4' : ''"
                />
              </button>
            </div>

            <!-- Episode list -->
            <div v-if="expandedSeasons.has(season.seasonNumber) && season.episodes?.length" class="divide-y divide-violet-900/10">
              <div
                v-for="ep in season.episodes"
                :key="ep.episodeNumber"
                class="flex items-center gap-3 px-4 py-2 pl-10"
              >
                <span class="text-xs text-gray-500 font-mono w-6 text-right flex-shrink-0">{{ ep.episodeNumber }}</span>
                <span class="flex-1 text-sm text-gray-300 truncate">{{ ep.title || `Episode ${ep.episodeNumber}` }}</span>
                <button
                  class="relative w-8 h-[18px] rounded-full transition-colors duration-200 flex-shrink-0"
                  :class="episodeMonitored.get(`${season.seasonNumber}-${ep.episodeNumber}`) ? 'bg-emerald-600' : 'bg-gray-600'"
                  @click="toggleEpisodeMonitor(season.seasonNumber, ep.episodeNumber)"
                >
                  <span
                    class="absolute top-[3px] left-[3px] w-3 h-3 bg-white rounded-full transition-transform duration-200"
                    :class="episodeMonitored.get(`${season.seasonNumber}-${ep.episodeNumber}`) ? 'translate-x-3.5' : ''"
                  />
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Actions -->
        <div class="flex gap-3 flex-shrink-0">
          <button
            class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
            :disabled="adding"
            @click="step = 'configure'"
          >
            Back
          </button>
          <button
            class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            :disabled="adding"
            @click="handleAdd"
          >
            {{ adding ? 'Adding...' : 'Add' }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>
