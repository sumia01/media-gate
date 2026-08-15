<script setup lang="ts">
import type { components } from '@/api/schema'

type ContentRating = components['schemas']['ContentRating']

// Rendered by both the library detail page and the external preview page, which
// display the same stats grid. The caller decides whether the tile exists at
// all (an absent contentRatings field means no countries are selected); this
// component only distinguishes "have ratings" from "none for your countries".
defineProps<{
  ratings: ContentRating[]
}>()
</script>

<template>
  <div class="px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20">
    <p class="text-xs text-gray-500 mb-1">Age Rating</p>
    <div v-if="ratings.length" class="flex flex-wrap items-center gap-1.5">
      <span
        v-for="r in ratings"
        :key="r.country"
        class="inline-flex items-baseline gap-1 px-1.5 py-0.5 rounded bg-violet-600/20 border border-violet-500/30"
      >
        <span class="text-[10px] text-violet-400/80">{{ r.country }}</span>
        <span class="text-sm font-semibold text-violet-100">{{ r.rating }}</span>
      </span>
    </div>
    <p v-else class="text-sm font-medium text-gray-500">No rating data found</p>
  </div>
</template>
