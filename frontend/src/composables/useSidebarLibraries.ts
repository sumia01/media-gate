import { ref } from 'vue'
import client from '@/api/client'
import type { Library } from '@/types/api'

const libraries = ref<Library[]>([])

async function fetchLibraries() {
  const { data } = await client.GET('/libraries')
  if (data) libraries.value = data
}

export function useSidebarLibraries() {
  return {
    libraries,
    refreshLibraries: fetchLibraries,
  }
}
