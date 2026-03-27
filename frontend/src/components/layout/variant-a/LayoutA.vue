<script setup lang="ts">
import { ref } from 'vue'
import SidebarA from './SidebarA.vue'
import TopBarA from './TopBarA.vue'

const collapsed = ref(false)
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-[#0f172a]">
    <SidebarA v-model:collapsed="collapsed" />

    <div class="flex-1 flex flex-col min-w-0">
      <TopBarA />

      <main class="flex-1 overflow-y-auto p-8">
        <slot />
      </main>
    </div>
  </div>
</template>
