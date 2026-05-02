import { ref, onMounted, onUnmounted } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'
import { useEventStream } from './useEventStream'

export type Worker = components['schemas']['Worker']

const workers = ref<Worker[]>([])

async function fetchWorkers() {
  const { data } = await client.GET('/workers')
  if (data) {
    workers.value = data.workers
  }
}

async function runWorker(name: string) {
  await client.POST('/workers/{name}/run', {
    params: { path: { name } },
  })
}

// Stable SSE handler references
const sseHandlers = new Map<string, (data: any) => void>()

function getHandler(eventType: string) {
  let handler = sseHandlers.get(eventType)
  if (!handler) {
    handler = () => {
      fetchWorkers()
    }
    sseHandlers.set(eventType, handler)
  }
  return handler
}

const sseEvents = ['worker.started', 'worker.finished']

let subscribers = 0

export function useWorkers() {
  subscribers++

  const { on, off } = useEventStream()

  for (const type of sseEvents) {
    on(type, getHandler(type))
  }

  onMounted(fetchWorkers)

  onUnmounted(() => {
    subscribers--
    for (const type of sseEvents) {
      off(type, getHandler(type))
    }
    if (subscribers <= 0) {
      subscribers = 0
    }
  })

  return {
    workers,
    fetchWorkers,
    runWorker,
  }
}
