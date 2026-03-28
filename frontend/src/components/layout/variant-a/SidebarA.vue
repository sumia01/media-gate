<script setup lang="ts">
import { onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useSidebarLibraries } from '@/composables/useSidebarLibraries'

const collapsed = defineModel<boolean>('collapsed', { default: false })
const route = useRoute()
const { libraries, refreshLibraries } = useSidebarLibraries()

const staticTop = [
  { icon: '◈', label: 'Discover', to: '/' },
]

const staticBottom = [
  { icon: '⛁', label: 'Libraries', to: '/libraries' },
  { icon: '⚙', label: 'Settings', to: '/settings' },
]

function mediaTypeIcon(type: string) {
  return type === 'movie' ? '◻' : '▤'
}

onMounted(refreshLibraries)
</script>

<template>
  <aside
    class="h-screen flex flex-col flex-shrink-0 bg-[#0c0f1a] border-r border-violet-900/20 transition-all duration-300"
    :class="collapsed ? 'w-16' : 'w-56'"
  >
    <!-- Brand -->
    <div class="flex items-center h-16 px-4 gap-3">
      <div class="w-8 h-8 rounded-lg flex items-center justify-center font-black text-sm text-violet-400 flex-shrink-0">
        MG
      </div>
      <span v-if="!collapsed" class="font-semibold text-sm tracking-tight text-violet-400 transition-opacity duration-200">
        Media Gate
      </span>
    </div>

    <!-- Nav -->
    <nav class="flex-1 px-2 mt-2 space-y-1 overflow-y-auto scrollbar-none">
      <!-- Static top items -->
      <RouterLink
        v-for="item in staticTop"
        :key="item.label"
        :to="item.to"
        class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
        :class="route.path === item.to
          ? 'bg-violet-600/20 text-violet-300'
          : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
      >
        <span class="text-base w-5 text-center flex-shrink-0">{{ item.icon }}</span>
        <span v-if="!collapsed" class="truncate">{{ item.label }}</span>
      </RouterLink>

      <!-- Libraries section -->
      <div v-if="libraries.length" class="pt-3">
        <p v-if="!collapsed" class="px-3 pb-1.5 text-[10px] font-semibold uppercase tracking-wider text-gray-600">
          Libraries
        </p>
        <div v-else class="border-t border-violet-900/20 mb-2" />
        <RouterLink
          v-for="lib in libraries"
          :key="lib.id"
          :to="'/library/' + lib.id"
          class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
          :class="route.path === '/library/' + lib.id
            ? 'bg-violet-600/20 text-violet-300'
            : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
        >
          <span class="text-base w-5 text-center flex-shrink-0">{{ mediaTypeIcon(lib.mediaType) }}</span>
          <span v-if="!collapsed" class="truncate">{{ lib.name }}</span>
        </RouterLink>
      </div>

      <!-- Divider -->
      <div class="pt-3">
        <div v-if="!collapsed" class="border-t border-violet-900/20 mb-2" />
        <div v-else class="border-t border-violet-900/20 mb-2" />
        <RouterLink
          v-for="item in staticBottom"
          :key="item.label"
          :to="item.to"
          class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
          :class="route.path === item.to && item.to !== '/'
            ? 'bg-violet-600/20 text-violet-300'
            : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
        >
          <span class="text-base w-5 text-center flex-shrink-0">{{ item.icon }}</span>
          <span v-if="!collapsed" class="truncate">{{ item.label }}</span>
        </RouterLink>
      </div>
    </nav>

    <!-- Collapse toggle -->
    <div class="px-2 mb-2">
      <button
        class="w-full flex items-center justify-center gap-3 px-3 py-2.5 rounded-lg text-sm text-gray-500 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
        @click="collapsed = !collapsed"
      >
        <span class="text-base transition-transform duration-300" :class="collapsed ? 'rotate-180' : ''">&#xAB;</span>
        <span v-if="!collapsed">Collapse</span>
      </button>
    </div>

    <!-- User -->
    <div class="px-3 py-3 border-t border-violet-900/20">
      <div class="flex items-center gap-3">
        <div class="w-8 h-8 rounded-full bg-gray-700 flex items-center justify-center text-xs text-gray-300 flex-shrink-0">U</div>
        <div v-if="!collapsed" class="overflow-hidden">
          <p class="text-sm font-medium text-gray-300 truncate">User</p>
        </div>
      </div>
    </div>
  </aside>
</template>
