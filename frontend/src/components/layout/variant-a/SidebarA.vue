<script setup lang="ts">
import {
  ArrowLeftRight,
  Clapperboard,
  Compass,
  Download,
  Eye,
  Library,
  Settings,
  SlidersHorizontal,
  Tv,
  Users,
} from 'lucide-vue-next'
import { type Component, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useSidebarLibraries } from '@/composables/useSidebarLibraries'
import { useSystemInfo } from '@/composables/useSystemInfo'
import { formatBytes } from '@/utils/media'

const collapsed = defineModel<boolean>('collapsed', { default: false })
const props = defineProps<{ isMobile: boolean }>()
const route = useRoute()
const { libraries, refreshLibraries } = useSidebarLibraries()
const { currentUser, isAdmin } = useAuth()
const { version, disk, refreshSystemInfo } = useSystemInfo()

const userInitial = computed(() => {
  const u = currentUser.value
  if (!u) return 'U'
  if (u.firstName) return u.firstName.charAt(0).toUpperCase()
  return u.email.charAt(0).toUpperCase()
})

const userName = computed(() => {
  const u = currentUser.value
  const parts = [u?.firstName, u?.lastName].filter(Boolean)
  return parts.length ? parts.join(' ') : (u?.email ?? 'User')
})

const staticTop = [
  { icon: Compass, label: 'Discover', to: '/' },
  { icon: Eye, label: 'Watched', to: '/watched' },
  { icon: Download, label: 'Downloads', to: '/downloads' },
]

const staticBottom = [
  { icon: Library, label: 'Libraries', to: '/libraries', admin: true },
  { icon: ArrowLeftRight, label: 'Indexers', to: '/indexers', admin: true },
  { icon: SlidersHorizontal, label: 'Profiles', to: '/media-profiles', admin: true },
  { icon: Settings, label: 'Settings', to: '/settings', admin: true },
  { icon: Users, label: 'Users', to: '/users', admin: true },
]

const visibleBottom = computed(() => staticBottom.filter((item) => !item.admin || isAdmin.value))

const mediaTypeIcons: Record<string, Component> = {
  movie: Clapperboard,
  series: Tv,
}

const diskPercent = computed(() => {
  if (!disk.value?.totalBytes) return 0
  return Math.round((disk.value.usedBytes / disk.value.totalBytes) * 100)
})

const diskLabel = computed(() => {
  if (!disk.value) return ''
  return `${formatBytes(disk.value.usedBytes)} / ${formatBytes(disk.value.totalBytes)}`
})

const diskTooltip = computed(() => {
  if (!disk.value) return version.value || ''
  return `${version.value}\n${diskLabel.value} (${diskPercent.value}% used)`
})

function closeMobile() {
  if (props.isMobile) collapsed.value = true
}

onMounted(() => {
  refreshLibraries()
  refreshSystemInfo()
})
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
    <router-link to="/" class="flex items-center h-16 px-4 gap-3 no-underline">
      <img src="/small_logo.png" alt="MediaGate" class="w-8 h-8 flex-shrink-0" />
      <span v-if="!collapsed" class="font-semibold text-sm tracking-tight text-[#c4b5fd] transition-opacity duration-200" style="text-shadow: 0 0 12px rgba(255, 255, 255, 0.3)">
        MediaGate
      </span>
    </router-link>

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
        <span class="text-base w-5 text-center flex-shrink-0">
          <component :is="item.icon" class="w-4 h-4 inline-block" />
        </span>
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
          <span class="text-base w-5 text-center flex-shrink-0">
            <component :is="mediaTypeIcons[lib.mediaType] ?? Clapperboard" class="w-4 h-4 inline-block" />
          </span>
          <span v-if="!collapsed" class="truncate">{{ lib.name }}</span>
        </RouterLink>
      </div>

      <!-- Divider -->
      <div class="pt-3">
        <div v-if="!collapsed" class="border-t border-violet-900/20 mb-2" />
        <div v-else class="border-t border-violet-900/20 mb-2" />
        <RouterLink
          v-for="item in visibleBottom"
          :key="item.label"
          :to="item.to"
          class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
          :class="(route.path === item.to || route.path.startsWith(item.to + '/')) && item.to !== '/'
            ? 'bg-violet-600/20 text-violet-300'
            : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
          @click="closeMobile"
        >
          <span class="text-base w-5 text-center flex-shrink-0">
            <component :is="item.icon" class="w-4 h-4 inline-block" />
          </span>
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

    <!-- System Info -->
    <div class="px-3 py-2 border-t border-violet-900/20">
      <template v-if="!collapsed">
        <p class="text-[10px] text-gray-600 truncate">
          {{ version }}<template v-if="disk"> · {{ diskLabel }}</template>
        </p>
        <div v-if="disk" class="mt-1 h-1 bg-gray-800 rounded-full overflow-hidden">
          <div
            class="h-full rounded-full transition-all duration-300"
            :class="diskPercent > 90 ? 'bg-red-500/70' : diskPercent > 75 ? 'bg-amber-500/50' : 'bg-violet-600/50'"
            :style="{ width: diskPercent + '%' }"
          />
        </div>
      </template>
      <div v-else class="text-[9px] text-gray-600 text-center" :title="diskTooltip">
        {{ version }}
      </div>
    </div>
  </aside>
</template>
