<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type SeasonSummary = components['schemas']['SeasonSummary']
type Episode = components['schemas']['Episode']

const props = defineProps<{
  mediaItemId: number
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

function toggleSeason(seasonNumber: number) {
  const s = new Set(expandedSeasons.value)
  if (s.has(seasonNumber)) {
    s.delete(seasonNumber)
  } else {
    s.add(seasonNumber)
  }
  expandedSeasons.value = s
}

function episodeStatus(ep: Episode): 'available' | 'missing' | 'unaired' {
  if (ep.hasFile) return 'available'
  if (!ep.airDate) return 'unaired'
  const airDate = new Date(ep.airDate)
  if (airDate > new Date()) return 'unaired'
  return 'missing'
}

onMounted(fetchEpisodes)
watch(() => props.mediaItemId, fetchEpisodes)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-4">
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500">Episodes</h2>
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
              v-if="!season.monitored"
              class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-gray-600/20 text-gray-400"
            >
              unmonitored
            </span>
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
            }"
          >
            <!-- Episode number badge -->
            <span
              class="flex-shrink-0 w-8 h-8 rounded-md flex items-center justify-center text-xs font-bold"
              :class="{
                'bg-emerald-600/20 text-emerald-300': episodeStatus(ep) === 'available',
                'bg-red-600/20 text-red-300': episodeStatus(ep) === 'missing',
                'bg-gray-600/20 text-gray-400': episodeStatus(ep) === 'unaired',
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

            <!-- Status indicator -->
            <span
              class="flex-shrink-0 text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="{
                'bg-emerald-600/20 text-emerald-300': episodeStatus(ep) === 'available',
                'bg-red-600/20 text-red-300': episodeStatus(ep) === 'missing',
                'bg-gray-600/20 text-gray-400': episodeStatus(ep) === 'unaired',
              }"
            >
              {{ episodeStatus(ep) === 'available' ? 'on disk' : episodeStatus(ep) === 'missing' ? 'missing' : 'unaired' }}
            </span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
