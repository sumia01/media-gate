<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import client from '@/api/client'
import ErrorBanner from '@/components/ErrorBanner.vue'
import SettingsDownloads from '@/components/settings/SettingsDownloads.vue'
import SettingsGeneral from '@/components/settings/SettingsGeneral.vue'
import SettingsMediaDb from '@/components/settings/SettingsMediaDb.vue'

const activeTab = ref<'media-db' | 'downloads' | 'general'>('general')

const tabs = [
  { key: 'general' as const, label: 'General' },
  { key: 'media-db' as const, label: 'Media DB' },
  { key: 'downloads' as const, label: 'Downloads' },
]

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
const qbSavePath = ref('')
const qbSavePathDirty = ref(false)
const qbCategory = ref('')
const qbCategoryDirty = ref(false)

const fsUrl = ref('')
const fsUrlDirty = ref(false)
const fsTest = ref<{ success: boolean; message: string } | null>(null)
const fsTesting = ref(false)

const discordUrl = ref('')
const discordUrlDirty = ref(false)
const showDiscordUrl = ref(false)
const discordTest = ref<{ success: boolean; message: string } | null>(null)
const discordTesting = ref(false)

const seasonPackPref = ref('prefer_packs')
const seasonPackPrefDirty = ref(false)

const watchedListMode = ref('global')
const watchedListModeDirty = ref(false)

const osApiKey = ref('')
const osApiKeyDirty = ref(false)
const showOsApiKey = ref(false)
const osUsername = ref('')
const osPassword = ref('')
const osRateLimit = ref('3')
const osUsernameDirty = ref(false)
const osPasswordDirty = ref(false)
const osRateLimitDirty = ref(false)
const showOsPassword = ref(false)
const osTest = ref<{ success: boolean; message: string } | null>(null)
const osTesting = ref(false)
const subtitleLanguages = ref('')
const subtitleLanguagesDirty = ref(false)
const subtitleAutoSearch = ref(false)
const subtitleAutoSearchDirty = ref(false)

const monitorInterval = ref('900')
const downloadInterval = ref('5')
const importerInterval = ref('10')
const metadataRefreshInterval = ref('21600')
const updateCheckInterval = ref('21600')
const otelEnabled = ref(false)
const otelEndpoint = ref('')
const otelService = ref('')
const otelLogLevel = ref('info')
const monitorIntervalDirty = ref(false)
const downloadIntervalDirty = ref(false)
const importerIntervalDirty = ref(false)
const metadataRefreshIntervalDirty = ref(false)
const updateCheckIntervalDirty = ref(false)
const otelEnabledDirty = ref(false)
const otelEndpointDirty = ref(false)
const otelServiceDirty = ref(false)
const otelLogLevelDirty = ref(false)

const dirtyMap: Record<string, { ref: { value: boolean } }> = {
  tmdb: { ref: tmdbDirty },
  tvdb: { ref: tvdbDirty },
  primarySource: { ref: primarySourceDirty },
  tmdbRateLimit: { ref: tmdbRateLimitDirty },
  tvdbRateLimit: { ref: tvdbRateLimitDirty },
  qbUrl: { ref: qbUrlDirty },
  qbUsername: { ref: qbUsernameDirty },
  qbPassword: { ref: qbPasswordDirty },
  qbDownloadPath: { ref: qbDownloadPathDirty },
  qbSavePath: { ref: qbSavePathDirty },
  qbCategory: { ref: qbCategoryDirty },
  fsUrl: { ref: fsUrlDirty },
  discord: { ref: discordUrlDirty },
  seasonPackPref: { ref: seasonPackPrefDirty },
  watchedListMode: { ref: watchedListModeDirty },
  osApiKey: { ref: osApiKeyDirty },
  osUsername: { ref: osUsernameDirty },
  osPassword: { ref: osPasswordDirty },
  osRateLimit: { ref: osRateLimitDirty },
  subtitleLanguages: { ref: subtitleLanguagesDirty },
  subtitleAutoSearch: { ref: subtitleAutoSearchDirty },
  monitorInterval: { ref: monitorIntervalDirty },
  downloadInterval: { ref: downloadIntervalDirty },
  importerInterval: { ref: importerIntervalDirty },
  metadataRefreshInterval: { ref: metadataRefreshIntervalDirty },
  updateCheckInterval: { ref: updateCheckIntervalDirty },
  otelEnabled: { ref: otelEnabledDirty },
  otelEndpoint: { ref: otelEndpointDirty },
  otelService: { ref: otelServiceDirty },
  otelLogLevel: { ref: otelLogLevelDirty },
}

