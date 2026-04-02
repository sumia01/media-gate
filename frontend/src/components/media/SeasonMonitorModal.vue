<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import type { SeasonSummary } from '@/types/api'

const props = defineProps<{
  seasons: SeasonSummary[]
}>()

const emit = defineEmits<{
  confirm: [monitors: { seasonNumber: number; monitored: boolean }[]]
  cancel: []
}>()

const expandedSeasons = ref<Set<number>>(new Set())
const seasonMonitored = ref<Map<number, boolean>>(new Map())

// Initialize all seasons as monitored by default
watch(() => props.seasons, (seasons) => {
  const sm = new Map<number, boolean>()
  for (const s of seasons) {
    sm.set(s.seasonNumber, true)
  }
  seasonMonitored.value = sm
}, { immediate: true })

function toggleSeason(seasonNumber: number) {
  const s = new Set(expandedSeasons.value)
  if (s.has(seasonNumber)) s.delete(seasonNumber)
  else s.add(seasonNumber)
  expandedSeasons.value = s
}

function toggleSeasonMonitor(seasonNumber: number) {
  const current = seasonMonitored.value.get(seasonNumber) ?? true
  seasonMonitored.value = new Map(seasonMonitored.value.set(seasonNumber, !current))
}

const allSeasonsMonitored = computed(() =>
  props.seasons.length > 0 &&
  props.seasons.every(s => seasonMonitored.value.get(s.seasonNumber)),
)

function toggleAllSeasons() {
  const newVal = !allSeasonsMonitored.value
  const sm = new Map<number, boolean>()
  for (const s of props.seasons) {
    sm.set(s.seasonNumber, newVal)
  }
  seasonMonitored.value = sm
}

function handleConfirm() {
  emit('confirm', props.seasons.map(s => ({
    seasonNumber: s.seasonNumber,
    monitored: seasonMonitored.value.get(s.seasonNumber) ?? true,
  })))
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <div class="absolute inset-0 bg-black/60" @click="emit('cancel')"></div>
    <div class="relative bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 shadow-2xl w-full max-w-lg max-h-[80vh] flex flex-col">
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

      <div v-if="!seasons.length" class="py-6 text-center text-gray-500 text-sm">
        No season data available.
      </div>

      <div v-else class="flex-1 overflow-y-auto min-h-0 space-y-2 mb-4">
        <div
          v-for="season in seasons"
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

          <!-- Episode list (read-only, for context) -->
          <div v-if="expandedSeasons.has(season.seasonNumber) && season.episodes?.length" class="divide-y divide-violet-900/10">
            <div
              v-for="ep in season.episodes"
              :key="ep.episodeNumber"
              class="flex items-center gap-3 px-4 py-2 pl-10"
            >
              <span class="text-xs text-gray-500 font-mono w-6 text-right flex-shrink-0">{{ ep.episodeNumber }}</span>
              <span class="flex-1 text-sm text-gray-300 truncate">{{ ep.title || `Episode ${ep.episodeNumber}` }}</span>
            </div>
          </div>
        </div>
      </div>

      <!-- Actions -->
      <div class="flex gap-3 flex-shrink-0">
        <button
          class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
          @click="emit('cancel')"
        >
          Cancel
        </button>
        <button
          class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
          @click="handleConfirm"
        >
          Enable Monitoring
        </button>
      </div>
    </div>
  </div>
</template>
