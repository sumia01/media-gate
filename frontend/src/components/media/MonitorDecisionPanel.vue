<script setup lang="ts">
import { ChevronRight } from 'lucide-vue-next'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useEventStream } from '@/composables/useEventStream'
import { monitorDecisionFreshness } from '@/utils/monitorDecision'

type MonitorDecision = components['schemas']['MonitorDecision']
type DecisionDetail = components['schemas']['MonitorDecisionDetail']

const props = defineProps<{
  mediaItemId: number
  monitored: boolean
  updatedAt: string
  active: boolean
}>()

const expanded = ref(false)
const decision = ref<MonitorDecision | null>(null)
const loaded = ref(false)
const loading = ref(false)
const error = ref('')
const { on, off } = useEventStream()
let request: AbortController | undefined
let generation = 0
let dirty = false

const freshness = computed(() => monitorDecisionFreshness(decision.value, props.updatedAt))

const collapsedSummary = computed(() => {
  if (decision.value) return decision.value.summary
  if (error.value) return error.value
  if (loading.value) return 'Loading saved check...'
  if (!loaded.value) return 'Open to load the latest saved check.'
  return props.monitored ? 'No check has been recorded yet.' : 'Auto-download is currently off.'
})

const labels: Record<string, string> = {
  grabbed: 'Download queued',
  error: 'Check failed',
  indexer_error: 'Search failed',
  no_indexers: 'No enabled indexers',
  partial_indexer_failure: 'Some indexers failed',
  blocked: 'Selection blocked',
  profile_rejected: 'Profile rejected',
  no_results: 'No results returned',
  no_match: 'No matching release',
  missing_metadata: 'Metadata needed',
  active_download: 'Existing download',
  already_present: 'Already in library',
  unaired: 'Not released yet',
  disabled: 'Not monitored',
  no_eligible_targets: 'No eligible targets',
  search_results: 'Search results',
}

function targetLabel(detail: DecisionDetail) {
  if (detail.seasonNumber == null) return 'Item'
  const season = `Season ${detail.seasonNumber}`
  return detail.episodeNumber == null ? season : `${season}, episode ${detail.episodeNumber}`
}

async function loadDecision() {
  if (!props.active || !expanded.value) return
  abortRequest()
  const capturedId = props.mediaItemId
  const requestGeneration = ++generation
  const current = new AbortController()
  request = current
  loading.value = true
  error.value = ''
  try {
    const { data, error: failure } = await client.GET('/media/{id}/monitor-decision', {
      params: { path: { id: props.mediaItemId } },
      signal: current.signal,
    })
    if (
      current.signal.aborted ||
      generation !== requestGeneration ||
      props.mediaItemId !== capturedId ||
      !props.active ||
      !expanded.value
    )
      return
    if (failure || !data) {
      error.value = 'Could not load the latest saved check. Close and reopen this section to try again.'
      return
    }
    decision.value = data.decision ?? null
    loaded.value = true
    dirty = false
  } catch {
    if (
      !current.signal.aborted &&
      generation === requestGeneration &&
      props.mediaItemId === capturedId &&
      props.active &&
      expanded.value
    ) {
      error.value = 'Could not load the latest saved check. Close and reopen this section to try again.'
    }
  } finally {
    if (generation === requestGeneration) {
      request = undefined
      loading.value = false
    }
  }
}

function abortRequest() {
  generation++
  request?.abort()
  request = undefined
  loading.value = false
}

function toggleExpanded() {
  expanded.value = !expanded.value
  if (expanded.value && props.active) void loadDecision()
  else abortRequest()
}

function onWorkerFinished(data: { name?: string }) {
  // A grab event can precede snapshot persistence. Worker completion is after it.
  if (data.name !== 'monitor') return
  dirty = true
  if (props.active && expanded.value) void loadDecision()
}

watch(
  () => props.mediaItemId,
  () => {
    abortRequest()
    expanded.value = false
    decision.value = null
    loaded.value = false
    error.value = ''
    dirty = false
  },
)

watch(
  () => props.active,
  (active) => {
    if (!active) {
      abortRequest()
      return
    }
    if (expanded.value) void loadDecision()
  },
)

onMounted(() => on('worker.finished', onWorkerFinished))
onUnmounted(() => {
  abortRequest()
  off('worker.finished', onWorkerFinished)
})
</script>

