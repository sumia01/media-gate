<script setup lang="ts">
import { Download, Menu, RefreshCw, Search } from 'lucide-vue-next'
import type { Ref } from 'vue'
import { computed, inject, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import { useGlobalSearch } from '@/composables/useGlobalSearch'
import { useUpdateCheck } from '@/composables/useUpdateCheck'
import { useWorkers } from '@/composables/useWorkers'

const router = useRouter()
const { workers, runWorker, fetchWorkers } = useWorkers()
const { openSearch } = useGlobalSearch()
const { currentUser, logout } = useAuth()
const { updateAvailable, latestVersion } = useUpdateCheck()
const showPanel = ref(false)
const showUserMenu = ref(false)
const showUpdatePanel = ref(false)

const isMobile = inject<Ref<boolean>>('isMobile')
const toggleSidebar = inject<() => void>('toggleSidebar')

const hasRunningWorker = computed(() => workers.value.some((w) => w.running))

const userInitial = computed(() => {
  const u = currentUser.value
  if (!u) return 'U'
  if (u.firstName) return u.firstName.charAt(0).toUpperCase()
  return u.email.charAt(0).toUpperCase()
})

async function handleLogout() {
  showUserMenu.value = false
  await logout()
  router.push('/login')
}

function openWorkersPanel() {
  fetchWorkers()
  showPanel.value = !showPanel.value
}

const workerLabels: Record<string, string> = {
  monitor: 'Monitor',
  'metadata-refresh': 'Metadata Refresh',
  'indexer-def-refresh': 'Indexer Definitions',
  'update-check': 'Update Check',
}

function workerLabel(name: string) {
  return workerLabels[name] || name
}

function formatTime(iso?: string) {
  if (!iso) return '\u2014'
  const d = new Date(iso)
  return d.toLocaleString(undefined, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

async function handleRun(name: string) {
  await runWorker(name)
}
</script>

<template>
  <header class="h-16 flex items-center gap-4 px-4 md:px-6 flex-shrink-0 sticky top-0 z-10 bg-[#0c0f1a]/80 backdrop-blur-md border-b border-violet-900/20">
    <!-- Hamburger (mobile only) -->
    <button
      v-if="isMobile"
      class="p-2 rounded-lg text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
      @click="toggleSidebar?.()"
    >
      <Menu class="w-5 h-5" />
    </button>

    <!-- Search -->
    <div class="flex-1 max-w-2xl">
      <div
        class="relative flex items-center border rounded-lg bg-[#161b2e] border-violet-800/30 hover:border-violet-500/50 transition-colors duration-200 cursor-pointer"
        @click="openSearch"
      >
        <Search class="w-4 h-4 ml-3 text-gray-500" />
        <span class="w-full py-2.5 px-3 text-sm text-gray-600 select-none">Search movies &amp; TV...</span>
      </div>
    </div>

    <div class="flex items-center gap-3 ml-auto">
      <!-- Workers panel -->
      <div class="relative">
        <button
          class="relative flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-gray-500 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
          title="Background Workers"
          @click="openWorkersPanel"
        >
          <!-- Cycle/recurring icon -->
          <RefreshCw class="w-4 h-4" :class="hasRunningWorker ? 'animate-spin text-violet-400' : ''" />
          <span class="text-xs hidden sm:inline">Workers</span>
          <span
            v-if="hasRunningWorker"
            class="absolute top-0.5 left-0.5 w-2 h-2 rounded-full bg-violet-500 animate-pulse"
          />
        </button>

        <!-- Workers dropdown -->
        <Teleport to="body">
          <div
            v-if="showPanel"
            class="fixed inset-0 z-40"
            @click="showPanel = false"
          />
          <div
            v-if="showPanel"
            class="fixed right-4 top-16 w-[32rem] bg-[#0c0f1a] border border-violet-900/20 rounded-xl shadow-2xl z-50 overflow-hidden"
            @click.stop
          >
          <div class="px-4 py-3 border-b border-violet-900/20">
            <p class="text-sm font-semibold text-gray-200">Background Workers</p>
          </div>
          <div class="overflow-x-auto">
            <table class="w-full text-xs">
              <thead>
                <tr class="text-gray-500 border-b border-violet-900/10">
                  <th class="text-left px-4 py-2 font-medium">Worker</th>
                  <th class="text-left px-3 py-2 font-medium">Last Run</th>
                  <th class="text-left px-3 py-2 font-medium">Next Run</th>
                  <th class="px-3 py-2 font-medium"></th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="w in workers"
                  :key="w.name"
                  class="border-b border-violet-900/10 last:border-0"
                >
                  <td class="px-4 py-2.5">
                    <div class="flex items-center gap-2">
                      <span
                        class="w-2 h-2 rounded-full flex-shrink-0 transition-colors duration-300"
                        :class="w.running ? 'bg-violet-400 animate-pulse shadow-[0_0_6px_rgba(167,139,250,0.6)]' : 'bg-gray-600'"
                      />
                      <span class="text-gray-200 whitespace-nowrap">{{ workerLabel(w.name) }}</span>
                    </div>
                  </td>
                  <td class="px-3 py-2.5 text-gray-400 whitespace-nowrap">{{ formatTime(w.lastRunAt) }}</td>
                  <td class="px-3 py-2.5 text-gray-400 whitespace-nowrap">{{ formatTime(w.nextRunAt) }}</td>
                  <td class="px-3 py-2.5 text-right">
                    <button
                      v-if="w.running"
                      class="px-2.5 py-1 text-[11px] font-medium rounded bg-violet-600/20 text-violet-400 cursor-not-allowed opacity-60"
                      disabled
                    >
                      Running&hellip;
                    </button>
                    <button
                      v-else
                      class="px-2.5 py-1 text-[11px] font-medium rounded bg-violet-600/20 text-violet-300 hover:bg-violet-600/40 transition-colors"
                      @click="handleRun(w.name)"
                    >
                      Run Now
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          </div>
        </Teleport>
      </div>

      <!-- Update indicator -->
      <div v-if="updateAvailable" class="relative">
        <button
          class="relative p-2 rounded-lg text-emerald-400 hover:text-emerald-300 transition-colors duration-200"
          @click="showUpdatePanel = !showUpdatePanel"
        >
          <Download class="w-5 h-5" />
          <span class="absolute top-1 right-1 w-2 h-2 rounded-full bg-emerald-500 animate-pulse" />
        </button>

        <Teleport to="body">
          <div
            v-if="showUpdatePanel"
            class="fixed inset-0 z-40"
            @click="showUpdatePanel = false"
          />
          <div
            v-if="showUpdatePanel"
            class="fixed right-4 top-16 w-72 bg-[#0c0f1a] border border-violet-900/20 rounded-xl shadow-2xl z-50 overflow-hidden"
            @click.stop
          >
            <div class="px-4 py-3 border-b border-violet-900/20">
              <p class="text-sm font-semibold text-gray-200">Update Available</p>
            </div>
            <div class="px-4 py-3 space-y-3">
              <div class="flex items-center gap-2 text-sm">
                <span class="text-gray-400">New version:</span>
                <span class="text-emerald-400 font-mono font-medium">{{ latestVersion }}</span>
              </div>
              <button
                class="w-full px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
                @click="showUpdatePanel = false; router.push('/settings')"
              >
                Go to Settings
              </button>
            </div>
          </div>
        </Teleport>
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
