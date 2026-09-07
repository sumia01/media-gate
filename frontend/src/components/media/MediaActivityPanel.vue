<script setup lang="ts">
import {
  Activity,
  CheckCircle2,
  ChevronRight,
  Download,
  Eye,
  FileClock,
  RefreshCw,
  Settings2,
  Subtitles,
} from 'lucide-vue-next'
import type { Component } from 'vue'
import { computed, nextTick, ref, toRef, watch } from 'vue'
import { useMediaActivity } from '@/composables/useMediaActivity'
import {
  activityAbsoluteTime,
  activityActionLabel,
  activityDetailLines,
  activityRelativeTime,
  activityTargetSummary,
  type MediaActivity,
} from '@/utils/mediaActivity'

const props = defineProps<{
  mediaItemId: number
  active: boolean
}>()

const expanded = ref(false)
watch(
  () => props.mediaItemId,
  () => {
    expanded.value = false
  },
)
const activity = useMediaActivity(toRef(props, 'mediaItemId'), toRef(props, 'active'), expanded)
const panelId = computed(() => `media-activity-${props.mediaItemId}`)
const disclosureButton = ref<HTMLButtonElement | null>(null)
const loadOlderButton = ref<HTMLButtonElement | null>(null)
const continueOlderButton = ref<HTMLButtonElement | null>(null)
const backToLatestButton = ref<HTMLButtonElement | null>(null)

function iconFor(item: MediaActivity): Component {
  if (item.action.startsWith('download.')) return Download
  if (item.action.startsWith('monitoring.') || item.action === 'request.made') return CheckCircle2
  if (item.action.startsWith('media.settings')) return Settings2
  if (item.action.startsWith('media.resync') || item.action === 'metadata.changed') return FileClock
  if (item.action.startsWith('subtitle.')) return Subtitles
  if (item.action.startsWith('watched.')) return Eye
  return Activity
}

function toggleExpanded() {
  expanded.value = !expanded.value
}

function focusFirst(...buttons: (HTMLButtonElement | null)[]) {
  buttons.find((button) => button)?.focus()
}

async function loadOlder() {
  if (!(await activity.loadOlder())) return
  await nextTick()
  focusFirst(loadOlderButton.value, continueOlderButton.value, backToLatestButton.value, disclosureButton.value)
}

async function continueOlder() {
  if (!(await activity.continueOlder())) return
  await nextTick()
  focusFirst(loadOlderButton.value, backToLatestButton.value, disclosureButton.value)
}

async function backToLatest() {
  if (!(await activity.backToLatest())) return
  await nextTick()
  focusFirst(loadOlderButton.value, disclosureButton.value)
}

defineExpose({ markDirty: activity.markDirty })
</script>