<template>
  <section class="rounded-lg border border-violet-900/30 bg-[#161b2e]">
    <h2>
      <button
        type="button"
        class="flex w-full items-start gap-3 px-4 py-3 text-left cursor-pointer hover:bg-violet-500/5 rounded-lg focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-violet-400"
        :aria-expanded="expanded"
        :aria-controls="`monitor-decision-${mediaItemId}`"
        @click="toggleExpanded"
      >
        <ChevronRight class="w-4 h-4 mt-0.5 shrink-0 text-violet-400 transition-transform motion-reduce:transition-none" :class="{ 'rotate-90': expanded }" />
        <span class="min-w-0 flex-1">
          <span class="block text-sm font-medium text-gray-200">Latest auto-download check</span>
          <span class="block mt-1 text-xs text-gray-400 break-words">
            {{ collapsedSummary }}
          </span>
        </span>
      </button>
    </h2>

    <div v-if="expanded" :id="`monitor-decision-${mediaItemId}`" class="border-t border-violet-900/20 px-4 py-4 space-y-4">
      <div class="text-xs text-gray-400 space-y-1">
        <p v-if="decision">
          Last check completed:
          <time :datetime="decision.checkedAt" class="text-gray-200">{{ new Date(decision.checkedAt).toLocaleString('en-US') }}</time>
        </p>
        <p>Saved worker result, not a live search. Closing and reopening reloads this section.</p>
      </div>

      <p v-if="loading" role="status" class="text-sm text-gray-400">Loading saved check...</p>
      <p v-if="error" role="alert" class="text-sm text-red-300">{{ error }} {{ decision ? 'The previously loaded snapshot is still shown below.' : '' }}</p>
      <p v-if="!monitored" class="text-xs text-amber-300">
        Auto-download is disabled. The worker skips this item; any snapshot below belongs to an earlier check.
      </p>
      <p v-if="freshness === 'stale'" class="text-xs text-amber-300">
        This item or its monitoring settings changed since the inputs used by this check. The saved result may not reflect the current state.
      </p>
      <p v-else-if="freshness === 'unknown'" class="text-xs text-amber-300">
        Input freshness is unknown for this saved check. Older snapshots did not record the item version used; a future monitor run will record it.
      </p>

      <template v-if="decision">
        <p class="text-sm text-gray-200">{{ decision.summary }}</p>
        <p class="text-xs text-gray-500">Counts describe each search or selection, not the number of episodes covered by a pack. Returned results are limited per search.</p>
        <ol class="space-y-2 max-h-[32rem] overflow-y-auto">
          <li v-for="(detail, index) in decision.details" :key="index" class="rounded-lg bg-[#0f1225] border border-violet-900/20 p-3">
            <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
              <span class="font-medium text-gray-200">{{ targetLabel(detail) }}</span>
              <span :class="{
                'text-emerald-300': detail.outcome === 'grabbed',
                'text-amber-300': ['error', 'indexer_error', 'no_indexers', 'partial_indexer_failure', 'blocked'].includes(detail.outcome),
                'text-violet-300': !['grabbed', 'error', 'indexer_error', 'no_indexers', 'partial_indexer_failure', 'blocked'].includes(detail.outcome),
              }">
                {{ labels[detail.outcome] ?? 'Check result' }}
              </span>
            </div>
            <p class="mt-1.5 text-xs leading-relaxed text-gray-400">{{ detail.explanation }}</p>
            <p v-if="detail.selectedTitle" class="mt-2 text-xs text-gray-200 break-all">{{ detail.selectedTitle }}</p>
            <div v-if="detail.totalResults || detail.blockedResults || ['no_results', 'profile_rejected', 'search_results'].includes(detail.outcome)" class="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-gray-500">
              <span>Returned: <span class="text-gray-300">{{ detail.totalResults }}</span></span>
              <span>Profile rejected: <span class="text-gray-300">{{ detail.rejectedResults }}</span></span>
              <span>Blocked selections: <span class="text-gray-300">{{ detail.blockedResults }}</span></span>
            </div>
            <p v-if="detail.downloadId" class="mt-2 text-[11px] text-emerald-300">Queued download #{{ detail.downloadId }}</p>
          </li>
        </ol>
        <p v-if="decision.truncated" class="text-xs text-amber-300">
          Detail limit reached. Showing up to 50 checks, prioritizing queued downloads and problems. The summary includes all checks.
        </p>
      </template>
      <p v-else-if="!loading && !error" class="text-sm text-gray-400">
        No auto-download check has been recorded for this item yet. {{ monitored ? 'A future monitor run will record its decision here, even if it does not search.' : 'Enable auto-download to include it in future monitor runs.' }}
      </p>
    </div>
  </section>
</template>