function markDirty(field: string) {
  const entry = dirtyMap[field]
  if (entry) entry.ref.value = true
}

const anyDirty = computed(
  () =>
    tmdbDirty.value ||
    tvdbDirty.value ||
    primarySourceDirty.value ||
    tmdbRateLimitDirty.value ||
    tvdbRateLimitDirty.value ||
    qbUrlDirty.value ||
    qbUsernameDirty.value ||
    qbPasswordDirty.value ||
    qbDownloadPathDirty.value ||
    qbSavePathDirty.value ||
    qbCategoryDirty.value ||
    fsUrlDirty.value ||
    discordUrlDirty.value ||
    seasonPackPrefDirty.value ||
    watchedListModeDirty.value ||
    osApiKeyDirty.value ||
    osUsernameDirty.value ||
    osPasswordDirty.value ||
    osRateLimitDirty.value ||
    subtitleLanguagesDirty.value ||
    subtitleAutoSearchDirty.value ||
    monitorIntervalDirty.value ||
    downloadIntervalDirty.value ||
    importerIntervalDirty.value ||
    metadataRefreshIntervalDirty.value ||
    updateCheckIntervalDirty.value ||
    otelEnabledDirty.value ||
    otelEndpointDirty.value ||
    otelServiceDirty.value ||
    otelLogLevelDirty.value,
)

function resetAllDirty() {
  for (const entry of Object.values(dirtyMap)) {
    entry.ref.value = false
  }
}

function applySettings(s: Record<string, unknown>) {
  tmdbKey.value = (s.tmdbApiKey as string) ?? ''
  tvdbKey.value = (s.tvdbApiKey as string) ?? ''
  tmdbFromEnv.value = (s.tmdbApiKeyFromEnv as boolean) ?? false
  tvdbFromEnv.value = (s.tvdbApiKeyFromEnv as boolean) ?? false
  primarySource.value = (s.metadataPrimarySource as string) ?? 'tmdb'
  tmdbRateLimit.value = String(s.tmdbRateLimit ?? 4)
  tvdbRateLimit.value = String(s.tvdbRateLimit ?? 4)
  qbUrl.value = (s.qbitUrl as string) ?? ''
  qbUsername.value = (s.qbitUsername as string) ?? ''
  qbPassword.value = (s.qbitPassword as string) ?? ''
  qbDownloadPath.value = (s.qbitDownloadPath as string) ?? ''
  qbSavePath.value = (s.qbitSavePath as string) ?? ''
  qbCategory.value = (s.qbitCategory as string) ?? ''
  fsUrl.value = (s.flaresolverrUrl as string) ?? ''
  discordUrl.value = (s.discordWebhookUrl as string) ?? ''
  seasonPackPref.value = (s.monitorSeasonPackPreference as string) ?? 'prefer_packs'
  watchedListMode.value = (s.watchedListMode as string) ?? 'global'
  osApiKey.value = (s.opensubtitlesApiKey as string) ?? ''
  osUsername.value = (s.opensubtitlesUsername as string) ?? ''
  osPassword.value = (s.opensubtitlesPassword as string) ?? ''
  osRateLimit.value = String(s.opensubtitlesRateLimit ?? 3)
  subtitleLanguages.value = ((s.subtitleLanguages as string[]) ?? []).join(', ')
  subtitleAutoSearch.value = (s.subtitleAutoSearch as boolean) ?? false
  monitorInterval.value = String(s.workerMonitorInterval ?? 900)
  downloadInterval.value = String(s.workerDownloadInterval ?? 5)
  importerInterval.value = String(s.workerImporterInterval ?? 10)
  metadataRefreshInterval.value = String(s.workerMetadataRefreshInterval ?? 21600)
  updateCheckInterval.value = String(s.workerUpdateCheckInterval ?? 21600)
  otelEnabled.value = (s.otelEnabled as boolean) ?? false
  otelEndpoint.value = (s.otelEndpoint as string) ?? ''
  otelService.value = (s.otelService as string) ?? ''
  otelLogLevel.value = (s.otelLogLevel as string) ?? 'info'
  resetAllDirty()
}

