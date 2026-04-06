<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useSidebarLibraries } from '@/composables/useSidebarLibraries'
import { useAuth } from '@/composables/useAuth'

const collapsed = defineModel<boolean>('collapsed', { default: false })
const props = defineProps<{ isMobile: boolean }>()
const route = useRoute()
const { libraries, refreshLibraries } = useSidebarLibraries()
const { currentUser } = useAuth()

const userInitial = computed(() => {
  const u = currentUser.value
  if (!u) return 'U'
  if (u.firstName) return u.firstName.charAt(0).toUpperCase()
  return u.email.charAt(0).toUpperCase()
})

const userName = computed(() => {
  const u = currentUser.value
  const parts = [u?.firstName, u?.lastName].filter(Boolean)
  return parts.length ? parts.join(' ') : u?.email ?? 'User'
})

const staticTop = [
  { icon: '\u25C8', label: 'Discover', to: '/' },
  { icon: '\u25C9', label: 'Watched', to: '/watched' },
]

const staticBottom = [
  { icon: '\u26C1', label: 'Libraries', to: '/libraries' },
  { icon: '\u21CC', label: 'Indexers', to: '/indexers' },
  { icon: '\u25A6', label: 'Profiles', to: '/media-profiles' },
  { icon: '\u2699', label: 'Settings', to: '/settings' },
  { icon: '\u2661', label: 'Users', to: '/users' },
]

function mediaTypeIcon(type: string) {
  return type === 'movie' ? '◻' : '▤'
}

function closeMobile() {
  if (props.isMobile) collapsed.value = true
}

onMounted(refreshLibraries)
</script>

<template>
  <aside
    v-show="!isMobile || !collapsed"
    class="h-screen flex flex-col flex-shrink-0 bg-[#0c0f1a] border-r border-violet-900/20 transition-all duration-300"
    :class="isMobile
      ? 'fixed inset-y-0 left-0 z-40 w-56'
      : collapsed ? 'w-16' : 'w-56'"
  >
    <!-- Brand -->
    <div class="flex items-center h-16 px-4 gap-3">
      <img src="/small_logo.png" alt="MediaGate" class="w-8 h-8 flex-shrink-0" />
      <span v-if="!collapsed" class="font-semibold text-sm tracking-tight text-[#c4b5fd] transition-opacity duration-200" style="text-shadow: 0 0 12px rgba(255, 255, 255, 0.3)">
        MediaGate
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
        @click="closeMobile"
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
          @click="closeMobile"
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
          :class="(route.path === item.to || route.path.startsWith(item.to + '/')) && item.to !== '/'
            ? 'bg-violet-600/20 text-violet-300'
            : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
          @click="closeMobile"
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
        <template v-if="isMobile">
          <span class="text-base">&times;</span>
          <span>Close</span>
        </template>
        <template v-else>
          <span class="text-base transition-transform duration-300" :class="collapsed ? 'rotate-180' : ''">&#xAB;</span>
          <span v-if="!collapsed">Collapse</span>
        </template>
      </button>
    </div>

    <!-- User -->
    <RouterLink to="/profile" class="block px-3 py-3 border-t border-violet-900/20 hover:bg-violet-600/10 transition-colors" @click="closeMobile">
      <div class="flex items-center gap-3">
        <div class="w-8 h-8 rounded-full bg-gray-700 flex items-center justify-center text-xs text-gray-300 flex-shrink-0">{{ userInitial }}</div>
        <div v-if="!collapsed" class="overflow-hidden">
          <p class="text-sm font-medium text-gray-300 truncate">{{ userName }}</p>
        </div>
      </div>
    </RouterLink>
  </aside>
</template>
