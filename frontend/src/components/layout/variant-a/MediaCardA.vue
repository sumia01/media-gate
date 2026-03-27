<script setup lang="ts">
import type { MediaItem } from '../../media/dummyData'

defineProps<{ item: MediaItem }>()
</script>

<template>
  <div class="group relative cursor-pointer transition-all duration-300 hover:shadow-[0_0_24px_rgba(139,92,246,0.3)] hover:scale-[1.03]">
    <div
      class="aspect-[2/3] w-full rounded-xl overflow-hidden relative"
      :style="{ backgroundColor: item.posterColor }"
    >
      <!-- Poster image -->
      <img
        :src="item.posterUrl"
        :alt="item.title"
        class="absolute inset-0 w-full h-full object-cover"
        loading="lazy"
      />

      <!-- Badge -->
      <span class="absolute top-2 left-2 z-10 px-2 py-0.5 text-[10px] font-bold uppercase tracking-wider rounded"
        :class="item.type === 'movie' ? 'bg-violet-600/90 text-violet-100' : 'bg-fuchsia-600/90 text-fuchsia-100'"
      >
        {{ item.type }}
      </span>

      <!-- Rating -->
      <div class="absolute bottom-2 right-2 z-10 text-[11px] font-semibold text-white/90 bg-black/50 px-1.5 py-0.5 rounded backdrop-blur-sm">
        ★ {{ item.rating }}
      </div>

      <!-- Bottom gradient -->
      <div class="absolute inset-x-0 bottom-0 h-1/3 bg-gradient-to-t from-black/70 to-transparent rounded-b-xl" />
    </div>
    <div class="mt-2 px-0.5">
      <p class="text-sm font-medium text-gray-100 truncate">{{ item.title }}</p>
      <p class="text-xs text-gray-500">{{ item.year }}</p>
    </div>
  </div>
</template>
