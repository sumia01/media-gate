import { computed, onUnmounted, ref } from 'vue'
import client from '@/api/client'
import type { Job } from '@/types/api'
import { useEventStream } from './useEventStream'

type JobDoneCallback = (libraryId: number, jobType: string) => void

const jobs = ref<Job[]>([])
let subscribers = 0
const jobDoneListeners = new Set<JobDoneCallback>()

const hasActiveJob = computed(() => jobs.value.some((j) => j.status === 'pending' || j.status === 'running'))

async function fetchJobs() {
  try {
    const { data } = await client.GET('/jobs')
    if (data) {
      jobs.value = data.jobs
    }
  } catch {
    // Keep the last snapshot. A status-refresh failure must not turn an
    // already accepted job submission into an apparent admission failure.
    return
  }
}

async function triggerSync(libraryId: number) {
  const result = await client.POST('/libraries/{id}/sync', {
    params: { path: { id: libraryId } },
  })
  if (result.data) {
    await fetchJobs()
  }
  return result
}

async function triggerMatch(libraryId: number, fullRematch = false) {
  const result = await client.POST('/libraries/{id}/match', {
    params: {
      path: { id: libraryId },
      query: fullRematch ? { fullRematch: true } : undefined,
    },
  })
  if (result.data) {
    await fetchJobs()
  }
  return result
}

function onJobDone(cb: JobDoneCallback) {
  jobDoneListeners.add(cb)
  return () => jobDoneListeners.delete(cb)
}

// Stable handler references for SSE (so off() works correctly)
const sseHandlers = new Map<string, (data: any) => void>()

function getHandler(eventType: string) {
  let handler = sseHandlers.get(eventType)
  if (!handler) {
    handler = (data: any) => {
      // Refresh job list on any library workflow event
      fetchJobs()

      // Notify job-done listeners on completion events
      if (
        eventType === 'library.sync_completed' ||
        eventType === 'library.sync_failed' ||
        eventType === 'library.match_completed' ||
        eventType === 'library.match_failed'
      ) {
        const libraryId = data.libraryId
        const jobType = eventType.startsWith('library.sync') ? 'sync_library' : 'match_library'
        if (libraryId) {
          jobDoneListeners.forEach((cb) => {
            cb(libraryId, jobType)
          })
        }
      }
    }
    sseHandlers.set(eventType, handler)
  }
  return handler
}

const eventTypes = [
  'library.sync_started',
  'library.sync_completed',
  'library.sync_failed',
  'library.match_started',
  'library.match_progress',
  'library.match_completed',
  'library.match_failed',
]

export function useJobQueue() {
  subscribers++

  const { on, off } = useEventStream()

  // Subscribe to SSE events
  for (const type of eventTypes) {
    on(type, getHandler(type))
  }

  // Initial fetch
  fetchJobs()

  onUnmounted(() => {
    subscribers--
    for (const type of eventTypes) {
      off(type, getHandler(type))
    }
    if (subscribers <= 0) {
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
