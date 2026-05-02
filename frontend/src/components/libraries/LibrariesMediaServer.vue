<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import client from '@/api/client'
import type { PlexSection, PlexMapping } from '@/types/api'

// Connection settings
const plexUrl = ref('')
const plexToken = ref('')
const savedPlexUrl = ref('')
const savedPlexToken = ref('')
const showToken = ref(false)
const testing = ref(false)
const testResult = ref<{ success: boolean; message: string } | null>(null)
const savingConnection = ref(false)
const connectionError = ref('')

// Integration state
const connected = computed(() => savedPlexUrl.value !== '' && savedPlexToken.value !== '')
const dirty = computed(
  () => plexUrl.value !== savedPlexUrl.value || plexToken.value !== savedPlexToken.value,
)

// Library mappings
const sections = ref<PlexSection[]>([])
const mappings = ref<PlexMapping[]>([])
const loadingMappings = ref(false)
const savingMappings = ref(false)
const mappingError = ref('')
const selectedSections = ref<Record<number, string>>({})

async function fetchSettings() {
  const { data } = await client.GET('/settings')
  if (data?.settings) {
    const s = data.settings as Record<string, unknown>
    plexUrl.value = (s.plexUrl as string) ?? ''
    plexToken.value = (s.plexToken as string) ?? ''
    savedPlexUrl.value = plexUrl.value
    savedPlexToken.value = plexToken.value
  }
}

async function saveConnection() {
  savingConnection.value = true
  connectionError.value = ''
  testResult.value = null

  const body: Record<string, unknown> = {}
  if (plexUrl.value !== savedPlexUrl.value) body.plexUrl = plexUrl.value
  if (plexToken.value !== savedPlexToken.value) body.plexToken = plexToken.value

  const { error: err } = await client.PUT('/settings', { body: body as never })
  savingConnection.value = false

  if (err) {
    connectionError.value = 'Failed to save settings'
    return
  }

  savedPlexUrl.value = plexUrl.value
  savedPlexToken.value = plexToken.value

  // After saving credentials, reload sections and auto-match
  if (connected.value) {
    await loadMappings()
  }
}

async function disconnect() {
  savingConnection.value = true
  connectionError.value = ''
  testResult.value = null

  const { error: err } = await client.PUT('/settings', {
    body: { plexUrl: '', plexToken: '' } as never,
  })
  savingConnection.value = false

  if (err) {
    connectionError.value = 'Failed to disconnect'
    return
  }

  plexUrl.value = ''
  plexToken.value = ''
  savedPlexUrl.value = ''
  savedPlexToken.value = ''
  sections.value = []
  mappings.value = []
  selectedSections.value = {}
}

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

async function loadMappings() {
  loadingMappings.value = true
  mappingError.value = ''
  const [sectionsRes, mappingsRes] = await Promise.all([
    client.GET('/plex/sections'),
    client.GET('/plex/mappings'),
  ])
  loadingMappings.value = false

  if (sectionsRes.data) {
    sections.value = sectionsRes.data.sections ?? []
  }
  if (mappingsRes.data) {
    mappings.value = mappingsRes.data.mappings ?? []
    selectedSections.value = {}
    for (const m of mappings.value) {
      if (m.plexSectionId) {
        selectedSections.value[m.libraryId] = m.plexSectionId
      }
    }
  }
}

async function saveMappings() {
  savingMappings.value = true
  mappingError.value = ''
  const items = Object.entries(selectedSections.value)
    .filter(([, sectionId]) => sectionId)
    .map(([libId, sectionId]) => ({ libraryId: Number(libId), plexSectionId: sectionId }))

  const { data, error: err } = await client.PUT('/plex/mappings', {
    body: { mappings: items },
  })
  savingMappings.value = false
  if (err) {
    mappingError.value = 'Failed to save mappings'
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

onMounted(async () => {
  await fetchSettings()
  if (connected.value) {
    await loadMappings()
  }
})
</script>

<template>
  <div class="space-y-6">
    <!-- Connection settings -->
    <div class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-gray-200">Plex Media Server</h3>
        <div class="flex items-center gap-2">
          <span
            class="text-[10px] px-2 py-0.5 rounded-full"
            :class="
              connected
                ? 'bg-green-500/10 text-green-400 border border-green-500/20'
                : 'bg-gray-500/10 text-gray-500 border border-gray-500/20'
            "
          >
            {{ connected ? 'Connected' : 'Not configured' }}
          </span>
        </div>
      </div>

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
            :type="showToken ? 'text' : 'password'"
            placeholder="Plex authentication token"
            class="w-full px-3 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>
      </div>

      <div class="flex items-center gap-3 flex-wrap">
        <button
          class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="!dirty || savingConnection"
          @click="saveConnection"
        >
          {{ savingConnection ? 'Saving...' : 'Save' }}
        </button>
        <button
          class="px-4 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 hover:border-violet-500/50 text-gray-300 text-sm font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="testing || (!plexUrl && !plexToken)"
          @click="testConnection"
        >
          {{ testing ? 'Testing...' : 'Test Connection' }}
        </button>
        <button
          class="px-3 py-2 rounded-lg text-xs text-gray-500 hover:text-gray-300 transition-colors duration-200"
          @click="showToken = !showToken"
        >
          {{ showToken ? 'Hide token' : 'Show token' }}
        </button>
        <button
          v-if="connected"
          class="px-3 py-2 rounded-lg text-xs text-red-400/70 hover:text-red-400 transition-colors duration-200"
          :disabled="savingConnection"
          @click="disconnect"
        >
          Disconnect
        </button>
        <span
          v-if="testResult"
          class="text-xs"
          :class="testResult.success ? 'text-green-400' : 'text-red-400'"
        >
          {{ testResult.message }}
        </span>
        <span v-if="connectionError" class="text-xs text-red-400">
          {{ connectionError }}
        </span>
      </div>
    </div>

    <!-- Library mappings (only shown when connected) -->
    <div v-if="connected" class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-gray-200">Library Mappings</h3>
        <button
          class="px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-xs font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="savingMappings"
          @click="saveMappings"
        >
          {{ savingMappings ? 'Saving...' : 'Save Mappings' }}
        </button>
      </div>

      <p v-if="mappingError" class="text-xs text-red-400 mb-3">{{ mappingError }}</p>
      <p v-if="loadingMappings" class="text-xs text-gray-500">Loading...</p>

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

        <p v-if="!mappings.length && !loadingMappings" class="text-xs text-gray-500">
          No libraries found. Add libraries first, then configure Plex mappings.
        </p>
      </div>
    </div>
  </div>
</template>
