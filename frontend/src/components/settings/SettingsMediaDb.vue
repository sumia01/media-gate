<script setup lang="ts">
defineProps<{
  tmdbKey: string
  tvdbKey: string
  showTmdbKey: boolean
  showTvdbKey: boolean
  tmdbFromEnv: boolean
  tvdbFromEnv: boolean
  tmdbTesting: boolean
  tvdbTesting: boolean
  tmdbTest: { success: boolean; message: string } | null
  tvdbTest: { success: boolean; message: string } | null
  primarySource: string
  tmdbRateLimit: string
  tvdbRateLimit: string
}>()

defineEmits<{
  'update:tmdbKey': [value: string]
  'update:tvdbKey': [value: string]
  'update:showTmdbKey': [value: boolean]
  'update:showTvdbKey': [value: boolean]
  'update:primarySource': [value: string]
  'update:tmdbRateLimit': [value: string]
  'update:tvdbRateLimit': [value: string]
  dirty: [field: string]
  testTmdb: []
  testTvdb: []
}>()
</script>

<template>
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
                :value="tmdbKey"
                :type="showTmdbKey ? 'text' : 'password'"
                placeholder="Enter TMDB API key"
                class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="$emit('update:tmdbKey', ($event.target as HTMLInputElement).value); $emit('dirty', 'tmdb')"
              />
              <button
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                @click="$emit('update:showTmdbKey', !showTmdbKey)"
              >
                {{ showTmdbKey ? 'Hide' : 'Show' }}
              </button>
            </div>
            <button
              class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
              :disabled="tmdbTesting"
              @click="$emit('testTmdb')"
            >
              {{ tmdbTesting ? 'Testing...' : 'Test Connection' }}
            </button>
          </div>
          <p v-if="tmdbFromEnv" class="text-[10px] text-gray-500 mt-1.5">Configured via environment variable<template v-if="!tmdbKey"> (active)</template></p>
        </div>

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
                :value="tvdbKey"
                :type="showTvdbKey ? 'text' : 'password'"
                placeholder="Enter TVDB API key"
                class="w-full px-3 py-2 pr-10 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
                @input="$emit('update:tvdbKey', ($event.target as HTMLInputElement).value); $emit('dirty', 'tvdb')"
              />
              <button
                type="button"
                class="absolute right-2 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 text-xs transition-colors duration-200"
                @click="$emit('update:showTvdbKey', !showTvdbKey)"
              >
                {{ showTvdbKey ? 'Hide' : 'Show' }}
              </button>
            </div>
            <button
              class="px-3 py-2 rounded-lg border border-violet-800/30 text-sm text-gray-400 hover:text-violet-300 hover:border-violet-500/50 transition-colors duration-200 whitespace-nowrap"
              :disabled="tvdbTesting"
              @click="$emit('testTvdb')"
            >
              {{ tvdbTesting ? 'Testing...' : 'Test Connection' }}
            </button>
          </div>
          <p v-if="tvdbFromEnv" class="text-[10px] text-gray-500 mt-1.5">Configured via environment variable<template v-if="!tvdbKey"> (active)</template></p>
        </div>

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

  <!-- Metadata section -->
  <h2 class="text-sm font-semibold uppercase tracking-wider text-gray-500 mb-4 mt-8">Metadata</h2>

  <div class="space-y-4">
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Primary Source</label>
          <select
            :value="primarySource"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @change="$emit('update:primarySource', ($event.target as HTMLSelectElement).value); $emit('dirty', 'primarySource')"
          >
            <option value="tmdb">TMDB</option>
            <option value="tvdb">TVDB</option>
          </select>
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">TMDB Rate Limit (req/sec)</label>
          <input
            :value="tmdbRateLimit"
            type="number"
            min="1"
            max="40"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:tmdbRateLimit', ($event.target as HTMLInputElement).value); $emit('dirty', 'tmdbRateLimit')"
          />
        </div>

        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">TVDB Rate Limit (req/sec)</label>
          <input
            :value="tvdbRateLimit"
            type="number"
            min="1"
            max="40"
            class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @input="$emit('update:tvdbRateLimit', ($event.target as HTMLInputElement).value); $emit('dirty', 'tvdbRateLimit')"
          />
        </div>
      </div>
    </div>
  </div>
</template>
