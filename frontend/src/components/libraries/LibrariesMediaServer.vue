<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import client from '@/api/client'
import type { PlexMapping, PlexSection, Settings } from '@/types/api'

const props = defineProps<{ server: 'plex' | 'jellyfin' }>()
// The parent keys this component by server so requests and drafts stay isolated.
const isPlex = props.server === 'plex'
const serverName = isPlex ? 'Plex' : 'Jellyfin'
const urlKey = isPlex ? 'plexUrl' : 'jellyfinUrl'
const secretKey = isPlex ? 'plexToken' : 'jellyfinApiKey'
const enabledKey = isPlex ? 'plexRefreshEnabled' : 'jellyfinRefreshEnabled'
const serverUrl = ref('')
const secret = ref('')
const savedUrl = ref('')
const savedSecret = ref('')
const refreshEnabled = ref(false)
const loadingSettings = ref(true)
const settingsLoaded = ref(false)
const savingToggle = ref(false)
const toggleError = ref('')
const showToken = ref(false)
const testing = ref(false)
const testResult = ref<{ success: boolean; message: string } | null>(null)
const savingConnection = ref(false)
const connectionError = ref('')

// Integration state
const connected = computed(() => savedUrl.value !== '' && savedSecret.value !== '')
const dirty = computed(() => serverUrl.value !== savedUrl.value || secret.value !== savedSecret.value)
const busy = computed(
  () => loadingSettings.value || savingConnection.value || savingToggle.value || !settingsLoaded.value,
)

// Library mappings
type Mapping = Omit<PlexMapping, 'plexSectionId' | 'plexSectionTitle'> & { sectionId: string; sectionTitle: string }
const sections = ref<PlexSection[]>([])
const mappings = ref<Mapping[]>([])
const loadingMappings = ref(false)
const savingMappings = ref(false)
const mappingError = ref('')
const selectedSections = ref<Record<number, string>>({})
const refreshing = ref('')
const mappingNotice = ref('')
let mounted = true
let mappingRequest = 0
let mappingController: AbortController | null = null

function cancelMappingLoad() {
  mappingRequest++
  mappingController?.abort()
  mappingController = null
  loadingMappings.value = false
}

onUnmounted(() => {
  mounted = false
  cancelMappingLoad()
})

function applySettings(settings: Settings) {
  serverUrl.value = settings[urlKey] ?? ''
  secret.value = settings[secretKey] ?? ''
  savedUrl.value = serverUrl.value
  savedSecret.value = secret.value
  refreshEnabled.value = settings[enabledKey] ?? isPlex
}

async function fetchSettings() {
  loadingSettings.value = true
  connectionError.value = ''
  try {
    const { data, response } = await client.GET('/settings')
    if (!response.ok || !data) throw new Error('settings')
    if (!mounted) return
    applySettings(data.settings)
    settingsLoaded.value = true
    if (connected.value) await loadMappings()
  } catch {
    connectionError.value = 'Failed to load settings. Please retry.'
  } finally {
    loadingSettings.value = false
  }
}

async function toggleRefresh() {
  if (busy.value) return
  savingToggle.value = true
  toggleError.value = ''
  try {
    const enabled = !refreshEnabled.value
    const { response } = await client.PUT('/settings', { body: { [enabledKey]: enabled } })
    if (!response.ok) throw new Error('toggle')
    refreshEnabled.value = enabled
  } catch {
    toggleError.value = 'Failed to change library refresh. Please retry.'
  } finally {
    savingToggle.value = false
  }
}

async function saveConnection() {
  cancelMappingLoad()
  savingConnection.value = true
  connectionError.value = ''
  testResult.value = null

  const body: Settings = {}
  if (serverUrl.value !== savedUrl.value) body[urlKey] = serverUrl.value
  if (secret.value !== savedSecret.value) body[secretKey] = secret.value
  try {
    const { data, response } = await client.PUT('/settings', { body })
    if (!response.ok || !data) throw new Error('save')
    applySettings(data.settings)
    if (connected.value) await loadMappings()
  } catch {
    connectionError.value = 'Failed to save settings'
  } finally {
    savingConnection.value = false
  }
}

