import { ref, computed, onUnmounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type Job = components['schemas']['Job']

const jobs = ref<Job[]>([])
let pollTimer: ReturnType<typeof setTimeout> | null = null
let subscribers = 0

const hasActiveJob = computed(() =>
  jobs.value.some((j) => j.status === 'pending' || j.status === 'running'),
)

async function fetchJobs() {
  const { data } = await client.GET('/jobs')
  if (data) {
    jobs.value = data.jobs
  }
}

function startPolling() {
  stopPolling()
  const interval = hasActiveJob.value ? 5000 : 30000
  pollTimer = setTimeout(async () => {
    await fetchJobs()
    if (subscribers > 0) startPolling()
  }, interval)
}

function stopPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

async function triggerSync(libraryId: number) {
  const { data } = await client.POST('/libraries/{id}/sync', {
    params: { path: { id: libraryId } },
  })
  if (data) {
    await fetchJobs()
    startPolling()
  }
  return data
}

export function useJobQueue() {
  subscribers++
  fetchJobs().then(() => startPolling())

  onUnmounted(() => {
    subscribers--
    if (subscribers <= 0) {
      stopPolling()
      subscribers = 0
    }
  })

  return {
    jobs,
    hasActiveJob,
    fetchJobs,
    triggerSync,
  }
}
