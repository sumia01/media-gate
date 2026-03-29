<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useGlobalSearch } from '@/composables/useGlobalSearch'

type Library = components['schemas']['Library']

const props = defineProps<{
  source: string
  externalId: number
  mediaType: string
}>()

const emit = defineEmits<{
  added: [mediaItemId: number]
  close: []
}>()

const router = useRouter()
const { activeLibraryId } = useGlobalSearch()

const libraries = ref<Library[]>([])
const selectedLibraryId = ref<number | null>(null)
const adding = ref(false)
const error = ref('')
const loadingLibraries = ref(false)

const compatibleLibraries = computed(() =>
  libraries.value.filter((lib) => lib.mediaType === props.mediaType),
)

onMounted(async () => {
  loadingLibraries.value = true
  const { data } = await client.GET('/libraries')
  loadingLibraries.value = false
  libraries.value = data ?? []

  // Pre-select active library if compatible
  if (activeLibraryId.value) {
    const match = compatibleLibraries.value.find((lib) => lib.id === activeLibraryId.value)
    if (match) {
      selectedLibraryId.value = match.id
    }
  }

  // Auto-select if there's only one compatible library
  if (!selectedLibraryId.value && compatibleLibraries.value.length === 1) {
    selectedLibraryId.value = compatibleLibraries.value[0]!.id
  }
})

async function handleAdd() {
  if (!selectedLibraryId.value) return

  adding.value = true
  error.value = ''

  const { data, error: err } = await client.POST('/libraries/{id}/media', {
    params: { path: { id: selectedLibraryId.value } },
    body: {
      source: props.source as 'tmdb' | 'tvdb',
      externalId: props.externalId,
    },
  })

  adding.value = false

  if (err) {
    // Check for 409 conflict
    const errBody = err as { code?: number; message?: string }
    if (errBody.code === 409) {
      error.value = 'This media already exists in the selected library'
    } else {
      error.value = 'Failed to add media'
    }
    return
  }

  if (data) {
    emit('added', data.id)
  }
}
</script>

<template>
  <div class="fixed inset-0 z-50 flex items-center justify-center">
    <div class="absolute inset-0 bg-black/60" @click="emit('close')"></div>
    <div class="relative bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 w-full max-w-sm shadow-2xl">
      <h3 class="text-base font-semibold text-gray-100 mb-4">Add to Library</h3>

      <!-- Loading -->
      <div v-if="loadingLibraries" class="py-6 text-center text-gray-500 text-sm">
        Loading libraries...
      </div>

      <!-- No compatible libraries -->
      <div v-else-if="!compatibleLibraries.length" class="py-6 text-center text-gray-500 text-sm">
        No {{ mediaType }} libraries found. Create one first.
      </div>

      <!-- Library list -->
      <div v-else class="space-y-2 mb-4">
        <button
          v-for="lib in compatibleLibraries"
          :key="lib.id"
          class="w-full text-left px-4 py-3 rounded-lg border transition-colors duration-200"
          :class="selectedLibraryId === lib.id
            ? 'bg-violet-600/10 border-violet-500/40'
            : 'bg-[#161b2e] border-violet-900/20 hover:border-violet-500/30'"
          @click="selectedLibraryId = lib.id"
        >
          <div class="flex items-center justify-between">
            <div>
              <p class="text-sm font-medium text-gray-200">{{ lib.name }}</p>
              <p class="text-xs text-gray-500 mt-0.5 font-mono">{{ lib.path }}</p>
            </div>
            <div
              v-if="selectedLibraryId === lib.id"
              class="w-4 h-4 rounded-full bg-violet-600 flex items-center justify-center flex-shrink-0"
            >
              <span class="text-white text-xs">&#10003;</span>
            </div>
          </div>
        </button>
      </div>

      <!-- Error -->
      <div
        v-if="error"
        class="mb-3 px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-xs"
      >
        {{ error }}
      </div>

      <!-- Actions -->
      <div class="flex gap-3">
        <button
          class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50 disabled:cursor-not-allowed"
          :disabled="!selectedLibraryId || adding"
          @click="handleAdd"
        >
          {{ adding ? 'Adding...' : 'Add' }}
        </button>
        <button
          class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
          @click="emit('close')"
        >
          Cancel
        </button>
      </div>
    </div>
  </div>
</template>
