<script setup lang="ts">
import { ref, computed } from 'vue'
import { useRouter } from 'vue-router'
import { useJobQueue } from '@/composables/useJobQueue'
import { useGlobalSearch } from '@/composables/useGlobalSearch'
import { useAuth } from '@/composables/useAuth'

const router = useRouter()
const { jobs, hasActiveJob } = useJobQueue()
const { openSearch } = useGlobalSearch()
const { currentUser, logout } = useAuth()
const showPanel = ref(false)
const showUserMenu = ref(false)

const userInitial = computed(() => {
  const u = currentUser.value
  if (u?.firstName) return u.firstName[0].toUpperCase()
  if (u?.email) return u.email[0].toUpperCase()
  return 'U'
})

async function handleLogout() {
  showUserMenu.value = false
  await logout()
  router.push('/login')
}

function statusColor(status: string) {
  switch (status) {
    case 'running': return 'text-violet-400'
    case 'pending': return 'text-yellow-400'
    case 'completed': return 'text-emerald-400'
    case 'failed': return 'text-red-400'
    default: return 'text-gray-400'
  }
}

function statusIcon(status: string) {
  switch (status) {
    case 'running': return '\u21BB'
    case 'pending': return '\u23F3'
    case 'completed': return '\u2713'
    case 'failed': return '\u2717'
    default: return '\u2022'
  }
}
</script>

<template>
  <header class="h-16 flex items-center gap-4 px-6 flex-shrink-0 sticky top-0 z-10 bg-[#0c0f1a]/80 backdrop-blur-md border-b border-violet-900/20">
    <!-- Search -->
    <div class="flex-1 max-w-2xl">
      <div
        class="relative flex items-center border rounded-lg bg-[#161b2e] border-violet-800/30 hover:border-violet-500/50 transition-colors duration-200 cursor-pointer"
        @click="openSearch"
      >
        <span class="pl-3 text-gray-500 text-sm">&#x2315;</span>
        <span class="w-full py-2.5 px-3 text-sm text-gray-600 select-none">Search movies &amp; TV...</span>
      </div>
    </div>

    <div class="flex items-center gap-3 ml-auto">
      <!-- Sync status -->
      <div class="relative">
        <button
          class="relative p-2 rounded-lg text-gray-500 hover:text-violet-300 transition-colors duration-200"
          @click="showPanel = !showPanel"
        >
          <span class="text-lg" :class="hasActiveJob ? 'animate-spin text-violet-400' : ''">&#x21bb;</span>
          <span
            v-if="hasActiveJob"
            class="absolute top-1 right-1 w-2 h-2 rounded-full bg-violet-500 animate-pulse"
          />
        </button>

        <!-- Jobs dropdown -->
        <Teleport to="body">
          <div
            v-if="showPanel"
            class="fixed inset-0 z-40"
            @click="showPanel = false"
          />
        </Teleport>
        <div
          v-if="showPanel"
          class="absolute right-0 top-full mt-2 w-80 bg-[#0c0f1a] border border-violet-900/20 rounded-xl shadow-2xl z-50 overflow-hidden"
        >
          <div class="px-4 py-3 border-b border-violet-900/20">
            <p class="text-sm font-semibold text-gray-200">Jobs</p>
          </div>
          <div class="max-h-64 overflow-y-auto scrollbar-none">
            <div v-if="!jobs.length" class="px-4 py-6 text-center text-gray-500 text-sm">
              No recent jobs
            </div>
            <div
              v-for="job in jobs"
              :key="job.id"
              class="px-4 py-3 border-b border-violet-900/10 last:border-0"
            >
              <div class="flex items-center gap-2">
                <span :class="statusColor(job.status)" class="text-sm font-mono">{{ statusIcon(job.status) }}</span>
                <span class="text-sm text-gray-200 truncate flex-1">
                  Sync {{ job.libraryName || 'Library' }}
                </span>
                <span class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full" :class="{
                  'bg-violet-600/20 text-violet-300': job.status === 'running',
                  'bg-yellow-600/20 text-yellow-300': job.status === 'pending',
                  'bg-emerald-600/20 text-emerald-300': job.status === 'completed',
                  'bg-red-600/20 text-red-300': job.status === 'failed',
                }">
                  {{ job.status }}
                </span>
              </div>
              <p v-if="job.progress?.message" class="text-xs text-gray-500 mt-1">{{ job.progress.message }}</p>
              <p v-if="job.error" class="text-xs text-red-400 mt-1">{{ job.error }}</p>
            </div>
          </div>
        </div>
      </div>

      <!-- User menu -->
      <div class="relative">
        <button
          class="w-8 h-8 rounded-full bg-gray-700 flex items-center justify-center text-xs text-gray-300 cursor-pointer hover:ring-2 hover:ring-violet-500/50 transition-all"
          @click="showUserMenu = !showUserMenu"
        >
          {{ userInitial }}
        </button>

        <Teleport to="body">
          <div
            v-if="showUserMenu"
            class="fixed inset-0 z-40"
            @click="showUserMenu = false"
          />
          <div
            v-if="showUserMenu"
            class="fixed right-6 top-14 w-48 bg-[#0c0f1a] border border-violet-900/20 rounded-xl shadow-2xl z-50 overflow-hidden"
          >
            <div class="px-4 py-3 border-b border-violet-900/20">
              <p class="text-sm font-medium text-gray-200 truncate">{{ currentUser?.email }}</p>
            </div>
            <div class="py-1">
              <button
                class="w-full text-left px-4 py-2 text-sm text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors"
                @click="showUserMenu = false; router.push('/profile')"
              >
                Profile
              </button>
              <button
                class="w-full text-left px-4 py-2 text-sm text-red-400/70 hover:text-red-400 hover:bg-red-600/10 transition-colors"
                @click="handleLogout"
              >
                Sign Out
              </button>
            </div>
          </div>
        </Teleport>
      </div>
    </div>
  </header>
</template>
