<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import client from '@/api/client'
import ErrorBanner from '@/components/ErrorBanner.vue'
import FolderBrowser from '@/components/FolderBrowser.vue'

const tmdbKey = ref('')
const tvdbKey = ref('')
const tmdbDirty = ref(false)
const tvdbDirty = ref(false)
const showTmdbKey = ref(false)
const showTvdbKey = ref(false)
const tmdbFromEnv = ref(false)
const tvdbFromEnv = ref(false)

const primarySource = ref('tmdb')
const tmdbRateLimit = ref('4')
const tvdbRateLimit = ref('4')
const primarySourceDirty = ref(false)
const tmdbRateLimitDirty = ref(false)
const tvdbRateLimitDirty = ref(false)

const saving = ref(false)
const loading = ref(false)
const error = ref('')
const saveSuccess = ref(false)

const tmdbTest = ref<{ success: boolean; message: string } | null>(null)
const tvdbTest = ref<{ success: boolean; message: string } | null>(null)
const tmdbTesting = ref(false)
const tvdbTesting = ref(false)

const qbUrl = ref('')
const qbUsername = ref('')
const qbPassword = ref('')
const qbUrlDirty = ref(false)
const qbUsernameDirty = ref(false)
const qbPasswordDirty = ref(false)
const showQbPassword = ref(false)
const qbTest = ref<{ success: boolean; message: string } | null>(null)
const qbTesting = ref(false)

const qbDownloadPath = ref('')
const qbDownloadPathDirty = ref(false)
const qbCategory = ref('')
const qbCategoryDirty = ref(false)

const fsUrl = ref('')
const fsUrlDirty = ref(false)
const fsTest = ref<{ success: boolean; message: string } | null>(null)
const fsTesting = ref(false)

const seasonPackPref = ref('prefer_packs')
const seasonPackPrefDirty = ref(false)

const monitorInterval = ref('900')
const downloadInterval = ref('5')
const importerInterval = ref('10')
const monitorIntervalDirty = ref(false)
const downloadIntervalDirty = ref(false)
const importerIntervalDirty = ref(false)

const anyDirty = computed(() =>
  tmdbDirty.value || tvdbDirty.value ||
  primarySourceDirty.value || tmdbRateLimitDirty.value || tvdbRateLimitDirty.value ||
  qbUrlDirty.value || qbUsernameDirty.value || qbPasswordDirty.value ||
  qbDownloadPathDirty.value || qbCategoryDirty.value ||
  fsUrlDirty.value ||
  seasonPackPrefDirty.value ||
  monitorIntervalDirty.value || downloadIntervalDirty.value || importerIntervalDirty.value
)

async function fetchSettings() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/settings')
  loading.value = false
  if (err) {
    error.value = 'Failed to load settings'
    return
  }
  const s = data?.settings
  if (s) {
    tmdbKey.value = s.tmdbApiKey ?? ''
    tvdbKey.value = s.tvdbApiKey ?? ''
    tmdbFromEnv.value = s.tmdbApiKeyFromEnv ?? false
    tvdbFromEnv.value = s.tvdbApiKeyFromEnv ?? false
    primarySource.value = s.metadataPrimarySource ?? 'tmdb'
    tmdbRateLimit.value = String(s.tmdbRateLimit ?? 4)
    tvdbRateLimit.value = String(s.tvdbRateLimit ?? 4)
    qbUrl.value = s.qbitUrl ?? ''
    qbUsername.value = s.qbitUsername ?? ''
    qbPassword.value = s.qbitPassword ?? ''
    qbDownloadPath.value = s.qbitDownloadPath ?? ''
    qbCategory.value = s.qbitCategory ?? ''
    fsUrl.value = s.flaresolverrUrl ?? ''
    seasonPackPref.value = s.monitorSeasonPackPreference ?? 'prefer_packs'
    monitorInterval.value = String(s.workerMonitorInterval ?? 900)
    downloadInterval.value = String(s.workerDownloadInterval ?? 5)
    importerInterval.value = String(s.workerImporterInterval ?? 10)
  }
  tmdbDirty.value = false
  tvdbDirty.value = false
  primarySourceDirty.value = false
  tmdbRateLimitDirty.value = false
  tvdbRateLimitDirty.value = false
  qbUrlDirty.value = false
  qbUsernameDirty.value = false
  qbPasswordDirty.value = false
  qbDownloadPathDirty.value = false
  qbCategoryDirty.value = false
  fsUrlDirty.value = false
  seasonPackPrefDirty.value = false
  monitorIntervalDirty.value = false
  downloadIntervalDirty.value = false
  importerIntervalDirty.value = false
}

