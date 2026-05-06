import { ref } from 'vue'
import client from '@/api/client'

const version = ref('')
const disk = ref<{ totalBytes: number; usedBytes: number; freeBytes: number } | null>(null)

async function fetchSystemInfo() {
  const { data } = await client.GET('/health')
  if (data) {
    version.value = data.version
    disk.value = data.disk
      ? {
          totalBytes: data.disk.totalBytes ?? 0,
          usedBytes: data.disk.usedBytes ?? 0,
          freeBytes: data.disk.freeBytes ?? 0,
        }
      : null
  }
}

export function useSystemInfo() {
  return { version, disk, refreshSystemInfo: fetchSystemInfo }
}
