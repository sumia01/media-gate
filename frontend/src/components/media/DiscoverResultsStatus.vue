<script setup lang="ts">
import { LibraryBig, Loader2, SearchX, TriangleAlert } from 'lucide-vue-next'

withDefaults(
  defineProps<{
    ready: boolean
    loading: boolean
    initialLoading: boolean
    loadFailed: boolean
    hasMore: boolean
    itemCount: number
    hiddenCount: number
    emptyTitle?: string
  }>(),
  { emptyTitle: 'No titles found' },
)
defineEmits<{ loadMore: [] }>()
</script>

<template>
  <div v-if="ready && !initialLoading" class="flex flex-col items-center gap-3 py-8 text-center">
    <div v-if="loadFailed" role="alert" class="text-sm text-amber-300">
      <TriangleAlert class="mx-auto mb-2 h-6 w-6" />
      Could not load titles. Please try again.
    </div>
    <div v-else-if="loading" role="status" class="flex items-center gap-2 text-sm text-violet-300">
      <Loader2 class="h-5 w-5 animate-spin" /> Looking for more titles...
    </div>
    <div v-else-if="!itemCount" class="rounded-xl border border-violet-900/20 bg-violet-950/10 px-6 py-6">
      <LibraryBig v-if="hiddenCount" class="mx-auto mb-3 h-7 w-7 text-violet-400" />
      <SearchX v-else class="mx-auto mb-3 h-7 w-7 text-gray-500" />
      <p class="text-sm font-medium text-gray-300">{{ hiddenCount ? 'All loaded titles are in your library' : emptyTitle }}</p>
      <p v-if="hiddenCount" class="mt-1 text-xs text-gray-500">
        <template v-if="hasMore">Load more titles or turn off Hide in library to see these results.</template>
        <template v-else>Turn off Hide in library to see these results.</template>
      </p>
      <p v-else class="mt-1 text-xs text-gray-500">Try again later, or check your TMDB API key in settings.</p>
    </div>
    <p v-else-if="!hasMore" class="text-sm text-gray-500">No more titles</p>
    <button
      v-if="!loading && (hasMore || loadFailed)"
      type="button"
      class="rounded-lg border border-violet-500/30 bg-violet-600/10 px-4 py-2 text-sm text-violet-300 transition-colors hover:bg-violet-600/20 focus-visible:outline-2 focus-visible:outline-violet-400"
      @click="$emit('loadMore')"
    >
      {{ loadFailed ? 'Retry' : 'Load more' }}
    </button>
  </div>
</template>