function isMasked(value: string) {
  return value.startsWith('****')
}

async function saveSettings() {
  saving.value = true
  error.value = ''
  saveSuccess.value = false

  const body: Record<string, unknown> = {}
  if (tmdbDirty.value && !isMasked(tmdbKey.value)) body.tmdbApiKey = tmdbKey.value
  if (tvdbDirty.value && !isMasked(tvdbKey.value)) body.tvdbApiKey = tvdbKey.value
  if (primarySourceDirty.value) body.metadataPrimarySource = primarySource.value
  if (tmdbRateLimitDirty.value) body.tmdbRateLimit = Number(tmdbRateLimit.value)
  if (tvdbRateLimitDirty.value) body.tvdbRateLimit = Number(tvdbRateLimit.value)
  if (qbUrlDirty.value) body.qbitUrl = qbUrl.value
  if (qbUsernameDirty.value) body.qbitUsername = qbUsername.value
  if (qbPasswordDirty.value && !isMasked(qbPassword.value)) body.qbitPassword = qbPassword.value
  if (qbDownloadPathDirty.value) body.qbitDownloadPath = qbDownloadPath.value
  if (qbCategoryDirty.value) body.qbitCategory = qbCategory.value
  if (fsUrlDirty.value) body.flaresolverrUrl = fsUrl.value
  if (seasonPackPrefDirty.value) body.monitorSeasonPackPreference = seasonPackPref.value
  if (monitorIntervalDirty.value) body.workerMonitorInterval = Number(monitorInterval.value)
  if (downloadIntervalDirty.value) body.workerDownloadInterval = Number(downloadInterval.value)
  if (importerIntervalDirty.value) body.workerImporterInterval = Number(importerInterval.value)

  if (Object.keys(body).length === 0) {
    saving.value = false
    return
  }

  const { data, error: err } = await client.PUT('/settings', { body })
  saving.value = false
  if (err) {
    error.value = 'Failed to save settings'
    return
  }

  saveSuccess.value = true
  setTimeout(() => { saveSuccess.value = false }, 3000)

  const s = data?.settings
  if (s) {
    tmdbKey.value = s.tmdbApiKey ?? ''
    tvdbKey.value = s.tvdbApiKey ?? ''
    tmdbFromEnv.value = s.tmdbApiKeyFromEnv ?? false
    tvdbFromEnv.value = s.tvdbApiKeyFromEnv ?? false
    primarySource.value = s.metadataPrimarySource ?? 'tmdb'
    tmdbRateLimit.value = String(s.tmdbRateLimit ?? 4)
    tvdbRateLimit.value = String(s.tvdbRateLimit ?? 4)
    qbUrl.value = s.qbitUrl ?? ''
    qbUsername.value = s.qbitUsername ?? ''
    qbPassword.value = s.qbitPassword ?? ''
    qbDownloadPath.value = s.qbitDownloadPath ?? ''
    qbCategory.value = s.qbitCategory ?? ''
    fsUrl.value = s.flaresolverrUrl ?? ''
    seasonPackPref.value = s.monitorSeasonPackPreference ?? 'prefer_packs'
    monitorInterval.value = String(s.workerMonitorInterval ?? 900)
    downloadInterval.value = String(s.workerDownloadInterval ?? 5)
    importerInterval.value = String(s.workerImporterInterval ?? 10)
  }
  tmdbDirty.value = false
  tvdbDirty.value = false
  primarySourceDirty.value = false
  tmdbRateLimitDirty.value = false
  tvdbRateLimitDirty.value = false
  qbUrlDirty.value = false
  qbUsernameDirty.value = false
  qbPasswordDirty.value = false
  qbDownloadPathDirty.value = false
  qbCategoryDirty.value = false
  fsUrlDirty.value = false
  seasonPackPrefDirty.value = false
  monitorIntervalDirty.value = false
  downloadIntervalDirty.value = false
  importerIntervalDirty.value = false
}