async function fetchSettings() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/settings')
  loading.value = false
  if (err) {
    error.value = 'Failed to load settings'
    return
  }
  if (data?.settings) applySettings(data.settings as unknown as Record<string, unknown>)
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
  if (qbSavePathDirty.value) body.qbitSavePath = qbSavePath.value
  if (qbCategoryDirty.value) body.qbitCategory = qbCategory.value
  if (fsUrlDirty.value) body.flaresolverrUrl = fsUrl.value
  if (discordUrlDirty.value && !isMasked(discordUrl.value)) body.discordWebhookUrl = discordUrl.value
  if (seasonPackPrefDirty.value) body.monitorSeasonPackPreference = seasonPackPref.value
  if (watchedListModeDirty.value) body.watchedListMode = watchedListMode.value
  if (osApiKeyDirty.value && !isMasked(osApiKey.value)) body.opensubtitlesApiKey = osApiKey.value
  if (osUsernameDirty.value) body.opensubtitlesUsername = osUsername.value
  if (osPasswordDirty.value && !isMasked(osPassword.value)) body.opensubtitlesPassword = osPassword.value
  if (osRateLimitDirty.value) body.opensubtitlesRateLimit = Number(osRateLimit.value)
  if (subtitleLanguagesDirty.value)
    body.subtitleLanguages = subtitleLanguages.value
      .split(',')
      .map((s: string) => s.trim())
      .filter(Boolean)
  if (subtitleAutoSearchDirty.value) body.subtitleAutoSearch = subtitleAutoSearch.value
  if (monitorIntervalDirty.value) body.workerMonitorInterval = Number(monitorInterval.value)
  if (downloadIntervalDirty.value) body.workerDownloadInterval = Number(downloadInterval.value)
  if (importerIntervalDirty.value) body.workerImporterInterval = Number(importerInterval.value)
  if (metadataRefreshIntervalDirty.value) body.workerMetadataRefreshInterval = Number(metadataRefreshInterval.value)
  if (updateCheckIntervalDirty.value) body.workerUpdateCheckInterval = Number(updateCheckInterval.value)
  if (otelEnabledDirty.value) body.otelEnabled = otelEnabled.value
  if (otelEndpointDirty.value) body.otelEndpoint = otelEndpoint.value
  if (otelServiceDirty.value) body.otelService = otelService.value
  if (otelLogLevelDirty.value) body.otelLogLevel = otelLogLevel.value

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
  setTimeout(() => {
    saveSuccess.value = false
  }, 3000)

  if (data?.settings) applySettings(data.settings as unknown as Record<string, unknown>)
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

async function testDiscord() {
  discordTesting.value = true
  discordTest.value = null
  const body: Record<string, string> = {}
  if (discordUrlDirty.value) body.url = discordUrl.value
  const { data, error: err } = await client.POST('/settings/test-discord', { body })
  discordTesting.value = false
  if (err) {
    discordTest.value = { success: false, message: 'Request failed' }
    return
  }
  discordTest.value = { success: data!.success, message: data!.message ?? '' }
}

async function testOpenSubtitles() {
  osTesting.value = true
  osTest.value = null
  const body: Record<string, string> = {}
  if (osApiKeyDirty.value) body.apiKey = osApiKey.value
  if (osUsernameDirty.value) body.username = osUsername.value
  if (osPasswordDirty.value) body.password = osPassword.value
  const { data, error: err } = await client.POST('/settings/test-opensubtitles', { body })
  osTesting.value = false
  if (err) {
    osTest.value = { success: false, message: 'Request failed' }
    return
  }
  osTest.value = { success: data!.success, message: data!.message ?? '' }
}