async function disconnect() {
  cancelMappingLoad()
  savingConnection.value = true
  connectionError.value = ''
  testResult.value = null

  try {
    const { data, response } = await client.PUT('/settings', {
      body: { [urlKey]: '', [secretKey]: '', [enabledKey]: false },
    })
    if (!response.ok || !data) throw new Error('disconnect')
    applySettings(data.settings)
    sections.value = []
    mappings.value = []
    selectedSections.value = {}
  } catch {
    connectionError.value = 'Failed to disconnect'
  } finally {
    savingConnection.value = false
  }
}

async function testConnection() {
  testing.value = true
  testResult.value = null
  // Only send fields the user has changed; omitted fields trigger backend fallback to saved values.
  const body: Record<string, string> = {}
  if (serverUrl.value !== savedUrl.value) body.url = serverUrl.value
  if (secret.value !== savedSecret.value) body[isPlex ? 'token' : 'apiKey'] = secret.value
  try {
    const { data, response } = isPlex
      ? await client.POST('/settings/test-plex', { body })
      : await client.POST('/settings/test-jellyfin', { body })
    if (!response.ok || !data) throw new Error('test')
    testResult.value = { success: data.success, message: data.message ?? '' }
  } catch {
    testResult.value = { success: false, message: 'Request failed' }
  } finally {
    testing.value = false
  }
}

async function loadMappings() {
  if (!mounted) return
  cancelMappingLoad()
  const request = mappingRequest
  const controller = new AbortController()
  mappingController = controller
  const current = () => mounted && request === mappingRequest
  loadingMappings.value = true
  mappingError.value = ''
  mappingNotice.value = ''
  try {
    if (isPlex) {
      const [sectionsRes, mappingsRes] = await Promise.all([
        client.GET('/plex/sections', { signal: controller.signal }),
        client.GET('/plex/mappings', { signal: controller.signal }),
      ])
      if (!current()) return
      if (!sectionsRes.response.ok || !mappingsRes.response.ok || !sectionsRes.data || !mappingsRes.data)
        throw new Error('mappings')
      sections.value = sectionsRes.data.sections
      mappings.value = mappingsRes.data.mappings.map((m) => ({
        ...m,
        sectionId: m.plexSectionId,
        sectionTitle: m.plexSectionTitle,
      }))
    } else {
      const [sectionsRes, mappingsRes] = await Promise.all([
        client.GET('/jellyfin/libraries', { signal: controller.signal }),
        client.GET('/jellyfin/mappings', { signal: controller.signal }),
      ])
      if (!current()) return
      if (!sectionsRes.response.ok || !mappingsRes.response.ok || !sectionsRes.data || !mappingsRes.data)
        throw new Error('mappings')
      sections.value = sectionsRes.data.libraries
      mappings.value = mappingsRes.data.mappings.map((m) => ({
        ...m,
        sectionId: m.jellyfinLibraryId,
        sectionTitle: m.jellyfinLibraryTitle,
      }))
    }
    selectedSections.value = {}
    for (const m of mappings.value) {
      selectedSections.value[m.libraryId] = m.sectionId
    }
  } catch {
    if (!current()) return
    sections.value = []
    mappings.value = []
    selectedSections.value = {}
    mappingError.value = `Failed to load ${serverName} libraries. Check the connection and retry.`
  } finally {
    if (current()) {
      loadingMappings.value = false
      mappingController = null
    }
  }
}

async function saveMappings() {
  savingMappings.value = true
  mappingError.value = ''
  mappingNotice.value = ''
  const items = mappings.value.map((m) => ({
    libraryId: m.libraryId,
    sectionId: selectedSections.value[m.libraryId] ?? '',
  }))
  try {
    const result = isPlex
      ? await client.PUT('/plex/mappings', {
          body: { mappings: items.map((m) => ({ libraryId: m.libraryId, plexSectionId: m.sectionId })) },
        })
      : await client.PUT('/jellyfin/mappings', {
          body: { mappings: items.map((m) => ({ libraryId: m.libraryId, jellyfinLibraryId: m.sectionId })) },
        })
    if (!result.response.ok) throw new Error('save mappings')
    mappingNotice.value = 'Mappings saved'
  } catch {
    mappingError.value = 'Failed to save mappings'
  } finally {
    savingMappings.value = false
  }
}

