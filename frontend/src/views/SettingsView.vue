<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type Setting = components['schemas']['Setting']

const tmdbKey = ref('')
const tvdbKey = ref('')
const tmdbDirty = ref(false)
const tvdbDirty = ref(false)
const showTmdbKey = ref(false)
const showTvdbKey = ref(false)

const saving = ref(false)
const loading = ref(false)
const error = ref('')
const saveSuccess = ref(false)

const tmdbTest = ref<{ success: boolean; message: string } | null>(null)
const tvdbTest = ref<{ success: boolean; message: string } | null>(null)
const tmdbTesting = ref(false)
const tvdbTesting = ref(false)

async function fetchSettings() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/settings')
  loading.value = false
  if (err) {
    error.value = 'Failed to load settings'
    return
  }
  const settings = data?.settings ?? []
  for (const s of settings) {
    if (s.key === 'tmdb_api_key') tmdbKey.value = s.value
    if (s.key === 'tvdb_api_key') tvdbKey.value = s.value
  }
  tmdbDirty.value = false
  tvdbDirty.value = false
}

function isMasked(value: string) {
  return value.startsWith('****')
}

async function saveSettings() {
  saving.value = true
  error.value = ''
  saveSuccess.value = false

  const items: { key: string; value: string }[] = []
  if (tmdbDirty.value && !isMasked(tmdbKey.value)) {
    items.push({ key: 'tmdb_api_key', value: tmdbKey.value })
  }
  if (tvdbDirty.value && !isMasked(tvdbKey.value)) {
    items.push({ key: 'tvdb_api_key', value: tvdbKey.value })
  }

  if (items.length === 0) {
    saving.value = false
    return
  }

  const { data, error: err } = await client.PUT('/settings', {
    body: { settings: items },
  })
  saving.value = false
  if (err) {
    error.value = 'Failed to save settings'
    return
  }

  saveSuccess.value = true
  setTimeout(() => { saveSuccess.value = false }, 3000)

  const settings = data?.settings ?? []
  for (const s of settings) {
    if (s.key === 'tmdb_api_key') tmdbKey.value = s.value
    if (s.key === 'tvdb_api_key') tvdbKey.value = s.value
  }
  tmdbDirty.value = false
  tvdbDirty.value = false
}

async function testTmdb() {
  tmdbTesting.value = true
  tmdbTest.value = null
  const { data, error: err } = await client.POST('/settings/test-tmdb', {
    body: { apiKey: tmdbKey.value },
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
    body: { apiKey: tvdbKey.value },
  })
  tvdbTesting.value = false
  if (err) {
    tvdbTest.value = { success: false, message: 'Request failed' }
    return
  }
  tvdbTest.value = { success: data!.success, message: data!.message ?? '' }
}

onMounted(fetchSettings)
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold text-gray-100 tracking-tight mb-6">Settings</h1>

    <!-- Error banner -->
    <div
      v-if="error"
      class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
    >
      {{ error }}
    </div>

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

      <!-- Save button -->
      <div class="flex items-center gap-3 mt-6">
        <button
          class="px-5 py-2.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="saving || (!tmdbDirty && !tvdbDirty)"
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
