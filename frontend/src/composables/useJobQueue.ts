import { ref, computed, onUnmounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type Job = components['schemas']['Job']
type JobDoneCallback = (libraryId: number, jobType: string) => void

const jobs = ref<Job[]>([])
let pollTimer: ReturnType<typeof setTimeout> | null = null
let subscribers = 0
const jobDoneListeners = new Set<JobDoneCallback>()

// Track which library IDs had active jobs last poll (keyed by libId:type)
let prevActiveKeys = new Set<string>()

const hasActiveJob = computed(() =>
  jobs.value.some((j) => j.status === 'pending' || j.status === 'running'),
)

function activeKey(libId: number, type_: string) {
  return `${libId}:${type_}`
}

async function fetchJobs() {
  const { data } = await client.GET('/jobs')
  if (data) {
    jobs.value = data.jobs
  }

  // Detect which libraries just finished a job
  const currentActiveKeys = new Set<string>()
  for (const j of jobs.value) {
    if ((j.status === 'pending' || j.status === 'running') && j.libraryId) {
      currentActiveKeys.add(activeKey(j.libraryId, j.type))
    }
  }
  for (const key of prevActiveKeys) {
    if (!currentActiveKeys.has(key)) {
      const [libIdStr, jobType] = key.split(':')
      jobDoneListeners.forEach((cb) => cb(Number(libIdStr), jobType ?? ''))
    }
  }
  prevActiveKeys = currentActiveKeys
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
    if (data.libraryId) {
      prevActiveKeys.add(activeKey(data.libraryId, data.type))
    }
    await fetchJobs()
    startPolling()
  }
  return data
}

async function triggerMatch(libraryId: number, fullRematch = false) {
  const { data } = await client.POST('/libraries/{id}/match', {
    params: {
      path: { id: libraryId },
      query: fullRematch ? { fullRematch: true } : undefined,
    },
  })
  if (data) {
    if (data.libraryId) {
      prevActiveKeys.add(activeKey(data.libraryId, data.type))
    }
    await fetchJobs()
    startPolling()
  }
  return data
}

function onJobDone(cb: JobDoneCallback) {
  jobDoneListeners.add(cb)
  return () => jobDoneListeners.delete(cb)
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
    triggerMatch,
    onJobDone,
  }
}
