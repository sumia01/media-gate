<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import client from '@/api/client'
import type { Indexer, IndexerDefinition, IndexerDefinitionSetting } from '@/types/api'
import ErrorBanner from '@/components/ErrorBanner.vue'
import BaseModal from '@/components/BaseModal.vue'
import IndexerTryModal from '@/components/media/IndexerTryModal.vue'

const indexers = ref<Indexer[]>([])
const definitions = ref<IndexerDefinition[]>([])
const loading = ref(false)
const error = ref('')

const showForm = ref(false)
const editing = ref<Indexer | null>(null)
const formName = ref('')
const formDefinitionId = ref('')
const formEnabled = ref(true)
const formPriority = ref(0)
const formSettings = ref<Record<string, string>>({})

const testingId = ref<number | null>(null)
const testResult = ref<{ success: boolean; message: string } | null>(null)
const tryingIndexer = ref<Indexer | null>(null)

const selectedDefinition = computed(() =>
  definitions.value.find((d) => d.id === formDefinitionId.value),
)

function resetForm() {
  formName.value = ''
  formDefinitionId.value = ''
  formEnabled.value = true
  formPriority.value = 0
  formSettings.value = {}
  testResult.value = null
}

function applyDefinitionDefaults(def: IndexerDefinition) {
  const settings: Record<string, string> = {}
  for (const s of def.settings ?? []) {
    settings[s.name] = s.default ?? ''
  }
  formSettings.value = settings
}

function onDefinitionChange() {
  const def = selectedDefinition.value
  if (def) {
    if (!formName.value) formName.value = def.name
    applyDefinitionDefaults(def)
  }
}

function isMasked(value: string) {
  return value.startsWith('****')
}

async function fetchAll() {
  loading.value = true
  error.value = ''
  const [indexerRes, defRes] = await Promise.all([
    client.GET('/indexers'),
    client.GET('/indexer-definitions'),
  ])
  loading.value = false
  if (indexerRes.error) {
    error.value = 'Failed to load indexers'
    return
  }
  if (defRes.error) {
    error.value = 'Failed to load indexer definitions'
    return
  }
  indexers.value = indexerRes.data?.indexers ?? []
  definitions.value = defRes.data?.definitions ?? []
}

function openAdd() {
  editing.value = null
  resetForm()
  showForm.value = true
}

function openEdit(indexer: Indexer) {
  editing.value = indexer
  formName.value = indexer.name
  formDefinitionId.value = indexer.definitionId
  formEnabled.value = indexer.enabled
  formPriority.value = indexer.priority
  formSettings.value = { ...(indexer.settings ?? {}) }
  testResult.value = null
  showForm.value = true
}

function cancelForm() {
  showForm.value = false
  editing.value = null
  error.value = ''
}

async function submitForm() {
  error.value = ''

  const settings: Record<string, string> = {}
  for (const [k, v] of Object.entries(formSettings.value)) {
    if (!isMasked(v)) {
      settings[k] = v
    }
  }

  if (editing.value) {
    const { error: err } = await client.PUT('/indexers/{id}', {
      params: { path: { id: editing.value.id } },
      body: {
        name: formName.value,
        settings,
        enabled: formEnabled.value,
        priority: formPriority.value,
      },
    })
    if (err) {
      error.value = 'Failed to update indexer'
      return
    }
  } else {
    const { error: err } = await client.POST('/indexers', {
      body: {
        name: formName.value,
        definitionId: formDefinitionId.value,
        settings,
        enabled: formEnabled.value,
        priority: formPriority.value,
      },
    })
    if (err) {
      error.value = 'Failed to create indexer'
      return
    }
  }
  showForm.value = false
  editing.value = null
  await fetchAll()
}

async function deleteIndexer(indexer: Indexer) {
  if (!confirm(`Delete indexer "${indexer.name}"?`)) return
  const { error: err } = await client.DELETE('/indexers/{id}', {
    params: { path: { id: indexer.id } },
  })
  if (err) {
    error.value = 'Failed to delete indexer'
    return
  }
  await fetchAll()
}

async function toggleEnabled(indexer: Indexer) {
  const { error: err } = await client.PUT('/indexers/{id}', {
    params: { path: { id: indexer.id } },
    body: { enabled: !indexer.enabled },
  })
  if (err) {
    error.value = 'Failed to update indexer'
    return
  }
  await fetchAll()
}

async function testConnection(indexer: Indexer) {
  testingId.value = indexer.id
  testResult.value = null
  const { data, error: err } = await client.POST('/indexers/{id}/test', {
    params: { path: { id: indexer.id } },
    body: {},
  })
  testingId.value = null
  if (err) {
    testResult.value = { success: false, message: 'Request failed' }
    return
  }
  testResult.value = { success: data!.success, message: data!.message ?? '' }
}

function settingInputType(setting: IndexerDefinitionSetting): string {
  if (setting.type === 'password') return 'password'
  return 'text'
}

function definitionName(defId: string): string {
  return definitions.value.find((d) => d.id === defId)?.name ?? defId
}

onMounted(fetchAll)
</script>

