<script setup lang="ts">
import { Check, X } from 'lucide-vue-next'

const props = defineProps<{
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
  contentRatingCountries: string[]
}>()

const emit = defineEmits<{
  'update:tmdbKey': [value: string]
  'update:tvdbKey': [value: string]
  'update:showTmdbKey': [value: boolean]
  'update:showTvdbKey': [value: boolean]
  'update:primarySource': [value: string]
  'update:tmdbRateLimit': [value: string]
  'update:tvdbRateLimit': [value: string]
  'update:contentRatingCountries': [value: string[]]
  dirty: [field: string]
  testTmdb: []
  testTvdb: []
}>()

// Countries offered for content/age certifications. Codes are ISO 3166-1
// alpha-2 to match TMDB; TVDB's alpha-3 codes are normalized backend-side.
const RATING_COUNTRIES: { code: string; name: string }[] = [
  { code: 'HU', name: 'Hungary' },
  { code: 'US', name: 'United States' },
  { code: 'GB', name: 'United Kingdom' },
  { code: 'DE', name: 'Germany' },
  { code: 'AT', name: 'Austria' },
  { code: 'CH', name: 'Switzerland' },
  { code: 'FR', name: 'France' },
  { code: 'IT', name: 'Italy' },
  { code: 'ES', name: 'Spain' },
  { code: 'PT', name: 'Portugal' },
  { code: 'NL', name: 'Netherlands' },
  { code: 'BE', name: 'Belgium' },
  { code: 'IE', name: 'Ireland' },
  { code: 'DK', name: 'Denmark' },
  { code: 'SE', name: 'Sweden' },
  { code: 'NO', name: 'Norway' },
  { code: 'FI', name: 'Finland' },
  { code: 'PL', name: 'Poland' },
  { code: 'CZ', name: 'Czechia' },
  { code: 'SK', name: 'Slovakia' },
  { code: 'RO', name: 'Romania' },
  { code: 'BG', name: 'Bulgaria' },
  { code: 'HR', name: 'Croatia' },
  { code: 'RS', name: 'Serbia' },
  { code: 'SI', name: 'Slovenia' },
  { code: 'GR', name: 'Greece' },
  { code: 'TR', name: 'Turkey' },
  { code: 'UA', name: 'Ukraine' },
  { code: 'RU', name: 'Russia' },
  { code: 'CA', name: 'Canada' },
  { code: 'MX', name: 'Mexico' },
  { code: 'BR', name: 'Brazil' },
  { code: 'AR', name: 'Argentina' },
  { code: 'AU', name: 'Australia' },
  { code: 'NZ', name: 'New Zealand' },
  { code: 'JP', name: 'Japan' },
  { code: 'KR', name: 'South Korea' },
  { code: 'IN', name: 'India' },
  { code: 'ZA', name: 'South Africa' },
]

// Selection order is display order: newly picked countries append, so the
// first one chosen stays first on the media detail page.
function toggleRatingCountry(code: string) {
  const current = props.contentRatingCountries
  const next = current.includes(code) ? current.filter((c) => c !== code) : [...current, code]
  emit('update:contentRatingCountries', next)
  emit('dirty', 'contentRatingCountries')
}
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
            <Check v-if="tmdbTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
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
            <Check v-if="tvdbTest.success" class="w-4 h-4 inline" /><X v-else class="w-4 h-4 inline" />
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

    <!-- Content ratings -->
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20">
      <label class="block text-xs font-medium text-gray-400 mb-1.5">Age Rating Countries</label>
      <p class="text-[11px] text-gray-500 mb-3">
        Certifications shown on media detail pages, in the order you pick them. Countries without a
        rating for a title are skipped. Applies immediately — no re-scan needed.
      </p>

      <div class="flex flex-wrap gap-2">
        <button
          v-for="c in RATING_COUNTRIES"
          :key="c.code"
          type="button"
          :title="c.name"
          class="px-2.5 py-1 rounded-md text-xs font-medium border transition-colors duration-200"
          :class="contentRatingCountries.includes(c.code)
            ? 'bg-violet-600/25 border-violet-500/50 text-violet-200'
            : 'bg-[#0c0f1a] border-violet-800/30 text-gray-400 hover:text-gray-200 hover:border-violet-700/50'"
          @click="toggleRatingCountry(c.code)"
        >
          <span
            v-if="contentRatingCountries.includes(c.code)"
            class="text-[10px] text-violet-400 mr-1"
          >{{ contentRatingCountries.indexOf(c.code) + 1 }}</span>{{ c.code }}
        </button>
      </div>

      <p v-if="contentRatingCountries.length === 0" class="text-[11px] text-amber-400/80 mt-3">
        No countries selected — age ratings stay hidden on media detail pages.
      </p>
    </div>
  </div>
</template>
