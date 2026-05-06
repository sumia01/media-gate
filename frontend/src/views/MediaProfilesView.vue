<script setup lang="ts">
import { onMounted, ref } from 'vue'
import client from '@/api/client'
import type { MediaProfile, MediaProfileCreate } from '@/types/api'

const profiles = ref<MediaProfile[]>([])
const loading = ref(false)
const error = ref('')

const showForm = ref(false)
const editing = ref<MediaProfile | null>(null)
const form = ref<MediaProfileCreate>({ name: '', resolutions: [], languages: [], languageMode: 'or' })
const excludeTagsInput = ref('')
const testingProfile = ref<MediaProfile | null>(null)

const globalExcludeTags = ref<string[]>([])
const showGlobalTagsModal = ref(false)
const globalTagsInput = ref('')
const globalTagsSaving = ref(false)

const resolutionOptions = ['2160p', '1080p', '720p', '480p']
const sourceOptions = ['webdl', 'webrip', 'bluray', 'hdtv', 'dvd']
const languageOptions = ['hun', 'eng', 'ger', 'fre', 'spa', 'ita', 'jpn', 'kor']

async function fetchProfiles() {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/media-profiles')
  loading.value = false
  if (err) {
    error.value = 'Failed to load media profiles'
    return
  }
  profiles.value = data?.profiles ?? []
}

function openAdd() {
  editing.value = null
  form.value = { name: '', resolutions: [], languages: [], languageMode: 'or' }
  excludeTagsInput.value = ''
  showForm.value = true
}

function openEdit(profile: MediaProfile) {
  editing.value = profile
  form.value = {
    name: profile.name,
    resolutions: [...profile.resolutions],
    languages: [...profile.languages],
    languageMode: profile.languageMode ?? 'or',
    sources: profile.sources ? [...profile.sources] : undefined,
    excludeTags: profile.excludeTags ? [...profile.excludeTags] : undefined,
  }
  excludeTagsInput.value = profile.excludeTags?.join(', ') ?? ''
  showForm.value = true
}

function cancelForm() {
  showForm.value = false
  editing.value = null
  error.value = ''
}

function toggleResolution(res: string) {
  const idx = form.value.resolutions.indexOf(res)
  if (idx >= 0) {
    form.value.resolutions.splice(idx, 1)
  } else {
    form.value.resolutions.push(res)
  }
}

function toggleSource(src: string) {
  if (!form.value.sources) form.value.sources = []
  const idx = form.value.sources.indexOf(src)
  if (idx >= 0) {
    form.value.sources.splice(idx, 1)
    if (form.value.sources.length === 0) form.value.sources = undefined
  } else {
    form.value.sources.push(src)
  }
}

function toggleLanguage(lang: string) {
  const idx = form.value.languages.indexOf(lang)
  if (idx >= 0) {
    form.value.languages.splice(idx, 1)
  } else {
    form.value.languages.push(lang)
  }
}

function languagePriority(lang: string): number {
  return form.value.languages.indexOf(lang) + 1
}

function resolutionPriority(res: string): number {
  return form.value.resolutions.indexOf(res) + 1
}

function sourcePriority(src: string): number {
  return (form.value.sources?.indexOf(src) ?? -1) + 1
}

async function submitForm() {
  error.value = ''

  const body: MediaProfileCreate = {
    name: form.value.name,
    resolutions: form.value.resolutions,
    languages: form.value.languages,
    languageMode: form.value.languageMode ?? 'or',
  }
  if (form.value.sources?.length) {
    body.sources = form.value.sources
  }
  const tags = excludeTagsInput.value
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
  if (tags.length) {
    body.excludeTags = tags
  }

  if (editing.value) {
    const { error: err } = await client.PUT('/media-profiles/{id}', {
      params: { path: { id: editing.value.id } },
      body,
    })
    if (err) {
      error.value = 'Failed to update profile'
      return
    }
  } else {
    const { error: err } = await client.POST('/media-profiles', { body })
    if (err) {
      error.value = 'Failed to create profile'
      return
    }
  }
  showForm.value = false
  editing.value = null
  await fetchProfiles()
}

async function deleteProfile(profile: MediaProfile) {
  if (!confirm(`Delete profile "${profile.name}"?`)) return
  const { error: err } = await client.DELETE('/media-profiles/{id}', {
    params: { path: { id: profile.id } },
  })
  if (err) {
    error.value = 'Failed to delete profile'
    return
  }
  await fetchProfiles()
}

async function fetchGlobalTags() {
  const { data } = await client.GET('/settings')
  globalExcludeTags.value = data?.settings?.globalExcludeTags ?? []
}

function openGlobalTags() {
  globalTagsInput.value = globalExcludeTags.value.join(', ')
  showGlobalTagsModal.value = true
}

