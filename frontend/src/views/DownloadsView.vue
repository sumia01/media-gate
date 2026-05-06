<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import { useEventStream } from '@/composables/useEventStream'
import type { Download, TorrentFile } from '@/types/api'
import { formatBytes, formatSize } from '@/utils/media'

const router = useRouter()
const { on, off } = useEventStream()

const downloads = ref<Download[]>([])
const loading = ref(false)
const statusFilter = ref<string>('')
const torrentFiles = ref<Map<number, TorrentFile[]>>(new Map())
const loadingFiles = ref<Set<number>>(new Set())
const confirmDeleteId = ref<number | null>(null)

const statusOrder: Record<string, number> = {
  downloading: 0,
  pending: 1,
  seeding: 2,
  downloaded: 3,
  importing: 4,
  failed: 5,
  import_failed: 6,
  completed: 7,
}

const sortedDownloads = computed(() =>
  [...downloads.value].sort((a, b) => {
    const so = (statusOrder[a.status] ?? 9) - (statusOrder[b.status] ?? 9)
    if (so !== 0) return so
    return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
  }),
)

const hasActiveDownloads = computed(() =>
  downloads.value.some(
    (d) =>
      d.status === 'pending' ||
      d.status === 'downloading' ||
      d.status === 'downloaded' ||
      d.status === 'importing' ||
      d.status === 'seeding',
  ),
)

// Progress polling
let progressTimer: ReturnType<typeof setInterval> | null = null

function startProgressPoll() {
  if (progressTimer) return
  progressTimer = setInterval(fetchDownloads, 3000)
}

function stopProgressPoll() {
  if (progressTimer) {
    clearInterval(progressTimer)
    progressTimer = null
  }
}

watch(hasActiveDownloads, (active) => {
  if (active) startProgressPoll()
  else stopProgressPoll()
})

async function fetchDownloads() {
  const query: Record<string, any> = {}
  if (statusFilter.value) {
    query.status = statusFilter.value
  }
  const { data } = await client.GET('/downloads', { params: { query } })
  downloads.value = data?.downloads ?? []
}

async function fetchWithLoading() {
  loading.value = true
  await fetchDownloads()
  loading.value = false
}

watch(statusFilter, () => fetchWithLoading())

async function fetchFiles(id: number) {
  if (loadingFiles.value.has(id)) return
  loadingFiles.value = new Set([...loadingFiles.value, id])
  const { data } = await client.GET('/downloads/{id}/files', {
    params: { path: { id } },
  })
  const newMap = new Map(torrentFiles.value)
  newMap.set(id, data?.files ?? [])
  torrentFiles.value = newMap
  const newLoading = new Set(loadingFiles.value)
  newLoading.delete(id)
  loadingFiles.value = newLoading
}

function toggleFiles(id: number) {
  if (torrentFiles.value.has(id)) {
    const newMap = new Map(torrentFiles.value)
    newMap.delete(id)
    torrentFiles.value = newMap
  } else {
    fetchFiles(id)
  }
}

async function retryDownload(id: number) {
  await client.PUT('/downloads/{id}', {
    params: { path: { id } },
    body: { status: 'pending' },
  })
  await fetchDownloads()
}

async function deleteDownload(id: number) {
  confirmDeleteId.value = null
  await client.DELETE('/downloads/{id}', {
    params: { path: { id }, query: { deleteFiles: true } },
  })
  const newMap = new Map(torrentFiles.value)
  newMap.delete(id)
  torrentFiles.value = newMap
  await fetchDownloads()
}

function openMedia(mediaItemId: number) {
  router.push({ name: 'media-detail', params: { id: mediaItemId } })
}

function statusColor(status: string) {
  switch (status) {
    case 'pending':
      return 'bg-gray-600/20 text-gray-400'
    case 'downloading':
      return 'bg-sky-600/20 text-sky-300'
    case 'downloaded':
      return 'bg-sky-600/20 text-sky-300'
    case 'importing':
      return 'bg-sky-600/20 text-sky-300'
    case 'seeding':
      return 'bg-emerald-600/20 text-emerald-300'
    case 'completed':
      return 'bg-emerald-600/20 text-emerald-300'
    case 'failed':
      return 'bg-red-600/20 text-red-300'
    case 'import_failed':
      return 'bg-red-600/20 text-red-300'
    default:
      return 'bg-gray-600/20 text-gray-400'
  }
}

function formatSpeed(bytesPerSec?: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return ''
  if (bytesPerSec < 1024) return `${bytesPerSec} B/s`
  const kb = bytesPerSec / 1024
  if (kb < 1024) return `${kb.toFixed(1)} KB/s`
  const mb = kb / 1024
  return `${mb.toFixed(1)} MB/s`
}

