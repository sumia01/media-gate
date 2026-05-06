<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import client from '@/api/client'
import type { IndexerDefinition, IndexerDefinitionSetting } from '@/types/api'

const emit = defineEmits<{ next: []; back: [] }>()

const definitions = ref<IndexerDefinition[]>([])
const selectedDefinition = ref<IndexerDefinition | null>(null)
const formName = ref('')
const formDefinitionId = ref('')
const formSettings = ref<Record<string, string>>({})
const formEnabled = ref(true)

const error = ref('')
const saving = ref(false)
const loading = ref(true)

onMounted(async () => {
  try {
    const { data } = await client.GET('/indexer-definitions')
    definitions.value = data?.definitions ?? []
  } catch {
    // Non-critical — user can skip
  } finally {
    loading.value = false
  }
})

const sortedDefinitions = computed(() =>
  [...definitions.value].sort((a, b) => (a.name ?? '').localeCompare(b.name ?? '')),
)

function onDefinitionChange() {
  const def = definitions.value.find((d) => d.id === formDefinitionId.value)
  selectedDefinition.value = def ?? null
  if (def) {
    if (!formName.value) formName.value = def.name ?? ''
    applyDefaults(def)
  }
}

function applyDefaults(def: IndexerDefinition) {
  const s: Record<string, string> = {}
  for (const setting of def.settings ?? []) {
    s[setting.name!] = setting.default ?? ''
  }
  formSettings.value = s
}

function settingInputType(setting: IndexerDefinitionSetting): string {
  if (setting.type === 'password') return 'password'
  return 'text'
}

async function handleSubmit() {
  if (!formDefinitionId.value) {
    error.value = 'Please select an indexer'
    return
  }

  error.value = ''
  saving.value = true
  try {
    const settings: Record<string, string> = {}
    for (const [k, v] of Object.entries(formSettings.value)) {
      if (v) settings[k] = v
    }

    const { error: err } = await client.POST('/indexers', {
      body: {
        name: formName.value || selectedDefinition.value?.name || 'Indexer',
        definitionId: formDefinitionId.value,
        settings,
        enabled: formEnabled.value,
        priority: 50,
      },
    })
    if (err) {
      error.value = 'Failed to add indexer'
      return
    }
    emit('next')
  } catch {
    error.value = 'Failed to add indexer'
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-5">
    <div>
      <h2 class="text-lg font-semibold text-gray-200">Add Indexer</h2>
      <p class="text-sm text-gray-500 mt-1">
        Indexers provide torrent search results. You can add more later from the Indexers page.
      </p>
    </div>

    <div v-if="loading" class="text-sm text-gray-500">Loading definitions...</div>

    <form v-else class="space-y-4" @submit.prevent="handleSubmit">
      <div v-if="error" class="px-3 py-2 rounded-lg bg-red-500/10 border border-red-500/20 text-red-400 text-sm">
        {{ error }}
      </div>

      <!-- Definition picker -->
      <div>
        <label class="block text-sm font-medium text-gray-400 mb-1.5">Indexer</label>
        <select
          v-model="formDefinitionId"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm focus:outline-none focus:border-violet-500/50 transition-colors"
          @change="onDefinitionChange"
        >
          <option value="" disabled>Select an indexer...</option>
          <option v-for="def in sortedDefinitions" :key="def.id" :value="def.id">
            {{ def.name }}
          </option>
        </select>
      </div>

      <!-- Name -->
      <div v-if="selectedDefinition">
        <label for="idx-name" class="block text-sm font-medium text-gray-400 mb-1.5">Name</label>
        <input
          id="idx-name"
          v-model="formName"
          type="text"
          class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          :placeholder="selectedDefinition.name ?? 'Indexer name'"
        />
      </div>

      <!-- Dynamic settings -->
      <div
        v-if="selectedDefinition?.settings?.length"
        class="space-y-3 pt-2 border-t border-violet-900/20"
      >
        <div v-for="setting in selectedDefinition.settings" :key="setting.name">
          <label class="block text-sm font-medium text-gray-400 mb-1.5">{{ setting.label }}</label>
          <input
            v-model="formSettings[setting.name!]"
            :type="settingInputType(setting)"
            :placeholder="setting.default ?? ''"
            class="w-full px-3 py-2.5 rounded-lg bg-[#161b2e] border border-violet-800/30 text-gray-200 text-sm placeholder-gray-600 focus:outline-none focus:border-violet-500/50 transition-colors"
          />
        </div>
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
          type="button"
          class="px-4 py-2.5 rounded-lg border border-violet-800/30 text-gray-400 hover:text-gray-200 text-sm font-medium transition-colors"
          @click="emit('next')"
        >
          Skip
        </button>
        <button
          type="submit"
          :disabled="saving || !formDefinitionId"
          class="flex-1 py-2.5 px-4 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 disabled:cursor-not-allowed text-white text-sm font-medium transition-colors"
        >
          {{ saving ? 'Adding...' : 'Add & Continue' }}
        </button>
      </div>
    </form>
  </div>
</template>