<template>
  <div>
    <!-- Header -->
    <div class="flex items-center justify-between mb-6">
      <h1 class="text-xl font-semibold text-gray-100 tracking-tight">Indexers</h1>
      <button
        class="flex items-center gap-2 px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium transition-colors duration-200"
        @click="openAdd"
      >
        <span class="text-lg leading-none">+</span>
        Add Indexer
      </button>
    </div>

    <ErrorBanner :message="error" />

    <!-- Loading -->
    <div v-if="loading" class="text-gray-500 text-sm">Loading...</div>

    <!-- Empty state -->
    <div
      v-else-if="!indexers.length && !showForm"
      class="flex flex-col items-center justify-center py-20 text-gray-500"
    >
      <span class="text-4xl mb-3">&#9632;</span>
      <p class="text-sm">No indexers configured. Add one to get started.</p>
    </div>

    <!-- Indexer list -->
    <div v-else class="space-y-3">
      <div
        v-for="indexer in indexers"
        :key="indexer.id"
        class="flex items-start gap-4 px-5 py-4 rounded-lg bg-[#161b2e] border border-violet-900/20"
      >
        <!-- Info -->
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2.5 mb-2">
            <p class="text-sm font-semibold text-gray-200">{{ indexer.name }}</p>
            <span
              class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full"
              :class="indexer.enabled
                ? 'bg-green-600/20 text-green-300'
                : 'bg-gray-600/20 text-gray-500'"
            >
              {{ indexer.enabled ? 'Enabled' : 'Disabled' }}
            </span>
          </div>
          <div class="flex flex-wrap gap-1.5">
            <span class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-violet-600/20 text-violet-300">
              {{ definitionName(indexer.definitionId) }}
            </span>
            <span v-if="indexer.priority" class="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-sky-600/20 text-sky-300">
              Priority: {{ indexer.priority }}
            </span>
          </div>

          <!-- Inline test result -->
          <div v-if="testResult && testingId === null && testResult.success !== undefined" class="mt-2">
            <span
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
              :class="testResult.success
                ? 'bg-green-500/10 text-green-400 border border-green-500/30'
                : 'bg-red-500/10 text-red-400 border border-red-500/30'"
            >
              <span>{{ testResult.success ? '\u2713' : '\u2717' }}</span>
              {{ testResult.message }}
            </span>
          </div>
        </div>

        <!-- Actions -->
        <div class="flex items-center gap-1 flex-shrink-0">
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
            @click="tryingIndexer = indexer"
          >
            Try it out
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
            :disabled="testingId === indexer.id"
            @click="testConnection(indexer)"
          >
            {{ testingId === indexer.id ? 'Testing...' : 'Test' }}
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs transition-colors duration-200"
            :class="indexer.enabled
              ? 'text-gray-400 hover:text-yellow-300 hover:bg-yellow-600/10'
              : 'text-gray-400 hover:text-green-300 hover:bg-green-600/10'"
            @click="toggleEnabled(indexer)"
          >
            {{ indexer.enabled ? 'Disable' : 'Enable' }}
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
            @click="openEdit(indexer)"
          >
            Edit
          </button>
          <button
            class="px-2.5 py-1.5 rounded-md text-xs text-gray-400 hover:text-red-400 hover:bg-red-600/10 transition-colors duration-200"
            @click="deleteIndexer(indexer)"
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
        {{ editing ? 'Edit Indexer' : 'Add Indexer' }}
      </h2>

      <form class="space-y-4" @submit.prevent="submitForm">
        <!-- Definition picker (only for new) -->
        <div v-if="!editing">
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Indexer Definition</label>
          <select
            v-model="formDefinitionId"
            required
            class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
            @change="onDefinitionChange"
          >
            <option value="" disabled>Select a definition...</option>
            <option v-for="def in definitions" :key="def.id" :value="def.id">
              {{ def.name }} ({{ def.language }})
            </option>
          </select>
          <p v-if="selectedDefinition?.description" class="mt-1 text-xs text-gray-500">
            {{ selectedDefinition.description }}
          </p>
        </div>

        <!-- Name -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Name</label>
          <input
            v-model="formName"
            type="text"
            required
            placeholder="e.g. nCore"
            class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>

        <!-- Dynamic settings from definition -->
        <div v-if="selectedDefinition?.settings?.length">
          <label class="block text-xs font-medium text-gray-400 mb-3">Indexer Settings</label>
          <div class="space-y-3">
            <div v-for="setting in selectedDefinition.settings" :key="setting.name">
              <label class="block text-xs text-gray-500 mb-1">{{ setting.label }}</label>
              <input
                v-model="formSettings[setting.name]"
                :type="settingInputType(setting)"
                :placeholder="setting.default || ''"
                class="w-full px-3 py-2 rounded-lg bg-[#0c0f1a] border border-violet-800/30 text-sm text-gray-200 placeholder-gray-600 focus:border-violet-500/50 focus:outline-none transition-colors duration-200 font-mono"
              />
            </div>
          </div>
        </div>

        <!-- Priority -->
        <div>
          <label class="block text-xs font-medium text-gray-400 mb-1.5">Priority <span class="text-gray-600 font-normal">(lower number = higher preference)</span></label>
          <input
            v-model.number="formPriority"
            type="number"
            min="0"
            class="w-full px-3 py-2 rounded-lg bg-[#161b2e] border border-violet-800/30 text-sm text-gray-200 focus:border-violet-500/50 focus:outline-none transition-colors duration-200"
          />
        </div>

        <!-- Enabled -->
        <div class="flex items-center gap-3">
          <label class="text-xs font-medium text-gray-400">Enabled</label>
          <button
            type="button"
            class="relative w-10 h-5 rounded-full transition-colors duration-200"
            :class="formEnabled ? 'bg-violet-600' : 'bg-gray-700'"
            @click="formEnabled = !formEnabled"
          >
            <span
              class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white transition-transform duration-200"
              :class="formEnabled ? 'translate-x-5' : ''"
            />
          </button>
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

    <!-- Try it out modal -->
    <IndexerTryModal
      v-if="tryingIndexer"
      :indexer-id="tryingIndexer.id"
      :indexer-name="tryingIndexer.name"
      @close="tryingIndexer = null"
    />
  </div>
</template>
