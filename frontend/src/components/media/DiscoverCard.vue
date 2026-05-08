<script setup lang="ts">
import { Eye, Home } from 'lucide-vue-next'
import type { DiscoverItem } from '@/types/api'

defineProps<{
  item: DiscoverItem
  inLibrary?: boolean
  watched?: boolean
}>()

defineEmits<{
  click: [item: DiscoverItem]
}>()
</script>

<template>
  <div
    class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 hover:border-violet-500/40 transition-colors duration-200 cursor-pointer"
    @click="$emit('click', item)"
  >
    <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center overflow-hidden relative">
      <img
        v-if="item.posterUrl"
        :src="item.posterUrl"
        :alt="item.title"
        class="w-full h-full object-cover"
        loading="lazy"
      />
      <div class="absolute top-2 left-2 z-10 flex items-center gap-1">
        <span class="px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded"
          :class="item.mediaType === 'movie' ? 'bg-violet-600/90 text-violet-100' : 'bg-fuchsia-600/90 text-fuchsia-100'"
        >
          {{ item.mediaType }}
        </span>
        <span v-if="inLibrary" class="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded bg-sky-600/90 text-sky-100">
          <Home class="w-2.5 h-2.5" />
          in library
        </span>
        <span v-if="watched" class="inline-flex items-center gap-0.5 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded bg-emerald-600/90 text-emerald-100">
          <Eye class="w-2.5 h-2.5" />
          seen
        </span>
      </div>
      <div v-if="item.rating" class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
        &#9733; {{ item.rating.toFixed(1) }}
      </div>
    </div>
    <div class="p-3">
      <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
      <p class="text-xs text-gray-500 mt-1">{{ item.year }}</p>
    </div>
  </div>
</template>
