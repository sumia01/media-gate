<script setup lang="ts">
import FolderBrowser from '@/components/FolderBrowser.vue'
import { Check, X } from 'lucide-vue-next'

defineProps<{
  qbUrl: string
  qbUsername: string
  qbPassword: string
  showQbPassword: boolean
  qbTesting: boolean
  qbTest: { success: boolean; message: string } | null
  qbDownloadPath: string
  qbSavePath: string
  qbCategory: string
  osApiKey: string
  showOsApiKey: boolean
  osUsername: string
  osPassword: string
  showOsPassword: boolean
  osRateLimit: string
  osTesting: boolean
  osTest: { success: boolean; message: string } | null
  subtitleLanguages: string
  subtitleAutoSearch: boolean
  fsUrl: string
  fsTesting: boolean
  fsTest: { success: boolean; message: string } | null
}>()

defineEmits<{
  'update:qbUrl': [value: string]
  'update:qbUsername': [value: string]
  'update:qbPassword': [value: string]
  'update:showQbPassword': [value: boolean]
  'update:qbDownloadPath': [value: string]
  'update:qbSavePath': [value: string]
  'update:qbCategory': [value: string]
  'update:osApiKey': [value: string]
  'update:showOsApiKey': [value: boolean]
  'update:osUsername': [value: string]
  'update:osPassword': [value: string]
  'update:showOsPassword': [value: boolean]
  'update:osRateLimit': [value: string]
  'update:subtitleLanguages': [value: string]
  'update:subtitleAutoSearch': [value: boolean]
  'update:fsUrl': [value: string]
  dirty: [field: string]
  testQbittorrent: []
  testOpenSubtitles: []
  testFlaresolverr: []
}>()
</script>