async function testTmdb() {
  tmdbTesting.value = true
  tmdbTest.value = null
  const { data, error: err } = await client.POST('/settings/test-tmdb', {
    body: tmdbDirty.value ? { apiKey: tmdbKey.value } : {},
  })
  tmdbTesting.value = false
  if (err) {
    tmdbTest.value = { success: false, message: 'Request failed' }
    return
  }
  tmdbTest.value = { success: data!.success, message: data!.message ?? '' }
}

async function testTvdb() {
  tvdbTesting.value = true
  tvdbTest.value = null
  const { data, error: err } = await client.POST('/settings/test-tvdb', {
    body: tvdbDirty.value ? { apiKey: tvdbKey.value } : {},
  })
  tvdbTesting.value = false
  if (err) {
    tvdbTest.value = { success: false, message: 'Request failed' }
    return
  }
  tvdbTest.value = { success: data!.success, message: data!.message ?? '' }
}

async function testQbittorrent() {
  qbTesting.value = true
  qbTest.value = null
  const body: Record<string, string> = {}
  if (qbUrlDirty.value) body.url = qbUrl.value
  if (qbUsernameDirty.value) body.username = qbUsername.value
  if (qbPasswordDirty.value) body.password = qbPassword.value
  const { data, error: err } = await client.POST('/settings/test-qbittorrent', { body })
  qbTesting.value = false
  if (err) {
    qbTest.value = { success: false, message: 'Request failed' }
    return
  }
  qbTest.value = { success: data!.success, message: data!.message ?? '' }
}

async function testFlaresolverr() {
  fsTesting.value = true
  fsTest.value = null
  const body: Record<string, string> = {}
  if (fsUrlDirty.value) body.url = fsUrl.value
  const { data, error: err } = await client.POST('/settings/test-flaresolverr', { body })
  fsTesting.value = false
  if (err) {
    fsTest.value = { success: false, message: 'Request failed' }
    return
  }
  fsTest.value = { success: data!.success, message: data!.message ?? '' }
}

