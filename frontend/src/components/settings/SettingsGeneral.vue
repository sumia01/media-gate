<script setup lang="ts">
import { Check, X } from 'lucide-vue-next'
import { computed, ref } from 'vue'
import { authFetch } from '@/api/client'
import { useAuth } from '@/composables/useAuth'
import { useUpdateCheck } from '@/composables/useUpdateCheck'

const { isAdmin } = useAuth()

const {
  updateEnabled,
  updateAvailable,
  latestVersion,
  releaseNotes,
  currentVersion,
  checking,
  applying,
  checkNow,
  applyUpdate,
} = useUpdateCheck()

const downloading = ref(false)

async function exportDatabase() {
  downloading.value = true
  try {
    const res = await authFetch('/api/v1/settings/database/export')
    if (!res.ok) throw new Error('Download failed')
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = res.headers.get('content-disposition')?.match(/filename="(.+)"/)?.[1] ?? 'media-gate.db'
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  } finally {
    downloading.value = false
  }
}

const props = defineProps<{
  watchedListMode: string
  discordUrl: string
  showDiscordUrl: boolean
  discordTesting: boolean
  discordTest: { success: boolean; message: string } | null
  saving: boolean
  seasonPackPref: string
  monitorInterval: string
  downloadInterval: string
  importerInterval: string
  metadataRefreshInterval: string
  updateCheckInterval: string
  otelEnabled: boolean
  otelEndpoint: string
  otelService: string
  otelLogLevel: string
}>()

defineEmits<{
  'update:watchedListMode': [value: string]
  'update:discordUrl': [value: string]
  'update:showDiscordUrl': [value: boolean]
  'update:seasonPackPref': [value: string]
  'update:monitorInterval': [value: string]
  'update:downloadInterval': [value: string]
  'update:importerInterval': [value: string]
  'update:metadataRefreshInterval': [value: string]
  'update:updateCheckInterval': [value: string]
  'update:otelEnabled': [value: boolean]
  'update:otelEndpoint': [value: string]
  'update:otelService': [value: string]
  'update:otelLogLevel': [value: string]
  dirty: [field: string]
  testDiscord: []
  disconnectDiscord: []
}>()

const discordConnected = computed(() => props.discordUrl !== '')
</script>

