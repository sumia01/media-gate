<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { X, Check, Download } from 'lucide-vue-next'
import client from '@/api/client'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import type { SubtitleSearchResult } from '@/types/api'

const props = defineProps<{
  mediaItemId: number
  title: string
  seasonNumber?: number
  episodeNumber?: number
}>()

const emit = defineEmits<{
  close: []
  downloaded: []
}>()

const results = ref<SubtitleSearchResult[]>([])
const loading = ref(false)
const error = ref('')
const downloadingIdx = ref<Set<number>>(new Set())
const downloadedIdx = ref<Set<number>>(new Set())

onMounted(() => {
  document.addEventListener('keydown', onKeydown)
  search()
})

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}

async function search() {
  loading.value = true
  error.value = ''
  results.value = []

  const { data, error: err } = await client.GET('/subtitles/search', {
    params: {
      query: {
        mediaItemId: props.mediaItemId,
        seasonNumber: props.seasonNumber,
        episodeNumber: props.episodeNumber,
      },
    },
  })
  loading.value = false
  if (err) {
    error.value = 'Search failed'
    return
  }
  results.value = data?.results ?? []
}

async function download(result: SubtitleSearchResult, idx: number) {
  if (downloadingIdx.value.has(idx) || downloadedIdx.value.has(idx)) return

  downloadingIdx.value = new Set([...downloadingIdx.value, idx])

  const { error: err } = await client.POST('/subtitles/download', {
    body: {
      mediaItemId: props.mediaItemId,
      providerName: result.providerName,
      providerFileId: result.providerFileId,
      language: result.language,
      seasonNumber: props.seasonNumber,
      episodeNumber: props.episodeNumber,
    },
  })

  const next = new Set(downloadingIdx.value)
  next.delete(idx)
  downloadingIdx.value = next

  if (err) {
    error.value = 'Failed to download subtitle'
    return
  }

  downloadedIdx.value = new Set([...downloadedIdx.value, idx])
  emit('downloaded')
}

function scoreColor(score: number): string {
  if (score >= 500) return 'text-emerald-400'
  if (score >= 200) return 'text-yellow-400'
  if (score >= 100) return 'text-orange-400'
  return 'text-gray-400'
}

function scoreBg(score: number): string {
  if (score >= 500) return 'bg-emerald-600/20'
  if (score >= 200) return 'bg-yellow-600/20'
  if (score >= 100) return 'bg-orange-600/20'
  return 'bg-gray-600/20'
}

function langLabel(code: string): string {
  return code.toUpperCase()
}
</script>

