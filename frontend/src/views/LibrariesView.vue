<script setup lang="ts">
import { Plus } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import client from '@/api/client'
import BaseModal from '@/components/BaseModal.vue'
import ErrorBanner from '@/components/ErrorBanner.vue'
import FolderBrowser from '@/components/FolderBrowser.vue'
import LibrariesMediaServer from '@/components/libraries/LibrariesMediaServer.vue'
import { useSidebarLibraries } from '@/composables/useSidebarLibraries'
import type { Library, LibraryCreate, MediaProfile, ScanEntry } from '@/types/api'

const { refreshLibraries: refreshSidebar } = useSidebarLibraries()

const activeTab = ref<'libraries' | 'media-server'>('libraries')

const tabs = [
  { key: 'libraries' as const, label: 'Libraries' },
  { key: 'media-server' as const, label: 'Media Server' },
]

const libraries = ref<Library[]>([])
const profiles = ref<MediaProfile[]>([])
const loading = ref(false)
const error = ref('')

const showForm = ref(false)
const editing = ref<Library | null>(null)
const form = ref<LibraryCreate>({ name: '', path: '', mediaType: 'movie' })

const scanning = ref<number | null>(null)
const scanEntries = ref<ScanEntry[]>([])
const scanError = ref('')
const formProfileId = ref<number | null>(null)

async function fetchLibraries() {
  loading.value = true
  error.value = ''
  const [libRes, profileRes] = await Promise.all([client.GET('/libraries'), client.GET('/media-profiles')])
  loading.value = false
  if (libRes.error) {
    error.value = 'Failed to load libraries'
    return
  }
  libraries.value = libRes.data ?? []
  profiles.value = profileRes.data?.profiles ?? []
}

function openAdd() {
  editing.value = null
  form.value = { name: '', path: '', mediaType: 'movie' }
  formProfileId.value = null
  showForm.value = true
}

function openEdit(lib: Library) {
  editing.value = lib
  form.value = { name: lib.name, path: lib.path, mediaType: lib.mediaType }
  formProfileId.value = lib.mediaProfileId ?? null
  showForm.value = true
}

function cancelForm() {
  showForm.value = false
  editing.value = null
  error.value = ''
}

async function submitForm() {
  error.value = ''
  const body: LibraryCreate & { mediaProfileId?: number } = { ...form.value }
  if (formProfileId.value) {
    body.mediaProfileId = formProfileId.value
  }
  if (editing.value) {
    const { error: err } = await client.PUT('/libraries/{id}', {
      params: { path: { id: editing.value.id } },
      body,
    })
    if (err) {
      error.value = 'Failed to update library'
      return
    }
  } else {
    const { error: err } = await client.POST('/libraries', {
      body,
    })
    if (err) {
      error.value = 'Failed to create library'
      return
    }
  }
  showForm.value = false
  editing.value = null
  await fetchLibraries()
  refreshSidebar()
}

async function deleteLibrary(lib: Library) {
  if (!confirm(`Delete library "${lib.name}"?`)) return
  const { error: err } = await client.DELETE('/libraries/{id}', {
    params: { path: { id: lib.id } },
  })
  if (err) {
    error.value = 'Failed to delete library'
    return
  }
  await fetchLibraries()
  refreshSidebar()
}

async function scanLibrary(lib: Library) {
  if (scanning.value === lib.id) {
    scanning.value = null
    return
  }
  scanning.value = lib.id
  scanEntries.value = []
  scanError.value = ''
  const { data, error: err } = await client.GET('/libraries/{id}/scan', {
    params: { path: { id: lib.id } },
  })
  if (err) {
    scanError.value = 'Failed to scan library'
    return
  }
  scanEntries.value = data?.entries ?? []
}

onMounted(fetchLibraries)
</script>

