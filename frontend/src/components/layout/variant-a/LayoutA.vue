<script setup lang="ts">
import { ref } from 'vue'
import { RouterView } from 'vue-router'
import SidebarA from './SidebarA.vue'
import TopBarA from './TopBarA.vue'
import GlobalSearchOverlay from '@/components/media/GlobalSearchOverlay.vue'
import { useGlobalSearch } from '@/composables/useGlobalSearch'

const collapsed = ref(false)
const { searchOpen } = useGlobalSearch()
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-[#0f172a]">
    <SidebarA v-model:collapsed="collapsed" />

    <div class="flex-1 flex flex-col min-w-0">
      <TopBarA />

      <main class="flex-1 overflow-y-auto p-8">
        <RouterView />
      </main>
    </div>

    <Teleport to="body">
      <GlobalSearchOverlay v-if="searchOpen" />
    </Teleport>
  </div>
</template>
