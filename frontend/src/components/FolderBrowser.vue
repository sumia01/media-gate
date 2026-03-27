<script setup lang="ts">
import { ref, watch, onMounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type ScanEntry = components['schemas']['ScanEntry']

const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const currentPath = ref('')
const entries = ref<ScanEntry[]>([])
const loading = ref(false)
const error = ref('')

async function browse(path?: string) {
  loading.value = true
  error.value = ''
  const { data, error: err } = await client.GET('/browse', {
    params: { query: path ? { path } : {} },
  })
  loading.value = false
  if (err || !data) {
    error.value = 'Failed to browse directory'
    return
  }
  currentPath.value = data.path
  entries.value = data.entries
  emit('update:modelValue', data.path)
}

function navigateTo(path: string) {
  browse(path)
}

const breadcrumbs = ref<{ name: string; path: string }[]>([])

watch(currentPath, (val) => {
  if (!val) return
  const parts = val.split('/').filter(Boolean)
  const crumbs: { name: string; path: string }[] = []
  for (let i = 0; i < parts.length; i++) {
    const part = parts[i]!
    crumbs.push({
      name: part,
      path: '/' + parts.slice(0, i + 1).join('/'),
    })
  }
  breadcrumbs.value = crumbs
})

onMounted(() => {
  browse(props.modelValue || undefined)
})
</script>

<template>
  <div class="rounded-lg border border-violet-800/30 bg-[#111827] overflow-hidden">
    <!-- Breadcrumb -->
    <div class="flex items-center gap-1 px-3 py-2 bg-[#0d1117] border-b border-violet-800/20 text-xs overflow-x-auto">
      <span class="text-gray-500 flex-shrink-0">/</span>
      <template v-for="(crumb, i) in breadcrumbs" :key="crumb.path">
        <button
          class="text-gray-400 hover:text-violet-300 transition-colors duration-200 flex-shrink-0"
          @click="navigateTo(crumb.path)"
        >
          {{ crumb.name }}
        </button>
        <span v-if="i < breadcrumbs.length - 1" class="text-gray-600 flex-shrink-0">/</span>
      </template>
    </div>

    <!-- Directory list -->
    <div class="max-h-52 overflow-y-auto scrollbar-none">
      <div v-if="loading" class="px-3 py-6 text-center text-gray-500 text-xs">Loading...</div>
      <div v-else-if="error" class="px-3 py-6 text-center text-red-400 text-xs">{{ error }}</div>
      <div v-else-if="!entries.length" class="px-3 py-6 text-center text-gray-500 text-xs">No subdirectories</div>
      <template v-else>
        <button
          v-for="entry in entries"
          :key="entry.path"
          class="w-full flex items-center gap-2 px-3 py-2 text-sm text-gray-300 hover:bg-violet-600/10 hover:text-violet-200 transition-colors duration-200 text-left"
          @click="navigateTo(entry.path)"
        >
          <span class="text-gray-500 text-base leading-none">&#128193;</span>
          <span class="truncate">{{ entry.name }}</span>
        </button>
      </template>
    </div>

    <!-- Footer: current path -->
    <div class="flex items-center gap-2 px-3 py-2 border-t border-violet-800/20 bg-[#0d1117]">
      <span class="text-xs text-gray-500">Selected:</span>
      <span class="flex-1 text-xs text-gray-300 font-mono truncate">{{ currentPath }}</span>
    </div>
  </div>
</template>
