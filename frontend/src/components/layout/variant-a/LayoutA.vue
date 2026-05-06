<script setup lang="ts">
import { onMounted, onUnmounted, provide, ref } from 'vue'
import GlobalSearchOverlay from '@/components/media/GlobalSearchOverlay.vue'
import { useGlobalSearch } from '@/composables/useGlobalSearch'
import SidebarA from './SidebarA.vue'
import TopBarA from './TopBarA.vue'

const MD_BREAKPOINT = 768
const isMobile = ref(window.innerWidth < MD_BREAKPOINT)
const collapsed = ref(isMobile.value)
const { searchOpen } = useGlobalSearch()

function onResize() {
  const mobile = window.innerWidth < MD_BREAKPOINT
  if (mobile !== isMobile.value) {
    isMobile.value = mobile
    collapsed.value = mobile
  }
}

function toggleSidebar() {
  collapsed.value = !collapsed.value
}

provide('isMobile', isMobile)
provide('toggleSidebar', toggleSidebar)

onMounted(() => window.addEventListener('resize', onResize))
onUnmounted(() => window.removeEventListener('resize', onResize))
</script>

<template>
  <div class="flex h-screen overflow-hidden bg-[#0f172a]">
    <!-- Mobile backdrop -->
    <Teleport to="body">
      <div
        v-if="isMobile && !collapsed"
        class="fixed inset-0 z-30 bg-black/50"
        @click="collapsed = true"
      />
    </Teleport>

    <SidebarA v-model:collapsed="collapsed" :is-mobile="isMobile" />

    <div class="flex-1 flex flex-col min-w-0">
      <TopBarA />

      <main class="flex-1 overflow-y-auto p-4 md:p-8">
        <RouterView />
      </main>
    </div>

    <Teleport to="body">
      <GlobalSearchOverlay v-if="searchOpen" />
    </Teleport>
  </div>
</template>
