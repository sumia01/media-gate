<script setup lang="ts">
import MediaCardA from '@/components/layout/variant-a/MediaCardA.vue'
import { recentlyAdded, trending } from '@/components/media/dummyData'
</script>

<template>
  <div>
    <!-- Recently Added -->
    <section class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Recently Added</h2>
      <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <MediaCardA v-for="item in recentlyAdded" :key="item.id" :item="item" />
      </div>
    </section>

    <!-- Trending -->
    <section class="mb-10">
      <h2 class="text-lg font-semibold mb-4 text-gray-100 tracking-tight">Trending</h2>
      <div class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 2xl:grid-cols-7 gap-5">
        <MediaCardA v-for="item in trending" :key="item.id" :item="item" />
      </div>
    </section>
  </div>
</template>