<template>
  <section class="rounded-lg border border-violet-900/30 bg-[#161b2e]">
    <h2>
      <button
        ref="disclosureButton"
        type="button"
        class="flex w-full cursor-pointer items-start gap-3 rounded-lg px-4 py-3 text-left hover:bg-violet-500/5 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400"
        :aria-expanded="expanded"
        :aria-controls="panelId"
        @click="toggleExpanded"
      >
        <ChevronRight
          class="mt-0.5 h-4 w-4 shrink-0 text-violet-400 transition-transform motion-reduce:transition-none"
          :class="{ 'rotate-90': expanded }"
        />
        <span class="min-w-0 flex-1">
          <span class="block text-sm font-medium text-gray-200">Activity log</span>
          <span class="mt-1 block break-words text-xs text-gray-400">
            {{ activity.loaded.value ? `${activity.items.value.length} recorded actions loaded.` : 'Open to load recorded actions.' }}
          </span>
        </span>
      </button>
    </h2>

    <div v-if="expanded" :id="panelId" class="space-y-4 border-t border-violet-900/20 px-4 py-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <p class="max-w-2xl text-xs leading-relaxed text-amber-200/90">
          Historical actions do not necessarily reflect current monitoring.
        </p>
        <button
          v-if="activity.loaded.value && activity.windowMode.value === 'latest' && !activity.dirty.value"
          type="button"
          class="inline-flex items-center gap-1.5 rounded-lg border border-violet-500/30 px-3 py-1.5 text-xs text-violet-300 hover:bg-violet-500/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 disabled:opacity-50"
          :disabled="activity.loading.value"
          @click="activity.refreshLatest"
        >
          <RefreshCw class="h-3.5 w-3.5" :class="{ 'animate-spin motion-reduce:animate-none': activity.loading.value }" />
          Refresh latest
        </button>
      </div>

      <div
        v-if="activity.dirty.value && active && expanded"
        role="status"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-amber-500/25 bg-amber-500/5 px-3 py-2 text-xs text-amber-200"
      >
        <span>Activity may have changed. The currently loaded history is retained until you refresh.</span>
        <button
          type="button"
          class="font-medium text-violet-300 underline decoration-violet-500 underline-offset-2 hover:text-violet-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400"
          :disabled="activity.loading.value"
          @click="activity.refreshLatest"
        >
          Refresh latest
        </button>
      </div>

      <p v-if="activity.loading.value && !activity.loaded.value" role="status" class="text-sm text-gray-400">
        Loading activity...
      </p>

      <div
        v-if="activity.error.value"
        role="alert"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-500/25 bg-red-500/5 px-3 py-2 text-sm text-red-200"
      >
        <span>
          {{ activity.error.value }}
          <template v-if="activity.loaded.value">The retained activity may be stale.</template>
        </span>
        <button
          type="button"
          class="font-medium text-violet-300 underline decoration-violet-500 underline-offset-2 hover:text-violet-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400"
          :disabled="activity.loading.value"
          @click="activity.retry"
        >
          Retry
        </button>
      </div>

      <p
        v-if="activity.loaded.value && !activity.items.value.length && !activity.loading.value"
        role="status"
        class="text-sm text-gray-400"
      >
        No recorded activity yet. Earlier actions were not recorded.
      </p>
      <p v-else-if="activity.loaded.value && !activity.loading.value" role="status" class="sr-only">
        {{ activity.items.value.length }} recorded actions loaded.
      </p>

      <ol v-if="activity.items.value.length" class="space-y-2" aria-label="Recorded media activity, newest first">
        <li
          v-for="entry in activity.items.value"
          :key="entry.id"
          class="rounded-lg border border-violet-900/20 bg-[#0f1225] px-3 py-3 sm:px-4"
        >
          <div class="flex flex-wrap items-start gap-x-3 gap-y-2">
            <component :is="iconFor(entry)" class="mt-0.5 h-4 w-4 shrink-0 text-violet-400" aria-hidden="true" />
            <div class="min-w-0 flex-1">
              <div class="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                <span class="text-xs font-medium text-gray-400">{{ entry.actor.name }}</span>
                <span class="text-sm font-medium text-gray-100">{{ activityActionLabel(entry) }}</span>
              </div>
              <p class="mt-1 break-words text-xs text-gray-400">{{ activityTargetSummary(entry) }}</p>
              <ul v-if="activityDetailLines(entry).length" class="mt-2 space-y-1 text-xs leading-relaxed text-gray-300">
                <li v-for="line in activityDetailLines(entry)" :key="line" class="break-words">{{ line }}</li>
              </ul>
            </div>
            <time
              :datetime="entry.recordedAt"
              :aria-label="activityAbsoluteTime(entry.recordedAt)"
              class="shrink-0 text-xs text-gray-500"
            >
              {{ activityRelativeTime(entry.recordedAt) }}
            </time>
          </div>
        </li>
      </ol>

      <div v-if="activity.loaded.value" class="flex flex-wrap items-center gap-3 pt-1">
        <button
          v-if="activity.hasMore.value && !activity.atCapacity.value"
          ref="loadOlderButton"
          type="button"
          class="rounded-lg border border-violet-500/30 px-3 py-1.5 text-xs text-violet-300 hover:bg-violet-500/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 disabled:opacity-50"
          :disabled="activity.loading.value"
          @click="loadOlder"
        >
          {{ activity.loadingKind.value === 'older' ? 'Loading older...' : 'Load older' }}
        </button>
        <button
          v-if="activity.atCapacity.value"
          ref="continueOlderButton"
          type="button"
          class="rounded-lg border border-violet-500/30 px-3 py-1.5 text-xs text-violet-300 hover:bg-violet-500/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 disabled:opacity-50"
          :disabled="activity.loading.value"
          @click="continueOlder"
        >
          {{ activity.loadingKind.value === 'continue' ? 'Loading older...' : 'Continue with older activity' }}
        </button>
        <button
          v-if="activity.windowMode.value === 'older'"
          ref="backToLatestButton"
          type="button"
          class="rounded-lg border border-violet-500/30 px-3 py-1.5 text-xs text-violet-300 hover:bg-violet-500/10 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400 disabled:opacity-50"
          :disabled="activity.loading.value"
          @click="backToLatest"
        >
          {{ activity.loadingKind.value === 'back' ? 'Loading latest...' : 'Back to latest' }}
        </button>
        <span v-if="activity.loading.value && activity.loaded.value" role="status" class="text-xs text-gray-500">
          Loading activity...
        </span>
      </div>
    </div>
  </section>
</template>
