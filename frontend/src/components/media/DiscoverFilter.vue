<script setup lang="ts">
import { LibraryBig, Loader2 } from 'lucide-vue-next'

defineProps<{
  hideInLibrary: boolean
  libraryLoading: boolean
  libraryFailed: boolean
}>()
defineEmits<{
  'update:hideInLibrary': [value: boolean]
  retry: []
}>()
</script>

<template>
  <div class="mb-6 rounded-lg border border-violet-900/30 bg-[#161b2e] px-4 py-3">
    <div class="flex flex-wrap items-center gap-x-5 gap-y-3">
      <label class="inline-flex cursor-pointer items-center gap-2.5 text-sm text-gray-200">
        <input
          type="checkbox"
          :checked="hideInLibrary"
          class="h-4 w-4 accent-violet-500 focus-visible:outline-2 focus-visible:outline-violet-400"
          @change="$emit('update:hideInLibrary', ($event.target as HTMLInputElement).checked)"
        />
        <LibraryBig class="h-4 w-4 text-violet-400" />
        Hide in library
      </label>
      <span v-if="hideInLibrary && libraryLoading" role="status" class="inline-flex items-center gap-2 text-xs text-violet-300">
        <Loader2 class="h-3.5 w-3.5 animate-spin" /> Checking your library...
      </span>
      <span v-else-if="libraryFailed" role="alert" class="text-sm text-amber-300">
        {{ hideInLibrary ? 'Could not check your library. Retry or turn off the filter to browse.' : 'Library badges are unavailable.' }}
        <button type="button" class="ml-2 underline underline-offset-2 hover:text-amber-200" @click="$emit('retry')">Retry</button>
      </span>
    </div>
    <p v-if="hideInLibrary" class="mt-2 text-xs text-gray-500">
      Matches use the same metadata provider. TVDB-matched titles may still appear in TMDB results.
    </p>
  </div>
</template>