onMounted(fetchSettings)
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold text-gray-100 tracking-tight mb-6">Settings</h1>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <template v-else>
      <!-- Integrations section -->
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4">Integrations</h2>

      <div class="space-y-4">
        <!-- TMDB -->
        <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
          <div class="flex items-center gap-3 mb-3">
            <span class="text-sm font-semibold text-gray-200">TMDB</span>
            <span class="text-[10px] text-gray-500">The Movie Database</span>
          </div>

          <div class="space-y-3">
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">API Key</label>
              <div class="flex gap-2">
                <div class="relative flex-1">
                  <input
                    v-model="tmdbKey"
                    :type="showTmdbKey ? 'text' : 'password'"
                    placeholder="Enter TMDB API key"
                    class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                    @input="tmdbDirty = true"
                  />
                  <button
                    type="button"
                    class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                    @click="showTmdbKey = !showTmdbKey"
                  >
                    {{ showTmdbKey ? 'Hide' : 'Show' }}
                  </button>
                </div>
                <button
                  class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
                  :disabled="tmdbTesting"
                  @click="testTmdb"
                >
                  {{ tmdbTesting ? 'Testing...' : 'Test Connection' }}
                </button>
              </div>
              <p v-if="tmdbFromEnv" class="text-[10px] text-gray-500 mt-1.5">Configured via environment variable<template v-if="!tmdbKey"> (active)</template></p>
            </div>

            <!-- TMDB test result -->
            <div v-if="tmdbTest" class="flex items-center gap-2">
              <span
                class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
                :class="tmdbTest.success
                  ? 'bg-green-500/10 text-green-400 border border-green-500/30'
                  : 'bg-red-500/10 text-red-400 border border-red-500/30'"
              >
                <span>{{ tmdbTest.success ? '\u2713' : '\u2717' }}</span>
                {{ tmdbTest.message }}
              </span>
            </div>
          </div>
        </div>

        <!-- TVDB -->
        <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
          <div class="flex items-center gap-3 mb-3">
            <span class="text-sm font-semibold text-gray-200">TVDB</span>
            <span class="text-[10px] text-gray-500">TheTVDB</span>
          </div>

          <div class="space-y-3">
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">API Key</label>
              <div class="flex gap-2">
                <div class="relative flex-1">
                  <input
                    v-model="tvdbKey"
                    :type="showTvdbKey ? 'text' : 'password'"
                    placeholder="Enter TVDB API key"
                    class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                    @input="tvdbDirty = true"
                  />
                  <button
                    type="button"
                    class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                    @click="showTvdbKey = !showTvdbKey"
                  >
                    {{ showTvdbKey ? 'Hide' : 'Show' }}
                  </button>
                </div>
                <button
                  class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
                  :disabled="tvdbTesting"
                  @click="testTvdb"
                >
                  {{ tvdbTesting ? 'Testing...' : 'Test Connection' }}
                </button>
              </div>
              <p v-if="tvdbFromEnv" class="text-[10px] text-gray-500 mt-1.5">Configured via environment variable<template v-if="!tvdbKey"> (active)</template></p>
            </div>

            <!-- TVDB test result -->
            <div v-if="tvdbTest" class="flex items-center gap-2">
              <span
                class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
                :class="tvdbTest.success
                  ? 'bg-green-500/10 text-green-400 border border-green-500/30'
                  : 'bg-red-500/10 text-red-400 border border-red-500/30'"
              >
                <span>{{ tvdbTest.success ? '\u2713' : '\u2717' }}</span>
                {{ tvdbTest.message }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Download Client section -->
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Download Client</h2>

      <div class="space-y-4">
        <!-- qBittorrent -->
        <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
          <div class="flex items-center gap-3 mb-3">
            <span class="text-sm font-semibold text-gray-200">qBittorrent</span>
            <span class="text-[10px] text-gray-500">Torrent Client</span>
          </div>

          <div class="space-y-3">
            <!-- URL -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">URL</label>
              <input
                v-model="qbUrl"
                type="text"
                placeholder="http://localhost:8080"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="qbUrlDirty = true"
              />
            </div>

            <!-- Username & Password -->
            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label class="block text-xs font-medium text-gray-400 mb-1.5">Username</label>
                <input
                  v-model="qbUsername"
                  type="text"
                  placeholder="admin"
                  class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                  @input="qbUsernameDirty = true"
                />
              </div>
              <div>
                <label class="block text-xs font-medium text-gray-400 mb-1.5">Password</label>
                <div class="relative">
                  <input
                    v-model="qbPassword"
                    :type="showQbPassword ? 'text' : 'password'"
                    placeholder="Enter password"
                    class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                    @input="qbPasswordDirty = true"
                  />
                  <button
                    type="button"
                    class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                    @click="showQbPassword = !showQbPassword"
                  >
                    {{ showQbPassword ? 'Hide' : 'Show' }}
                  </button>
                </div>
              </div>
            </div>

            <!-- Test Connection -->
            <div class="flex items-center gap-3">
              <button
                class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
                :disabled="qbTesting"
                @click="testQbittorrent"
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
                <span>{{ qbTest.success ? '\u2713' : '\u2717' }}</span>
                {{ qbTest.message }}
              </span>
            </div>

            <!-- Download Path -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Download Path</label>
              <p class="text-[10px] text-gray-500 mb-2">Folder where qBittorrent saves downloaded files. Must be within the base path and cannot be a library folder.</p>
              <FolderBrowser v-model="qbDownloadPath" @update:model-value="qbDownloadPathDirty = true" />
            </div>

            <!-- Category -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Category</label>
              <p class="text-[10px] text-gray-500 mb-2">qBittorrent category for downloads. Defaults to media-gate-dl if empty.</p>
              <input
                v-model="qbCategory"
                type="text"
                placeholder="media-gate-dl"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="qbCategoryDirty = true"
              />
            </div>
          </div>
        </div>
      </div>

      <!-- Metadata section -->
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Proxy</h2>

      <div class="space-y-4">
        <!-- FlareSolverr -->
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
                  v-model="fsUrl"
                  type="text"
                  placeholder="http://localhost:8191"
                  class="flex-1 px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                  @input="fsUrlDirty = true"
                />
                <button
                  class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
                  :disabled="fsTesting"
                  @click="testFlaresolverr"
                >
                  {{ fsTesting ? 'Testing...' : 'Test Connection' }}
                </button>
              </div>
            </div>

            <!-- FlareSolverr test result -->
            <div v-if="fsTest" class="flex items-center gap-2">
              <span
                class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
                :class="fsTest.success
                  ? 'bg-green-500/10 text-green-400 border border-green-500/30'
                  : 'bg-red-500/10 text-red-400 border border-red-500/30'"
              >
                <span>{{ fsTest.success ? '\u2713' : '\u2717' }}</span>
                {{ fsTest.message }}
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- Metadata section -->
      <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Metadata</h2>

      <div class="space-y-4">
        <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <!-- Primary source -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Primary Source</label>
              <select
                v-model="primarySource"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @change="primarySourceDirty = true"
              >
                <option value="tmdb">TMDB</option>
                <option value="tvdb">TVDB</option>
              </select>
            </div>

            <!-- TMDB rate limit -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">TMDB Rate Limit (req/sec)</label>
              <input
                v-model="tmdbRateLimit"
                type="number"
                min="1"
                max="40"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="tmdbRateLimitDirty = true"
              />
            </div>

            <!-- TVDB rate limit -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">TVDB Rate Limit (req/sec)</label>
              <input
                v-model="tvdbRateLimit"
                type="number"
                min="1"
                max="40"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="tvdbRateLimitDirty = true"
              />
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
              v-model="seasonPackPref"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              @change="seasonPackPrefDirty = true"
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
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
            <!-- Monitor interval -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Monitor Interval (seconds)</label>
              <input
                v-model="monitorInterval"
                type="number"
                min="60"
                max="86400"
                placeholder="900"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="monitorIntervalDirty = true"
              />
              <p class="text-[10px] text-gray-500 mt-1">How often to scan for new releases. Default: 900 (15 min)</p>
            </div>

            <!-- Download interval -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Download Interval (seconds)</label>
              <input
                v-model="downloadInterval"
                type="number"
                min="1"
                max="3600"
                placeholder="5"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="downloadIntervalDirty = true"
              />
              <p class="text-[10px] text-gray-500 mt-1">How often to check download progress. Default: 5</p>
            </div>

            <!-- Importer interval -->
            <div>
              <label class="block text-xs font-medium text-gray-400 mb-1.5">Importer Interval (seconds)</label>
              <input
                v-model="importerInterval"
                type="number"
                min="1"
                max="3600"
                placeholder="10"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
                @input="importerIntervalDirty = true"
              />
              <p class="text-[10px] text-gray-500 mt-1">How often to import completed downloads. Default: 10</p>
            </div>
          </div>
        </div>
      </div>

      <!-- Save button -->
      <div class="flex items-center gap-3 mt-6">
        <button
          class="px-5 py-2.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="saving || !anyDirty"
          @click="saveSettings"
        >
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
        <span
          v-if="saveSuccess"
          class="text-sm text-green-400"
        >
          Settings saved
        </span>
      </div>
    </template>
  </div>
</template>