<template>
  <!-- Download Client section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4">Download Client</h2>

  <div class="space-y-4">
    <!-- qBittorrent -->
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center gap-3 mb-3">
        <span class="text-sm font-semibold text-gray-200">qBittorrent</span>
        <span class="text-[10px] text-gray-500">Torrent Client</span>
      </div>

      <div class="space-y-3">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">URL</label>
          <input
            :value="qbUrl"
            type="text"
            placeholder="http://localhost:8080"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
            @input="$emit('update:qbUrl', ($event.target as HTMLInputElement).value); $emit('dirty', 'qbUrl')"
          />
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Username</label>
            <input
              :value="qbUsername"
              type="text"
              placeholder="admin"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              @input="$emit('update:qbUsername', ($event.target as HTMLInputElement).value); $emit('dirty', 'qbUsername')"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Password</label>
            <div class="relative">
              <input
                :value="qbPassword"
                :type="showQbPassword ? 'text' : 'password'"
                placeholder="Enter password"
                class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="$emit('update:qbPassword', ($event.target as HTMLInputElement).value); $emit('dirty', 'qbPassword')"
              />
              <button
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                @click="$emit('update:showQbPassword', !showQbPassword)"
              >
                {{ showQbPassword ? 'Hide' : 'Show' }}
              </button>
            </div>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <button
            class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
            :disabled="qbTesting"
            @click="$emit('testQbittorrent')"
          >
            {{ qbTesting ? 'Testing...' : 'Test Connection' }}
          </button>
          <span
            v-if="qbTest"
            class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
            :class="qbTest.success
              ? 'bg-green-500/10 text-green-400 border border-green-500/30'
              : 'bg-red-500/10 text-red-400 border border-red-500/30'"
          >
            <Check v-if="qbTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
            {{ qbTest.message }}
          </span>
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Download Path</label>
          <p class="text-[10px] text-gray-500 mb-2">Folder where qBittorrent saves downloaded files. Must be within the base path and cannot be a library folder.</p>
          <FolderBrowser :model-value="qbDownloadPath" @update:model-value="$emit('update:qbDownloadPath', $event); $emit('dirty', 'qbDownloadPath')" />
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">qBittorrent Save Path <span class="text-gray-600 font-normal">(Optional)</span></label>
          <p class="text-[10px] text-gray-500 mb-2">If your qBittorrent client's NAS mount is at a different path than MediaGate's, enter the absolute path on the qBittorrent host that corresponds to the download path above.</p>
          <input
            :value="qbSavePath"
            type="text"
            placeholder="/mnt/nas/downloads"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:qbSavePath', ($event.target as HTMLInputElement).value); $emit('dirty', 'qbSavePath')"
          />
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Category</label>
          <p class="text-[10px] text-gray-500 mb-2">qBittorrent category for downloads. Defaults to media-gate-dl if empty.</p>
          <input
            :value="qbCategory"
            type="text"
            placeholder="media-gate-dl"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:qbCategory', ($event.target as HTMLInputElement).value); $emit('dirty', 'qbCategory')"
          />
        </div>
      </div>
    </div>
  </div>

  <!-- Subtitles section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Subtitles</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center gap-3 mb-3">
        <span class="text-sm font-semibold text-gray-200">OpenSubtitles</span>
        <span class="text-[10px] text-gray-500">opensubtitles.com</span>
      </div>

      <div class="space-y-3">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">API Key</label>
          <p class="text-[10px] text-gray-500 mb-2">Register your app at opensubtitles.com/consumers to get an API key.</p>
          <div class="relative">
            <input
              :value="osApiKey"
              :type="showOsApiKey ? 'text' : 'password'"
              placeholder="Enter API key"
              class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
              @input="$emit('update:osApiKey', ($event.target as HTMLInputElement).value); $emit('dirty', 'osApiKey')"
            />
            <button
              type="button"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
              @click="$emit('update:showOsApiKey', !showOsApiKey)"
            >
              {{ showOsApiKey ? 'Hide' : 'Show' }}
            </button>
          </div>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Username</label>
            <input
              :value="osUsername"
              type="text"
              placeholder="Enter username"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              @input="$emit('update:osUsername', ($event.target as HTMLInputElement).value); $emit('dirty', 'osUsername')"
            />
          </div>
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Password</label>
            <div class="relative">
              <input
                :value="osPassword"
                :type="showOsPassword ? 'text' : 'password'"
                placeholder="Enter password"
                class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="$emit('update:osPassword', ($event.target as HTMLInputElement).value); $emit('dirty', 'osPassword')"
              />
              <button
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                @click="$emit('update:showOsPassword', !showOsPassword)"
              >
                {{ showOsPassword ? 'Hide' : 'Show' }}
              </button>
            </div>
          </div>
        </div>

        <div class="flex items-center gap-3">
          <button
            class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
            :disabled="osTesting"
            @click="$emit('testOpenSubtitles')"
          >
            {{ osTesting ? 'Testing...' : 'Test Connection' }}
          </button>
        </div>

        <div v-if="osTest" class="flex items-center gap-2">
          <span
            class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
            :class="osTest.success
              ? 'bg-green-500/10 text-green-400 border border-green-500/30'
              : 'bg-red-500/10 text-red-400 border border-red-500/30'"
          >
            <Check v-if="osTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
            {{ osTest.message }}
          </span>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3 mt-3">
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Preferred Languages</label>
            <input
              :value="subtitleLanguages"
              type="text"
              placeholder="hu, en"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              @input="$emit('update:subtitleLanguages', ($event.target as HTMLInputElement).value); $emit('dirty', 'subtitleLanguages')"
            />
            <p class="text-[10px] text-gray-500 mt-1">ISO 639-1 codes, comma-separated. First language gets highest priority.</p>
          </div>
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Rate Limit (req/sec)</label>
            <input
              :value="osRateLimit"
              type="number"
              min="1"
              max="20"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              @input="$emit('update:osRateLimit', ($event.target as HTMLInputElement).value); $emit('dirty', 'osRateLimit')"
            />
          </div>
        </div>

        <div class="flex items-center gap-3 mt-2">
          <button
            class="flex items-center gap-2 px-2.5 py-1.5 rounded-lg text-xs font-medium transition-colors duration-200 cursor-pointer"
            :class="subtitleAutoSearch
              ? 'bg-emerald-600/20 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-600/30'
              : 'text-gray-500 border border-violet-900/20 hover:text-violet-300 hover:bg-violet-600/10'"
            @click="$emit('update:subtitleAutoSearch', !subtitleAutoSearch); $emit('dirty', 'subtitleAutoSearch')"
          >
            <span
              class="relative w-7 h-4 rounded-full transition-colors duration-200 flex-shrink-0"
              :class="subtitleAutoSearch ? 'bg-emerald-600' : 'bg-gray-600'"
            >
              <span
                class="absolute top-0.5 left-0.5 w-3 h-3 bg-white rounded-full transition-transform duration-200"
                :class="subtitleAutoSearch ? 'translate-x-3' : ''"
              />
            </span>
            <span>Auto-search after import</span>
          </button>
          <p class="text-[10px] text-gray-500">Automatically search and download subtitles when a new media file is imported.</p>
        </div>
      </div>
    </div>
  </div>

  <!-- Proxy section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Proxy</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="flex items-center gap-3 mb-3">
        <span class="text-sm font-semibold text-gray-200">FlareSolverr</span>
        <span class="text-[10px] text-gray-500">Cloudflare Bypass Proxy</span>
      </div>

      <div class="space-y-3">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">URL</label>
          <p class="text-[10px] text-gray-500 mb-2">Required for indexers behind Cloudflare protection (e.g. 1337x). Run FlareSolverr as a Docker sidecar.</p>
          <div class="flex gap-2">
            <input
              :value="fsUrl"
              type="text"
              placeholder="http://localhost:8191"
              class="flex-1 px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
              @input="$emit('update:fsUrl', ($event.target as HTMLInputElement).value); $emit('dirty', 'fsUrl')"
            />
            <button
              class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
              :disabled="fsTesting"
              @click="$emit('testFlaresolverr')"
            >
              {{ fsTesting ? 'Testing...' : 'Test Connection' }}
            </button>
          </div>
        </div>

        <div v-if="fsTest" class="flex items-center gap-2">
          <span
            class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
            :class="fsTest.success
              ? 'bg-green-500/10 text-green-400 border border-green-500/30'
              : 'bg-red-500/10 text-red-400 border border-red-500/30'"
          >
            <Check v-if="fsTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
            {{ fsTest.message }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
