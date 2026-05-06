<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'

const emit = defineEmits<{ next: []; back: [] }>()

const basePath = ref('/mnt')
const fromEnv = ref(false)
const error = ref('')
const loading = ref(false)
const saving = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    const { data } = await client.GET('/settings')
    if (data?.settings?.libraryBasePath) {
      basePath.value = data.settings.libraryBasePath
    }
    fromEnv.value = data?.settings?.libraryBasePathFromEnv ?? false
  } catch {
    // Use default
  } finally {
    loading.value = false
  }
})

async function handleSubmit() {
  if (!basePath.value.trim()) {
    error.value = 'Base path is required'
    return
  }
  if (!basePath.value.startsWith('/') && !/^[a-zA-Z]:[/\\]/.test(basePath.value)) {
    error.value = 'Base path must be an absolute path'
    return
  }

  error.value = ''
  saving.value = true
  try {
    const { error: err } = await client.PUT('/settings', {
      body: { libraryBasePath: basePath.value.trim() },
    })
    if (err) {
      error.value = 'Failed to save base path'
      return
    }
    emit('next')
  } catch {
    error.value = 'Failed to save base path'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-gray-200">Library Base Path</h2>
      <p class="text-sm text-gray-500 mt-1">
        This is the root directory where all your media libraries will live. All library folders and the
        download directory must be inside this path. MediaGate cannot access files outside of it.
      </p>
    </div>

    <form class="space-y-4" @submit.prevent="handleSubmit">
      <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ error }}
      </div>

      <div>
        <label for="base-path" class="block text-sm font-medium text-gray-400 mb-1.5">Base Path</label>
        <input
          id="base-path"
          v-model="basePath"
          type="text"
          required
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm font-mono placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          placeholder="/mnt/media"
        />
        <p v-if="fromEnv" class="text-xs text-gray-500 mt-1">
          Pre-filled from environment variable (MEDIAGATE_LIBRARY_BASEPATH)
        </p>
        <p class="text-xs text-gray-500 mt-1">
          Example: <span class="font-mono text-gray-400">/mnt/media</span>, <span class="font-mono text-gray-400">/data</span>, or <span class="font-mono text-gray-400">D:\media</span>
        </p>
      </div>

      <div class="flex gap-3">
        <button
          type="button"
          class="px-4 py-2.5 rounded-lg border border-violet-800/30 text-gray-400 hover:text-gray-200 text-sm font-medium transition-colors"
          @click="emit('back')"
        >
          Back
        </button>
        <button
          type="submit"
          :disabled="saving || !basePath.trim()"
          class="flex-1 py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
        >
          {{ saving ? 'Saving...' : 'Continue' }}
        </button>
      </div>
    </form>
  </div>
</template>
