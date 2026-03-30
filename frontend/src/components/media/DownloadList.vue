<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import client from '@/api/client'
import type { Download, TorrentFile } from '@/types/api'

const props = defineProps<{
  mediaItemId: number
  imdbId?: string
  mediaType: 'movie' | 'series'
  title: string
  refreshKey?: number
}>()

const emit = defineEmits<{
  openSearch: [seasonNumber?: number, episodeNumber?: number, episodeId?: number]
  replace: [downloadId: number, seasonNumber?: number, episodeNumber?: number, episodeId?: number]
  downloadsChanged: []
}>()

const downloads = ref<Download[]>([])
const expanded = ref(true)
const torrentFiles = ref<Map<number, TorrentFile[]>>(new Map())
const loadingFiles = ref<Set<number>>(new Set())
const pollTimer = ref<ReturnType<typeof setInterval> | null>(null)

const statusOrder: Record<string, number> = {
  downloading: 0,
  pending: 1,
  seeding: 2,
  downloaded: 3,
  importing: 4,
  completed: 5,
  failed: 6,
}

const sortedDownloads = computed(() =>
  [...downloads.value].sort((a, b) => (statusOrder[a.status] ?? 9) - (statusOrder[b.status] ?? 9))
)

const hasActiveDownloads = computed(() =>
  downloads.value.some(d => d.status === 'pending' || d.status === 'downloading' || d.status === 'seeding')
)

async function fetchDownloads() {
  const { data } = await client.GET('/downloads', {
    params: { query: { mediaItemId: props.mediaItemId } },
  })
  downloads.value = data?.downloads ?? []
  managePolling()
}

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

async function deleteDownload(id: number, deleteFiles = false) {
  await client.DELETE('/downloads/{id}', {
    params: { path: { id }, query: { deleteFiles } },
  })
  const newMap = new Map(torrentFiles.value)
  newMap.delete(id)
  torrentFiles.value = newMap
  await fetchDownloads()
  emit('downloadsChanged')
}

function replaceDownload(dl: Download) {
  emit('replace', dl.id, dl.seasonNumber ?? undefined, undefined, dl.episodeId ?? undefined)
}

function managePolling() {
  if (hasActiveDownloads.value && !pollTimer.value) {
    pollTimer.value = setInterval(fetchDownloads, 5000)
  } else if (!hasActiveDownloads.value && pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

function stopPolling() {
  if (pollTimer.value) {
    clearInterval(pollTimer.value)
    pollTimer.value = null
  }
}

function statusColor(status: string) {
  switch (status) {
    case 'pending': return 'bg-gray-600/20 text-gray-400'
    case 'downloading': return 'bg-sky-600/20 text-sky-300'
    case 'downloaded': return 'bg-sky-600/20 text-sky-300'
    case 'importing': return 'bg-sky-600/20 text-sky-300'
    case 'seeding': return 'bg-emerald-600/20 text-emerald-300'
    case 'completed': return 'bg-emerald-600/20 text-emerald-300'
    case 'failed': return 'bg-red-600/20 text-red-300'
    default: return 'bg-gray-600/20 text-gray-400'
  }
}

function formatSize(sizeStr?: string): string {
  if (!sizeStr) return ''
  const bytes = parseInt(sizeStr, 10)
  if (isNaN(bytes) || bytes === 0) return sizeStr
  if (bytes < 1024) return bytes + ' B'
  const kb = bytes / 1024
  if (kb < 1024) return kb.toFixed(1) + ' KB'
  const mb = kb / 1024
  if (mb < 1024) return mb.toFixed(1) + ' MB'
  return (mb / 1024).toFixed(1) + ' GB'
}

function formatSizeNum(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  const kb = bytes / 1024
  if (kb < 1024) return kb.toFixed(1) + ' KB'
  const mb = kb / 1024
  if (mb < 1024) return mb.toFixed(1) + ' MB'
  return (mb / 1024).toFixed(1) + ' GB'
}

function formatSpeed(bytesPerSec?: number): string {
  if (!bytesPerSec || bytesPerSec <= 0) return ''
  if (bytesPerSec < 1024) return bytesPerSec + ' B/s'
  const kb = bytesPerSec / 1024
  if (kb < 1024) return kb.toFixed(1) + ' KB/s'
  const mb = kb / 1024
  return mb.toFixed(1) + ' MB/s'
}

onMounted(fetchDownloads)
onUnmounted(stopPolling)
watch(() => props.mediaItemId, fetchDownloads)
watch(() => props.refreshKey, fetchDownloads)
</script>

<template>
  <div v-if="downloads.length">
    <button
      class="flex items-center gap-3 group cursor-pointer"
      @click="expanded = !expanded"
    >
      <span
        class="text-gray-500 text-xs transition-transform duration-200"
        :class="{ 'rotate-90': expanded }"
      >&#9654;</span>
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 group-hover:text-gray-400 transition-colors duration-200">Downloads</h2>
      <span
        class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
      >
        {{ downloads.length }}
      </span>
    </button>

    <div v-if="expanded" class="mt-4 space-y-2">
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
                  {{ dl.status }}
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

              <p class="text-sm font-medium text-gray-200 truncate mt-1">{{ dl.title }}</p>

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
              <span v-if="dl.size" class="text-xs text-gray-500">{{ formatSize(dl.size) }}</span>

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
                v-if="dl.status === 'failed'"
                class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-emerald-300 hover:border-emerald-500/50 transition-colors duration-200"
                @click="retryDownload(dl.id)"
              >
                Retry
              </button>

              <!-- Replace button -->
              <button
                v-if="dl.status !== 'pending'"
                class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200"
                @click="replaceDownload(dl)"
              >
                Replace
              </button>

              <!-- Delete button -->
              <button
                class="text-[10px] px-2 py-1 rounded border border-violet-800/30 text-gray-400 hover:text-red-300 hover:border-red-500/50 transition-colors duration-200"
                @click="deleteDownload(dl.id)"
              >
                Delete
              </button>
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
              <!-- Per-file progress bar -->
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
              <span class="text-[10px] text-gray-500">{{ formatSizeNum(file.size) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
