<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import { Trash2 } from 'lucide-vue-next'
import client from '@/api/client'
import { useEventStream } from '@/composables/useEventStream'
import type { Subtitle } from '@/types/api'

const props = defineProps<{
  mediaItemId: number
  refreshKey?: number
}>()

const emit = defineEmits<{
  searchSubtitles: []
}>()

const { on, off } = useEventStream()
const subtitles = ref<Subtitle[]>([])
const loading = ref(false)

async function fetchSubtitles() {
  if (!subtitles.value.length) loading.value = true
  const { data } = await client.GET('/subtitles', {
    params: { query: { mediaItemId: props.mediaItemId } },
  })
  subtitles.value = data?.items ?? []
  loading.value = false
}

async function deleteSubtitle(id: number) {
  await client.DELETE('/subtitles/{id}', {
    params: { path: { id } },
  })
  subtitles.value = subtitles.value.filter((s) => s.id !== id)
}

function handleSubtitleEvent(data: any) {
  if (data.mediaItemId === props.mediaItemId) {
    fetchSubtitles()
  }
}

function langLabel(code: string): string {
  return code.toUpperCase()
}

function formatDate(dateStr: string): string {
  return new Date(dateStr).toLocaleDateString('hu-HU', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

onMounted(() => {
  fetchSubtitles()
  on('subtitle.downloaded', handleSubtitleEvent)
  on('subtitle.deleted', handleSubtitleEvent)
  on('subtitle.auto_search_completed', handleSubtitleEvent)
})

onUnmounted(() => {
  off('subtitle.downloaded', handleSubtitleEvent)
  off('subtitle.deleted', handleSubtitleEvent)
  off('subtitle.auto_search_completed', handleSubtitleEvent)
})

watch(
  () => props.mediaItemId,
  () => fetchSubtitles(),
)
watch(
  () => props.refreshKey,
  () => fetchSubtitles(),
)
</script>

<template>
  <div>
    <div class="flex items-center gap-3 mb-4">
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500">Subtitles</h2>
      <span
        v-if="subtitles.length"
        class="text-[10px] font-bold px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
      >
        {{ subtitles.length }}
      </span>
      <button
        class="ml-auto px-3 py-1.5 rounded-lg border border-violet-800/30 text-xs text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200"
        @click="emit('searchSubtitles')"
      >
        Search Subtitles
      </button>
    </div>

    <div v-if="loading" class="text-sm text-gray-500">Loading...</div>

    <div v-else-if="!subtitles.length" class="text-sm text-gray-500">No subtitles downloaded.</div>

    <div v-else class="space-y-2">
      <div
        v-for="sub in subtitles"
        :key="sub.id"
        class="flex items-center gap-3 px-4 py-2.5 rounded-lg bg-[#161b2e] border border-violet-900/20"
      >
        <!-- Language badge -->
        <span class="flex-shrink-0 text-xs font-bold px-2 py-0.5 rounded bg-violet-600/20 text-violet-300">
          {{ langLabel(sub.language) }}
        </span>

        <!-- File info -->
        <div class="flex-1 min-w-0">
          <p class="text-sm text-gray-200 truncate" :title="sub.fileName">{{ sub.fileName }}</p>
          <div class="flex items-center gap-2 mt-0.5">
            <span class="text-[11px] text-gray-500">{{ sub.provider }}</span>
            <span v-if="sub.format" class="text-[11px] text-gray-500">{{ sub.format }}</span>
            <span v-if="sub.score" class="text-[11px] text-gray-500">Score: {{ sub.score }}</span>
            <span v-if="sub.source === 'auto'" class="text-[10px] font-bold px-1.5 py-0.5 rounded bg-sky-600/20 text-sky-300">AUTO</span>
            <span v-if="sub.hearingImpaired" class="text-[10px] font-bold px-1.5 py-0.5 rounded bg-amber-600/20 text-amber-300">HI</span>
          </div>
        </div>

        <!-- Date -->
        <span v-if="sub.createdAt" class="flex-shrink-0 text-[11px] text-gray-500 hidden md:block">
          {{ formatDate(sub.createdAt) }}
        </span>

        <!-- Delete button -->
        <button
          class="flex-shrink-0 text-gray-500 hover:text-red-400 transition-colors duration-200"
          title="Delete subtitle"
          @click="deleteSubtitle(sub.id)"
        >
          <Trash2 class="w-4 h-4" />
        </button>
      </div>
    </div>
  </div>
</template>