<template>
  <BaseModal max-width="max-w-5xl" @close="emit('close')">
    <!-- Header -->
    <div class="flex items-center justify-between mb-5">
      <h2 class="text-lg font-semibold text-gray-100">
        Subtitles &mdash; {{ title }}
        <span v-if="seasonNumber != null" class="text-gray-500 text-sm ml-1">
          S{{ String(seasonNumber).padStart(2, '0') }}<template v-if="episodeNumber != null">E{{ String(episodeNumber).padStart(2, '0') }}</template>
        </span>
      </h2>
      <button
        class="text-gray-500 hover:text-gray-300 text-lg transition-colors"
        @click="emit('close')"
      >
        <X class="w-4 h-4" />
      </button>
    </div>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading" class="flex items-center justify-center py-12">
      <span class="text-gray-500 text-sm animate-pulse">Searching for subtitles...</span>
    </div>

    <!-- No results -->
    <div
      v-else-if="!results.length"
      class="flex flex-col items-center justify-center py-12 text-gray-500"
    >
      <p class="text-sm">No subtitles found.</p>
    </div>

    <!-- Results table (desktop) -->
    <div v-else class="overflow-auto min-h-0">
      <table class="hidden md:table w-full text-sm">
        <thead class="sticky top-0 bg-[#0f1225]">
          <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-500 border-b border-violet-900/20">
            <th class="px-3 py-2.5">Language</th>
            <th class="px-3 py-2.5">Release</th>
            <th class="px-3 py-2.5 text-center">Score</th>
            <th class="px-3 py-2.5 text-center">Downloads</th>
            <th class="px-3 py-2.5 text-center">Flags</th>
            <th class="px-3 py-2.5"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(result, idx) in results"
            :key="idx"
            :class="[
              'border-b border-violet-900/10 hover:bg-violet-600/5 transition-colors duration-200',
              result.hashMatch ? 'bg-emerald-500/10' : '',
            ]"
          >
            <!-- Language -->
            <td class="px-3 py-2.5">
              <span class="text-xs font-bold px-2 py-0.5 rounded bg-violet-600/20 text-violet-300">
                {{ langLabel(result.language) }}
              </span>
            </td>

            <!-- Release name -->
            <td class="px-3 py-2.5">
              <span class="text-gray-200 truncate max-w-md block" :title="result.releaseName || result.fileName || ''">
                {{ result.releaseName || result.fileName || 'Unknown' }}
              </span>
            </td>

            <!-- Score -->
            <td class="px-3 py-2.5 text-center">
              <span
                class="text-xs font-bold px-2 py-0.5 rounded"
                :class="[scoreBg(result.score), scoreColor(result.score)]"
              >
                {{ result.score }}
              </span>
            </td>

            <!-- Downloads count -->
            <td class="px-3 py-2.5 text-center text-gray-400">
              {{ result.downloadCount ?? '-' }}
            </td>

            <!-- Flags -->
            <td class="px-3 py-2.5 text-center">
              <div class="flex items-center justify-center gap-1.5">
                <span
                  v-if="result.hashMatch"
                  class="text-[9px] font-bold px-1.5 py-0.5 rounded bg-emerald-600/20 text-emerald-300"
                  title="File hash match"
                >
                  HASH
                </span>
                <span
                  v-if="result.trusted"
                  class="text-[9px] font-bold px-1.5 py-0.5 rounded bg-blue-600/20 text-blue-300"
                  title="Trusted uploader"
                >
                  TRUSTED
                </span>
                <span
                  v-if="result.hearingImpaired"
                  class="text-[9px] font-bold px-1.5 py-0.5 rounded bg-amber-600/20 text-amber-300"
                  title="Hearing impaired"
                >
                  HI
                </span>
                <span
                  v-if="result.foreignPartsOnly"
                  class="text-[9px] font-bold px-1.5 py-0.5 rounded bg-red-600/20 text-red-300"
                  title="Foreign parts only"
                >
                  FPO
                </span>
              </div>
            </td>

            <!-- Actions -->
            <td class="px-3 py-2.5">
              <button
                v-if="downloadedIdx.has(idx)"
                class="px-2.5 py-1.5 rounded-md text-xs text-emerald-400"
                disabled
              >
                Downloaded
              </button>
              <button
                v-else
                class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-emerald-300 hover:bg-emerald-600/10 transition-colors duration-200"
                :disabled="downloadingIdx.has(idx)"
                @click="download(result, idx)"
              >
                {{ downloadingIdx.has(idx) ? 'Downloading...' : 'Download' }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>

      <!-- Results cards (mobile) -->
      <div class="md:hidden space-y-2">
        <div
          v-for="(result, idx) in results"
          :key="'m-' + idx"
          :class="[
            'px-3 py-2.5 rounded-lg border border-violet-900/20',
            result.hashMatch ? 'bg-emerald-500/10' : 'bg-[#161b2e]',
          ]"
        >
          <!-- Row 1: Language + Release -->
          <div class="flex items-center gap-2 mb-1.5">
            <span class="text-[10px] font-bold px-1.5 py-0.5 rounded bg-violet-600/20 text-violet-300 flex-shrink-0">
              {{ langLabel(result.language) }}
            </span>
            <span class="text-xs text-gray-200 truncate" :title="result.releaseName || result.fileName || ''">
              {{ result.releaseName || result.fileName || 'Unknown' }}
            </span>
          </div>
          <!-- Row 2: Score, flags, download -->
          <div class="flex items-center gap-2 text-[11px]">
            <span :class="[scoreBg(result.score), scoreColor(result.score)]" class="px-1.5 py-0.5 rounded font-bold">{{ result.score }}</span>
            <span v-if="result.downloadCount" class="text-gray-500">{{ result.downloadCount }} DL</span>
            <span v-if="result.hashMatch" class="text-emerald-300 font-bold">HASH</span>
            <span v-if="result.trusted" class="text-blue-300 font-bold">TRUSTED</span>
            <span v-if="result.hearingImpaired" class="text-amber-300">HI</span>
            <span class="ml-auto">
              <button
                v-if="downloadedIdx.has(idx)"
                class="text-emerald-400"
                disabled
              >
                <Check class="w-3.5 h-3.5" />
              </button>
              <button
                v-else
                class="text-gray-400 hover:text-emerald-300 transition-colors"
                :disabled="downloadingIdx.has(idx)"
                @click="download(result, idx)"
              >
                <Download class="w-3.5 h-3.5" />
              </button>
            </span>
          </div>
        </div>
      </div>

      <p class="mt-3 text-xs text-gray-600">{{ results.length }} results</p>
    </div>

    <!-- Hash match legend -->
    <div v-if="results.some(r => r.hashMatch)" class="flex items-center gap-1.5 mt-3 text-xs text-gray-500">
      <span class="inline-block w-3 h-3 rounded-sm bg-emerald-500/30"></span>
      <span>File hash match</span>
    </div>
  </BaseModal>
</template>