<template>
  <div>
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">Libraries</h1>
      <button
        v-if="activeTab === 'libraries'"
        class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
        @click="openAdd"
      >
        <Plus class="w-4 h-4" />
        Add Library
      </button>
    </div>

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

    <!-- Libraries tab -->
    <template v-if="activeTab === 'libraries'">
      <ErrorBanner :message="error" />

      <!-- Loading -->
      <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

      <!-- Empty state -->
      <div
        v-else-if="!libraries.length && !showForm"
        class="flex flex-col items-center justify-center py-20 text-gray-500"
      >
        <span class="text-4xl mb-3">&#128218;</span>
        <p class="text-sm">No libraries yet. Add one to get started.</p>
      </div>

      <!-- Library list -->
      <div v-else class="space-y-3">
        <div
          v-for="lib in libraries"
          :key="lib.id"
        >
          <div class="flex items-center gap-4 px-4 py-3 rounded-lg bg-[#161b2e] border border-violet-900/20 group"
            :class="scanning === lib.id ? 'rounded-b-none' : ''"
          >
            <!-- Type badge -->
            <span
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full flex-shrink-0"
              :class="lib.mediaType === 'movie'
                ? 'bg-violet-600/20 text-violet-300'
                : 'bg-fuchsia-600/20 text-fuchsia-300'"
            >
              {{ lib.mediaType }}
            </span>

            <!-- Info -->
            <div class="flex-1 min-w-0">
              <p class="text-sm font-medium text-gray-200 truncate">{{ lib.name }}</p>
              <div class="flex items-center gap-2">
                <p class="text-xs text-gray-500 truncate font-mono">{{ lib.path }}</p>
                <span
                  v-if="lib.mediaProfileId && profiles.find(p => p.id === lib.mediaProfileId)"
                  class="text-[10px] px-1.5 py-0.5 rounded bg-violet-600/15 text-violet-400 flex-shrink-0"
                >
                  {{ profiles.find(p => p.id === lib.mediaProfileId)?.name }}
                </span>
              </div>
            </div>

            <!-- Actions -->
            <div class="flex items-center gap-1">
              <button
                class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
                @click="scanLibrary(lib)"
              >
                {{ scanning === lib.id ? 'Close' : 'View content' }}
              </button>
              <button
                class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
                @click="openEdit(lib)"
              >
                Edit
              </button>
              <button
                class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-red-400 hover:bg-red-600/10 transition-colors duration-200"
                @click="deleteLibrary(lib)"
              >
                Delete
              </button>
            </div>
          </div>

          <!-- Scan results -->
          <div
            v-if="scanning === lib.id"
            class="border border-t-0 border-violet-900/20 rounded-b-lg bg-[#111827] px-4 py-3"
          >
            <div v-if="scanError" class="text-red-400 text-xs">{{ scanError }}</div>
            <div v-else-if="!scanEntries.length" class="text-gray-500 text-xs">Scanning...</div>
            <div v-else class="max-h-64 overflow-y-auto scrollbar-none">
              <table class="w-full text-xs">
                <thead>
                  <tr class="text-gray-500 border-b border-violet-900/10">
                    <th class="text-left py-1.5 font-medium">Name</th>
                    <th class="text-right py-1.5 font-medium w-36">Modified</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="entry in scanEntries"
                    :key="entry.path"
                    class="border-b border-violet-900/5 text-gray-300"
                  >
                    <td class="py-1.5 truncate max-w-0">
                      <span class="mr-1.5 text-gray-500">{{ entry.isDirectory ? '&#128193;' : '&#128196;' }}</span>
                      {{ entry.name }}
                    </td>
                    <td class="py-1.5 text-right text-gray-500">
                      {{ new Date(entry.modifiedAt).toLocaleDateString() }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </div>
      </div>

      <!-- Add/Edit form modal -->
      <BaseModal
        v-if="showForm"
        max-width="max-w-xl"
        @close="cancelForm"
      >
        <h2 class="text-lg font-semibold text-gray-100 mb-5">
          {{ editing ? 'Edit Library' : 'Add Library' }}
        </h2>

        <form class="space-y-4" @submit.prevent="submitForm">
          <!-- Name -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Name</label>
            <input
              v-model="form.name"
              type="text"
              required
              placeholder="e.g. Movies"
              class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            />
          </div>

          <!-- Path -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Path</label>
            <FolderBrowser v-model="form.path" />
          </div>

          <!-- Media type -->
          <div>
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Media Type</label>
            <div class="flex gap-3">
              <label
                class="flex-1 flex items-center justify-center gap-2 px-3 py-2 rounded-lg border text-sm font-medium cursor-pointer transition-colors duration-200"
                :class="form.mediaType === 'movie'
                  ? 'bg-violet-600/20 border-violet-500/50 text-violet-300'
                  : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              >
                <input v-model="form.mediaType" type="radio" value="movie" class="sr-only" />
                <span>Movie</span>
              </label>
              <label
                class="flex-1 flex items-center justify-center gap-2 px-3 py-2 rounded-lg border text-sm font-medium cursor-pointer transition-colors duration-200"
                :class="form.mediaType === 'series'
                  ? 'bg-fuchsia-600/20 border-fuchsia-500/50 text-fuchsia-300'
                  : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              >
                <input v-model="form.mediaType" type="radio" value="series" class="sr-only" />
                <span>Series</span>
              </label>
            </div>
          </div>

          <!-- Default quality profile -->
          <div v-if="profiles.length">
            <label class="block text-xs font-medium text-gray-400 mb-1.5">Default Quality Profile</label>
            <select
              class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
              :value="formProfileId ?? ''"
              @change="formProfileId = ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null"
            >
              <option value="">None</option>
              <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
            </select>
            <p class="text-[11px] text-gray-500 mt-1">Pre-selected when adding monitored media to this library</p>
          </div>

          <!-- Buttons -->
          <div class="flex justify-end gap-3 pt-2">
            <button
              type="button"
              class="px-4 py-2 rounded-lg text-sm text-gray-400 hover:text-gray-200 transition-colors duration-200"
              @click="cancelForm"
            >
              Cancel
            </button>
            <button
              type="submit"
              class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
            >
              {{ editing ? 'Save' : 'Add' }}
            </button>
          </div>
        </form>
      </BaseModal>
    </template>

    <!-- Media Server tab -->
    <LibrariesMediaServer v-if="activeTab === 'media-server'" />
  </div>
</template>