async function refreshSection(sectionId: string) {
  refreshing.value = sectionId
  mappingError.value = ''
  mappingNotice.value = ''
  try {
    const { response } = isPlex
      ? await client.POST('/plex/refresh/{sectionId}', { params: { path: { sectionId } } })
      : await client.POST('/jellyfin/refresh/{libraryId}', { params: { path: { libraryId: sectionId } } })
    if (!response.ok) throw new Error('refresh')
    mappingNotice.value = 'Library refresh requested'
  } catch {
    mappingError.value = 'Failed to refresh library. Check that refresh is enabled and the server is reachable.'
  } finally {
    refreshing.value = ''
  }
}

function compatibleSections(libraryType: string): PlexSection[] {
  return sections.value.filter((s) => {
    if (libraryType === 'movie') return s.type === 'movie'
    if (libraryType === 'series' || libraryType === 'tv') return s.type === 'show'
    return false
  })
}

onMounted(fetchSettings)
</script>

<template>
  <div class="space-y-6">
    <div class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between gap-4">
        <div>
          <h3 :id="`${server}-refresh-label`" class="text-sm font-medium text-gray-200">Enable library refresh</h3>
          <p class="text-xs text-gray-500 mt-1">Refresh {{ serverName }} after imports and deletions. Turning this off preserves your connection and mappings.</p>
        </div>
        <button
          type="button"
          role="switch"
          :aria-checked="refreshEnabled"
          :aria-labelledby="`${server}-refresh-label`"
          :disabled="busy"
          class="relative inline-flex h-6 w-11 shrink-0 rounded-full transition-colors disabled:opacity-50 focus-visible:outline-2 focus-visible:outline-violet-400"
          :class="refreshEnabled ? 'bg-violet-600' : 'bg-gray-700'"
          @click="toggleRefresh"
        >
          <span class="inline-block h-5 w-5 rounded-full bg-white transition-transform mt-0.5" :class="refreshEnabled ? 'translate-x-5.5' : 'translate-x-0.5'" />
        </button>
      </div>
      <p class="text-xs text-gray-400 mt-2" role="status">{{ savingToggle ? 'Saving...' : refreshEnabled ? 'Library refresh enabled' : 'Library refresh disabled' }}</p>
      <p v-if="toggleError" class="text-xs text-red-400 mt-2" role="alert">{{ toggleError }}</p>
    </div>
    <!-- Page summary -->
    <p class="text-xs text-gray-500 leading-relaxed">
      Connect your {{ serverName }} server, then save a mapping for each MediaGate library so new content appears without manual rescanning.
    </p>
    <p v-if="loadingSettings" class="text-xs text-gray-500">Loading settings...</p>
    <button v-if="!settingsLoaded && !loadingSettings" class="text-sm text-violet-400" @click="fetchSettings">Retry loading settings</button>
    <p v-if="connectionError" class="text-xs text-red-400" role="alert">{{ connectionError }}</p>

    <!-- Connection settings -->
    <fieldset :disabled="busy" class="min-w-0 rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-gray-200">{{ isPlex ? 'Plex server settings' : 'Jellyfin server' }}</h3>
        <div class="flex items-center gap-2">
          <span
            class="text-[10px] px-2 py-0.5 rounded-full"
            :class="
              connected
                ? 'bg-green-500/10 text-green-400 border border-green-500/20'
                : 'bg-gray-500/10 text-gray-500 border border-gray-500/20'
            "
          >
            {{ connected ? 'Configured' : 'Not configured' }}
          </span>
        </div>
      </div>

      <div class="grid grid-cols-1 md:grid-cols-2 gap-4 mb-4">
        <div>
          <label :for="`${server}-url`" class="block text-xs font-medium text-gray-400 mb-1.5">Server URL</label>
          <input
            :id="`${server}-url`"
            v-model="serverUrl"
            type="text"
            :placeholder="isPlex ? 'http://192.168.1.10:32400' : 'http://192.168.1.10:8096'"
            class="w-full px-3 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>
        <div>
          <label :for="`${server}-secret`" class="block text-xs font-medium text-gray-400 mb-1.5">{{ isPlex ? 'Token' : 'API key' }}</label>
          <input
            :id="`${server}-secret`"
            v-model="secret"
            :type="showToken ? 'text' : 'password'"
            :placeholder="isPlex ? 'Plex authentication token' : 'Jellyfin API key'"
            class="w-full px-3 py-2 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>
      </div>
      <p v-if="!isPlex" class="text-xs text-gray-500 mb-4">Create an API key in Jellyfin under Dashboard → API Keys. Include the base path in the server URL if one is configured.</p>

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
          :disabled="testing || !serverUrl || !secret"
          @click="testConnection"
        >
          {{ testing ? 'Testing...' : 'Test Connection' }}
        </button>
        <button
          class="px-3 py-2 rounded-lg text-xs text-gray-500 hover:text-gray-300 transition-colors duration-200"
          @click="showToken = !showToken"
        >
          {{ showToken ? 'Hide' : 'Show' }} {{ isPlex ? 'token' : 'API key' }}
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
      </div>
    </fieldset>

    <!-- Library mappings (only shown when connected) -->
    <div v-if="connected" class="rounded-lg bg-[#161b2e] border border-violet-900/20 p-5">
      <div class="flex items-center justify-between mb-4">
        <h3 class="text-sm font-medium text-gray-200">Library Mappings</h3>
        <button
          class="px-3 py-1.5 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-xs font-medium transition-colors duration-200 disabled:opacity-50"
          :disabled="savingMappings || loadingMappings || busy || !mappings.length"
          @click="saveMappings"
        >
          {{ savingMappings ? 'Saving...' : 'Save Mappings' }}
        </button>
      </div>

      <p v-if="mappingError" class="text-xs text-red-400 mb-3">{{ mappingError }}</p>
      <p v-if="mappingNotice" class="text-xs text-green-400 mb-3" role="status">{{ mappingNotice }}</p>
      <p class="text-xs text-gray-500 mb-3">Auto-matches are suggestions; use Save Mappings to activate them. Choose Not mapped to stop refreshing a library.</p>
      <button class="text-xs text-violet-400 mb-3 disabled:opacity-50" :disabled="loadingMappings || savingMappings || busy" @click="loadMappings">Reload libraries</button>
      <p v-if="loadingMappings" class="text-xs text-gray-500">Loading...</p>

      <div v-else class="space-y-3">
        <div
          v-for="m in mappings"
          :key="m.libraryId"
          class="flex flex-col sm:flex-row sm:items-center gap-3 py-2 border-b border-violet-900/10 last:border-0"
        >
          <div class="flex-1 min-w-0">
            <p class="text-sm text-gray-200 truncate">{{ m.libraryName }}</p>
            <p class="text-xs text-gray-500 truncate font-mono">{{ m.libraryPath }}</p>
          </div>

          <select
            class="w-full sm:w-44 xl:w-56 min-w-0 px-3 py-1.5 rounded-lg bg-[#0d1117] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            :aria-label="`${m.libraryName} ${serverName} library`"
            :disabled="savingMappings || busy"
            :value="selectedSections[m.libraryId] ?? ''"
            @change="selectedSections[m.libraryId] = ($event.target as HTMLSelectElement).value"
          >
            <option value="">Not mapped</option>
            <option v-if="m.sectionId && !sections.some((s) => s.id === m.sectionId)" :value="m.sectionId">{{ m.sectionTitle || m.sectionId }} (unavailable)</option>
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
            :disabled="!refreshEnabled || !!refreshing || busy"
            @click="refreshSection(selectedSections[m.libraryId] ?? '')"
          >
            {{ refreshing === selectedSections[m.libraryId] ? 'Refreshing...' : 'Refresh' }}
          </button>
        </div>

        <p v-if="!mappings.length && !loadingMappings" class="text-xs text-gray-500">
          No libraries found. Add libraries first, then configure {{ serverName }} mappings.
        </p>
      </div>
    </div>
  </div>
</template>