<template>
  <!-- Watched section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4">Watched</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div>
        <label class="block text-xs font-medium text-gray-400 mb-1.5">Watched List Mode</label>
        <select
          :value="watchedListMode"
          class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          @change="$emit('update:watchedListMode', ($event.target as HTMLSelectElement).value); $emit('dirty', 'watchedListMode')"
        >
          <option value="global">Global</option>
          <option value="per_user">Per User</option>
        </select>
        <p class="text-[10px] text-gray-500 mt-2">
          <template v-if="watchedListMode === 'global'">All users share a single watched list. When anyone marks media as watched, it appears watched for everyone.</template>
          <template v-else>Each user has their own watched list. Marking media as watched is private to the current user.</template>
        </p>
      </div>
    </div>
  </div>

  <!-- Notifications section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Notifications</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center gap-3 mb-3">
        <span class="text-sm font-semibold text-gray-200">Discord</span>
        <span class="text-[10px] text-gray-500">Webhook Notifications</span>
      </div>

      <div class="space-y-3">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Webhook URL</label>
          <p class="text-[10px] text-gray-500 mb-2">Receive notifications when downloads are imported. Create a webhook in Discord channel settings.</p>
          <div class="flex gap-2">
            <div class="relative flex-1">
              <input
                :value="discordUrl"
                :type="showDiscordUrl ? 'text' : 'password'"
                placeholder="https://discord.com/api/webhooks/..."
                class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="$emit('update:discordUrl', ($event.target as HTMLInputElement).value); $emit('dirty', 'discord')"
              />
              <button
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                @click="$emit('update:showDiscordUrl', !showDiscordUrl)"
              >
                {{ showDiscordUrl ? 'Hide' : 'Show' }}
              </button>
            </div>
            <button
              class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
              :disabled="discordTesting"
              @click="$emit('testDiscord')"
            >
              {{ discordTesting ? 'Testing...' : 'Test Webhook' }}
            </button>
            <button
              v-if="discordConnected"
              class="px-3 py-2 rounded-lg border border-red-800/30 text-sm text-red-400 hover:text-red-300 hover:border-red-500/50 transition-colors duration-200 whitespace-nowrap"
              :disabled="saving"
              @click="$emit('disconnectDiscord')"
            >
              Disconnect
            </button>
          </div>
        </div>

        <div v-if="discordTest" class="flex items-center gap-2">
          <span
            class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
            :class="discordTest.success
              ? 'bg-green-500/10 text-green-400 border border-green-500/30'
              : 'bg-red-500/10 text-red-400 border border-red-500/30'"
          >
            <Check v-if="discordTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
            {{ discordTest.message }}
          </span>
        </div>
      </div>
    </div>
  </div>

  <!-- Monitor section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Monitor</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div>
        <label class="block text-xs font-medium text-gray-400 mb-1.5">Season Pack Preference</label>
        <select
          :value="seasonPackPref"
          class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          @change="$emit('update:seasonPackPref', ($event.target as HTMLSelectElement).value); $emit('dirty', 'seasonPackPref')"
        >
          <option value="prefer_packs">Prefer season packs</option>
          <option value="prefer_episodes">Prefer episodes</option>
          <option value="packs_only">Season packs only</option>
        </select>
        <p class="text-[10px] text-gray-500 mt-2">
          <template v-if="seasonPackPref === 'prefer_packs'">Downloads full season packs when 70% or more episodes are missing. Falls back to individual episodes otherwise.</template>
          <template v-else-if="seasonPackPref === 'prefer_episodes'">Always downloads individual episodes. Falls back to season packs only when no episode match is found.</template>
          <template v-else>Only downloads full season packs. Never downloads individual episodes.</template>
        </p>
      </div>
    </div>
  </div>

  <!-- Workers section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Workers</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <p class="text-[10px] text-gray-500 mb-4">Poll intervals for background workers. Changes take effect immediately without restart.</p>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Monitor Interval (seconds)</label>
          <input
            :value="monitorInterval"
            type="number"
            min="60"
            max="86400"
            placeholder="900"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:monitorInterval', ($event.target as HTMLInputElement).value); $emit('dirty', 'monitorInterval')"
          />
          <p class="text-[10px] text-gray-500 mt-1">How often to scan for new releases. Default: 900 (15 min)</p>
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Download Interval (seconds)</label>
          <input
            :value="downloadInterval"
            type="number"
            min="1"
            max="3600"
            placeholder="5"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:downloadInterval', ($event.target as HTMLInputElement).value); $emit('dirty', 'downloadInterval')"
          />
          <p class="text-[10px] text-gray-500 mt-1">How often to check download progress. Default: 5</p>
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Importer Interval (seconds)</label>
          <input
            :value="importerInterval"
            type="number"
            min="1"
            max="3600"
            placeholder="10"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:importerInterval', ($event.target as HTMLInputElement).value); $emit('dirty', 'importerInterval')"
          />
          <p class="text-[10px] text-gray-500 mt-1">How often to import completed downloads. Default: 10</p>
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Metadata Refresh (seconds)</label>
          <input
            :value="metadataRefreshInterval"
            type="number"
            min="3600"
            max="604800"
            placeholder="21600"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:metadataRefreshInterval', ($event.target as HTMLInputElement).value); $emit('dirty', 'metadataRefreshInterval')"
          />
          <p class="text-[10px] text-gray-500 mt-1">How often to check for new seasons. Default: 21600 (6 hours)</p>
        </div>
      </div>
    </div>
  </div>

  <!-- Updates section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Updates</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <template v-if="updateEnabled">
        <div class="flex items-center justify-between mb-3">
          <div>
            <span class="text-sm font-semibold text-gray-200">Current Version</span>
            <span class="ml-2 text-sm font-mono text-gray-400">{{ currentVersion }}</span>
          </div>
          <button
            class="px-3 py-1.5 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200"
            :disabled="checking"
            @click="checkNow"
          >
            {{ checking ? 'Checking...' : 'Check Now' }}
          </button>
        </div>

        <div v-if="updateAvailable" class="mt-3 p-3 rounded-lg bg-emerald-500/10 border border-emerald-500/20">
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm text-emerald-400 font-medium">
                Update available: <span class="font-mono">{{ latestVersion }}</span>
              </p>
              <p v-if="releaseNotes" class="text-xs text-gray-400 mt-1 line-clamp-2">{{ releaseNotes }}</p>
            </div>
            <button
              class="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap ml-4"
              :disabled="applying"
              @click="applyUpdate"
            >
              {{ applying ? 'Updating...' : 'Apply Update' }}
            </button>
          </div>
        </div>

        <div v-else-if="!checking" class="text-xs text-gray-500 mt-1">
          You're running the latest version.
        </div>

        <div class="mt-4">
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Update Check Interval (seconds)</label>
          <input
            :value="updateCheckInterval"
            type="number"
            min="3600"
            max="604800"
            placeholder="21600"
            class="w-full max-w-xs px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:updateCheckInterval', ($event.target as HTMLInputElement).value); $emit('dirty', 'updateCheckInterval')"
          />
          <p class="text-[10px] text-gray-500 mt-1">How often to check for new releases. Default: 21600 (6 hours)</p>
        </div>
      </template>

      <template v-else>
        <p class="text-sm text-gray-400">Self-update is not available.</p>
        <p class="text-[10px] text-gray-500 mt-1">Requires Linux, a non-dev build, and GitHub credentials (MEDIAGATE_GITHUB_TOKEN / MEDIAGATE_GITHUB_REPO).</p>
      </template>
    </div>
  </div>

  <!-- Observability section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Observability</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center gap-3 mb-4">
        <label class="relative inline-flex items-center cursor-pointer">
          <input
            type="checkbox"
            :checked="otelEnabled"
            class="sr-only peer"
            @change="$emit('update:otelEnabled', ($event.target as HTMLInputElement).checked); $emit('dirty', 'otelEnabled')"
          />
          <div class="w-9 h-5 bg-gray-700 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-4 after:w-4 after:transition-all peer-checked:bg-violet-600"></div>
        </label>
        <span class="text-sm font-semibold text-gray-200">OpenTelemetry</span>
      </div>
      <p class="text-[10px] text-gray-500 mb-4">Send traces and logs to an OTLP-compatible backend (e.g. SigNoz, Jaeger, Grafana Tempo). Changes take effect immediately.</p>

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">OTLP Endpoint</label>
          <input
            :value="otelEndpoint"
            type="text"
            placeholder="http://signoz:4318"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
            @input="$emit('update:otelEndpoint', ($event.target as HTMLInputElement).value); $emit('dirty', 'otelEndpoint')"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Service Name</label>
          <input
            :value="otelService"
            type="text"
            placeholder="media-gate"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:otelService', ($event.target as HTMLInputElement).value); $emit('dirty', 'otelService')"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Log Level</label>
          <select
            :value="otelLogLevel"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @change="$emit('update:otelLogLevel', ($event.target as HTMLSelectElement).value); $emit('dirty', 'otelLogLevel')"
          >
            <option value="debug">Debug</option>
            <option value="info">Info</option>
            <option value="warn">Warn</option>
            <option value="error">Error</option>
          </select>
          <p class="text-[10px] text-gray-500 mt-1">Minimum log severity exported to the OTLP backend</p>
        </div>
      </div>
    </div>
  </div>

  <!-- Database section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Database</h2>

  <div v-if="isAdmin" class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center justify-between">
        <div>
          <span class="text-sm font-semibold text-gray-200">Export Database</span>
          <p class="text-[10px] text-gray-500 mt-1">Download a copy of the SQLite database file for backup or debugging.</p>
        </div>
        <button
          class="px-4 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="downloading"
          @click="exportDatabase"
        >
          {{ downloading ? 'Downloading...' : 'Download .db' }}
        </button>
      </div>
    </div>
  </div>
</template>
