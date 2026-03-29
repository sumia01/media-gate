<script setup lang="ts">
import { ref } from 'vue'
import client from '@/api/client'
import type { TorrentResult } from '@/types/api'
import ErrorBanner from '@/components/ErrorBanner.vue'

const query = ref('')
const searchType = ref('search')
const season = ref('')
const episode = ref('')
const imdbId = ref('')

const results = ref<TorrentResult[]>([])
const loading = ref(false)
const error = ref('')
const searched = ref(false)

async function search() {
  if (!query.value && !imdbId.value) return
  loading.value = true
  error.value = ''
  searched.value = true

  const params: Record<string, string | number> = {}
  if (query.value) params.query = query.value
  if (imdbId.value) params.imdbId = imdbId.value
  if (searchType.value !== 'search') params.type = searchType.value
  if (season.value) params.season = season.value
  if (episode.value) params.episode = episode.value
  params.limit = 100

  const { data, error: err } = await client.GET('/indexers/search', {
    params: { query: params as any },
  })
  loading.value = false
  if (err) {
    error.value = 'Search failed'
    return
  }
  results.value = data?.results ?? []
}

function formatSize(size: string): string {
  const bytes = parseFloat(size)
  if (isNaN(bytes) || bytes === 0) return size
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return (bytes / Math.pow(1024, i)).toFixed(1) + ' ' + units[i]
}

function formatDate(unix: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleDateString('hu-HU', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

function volumeLabel(dl: number | undefined, ul: number | undefined): string {
  const parts: string[] = []
  if (dl !== undefined && dl !== 1) {
    if (dl === 0) parts.push('Freeleech')
    else parts.push(`DL: ${dl}x`)
  }
  if (ul !== undefined && ul !== 1) {
    parts.push(`UL: ${ul}x`)
  }
  return parts.join(' / ')
}
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold text-gray-100 tracking-tight mb-6">Indexer Search</h1>

    <!-- Search form -->
    <div class="px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20 mb-6">
      <form class="space-y-4" @submit.prevent="search">
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <!-- Query -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Search Query</label>
            <input
              v-model="query"
              type="text"
              placeholder="Enter search term..."
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            />
          </div>

          <!-- IMDb ID -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">IMDb ID <span class="text-gray-600">(optional)</span></label>
            <input
              v-model="imdbId"
              type="text"
              placeholder="e.g. tt1234567"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
            />
          </div>
        </div>

        <div class="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <!-- Type -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Type</label>
            <select
              v-model="searchType"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            >
              <option value="search">General</option>
              <option value="movie-search">Movie</option>
              <option value="tv-search">TV</option>
            </select>
          </div>

          <!-- Season -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Season</label>
            <input
              v-model="season"
              type="text"
              placeholder="e.g. 1"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            />
          </div>

          <!-- Episode -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Episode</label>
            <input
              v-model="episode"
              type="text"
              placeholder="e.g. 5"
              class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            />
          </div>

          <!-- Search button -->
          <div class="flex items-end">
            <button
              type="submit"
              :disabled="loading || (!query && !imdbId)"
              class="w-full px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {{ loading ? 'Searching...' : 'Search' }}
            </button>
          </div>
        </div>
      </form>
    </div>

    <ErrorBanner :message="error" />

    <!-- Results -->
    <div v-if="loading" class="text-gray-500 text-sm">Searching indexers...</div>

    <div
      v-else-if="searched && !results.length"
      class="flex flex-col items-center justify-center py-16 text-gray-500"
    >
      <p class="text-sm">No results found.</p>
    </div>

    <div v-else-if="results.length" class="overflow-x-auto">
      <table class="w-full text-sm">
        <thead>
          <tr class="text-left text-xs font-semibold uppercase tracking-wider text-gray-500 border-b border-violet-900/20">
            <th class="px-3 py-2.5">Title</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Size</th>
            <th class="px-3 py-2.5 text-center whitespace-nowrap">S</th>
            <th class="px-3 py-2.5 text-center whitespace-nowrap">L</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Indexer</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Category</th>
            <th class="px-3 py-2.5 whitespace-nowrap">Date</th>
            <th class="px-3 py-2.5"></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="(result, idx) in results"
            :key="idx"
            class="border-b border-violet-900/10 hover:bg-violet-600/5 transition-colors duration-200"
          >
            <!-- Title -->
            <td class="px-3 py-2.5">
              <div class="flex items-center gap-2">
                <a
                  v-if="result.detailsUrl"
                  :href="result.detailsUrl"
                  target="_blank"
                  rel="noopener"
                  class="text-gray-200 hover:text-violet-300 transition-colors duration-200 truncate max-w-md"
                  :title="result.title"
                >
                  {{ result.title }}
                </a>
                <span v-else class="text-gray-200 truncate max-w-md" :title="result.title">
                  {{ result.title }}
                </span>
                <span
                  v-if="volumeLabel(result.downloadVolumeFactor, result.uploadVolumeFactor)"
                  class="text-[10px] font-bold uppercase px-1.5 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300 whitespace-nowrap flex-shrink-0"
                >
                  {{ volumeLabel(result.downloadVolumeFactor, result.uploadVolumeFactor) }}
                </span>
              </div>
            </td>

            <!-- Size -->
            <td class="px-3 py-2.5 text-gray-400 whitespace-nowrap">
              {{ formatSize(result.size) }}
            </td>

            <!-- Seeders -->
            <td class="px-3 py-2.5 text-center text-green-400">
              {{ result.seeders }}
            </td>

            <!-- Leechers -->
            <td class="px-3 py-2.5 text-center text-red-400">
              {{ result.leechers }}
            </td>

            <!-- Indexer -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ result.indexerName }}
            </td>

            <!-- Category -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ result.categoryDesc || result.category || '' }}
            </td>

            <!-- Date -->
            <td class="px-3 py-2.5 text-gray-500 whitespace-nowrap">
              {{ formatDate(result.date) }}
            </td>

            <!-- Download -->
            <td class="px-3 py-2.5">
              <a
                v-if="result.downloadUrl"
                :href="result.downloadUrl"
                target="_blank"
                rel="noopener"
                class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
              >
                Download
              </a>
            </td>
          </tr>
        </tbody>
      </table>

      <p class="mt-3 text-xs text-gray-600">{{ results.length }} results</p>
    </div>
  </div>
</template>
