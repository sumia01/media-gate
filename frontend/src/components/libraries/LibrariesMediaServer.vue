<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'
import type { PlexSection, PlexMapping } from '@/types/api'

const plexUrl = ref('')
const plexToken = ref('')
const testing = ref(false)
const testResult = ref<{ success: boolean; message: string } | null>(null)

const sections = ref<PlexSection[]>([])
const mappings = ref<PlexMapping[]>([])
const loading = ref(false)
const saving = ref(false)
const error = ref('')

// Selected section IDs keyed by libraryId.
const selectedSections = ref<Record<number, string>>({})

async function testConnection() {
  testing.value = true
  testResult.value = null
  const { data, error: err } = await client.POST('/settings/test-plex', {
    body: { url: plexUrl.value || undefined, token: plexToken.value || undefined },
  })
  testing.value = false
  if (err || !data) {
    testResult.value = { success: false, message: 'Request failed' }
    return
  }
  testResult.value = { success: data.success, message: data.message ?? '' }
}

async function loadData() {
  loading.value = true
  error.value = ''
  const [sectionsRes, mappingsRes] = await Promise.all([
    client.GET('/plex/sections'),
    client.GET('/plex/mappings'),
  ])
  loading.value = false

  if (sectionsRes.data) {
    sections.value = sectionsRes.data.sections ?? []
  }
  if (mappingsRes.data) {
    mappings.value = mappingsRes.data.mappings ?? []
    // Initialize selections from current mappings.
    for (const m of mappings.value) {
      if (m.plexSectionId) {
        selectedSections.value[m.libraryId] = m.plexSectionId
      }
    }
  }
}

async function saveMappings() {
  saving.value = true
  error.value = ''
  const items = Object.entries(selectedSections.value)
    .filter(([, sectionId]) => sectionId)
    .map(([libId, sectionId]) => ({ libraryId: Number(libId), plexSectionId: sectionId }))

  const { data, error: err } = await client.PUT('/plex/mappings', {
    body: { mappings: items },
  })
  saving.value = false
  if (err) {
    error.value = 'Failed to save mappings'
    return
  }
  if (data) {
    mappings.value = data.mappings ?? []
  }
}

async function refreshSection(sectionId: string) {
  await client.POST('/plex/refresh/{sectionId}', {
    params: { path: { sectionId } },
  })
}

function compatibleSections(libraryType: string): PlexSection[] {
  return sections.value.filter((s) => {
    if (libraryType === 'movie') return s.type === 'movie'
    if (libraryType === 'series' || libraryType === 'tv') return s.type === 'show'
    return false
  })
}

onMounted(loadData)
</script>

<template>
  <div class="space-y-6">
    <!-- Connection settings -->
    <div class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <h3 class="text-sm font-medium text-gray-200 mb-4">Plex Media Server</h3>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Server URL</label>
          <input
            v-model="plexUrl"
            type="text"
            placeholder="http://192.168.1.10:32400"
            class="w-full px-3 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Token</label>
          <input
            v-model="plexToken"
            type="password"
            placeholder="Plex authentication token"
            class="w-full px-3 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>
      </div>
      <div class="flex items-center gap-3">
        <button
          class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="testing"
          @click="testConnection"
        >
          {{ testing ? 'Testing...' : 'Test Connection' }}
        </button>
        <span
          v-if="testResult"
          class="text-xs"
          :class="testResult.success ? 'text-green-400' : 'text-red-400'"
        >
          {{ testResult.message }}
        </span>
      </div>
    </div>

    <!-- Library mappings -->
    <div class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-gray-200">Library Mappings</h3>
        <button
          class="px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-xs font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="saving"
          @click="saveMappings"
        >
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </div>

      <p v-if="error" class="text-xs text-red-400 mb-3">{{ error }}</p>
      <p v-if="loading" class="text-xs text-gray-500">Loading...</p>

      <div v-else class="space-y-3">
        <div
          v-for="m in mappings"
          :key="m.libraryId"
          class="flex items-center gap-4 py-2 border-b border-violet-900/10 last:border-0"
        >
          <div class="flex-1 min-w-0">
            <p class="text-sm text-gray-200 truncate">{{ m.libraryName }}</p>
            <p class="text-xs text-gray-500 truncate font-mono">{{ m.libraryPath }}</p>
          </div>

          <select
            class="w-56 px-3 py-1.5 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            :value="selectedSections[m.libraryId] ?? ''"
            @change="selectedSections[m.libraryId] = ($event.target as HTMLSelectElement).value"
          >
            <option value="">Not mapped</option>
            <option
              v-for="s in compatibleSections(m.libraryType)"
              :key="s.id"
              :value="s.id"
            >
              {{ s.title }}
            </option>
          </select>

          <button
            v-if="selectedSections[m.libraryId]"
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
            title="Trigger library refresh"
            @click="refreshSection(selectedSections[m.libraryId] ?? '')"
          >
            Refresh
          </button>
        </div>

        <p v-if="!mappings.length && !loading" class="text-xs text-gray-500">
          No libraries found. Add libraries first, then configure Plex mappings.
        </p>
      </div>
    </div>
  </div>
</template>
