<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'

const emit = defineEmits<{ next: []; back: [] }>()

const apiKey = ref('')
const error = ref('')
const saving = ref(false)
const testResult = ref<{ success: boolean; message?: string } | null>(null)
const testing = ref(false)

onMounted(async () => {
  try {
    const { data } = await client.GET('/settings')
    const s = data?.settings
    if (s?.tmdbApiKey) {
      apiKey.value = s.tmdbApiKey
    }
  } catch {
    // Use default
  }
})

async function testConnection() {
  testResult.value = null
  testing.value = true
  try {
    const { data } = await client.POST('/settings/test-tmdb', {
      body: { apiKey: apiKey.value || undefined },
    })
    testResult.value = data ?? null
  } catch {
    testResult.value = { success: false, message: 'Test request failed' }
  } finally {
    testing.value = false
  }
}

async function handleSubmit() {
  if (!apiKey.value.trim()) {
    error.value = 'TMDB API key is required'
    return
  }

  error.value = ''
  saving.value = true
  try {
    const { error: err } = await client.PUT('/settings', {
      body: { tmdbApiKey: apiKey.value.trim() },
    })
    if (err) {
      error.value = 'Failed to save TMDB API key'
      return
    }
    emit('next')
  } catch {
    error.value = 'Failed to save settings'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-gray-200">TMDB API Key</h2>
      <p class="text-sm text-gray-500 mt-1">
        MediaGate uses The Movie Database (TMDB) for movie and TV show metadata. A free API key is required.
      </p>
    </div>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ error }}
      </div>

      <div>
        <label for="tmdb-key" class="block text-sm font-medium text-gray-400 mb-1.5">API Key (v3 auth)</label>
        <input
          id="tmdb-key"
          v-model="apiKey"
          type="text"
          required
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm font-mono placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          placeholder="Your TMDB API key"
        />
        <p class="text-xs text-gray-500 mt-1">
          Don't have one? Register at
          <a
            href="https://www.themoviedb.org/settings/api"
            target="_blank"
            rel="noopener noreferrer"
            class="text-violet-400 hover:text-violet-300 underline"
          >themoviedb.org</a>
          — it's free.
        </p>
      </div>

      <!-- Test -->
      <div class="flex items-center gap-3">
        <button
          type="button"
          :disabled="testing || !apiKey"
          class="px-4 py-2 rounded-lg border border-violet-800/30 text-gray-300 hover:text-white text-sm font-medium transition-colors disabled:opacity-50"
          @click="testConnection"
        >
          {{ testing ? 'Testing...' : 'Test Connection' }}
        </button>
        <span
          v-if="testResult"
          class="text-sm"
          :class="testResult.success ? 'text-green-400' : 'text-red-400'"
        >
          {{ testResult.message }}
        </span>
      </div>

      <div class="flex gap-3 pt-2">
        <button
          type="button"
          class="px-4 py-2.5 rounded-lg border border-violet-800/30 text-gray-400 hover:text-gray-200 text-sm font-medium transition-colors"
          @click="emit('back')"
        >
          Back
        </button>
        <button
          type="submit"
          :disabled="saving || !apiKey.trim()"
          class="flex-1 py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
        >
          {{ saving ? 'Saving...' : 'Continue' }}
        </button>
      </div>
    </form>
  </div>
</template>
