<script setup lang="ts">
import { ref } from 'vue'
import SidebarA from './SidebarA.vue'
import TopBarA from './TopBarA.vue'
import MediaCardA from './MediaCardA.vue'
import { recentlyAdded, trending } from '../../media/dummyData'

const collapsed = ref(false)
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-[#0f172a]">
    <SidebarA v-model:collapsed="collapsed" />

    <div class="flex-1 flex flex-col min-w-0">
      <TopBarA />

      <main class="flex-1 overflow-y-auto p-8">
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
      </main>
    </div>
  </div>
</template>