async function disconnectDiscord() {
  saving.value = true
  error.value = ''
  const { error: err } = await client.PUT('/settings', { body: { discordWebhookUrl: '' } })
  saving.value = false
  if (err) {
    error.value = 'Failed to disconnect Discord'
    return
  }
  discordUrl.value = ''
  discordUrlDirty.value = false
  discordTest.value = null
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
      <!-- Tab bar -->
      <div class="flex gap-1 border-b border-violet-900/30 mb-6">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          class="px-4 py-2.5 text-sm font-medium transition-colors duration-200 border-b-2 -mb-px"
          :class="activeTab === tab.key
            ? 'text-violet-400 border-violet-500'
            : 'text-gray-500 border-transparent hover:text-gray-300 hover:border-violet-800/50'"
          @click="activeTab = tab.key"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- Media DB tab -->
      <SettingsMediaDb
        v-if="activeTab === 'media-db'"
        v-model:tmdb-key="tmdbKey"
        v-model:tvdb-key="tvdbKey"
        v-model:show-tmdb-key="showTmdbKey"
        v-model:show-tvdb-key="showTvdbKey"
        v-model:primary-source="primarySource"
        v-model:tmdb-rate-limit="tmdbRateLimit"
        v-model:tvdb-rate-limit="tvdbRateLimit"
        :tmdb-from-env="tmdbFromEnv"
        :tvdb-from-env="tvdbFromEnv"
        :tmdb-testing="tmdbTesting"
        :tvdb-testing="tvdbTesting"
        :tmdb-test="tmdbTest"
        :tvdb-test="tvdbTest"
        @dirty="markDirty"
        @test-tmdb="testTmdb"
        @test-tvdb="testTvdb"
      />

      <!-- Downloads tab -->
      <SettingsDownloads
        v-if="activeTab === 'downloads'"
        v-model:qb-url="qbUrl"
        v-model:qb-username="qbUsername"
        v-model:qb-password="qbPassword"
        v-model:show-qb-password="showQbPassword"
        v-model:qb-download-path="qbDownloadPath"
        v-model:qb-save-path="qbSavePath"
        v-model:qb-category="qbCategory"
        v-model:os-api-key="osApiKey"
        v-model:show-os-api-key="showOsApiKey"
        v-model:os-username="osUsername"
        v-model:os-password="osPassword"
        v-model:show-os-password="showOsPassword"
        v-model:os-rate-limit="osRateLimit"
        v-model:subtitle-languages="subtitleLanguages"
        v-model:subtitle-auto-search="subtitleAutoSearch"
        v-model:fs-url="fsUrl"
        :qb-testing="qbTesting"
        :qb-test="qbTest"
        :os-testing="osTesting"
        :os-test="osTest"
        :fs-testing="fsTesting"
        :fs-test="fsTest"
        @dirty="markDirty"
        @test-qbittorrent="testQbittorrent"
        @test-open-subtitles="testOpenSubtitles"
        @test-flaresolverr="testFlaresolverr"
      />

      <!-- General tab -->
      <SettingsGeneral
        v-if="activeTab === 'general'"
        v-model:watched-list-mode="watchedListMode"
        v-model:discord-url="discordUrl"
        v-model:show-discord-url="showDiscordUrl"
        v-model:season-pack-pref="seasonPackPref"
        v-model:monitor-interval="monitorInterval"
        v-model:download-interval="downloadInterval"
        v-model:importer-interval="importerInterval"
        v-model:metadata-refresh-interval="metadataRefreshInterval"
        v-model:update-check-interval="updateCheckInterval"
        v-model:otel-enabled="otelEnabled"
        v-model:otel-endpoint="otelEndpoint"
        v-model:otel-service="otelService"
        v-model:otel-log-level="otelLogLevel"
        :discord-testing="discordTesting"
        :discord-test="discordTest"
        :saving="saving"
        @dirty="markDirty"
        @test-discord="testDiscord"
        @disconnect-discord="disconnectDiscord"
      />

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
