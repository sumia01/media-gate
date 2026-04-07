<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import client from '@/api/client'
import type { SeasonSummary, Episode } from '@/types/api'

const props = defineProps<{
  mediaItemId: number
  monitored: boolean
  monitorNewSeasons: boolean
  refreshKey?: number
}>()

const emit = defineEmits<{
  searchSeason: [seasonNumber: number]
  searchEpisode: [seasonNumber: number, episodeNumber: number, episodeId: number]
}>()

const seasons = ref<SeasonSummary[]>([])
const loading = ref(false)
const expandedSeasons = ref<Set<number>>(new Set())

async function fetchEpisodes() {
  loading.value = true
  const { data } = await client.GET('/media/{id}/episodes', {
    params: { path: { id: props.mediaItemId } },
  })
  seasons.value = data?.seasons ?? []
  loading.value = false
}

async function toggleSeasonMonitor(seasonNumber: number, currentMonitored: boolean) {
  await client.PUT('/media/{id}/season-monitors/{seasonNumber}', {
    params: { path: { id: props.mediaItemId, seasonNumber } },
    body: { monitored: !currentMonitored },
  })
  // Update local state
  const season = seasons.value.find(s => s.seasonNumber === seasonNumber)
  if (season) {
    season.monitored = !currentMonitored
  }
}

function toggleSeason(seasonNumber: number) {
  const s = new Set(expandedSeasons.value)
  if (s.has(seasonNumber)) {
    s.delete(seasonNumber)
  } else {
    s.add(seasonNumber)
  }
  expandedSeasons.value = s
}

type EpStatus = 'available' | 'missing' | 'unaired' | 'pending' | 'downloading' | 'downloaded' | 'importing' | 'seeding'

function episodeStatus(ep: Episode): EpStatus {
  if (ep.hasFile) return 'available'
  if (ep.downloadStatus && ep.downloadStatus !== 'completed' && ep.downloadStatus !== 'failed') {
    return ep.downloadStatus as EpStatus
  }
  if (!ep.airDate) return 'unaired'
  const airDate = new Date(ep.airDate)
  if (airDate > new Date()) return 'unaired'
  return 'missing'
}

function isDownloadStatus(status: EpStatus): boolean {
  return ['pending', 'downloading', 'downloaded', 'importing', 'seeding'].includes(status)
}

function statusLabel(status: EpStatus): string {
  if (status === 'available') return 'on disk'
  return status
}

onMounted(fetchEpisodes)
watch(() => props.mediaItemId, fetchEpisodes)
watch(() => props.refreshKey, fetchEpisodes)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-4">
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500">Episodes</h2>
      <span
        v-if="props.monitored && props.monitorNewSeasons"
        class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300"
      >
        New seasons auto-monitored
      </span>
    </div>

    <div v-if="loading" class="text-sm text-gray-500">Loading episodes...</div>

    <div v-else-if="!seasons.length" class="text-sm text-gray-500">No episode data available.</div>

    <div v-else class="space-y-3">
      <div
        v-for="season in seasons"
        :key="season.seasonNumber"
        class="rounded-lg border border-violet-900/20 overflow-hidden"
      >
        <!-- Season header -->
        <button
          class="w-full flex items-center justify-between px-4 py-3 bg-[#161b2e] hover:bg-[#1a2038] transition-colors duration-200 cursor-pointer"
          @click="toggleSeason(season.seasonNumber)"
        >
          <div class="flex items-center gap-3">
            <span class="text-sm font-medium text-gray-200">Season {{ season.seasonNumber }}</span>
            <span
              class="text-[10px] font-bold px-2 py-0.5 rounded-full"
              :class="season.availableEpisodes === season.totalEpisodes
                ? 'bg-emerald-600/20 text-emerald-300'
                : season.availableEpisodes > 0
                  ? 'bg-yellow-600/20 text-yellow-300'
                  : 'bg-red-600/20 text-red-300'"
            >
              {{ season.availableEpisodes }}/{{ season.totalEpisodes }} episodes
            </span>
            <span
              v-if="props.monitored && !season.monitored"
              class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-gray-600/20 text-gray-400 cursor-pointer hover:bg-emerald-600/20 hover:text-emerald-300 transition-colors duration-200"
              title="Click to monitor this season"
              @click.stop="toggleSeasonMonitor(season.seasonNumber, false)"
            >
              unmonitored
            </span>
            <span
              v-else-if="props.monitored"
              class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300 cursor-pointer hover:bg-gray-600/20 hover:text-gray-400 transition-colors duration-200"
              title="Click to unmonitor this season"
              @click.stop="toggleSeasonMonitor(season.seasonNumber, true)"
            >
              monitored
            </span>
            <button
              class="px-2 py-0.5 rounded-md text-[10px] text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
              title="Search indexers for this season"
              @click.stop="emit('searchSeason', season.seasonNumber)"
            >
              Search
            </button>
          </div>
          <span class="text-gray-500 text-xs transition-transform duration-200" :class="expandedSeasons.has(season.seasonNumber) ? 'rotate-180' : ''">
            &#9660;
          </span>
        </button>

        <!-- Episode list -->
        <div v-if="expandedSeasons.has(season.seasonNumber) && season.episodes?.length" class="divide-y divide-violet-900/10">
          <div
            v-for="ep in season.episodes"
            :key="ep.episodeNumber"
            class="flex items-center gap-3 px-4 py-2.5"
            :class="{
              'bg-emerald-600/5': episodeStatus(ep) === 'available',
              'bg-red-600/5': episodeStatus(ep) === 'missing',
              'bg-gray-600/5': episodeStatus(ep) === 'unaired',
              'bg-sky-600/5': isDownloadStatus(episodeStatus(ep)),
            }"
          >
            <!-- Episode number badge -->
            <span
              class="flex-shrink-0 w-8 h-8 rounded-md flex items-center justify-center text-xs font-bold"
              :class="{
                'bg-emerald-600/20 text-emerald-300': episodeStatus(ep) === 'available',
                'bg-red-600/20 text-red-300': episodeStatus(ep) === 'missing',
                'bg-gray-600/20 text-gray-400': episodeStatus(ep) === 'unaired',
                'bg-sky-600/20 text-sky-300': isDownloadStatus(episodeStatus(ep)),
              }"
            >
              {{ ep.episodeNumber }}
            </span>

            <!-- Title & details -->
            <div class="flex-1 min-w-0">
              <p class="text-sm text-gray-200 truncate">
                {{ ep.title || `Episode ${ep.episodeNumber}` }}
              </p>
              <div class="flex items-center gap-2 mt-0.5">
                <span v-if="ep.airDate" class="text-[11px] text-gray-500">{{ ep.airDate }}</span>
                <span v-if="ep.runtime" class="text-[11px] text-gray-500">{{ ep.runtime }}min</span>
              </div>
            </div>

            <!-- Search button -->
            <button
              class="flex-shrink-0 px-2 py-0.5 rounded-md text-[10px] text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
              title="Search indexers for this episode"
              @click="emit('searchEpisode', season.seasonNumber, ep.episodeNumber, ep.id)"
            >
              Search
            </button>

            <!-- Status indicator -->
            <span
              class="flex-shrink-0 text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="{
                'bg-emerald-600/20 text-emerald-300': episodeStatus(ep) === 'available',
                'bg-red-600/20 text-red-300': episodeStatus(ep) === 'missing',
                'bg-gray-600/20 text-gray-400': episodeStatus(ep) === 'unaired',
                'bg-sky-600/20 text-sky-300': isDownloadStatus(episodeStatus(ep)),
              }"
            >
              {{ statusLabel(episodeStatus(ep)) }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
