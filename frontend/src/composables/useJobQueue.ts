import { ref, computed, onUnmounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type Job = components['schemas']['Job']
type SyncDoneCallback = (libraryId: number) => void

const jobs = ref<Job[]>([])
let pollTimer: ReturnType<typeof setTimeout> | null = null
let subscribers = 0
const syncDoneListeners = new Set<SyncDoneCallback>()

// Track which library IDs had active jobs last poll
let prevActiveLibraryIds = new Set<number>()

const hasActiveJob = computed(() =>
  jobs.value.some((j) => j.status === 'pending' || j.status === 'running'),
)

async function fetchJobs() {
  const { data } = await client.GET('/jobs')
  if (data) {
    jobs.value = data.jobs
  }

  // Detect which libraries just finished syncing
  const currentActiveIds = new Set<number>()
  for (const j of jobs.value) {
    if ((j.status === 'pending' || j.status === 'running') && j.libraryId) {
      currentActiveIds.add(j.libraryId)
    }
  }
  for (const libId of prevActiveLibraryIds) {
    if (!currentActiveIds.has(libId)) {
      syncDoneListeners.forEach((cb) => cb(libId))
    }
  }
  prevActiveLibraryIds = currentActiveIds
}

function startPolling() {
  stopPolling()
  const interval = hasActiveJob.value ? 2000 : 30000
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
    // Mark this library as active immediately so we detect when it finishes
    if (data.libraryId) {
      prevActiveLibraryIds.add(data.libraryId)
    }
    await fetchJobs()
    startPolling()
  }
  return data
}

function onSyncDone(cb: SyncDoneCallback) {
  syncDoneListeners.add(cb)
  return () => syncDoneListeners.delete(cb)
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
    onSyncDone,
  }
}
