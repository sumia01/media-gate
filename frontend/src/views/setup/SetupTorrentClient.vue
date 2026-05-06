<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'
import FolderBrowser from '@/components/FolderBrowser.vue'

const emit = defineEmits<{ next: []; back: [] }>()

const torrentClient = ref('qbittorrent')
const qbUrl = ref('')
const qbUsername = ref('')
const qbPassword = ref('')
const qbDownloadPath = ref('')
const qbSavePath = ref('')
const qbCategory = ref('')

const error = ref('')
const saving = ref(false)
const testResult = ref<{ success: boolean; message?: string } | null>(null)
const testing = ref(false)

onMounted(async () => {
  try {
    const { data } = await client.GET('/settings')
    const s = data?.settings
    if (s) {
      qbUrl.value = s.qbitUrl ?? ''
      qbUsername.value = s.qbitUsername ?? ''
      qbDownloadPath.value = s.qbitDownloadPath ?? ''
      qbSavePath.value = s.qbitSavePath ?? ''
      qbCategory.value = s.qbitCategory ?? ''
    }
  } catch {
    // Use defaults
  }
})

async function testConnection() {
  testResult.value = null
  testing.value = true
  try {
    const { data } = await client.POST('/settings/test-qbittorrent', {
      body: {
        url: qbUrl.value || undefined,
        username: qbUsername.value || undefined,
        password: qbPassword.value || undefined,
      },
    })
    testResult.value = data ?? null
  } catch {
    testResult.value = { success: false, message: 'Test request failed' }
  } finally {
    testing.value = false
  }
}

async function handleSubmit() {
  if (!qbUrl.value.trim()) {
    error.value = 'qBittorrent URL is required'
    return
  }

  error.value = ''
  saving.value = true
  try {
    const body: Record<string, any> = {
      qbitUrl: qbUrl.value.trim(),
    }
    if (qbUsername.value) body.qbitUsername = qbUsername.value
    if (qbPassword.value) body.qbitPassword = qbPassword.value
    if (qbDownloadPath.value) body.qbitDownloadPath = qbDownloadPath.value
    if (qbSavePath.value) body.qbitSavePath = qbSavePath.value
    if (qbCategory.value) body.qbitCategory = qbCategory.value

    const { error: err } = await client.PUT('/settings', { body })
    if (err) {
      error.value = 'Failed to save torrent client settings'
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
      <h2 class="text-lg font-semibold text-gray-200">Torrent Client</h2>
      <p class="text-sm text-gray-500 mt-1">
        MediaGate uses a torrent client to download media. Configure the connection to your client below.
      </p>
    </div>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ error }}
      </div>

      <!-- Client selection -->
      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Client</label>
        <select
          v-model="torrentClient"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm focus:outline-none focus:border-violet-500/50 transition-colors"
        >
          <option value="qbittorrent">qBittorrent</option>
        </select>
      </div>

      <!-- qBittorrent settings -->
      <div class="space-y-3 pt-2 border-t border-violet-900/20">
        <div>
          <label for="qb-url" class="block text-sm font-medium text-gray-400 mb-1.5">URL</label>
          <input
            id="qb-url"
            v-model="qbUrl"
            type="text"
            required
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm font-mono placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="http://localhost:8080"
          />
        </div>

        <div class="grid grid-cols-2 gap-3">
          <div>
            <label for="qb-user" class="block text-sm font-medium text-gray-400 mb-1.5">Username</label>
            <input
              id="qb-user"
              v-model="qbUsername"
              type="text"
              class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
              placeholder="admin"
            />
          </div>
          <div>
            <label for="qb-pass" class="block text-sm font-medium text-gray-400 mb-1.5">Password</label>
            <input
              id="qb-pass"
              v-model="qbPassword"
              type="password"
              class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
              placeholder="Password"
            />
          </div>
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">Download Path</label>
          <FolderBrowser v-model="qbDownloadPath" />
          <p class="text-xs text-gray-500 mt-1">Where torrent files are downloaded before import. Must be within the library base path.</p>
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-400 mb-1.5">qBittorrent Save Path <span class="text-gray-600 font-normal">(Optional)</span></label>
          <input
            v-model="qbSavePath"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="/mnt/nas/downloads"
          />
          <p class="text-xs text-gray-500 mt-1">If your qBittorrent's NAS mount path differs from MediaGate's, enter the absolute path on the qBittorrent host.</p>
        </div>

        <div>
          <label for="qb-category" class="block text-sm font-medium text-gray-400 mb-1.5">Category / Tag</label>
          <input
            id="qb-category"
            v-model="qbCategory"
            type="text"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
            placeholder="media-gate"
          />
          <p class="text-xs text-gray-500 mt-1">Optional. Torrents added by MediaGate will use this category.</p>
        </div>
      </div>

      <!-- Test -->
      <div class="flex items-center gap-3">
        <button
          type="button"
          :disabled="testing || !qbUrl"
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
          :disabled="saving || !qbUrl.trim()"
          class="flex-1 py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
        >
          {{ saving ? 'Saving...' : 'Continue' }}
        </button>
      </div>
    </form>
  </div>
</template>
