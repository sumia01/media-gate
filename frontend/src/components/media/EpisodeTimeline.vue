<script setup lang="ts">
import { CalendarDays, ChevronLeft, ChevronRight, LoaderCircle, Tv } from 'lucide-vue-next'
import { useEpisodeTimeline } from '@/composables/useEpisodeTimeline'
import { formatTimelineDay, timelineAvailability, WINDOW_DAYS } from '@/utils/episodeTimeline'
import { posterUrl } from '@/utils/media'

const {
  rail,
  today,
  start,
  days,
  visibleRange,
  loading,
  failedWindows,
  empty,
  dragging,
  railWidth,
  dayWidth,
  goToday,
  move,
  onScroll,
  onKeydown,
  onPointerDown,
  onPointerMove,
  stopDrag,
  onClick,
  retry,
} = useEpisodeTimeline()

function episodeCode(season: number, episode: number) {
  return `S${String(season).padStart(2, '0')}E${String(episode).padStart(2, '0')}`
}
</script>

<template>
  <section class="episode-timeline rounded-xl border border-violet-900/30 bg-[#111628] overflow-hidden" aria-label="Followed series episode timeline">
    <header class="flex flex-wrap items-center justify-between gap-3 px-4 pt-4 sm:px-5">
      <div class="flex items-center gap-3">
        <div class="rounded-lg bg-violet-500/10 p-2 text-violet-300"><CalendarDays class="h-4 w-4" aria-hidden="true" /></div>
        <div>
          <h2 class="text-sm font-semibold text-gray-100">Your episode timeline</h2>
          <p class="mt-0.5 text-xs text-gray-400">Followed series, past and upcoming</p>
        </div>
      </div>
      <div class="flex items-center gap-1.5">
        <button type="button" class="timeline-control px-3 text-xs" @click="goToday">Today</button>
        <button type="button" class="timeline-control" aria-label="Previous dates" @click="move(-1)"><ChevronLeft class="h-4 w-4" aria-hidden="true" /></button>
        <button type="button" class="timeline-control" aria-label="Next dates" @click="move(1)"><ChevronRight class="h-4 w-4" aria-hidden="true" /></button>
      </div>
    </header>

    <div class="flex flex-wrap items-center justify-between gap-2 px-4 py-3 sm:px-5">
      <p class="text-xs font-medium text-violet-200">
        {{ formatTimelineDay(visibleRange.first, { month: 'short', day: 'numeric', year: 'numeric' }) }}
        <span class="mx-1 text-gray-600">/</span>
        {{ formatTimelineDay(visibleRange.last, { month: 'short', day: 'numeric', year: 'numeric' }) }}
      </p>
      <p class="text-[11px] text-gray-500">Drag or scroll to explore</p>
    </div>

    <p class="sr-only">Scroll horizontally or use Left and Right arrows for one day, Page Up and Page Down for more dates, and Home for today. Episode cards open series details.</p>
    <div
      ref="rail"
      class="timeline-rail relative overflow-x-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-violet-400"
      :class="dragging ? 'cursor-grabbing select-none' : 'cursor-grab'"
      tabindex="0"
      role="region"
      aria-label="Episode dates. Use arrow keys to browse."
      @scroll.passive="onScroll"
      @keydown="onKeydown"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @lostpointercapture="stopDrag"
      @click.capture="onClick"
      @dragstart.prevent
    >
      <div class="relative h-[354px]" :style="{ width: `${railWidth}px` }">
        <article
          v-for="day in days"
          :key="day.date"
          class="absolute top-0 h-full px-2 pb-3"
          :style="{ left: `${(day.day - start) * dayWidth}px`, width: `${dayWidth}px` }"
          :aria-label="formatTimelineDay(day.day, { dateStyle: 'full' })"
        >
          <div class="relative mb-3 border-t pt-3" :class="day.day === today ? 'border-violet-400' : 'border-violet-900/40'">
            <span class="absolute -top-1 h-2 w-2 rounded-full" :class="day.day === today ? 'bg-violet-400 ring-4 ring-violet-400/10' : 'bg-[#3b315c]'" />
            <time :datetime="day.date" class="flex items-center gap-2 text-xs font-medium" :class="day.day === today ? 'text-violet-300' : 'text-gray-400'">
              {{ formatTimelineDay(day.day, { weekday: 'short', month: 'short', day: 'numeric' }) }}
              <span v-if="day.day === today" class="rounded bg-violet-500/15 px-1.5 py-0.5 text-[10px]">Today</span>
            </time>
          </div>

          <div v-if="!day.window || day.window.status === 'loading'" class="h-36 animate-pulse rounded-lg border border-violet-900/20 bg-violet-400/5" aria-label="Loading episodes" />
          <div v-else-if="day.window.status === 'error'" class="rounded-lg border border-rose-400/15 px-3 py-5 text-xs text-rose-200/80">
            Dates not loaded
            <button type="button" class="mt-2 block rounded px-1 py-1 text-violet-300 underline underline-offset-4 focus-visible:outline-2" @click="retry(day.window.from)">Retry this window</button>
          </div>
          <p v-else-if="!day.items.length" class="rounded-lg border border-dashed border-violet-900/20 px-3 py-6 text-xs text-gray-600">No episodes scheduled</p>
          <ul v-else class="timeline-day-list space-y-2 overflow-y-auto pr-1" :aria-label="`Episodes on ${day.date}`">
            <li v-for="item in day.items" :key="item.episode.id">
              <RouterLink
                :to="{ name: 'media-detail', params: { id: item.episode.mediaItemId } }"
                class="block cursor-pointer rounded-lg border border-violet-900/30 bg-[#171c30] p-3 transition-colors hover:border-violet-400/50 hover:bg-[#1c2139] focus-visible:outline-2 focus-visible:outline-violet-400"
                :aria-label="`${item.seriesTitle}, ${episodeCode(item.episode.seasonNumber, item.episode.episodeNumber)}, ${item.episode.title || 'Untitled episode'}. ${timelineAvailability(item, today)}${item.episode.monitored ? '' : '. Unmonitored'}`"
                :draggable="false"
              >
                <div class="flex items-start gap-2.5">
                  <div class="flex h-[66px] w-11 shrink-0 items-center justify-center overflow-hidden rounded bg-violet-900/20 text-violet-300/40">
                    <img v-if="item.posterPath" :src="posterUrl({ id: item.episode.mediaItemId })" alt="" class="h-full w-full object-cover" loading="lazy" :draggable="false" />
                    <Tv v-else class="h-5 w-5" aria-hidden="true" />
                  </div>
                  <div class="min-w-0">
                    <h3 class="line-clamp-2 text-xs font-semibold leading-5 text-gray-100">{{ item.seriesTitle }}</h3>
                    <p class="mt-1 font-mono text-[11px] text-violet-300">{{ episodeCode(item.episode.seasonNumber, item.episode.episodeNumber) }}</p>
                  </div>
                </div>
                <p class="mt-2 line-clamp-2 text-xs leading-5 text-gray-400">{{ item.episode.title || 'Untitled episode' }}</p>
                <div class="mt-2 flex flex-wrap gap-1.5 text-[10px] font-medium">
                  <span class="rounded px-1.5 py-0.5" :class="{
                    'bg-emerald-500/10 text-emerald-300': item.episode.hasFile,
                    'bg-sky-500/10 text-sky-300': !item.episode.hasFile && item.episode.downloadStatus,
                    'bg-violet-500/10 text-violet-300': !item.episode.hasFile && !item.episode.downloadStatus,
                  }">{{ timelineAvailability(item, today) }}</span>
                  <span v-if="!item.episode.monitored" class="rounded border border-gray-600/40 px-1.5 py-0.5 text-gray-400">Unmonitored</span>
                </div>
              </RouterLink>
            </li>
          </ul>
        </article>
      </div>
    </div>

    <footer class="min-h-10 border-t border-violet-900/20 px-4 py-2.5 text-[11px] text-gray-500 sm:px-5">
      <div aria-live="polite" role="status">
        <span v-if="loading" class="flex items-center gap-2"><LoaderCircle class="h-3 w-3 animate-spin" aria-hidden="true" />Loading nearby dates...</span>
        <span v-else-if="empty">No dated episodes here. Keep exploring, or follow more series in your library.</span>
        <span v-else-if="!failedWindows.length">Dates follow your series metadata. Unmonitored episodes are included.</span>
      </div>
      <div v-for="window in failedWindows" :key="window.from" class="flex flex-wrap items-center gap-2 text-rose-300" role="alert">
        <span>{{ formatTimelineDay(window.from, { month: 'short', day: 'numeric' }) }} to {{ formatTimelineDay(window.from + WINDOW_DAYS - 1, { month: 'short', day: 'numeric' }) }}: {{ window.error }}</span>
        <button type="button" class="rounded px-1 py-1 text-violet-300 underline underline-offset-4 focus-visible:outline-2" @click="retry(window.from)">Retry</button>
      </div>
    </footer>
  </section>
</template>

<style scoped>
.timeline-control {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 44px;
  min-height: 44px;
  border: 1px solid rgb(139 92 246 / 18%);
  border-radius: 7px;
  color: #c4b5fd;
  cursor: pointer;
}
.timeline-control:hover { background: rgb(139 92 246 / 12%); }
.timeline-control:focus-visible { outline: 2px solid #a78bfa; outline-offset: 2px; }
.timeline-rail { overflow-anchor: none; scrollbar-color: #4c3b70 #111628; scrollbar-width: thin; }
.timeline-day-list { max-height: 288px; scrollbar-color: #4c3b70 transparent; scrollbar-width: thin; }
@media (prefers-reduced-motion: reduce) {
  .animate-pulse, .animate-spin { animation: none; }
}
</style>