async function saveGlobalTags() {
  globalTagsSaving.value = true
  error.value = ''
  const tags = globalTagsInput.value
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
  const { data, error: err } = await client.PUT('/settings', {
    body: { globalExcludeTags: tags },
  })
  globalTagsSaving.value = false
  if (err) {
    error.value = 'Failed to save global exclude tags'
    return
  }
  globalExcludeTags.value = data?.settings?.globalExcludeTags ?? tags
  showGlobalTagsModal.value = false
}

onMounted(() => {
  fetchProfiles()
  fetchGlobalTags()
})
</script>

<template>
  <div>
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">Media Profiles</h1>
      <div class="flex items-center gap-3">
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg border border-red-600/30 hover:bg-red-600/10 text-red-300 text-sm font-medium transition-colors duration-200"
          @click="openGlobalTags"
        >
          Global Exclude Tags
        </button>
        <button
          class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
          @click="openAdd"
        >
          <span class="text-lg leading-none">+</span>
          Add Profile
        </button>
      </div>
    </div>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <!-- Empty state -->
    <div
      v-else-if="!profiles.length && !showForm"
      class="flex flex-col items-center justify-center py-20 text-gray-500"
    >
      <span class="text-4xl mb-3">&#9632;</span>
      <p class="text-sm">No media profiles yet. Add one to get started.</p>
    </div>

    <!-- Profile list -->
    <div v-else class="space-y-3">
      <div
        v-for="profile in profiles"
        :key="profile.id"
        class="flex items-start gap-4 px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20"
      >
        <!-- Info -->
        <div class="flex-1 min-w-0">
          <p class="text-sm font-semibold text-gray-200 mb-2">{{ profile.name }}</p>
          <div class="flex flex-wrap gap-1.5">
            <!-- Language pills (numbered, green) -->
            <span
              v-for="(lang, idx) in profile.languages"
              :key="'lang-' + lang"
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-emerald-600/20 text-emerald-300"
            >
              <template v-if="profile.languageMode === 'or'">{{ idx + 1 }}. </template>{{ lang }}
              <template v-if="idx < profile.languages.length - 1">
                <span class="text-emerald-500/60 ml-0.5">{{ profile.languageMode === 'and' ? '+' : '/' }}</span>
              </template>
            </span>
            <!-- Resolution pills (numbered, violet) -->
            <span
              v-for="(res, idx) in profile.resolutions"
              :key="res"
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300"
            >
              {{ idx + 1 }}. {{ res }}
            </span>
            <!-- Source pills (numbered, sky) -->
            <span
              v-for="(src, idx) in profile.sources ?? []"
              :key="src"
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-sky-600/20 text-sky-300"
            >
              {{ idx + 1 }}. {{ src }}
            </span>
            <!-- Exclude tag pills -->
            <span
              v-for="tag in profile.excludeTags ?? []"
              :key="tag"
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-red-600/20 text-red-300"
            >
              {{ tag }}
            </span>
            <!-- Global exclude tag pills (muted, shown for context) -->
            <template v-if="globalExcludeTags.length">
              <span class="text-[10px] text-gray-600 px-0.5">(</span>
              <span
                v-for="tag in globalExcludeTags"
                :key="'gtag-' + tag"
                class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-red-600/10 text-red-400/50"
              >
                {{ tag }}
              </span>
              <span class="text-[10px] text-gray-600 px-0.5">)</span>
            </template>
          </div>
        </div>

        <!-- Actions -->
        <div class="flex items-center gap-1 flex-shrink-0">
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-emerald-300 hover:bg-emerald-600/10 transition-colors duration-200"
            @click="testingProfile = profile"
          >
            Test
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
            @click="openEdit(profile)"
          >
            Edit
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-red-400 hover:bg-red-600/10 transition-colors duration-200"
            @click="deleteProfile(profile)"
          >
            Delete
          </button>
        </div>
      </div>
    </div>

    <!-- Add/Edit modal -->
    <BaseModal
      v-if="showForm"
      max-width="max-w-xl"
      @close="cancelForm"
    >
      <h2 class="text-lg font-semibold text-gray-100 mb-5">
        {{ editing ? 'Edit Profile' : 'Add Profile' }}
      </h2>

      <form class="space-y-4" @submit.prevent="submitForm">
        <!-- Name -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Name</label>
          <input
            v-model="form.name"
            type="text"
            required
            placeholder="e.g. HUN 4K"
            class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>

        <!-- Languages -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Languages <span class="text-gray-600">(click order = priority in OR mode)</span></label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="lang in languageOptions"
              :key="lang"
              type="button"
              class="px-3 py-1.5 rounded-lg border text-sm font-medium transition-colors duration-200 relative"
              :class="form.languages.includes(lang)
                ? 'bg-emerald-600/20 border-emerald-500/50 text-emerald-300'
                : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              @click="toggleLanguage(lang)"
            >
              <span v-if="form.languages.includes(lang) && form.languageMode === 'or'" class="text-[10px] mr-1 opacity-70">{{ languagePriority(lang) }}.</span>
              {{ lang.toUpperCase() }}
            </button>
          </div>
          <!-- Language mode toggle -->
          <div v-if="form.languages.length > 0" class="mt-2 flex items-center gap-2">
            <span class="text-xs text-gray-500">Mode:</span>
            <button
              type="button"
              class="px-2.5 py-1 rounded-md border text-xs font-medium transition-colors duration-200"
              :class="form.languageMode === 'or'
                ? 'bg-emerald-600/20 border-emerald-500/50 text-emerald-300'
                : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              @click="form.languageMode = 'or'"
            >
              OR
            </button>
            <button
              type="button"
              class="px-2.5 py-1 rounded-md border text-xs font-medium transition-colors duration-200"
              :class="form.languageMode === 'and'
                ? 'bg-emerald-600/20 border-emerald-500/50 text-emerald-300'
                : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              @click="form.languageMode = 'and'"
            >
              AND
            </button>
            <span class="text-[10px] text-gray-600 ml-1">
              {{ form.languageMode === 'and' ? 'Release must contain ALL selected languages' : 'Release must contain at least ONE (order = preference)' }}
            </span>
          </div>
        </div>

        <!-- Resolutions -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Resolutions <span class="text-gray-600">(click order = priority)</span></label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="res in resolutionOptions"
              :key="res"
              type="button"
              class="px-3 py-1.5 rounded-lg border text-sm font-medium transition-colors duration-200"
              :class="form.resolutions.includes(res)
                ? 'bg-violet-600/20 border-violet-500/50 text-violet-300'
                : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              @click="toggleResolution(res)"
            >
              <span v-if="form.resolutions.includes(res)" class="text-[10px] mr-1 opacity-70">{{ resolutionPriority(res) }}.</span>
              {{ res }}
            </button>
          </div>
        </div>

        <!-- Sources -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Sources <span class="text-gray-600">(optional, click order = priority)</span></label>
          <div class="flex flex-wrap gap-2">
            <button
              v-for="src in sourceOptions"
              :key="src"
              type="button"
              class="px-3 py-1.5 rounded-lg border text-sm font-medium transition-colors duration-200"
              :class="form.sources?.includes(src)
                ? 'bg-sky-600/20 border-sky-500/50 text-sky-300'
                : 'bg-[#161b2e] border-violet-800/30 text-gray-500 hover:text-gray-300'"
              @click="toggleSource(src)"
            >
              <span v-if="form.sources?.includes(src)" class="text-[10px] mr-1 opacity-70">{{ sourcePriority(src) }}.</span>
              {{ src }}
            </button>
          </div>
        </div>

        <!-- Exclude tags -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Exclude Tags <span class="text-gray-600">(comma-separated, optional)</span></label>
          <input
            v-model="excludeTagsInput"
            type="text"
            placeholder="e.g. 3d, cam, ts"
            class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
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

    <!-- Test profile modal -->
    <TestProfileModal
      v-if="testingProfile"
      :profile-id="testingProfile.id"
      :profile-name="testingProfile.name"
      @close="testingProfile = null"
    />

    <!-- Global exclude tags modal -->
    <BaseModal
      v-if="showGlobalTagsModal"
      max-width="max-w-md"
      @close="showGlobalTagsModal = false"
    >
      <h2 class="text-lg font-semibold text-gray-100 mb-2">Global Exclude Tags</h2>
      <p class="text-xs text-gray-500 mb-4">
        These tags are excluded from ALL profile-based searches (monitor and test-search).
        They are merged with each profile's own exclude tags.
      </p>
      <form @submit.prevent="saveGlobalTags">
        <input
          v-model="globalTagsInput"
          type="text"
          placeholder="e.g. 3d, dolby vision, cam"
          class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
        />
        <div class="flex justify-end gap-3 pt-4">
          <button
            type="button"
            class="px-4 py-2 rounded-lg text-sm text-gray-400 hover:text-gray-200 transition-colors duration-200"
            @click="showGlobalTagsModal = false"
          >
            Cancel
          </button>
          <button
            type="submit"
            :disabled="globalTagsSaving"
            class="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200 disabled:opacity-50"
          >
            {{ globalTagsSaving ? 'Saving...' : 'Save' }}
          </button>
        </div>
      </form>
    </BaseModal>
  </div>
</template>