function formatRetryTime(dateStr: string): string {
  const target = new Date(dateStr)
  const now = new Date()
  const diffMs = target.getTime() - now.getTime()
  if (diffMs <= 0) return 'soon'
  const diffSec = Math.floor(diffMs / 1000)
  if (diffSec < 60) return `in ${diffSec}s`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `in ${diffMin}m`
  const diffHr = Math.floor(diffMin / 60)
  const remMin = diffMin % 60
  return remMin > 0 ? `in ${diffHr}h ${remMin}m` : `in ${diffHr}h`
}

// SSE
function handleDownloadEvent() {
  fetchDownloads()
}

const downloadEvents = [
  'download.created',
  'download.sent_to_client',
  'download.failed',
  'download.completed',
  'download.import_started',
  'download.import_completed',
  'download.import_failed',
  'download.seeding_completed',
]

onMounted(() => {
  fetchWithLoading().then(() => {
    if (hasActiveDownloads.value) startProgressPoll()
  })
  for (const type of downloadEvents) {
    on(type, handleDownloadEvent)
  }
})

onUnmounted(() => {
  stopProgressPoll()
  for (const type of downloadEvents) {
    off(type, handleDownloadEvent)
  }
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">Downloads</h1>

      <!-- Status filter -->
      <select
        v-model="statusFilter"
        class="text-sm bg-[#161b2e] border border-violet-900/30 rounded-lg px-3 py-1.5 text-gray-300 focus:outline-none focus:border-violet-500/50"
      >
        <option value="">All statuses</option>
        <option value="pending">Pending</option>
        <option value="downloading">Downloading</option>
        <option value="downloaded">Downloaded</option>
        <option value="importing">Importing</option>
        <option value="seeding">Seeding</option>
        <option value="completed">Completed</option>
        <option value="failed">Failed</option>
        <option value="import_failed">Import failed</option>
      </select>
    </div>

    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <div v-else-if="sortedDownloads.length === 0" class="text-gray-500 text-sm">
      No downloads{{ statusFilter ? ' with this status' : '' }}.
    </div>

    <div v-else class="space-y-2">
      <div
        v-for="dl in sortedDownloads"
        :key="dl.id"
      >
        <!-- Download row -->
        <div class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2 flex-wrap">
                <!-- Status badge -->
                <span
                  class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
                  :class="statusColor(dl.status)"
                >
                  {{ dl.status === 'import_failed' ? 'import failed' : dl.status }}
                </span>

                <!-- Season/Episode badge -->
                <span
                  v-if="dl.seasonNumber != null"
                  class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-fuchsia-600/20 text-fuchsia-300"
                >
                  S{{ String(dl.seasonNumber).padStart(2, '0') }}{{ dl.episodeId ? '' : '+' }}
                </span>

                <!-- Indexer badge -->
                <span class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-sky-600/20 text-sky-300">
                  {{ dl.indexerName }}
                </span>
              </div>

              <!-- Media item title link -->
              <button
                v-if="dl.mediaItemTitle"
                class="text-xs text-violet-400 hover:text-violet-300 transition-colors duration-200 mt-1 truncate block max-w-full text-left cursor-pointer"
                @click="openMedia(dl.mediaItemId)"
              >
                {{ dl.mediaItemTitle }}
              </button>

              <p class="text-sm font-medium text-gray-200 truncate mt-0.5">{{ dl.title }}</p>

              <!-- Last error message -->
              <p
                v-if="dl.lastError && (dl.status === 'failed' || dl.status === 'import_failed')"
                class="text-[10px] text-red-400/80 truncate mt-1"
                :title="dl.lastError"
              >
                {{ dl.lastError }}
              </p>

              <!-- Retry backoff info -->
              <p
                v-if="dl.status === 'pending' && dl.retryCount && dl.nextRetryAt"
                class="text-[10px] text-amber-400/70 mt-1"
              >
                Retry {{ dl.retryCount }}/5 &mdash; next attempt {{ formatRetryTime(dl.nextRetryAt) }}
              </p>

              <!-- Progress bar -->
              <div
                v-if="dl.status === 'downloading' && dl.progress != null"
                class="mt-2 flex items-center gap-3"
              >
                <div class="flex-1 h-1.5 rounded-full bg-gray-700/50 overflow-hidden">
                  <div
                    class="h-full rounded-full bg-violet-600 transition-all duration-500"
                    :style="{ width: (dl.progress * 100).toFixed(1) + '%' }"
                  />
                </div>
                <span class="text-[10px] text-gray-400 flex-shrink-0">{{ (dl.progress * 100).toFixed(1) }}%</span>
              </div>

              <!-- Speed display -->
              <div
                v-if="(dl.status === 'downloading' || dl.status === 'seeding') && (dl.downloadSpeed || dl.uploadSpeed)"
                class="mt-1 flex items-center gap-3 text-[10px] text-gray-500"
              >
                <span v-if="dl.downloadSpeed">&darr; {{ formatSpeed(dl.downloadSpeed) }}</span>
                <span v-if="dl.uploadSpeed">&uarr; {{ formatSpeed(dl.uploadSpeed) }}</span>
              </div>
            </div>

            <!-- Right side: size + actions -->
            <div class="flex items-center gap-2 flex-shrink-0">
              <span v-if="dl.size" class="text-xs text-gray-500 hidden sm:inline">{{ formatSize(dl.size) }}</span>

              <!-- Open in library button -->
              <button
                class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200"
                title="Open media in library"
                @click="openMedia(dl.mediaItemId)"
              >
                <svg class="w-3.5 h-3.5 inline-block" fill="none" stroke="currentColor" stroke-width="2" viewBox="0 0 24 24">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M13.5 6H5.25A2.25 2.25 0 003 8.25v10.5A2.25 2.25 0 005.25 21h10.5A2.25 2.25 0 0018 18.75V10.5m-10.5 6L21 3m0 0h-5.25M21 3v5.25" />
                </svg>
              </button>

              <!-- Files button -->
              <button
                v-if="dl.clientTorrentHash"
                class="text-[10px] px-2 py-1 rounded border transition-colors duration-200"
                :class="torrentFiles.has(dl.id)
                  ? 'border-violet-500/50 text-violet-300'
                  : 'border-violet-800/30 text-gray-400 hover:text-violet-300 hover:border-violet-500/50'"
                :disabled="loadingFiles.has(dl.id)"
                @click="toggleFiles(dl.id)"
              >
                {{ loadingFiles.has(dl.id) ? '...' : 'Files' }}
              </button>

              <!-- Retry button (failed only) -->
              <button
                v-if="dl.status === 'failed' || dl.status === 'import_failed'"
                class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-emerald-300 hover:border-emerald-500/50 transition-colors duration-200"
                @click="retryDownload(dl.id)"
              >
                Retry
              </button>

              <!-- Delete button -->
              <template v-if="confirmDeleteId !== dl.id">
                <button
                  class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-red-300 hover:border-red-500/50 transition-colors duration-200"
                  @click="confirmDeleteId = dl.id"
                >
                  Delete
                </button>
              </template>
              <template v-else>
                <span class="text-[10px] text-red-400">Delete files?</span>
                <button
                  class="text-[10px] px-2 py-1 rounded border border-red-500/50 text-red-300 hover:bg-red-500/20 transition-colors duration-200"
                  @click="deleteDownload(dl.id)"
                >
                  Yes
                </button>
                <button
                  class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-gray-300 transition-colors duration-200"
                  @click="confirmDeleteId = null"
                >
                  No
                </button>
              </template>
            </div>
          </div>
        </div>

        <!-- Torrent files tree -->
        <div
          v-if="torrentFiles.has(dl.id)"
          class="ml-8 pl-4 border-l-2 border-violet-900/20 mt-1 space-y-1"
        >
          <div
            v-if="!torrentFiles.get(dl.id)?.length"
            class="text-xs text-gray-500 py-1"
          >
            No file info available.
          </div>
          <div
            v-for="(file, idx) in torrentFiles.get(dl.id)"
            :key="idx"
            class="flex items-center justify-between gap-4 py-1.5 px-3 rounded bg-[#0f1320]/50"
          >
            <div class="min-w-0 flex-1">
              <p class="text-xs text-gray-300 truncate">{{ file.name }}</p>
              <div
                v-if="file.progress < 1"
                class="mt-1 h-1 rounded-full bg-gray-700/50 overflow-hidden"
              >
                <div
                  class="h-full rounded-full bg-violet-600/70"
                  :style="{ width: (file.progress * 100).toFixed(1) + '%' }"
                />
              </div>
            </div>
            <div class="flex items-center gap-2 flex-shrink-0">
              <span v-if="file.progress < 1" class="text-[10px] text-gray-500">{{ (file.progress * 100).toFixed(0) }}%</span>
              <span class="text-[10px] text-gray-500">{{ formatBytes(file.size) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
