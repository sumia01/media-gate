import { onUnmounted, ref } from 'vue'
import { useAuth } from './useAuth'

type EventCallback = (data: any) => void

const eventSource = ref<EventSource | null>(null)
const connected = ref(false)
let subscribers = 0
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let connecting = false
const listeners = new Map<string, Set<EventCallback>>()

function connect() {
  if (eventSource.value || connecting) return
  connecting = true

  const { getAccessToken } = useAuth()
  const token = getAccessToken()

  // Exchange JWT for a single-use SSE ticket to avoid exposing the token in the URL.
  const openSSE = (ticketParam: string) => {
    const es = new EventSource(`/api/v1/events${ticketParam}`)

    es.onopen = () => {
      connected.value = true
    }

    es.onerror = () => {
      connected.value = false
      connecting = false
      es.close()
      eventSource.value = null
      // Reconnect after delay
      if (subscribers > 0 && !reconnectTimer) {
        reconnectTimer = setTimeout(() => {
          reconnectTimer = null
          if (subscribers > 0) connect()
        }, 3000)
      }
    }

    // Listen for all registered event types
    for (const [type] of listeners) {
      addESListener(es, type)
    }

    eventSource.value = es
    connecting = false
  }

  if (token) {
    fetch('/api/v1/auth/sse-ticket', {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    })
      .then((res) => (res.ok ? res.json() : null))
      .then((data) => {
        if (data?.ticket) {
          openSSE(`?ticket=${encodeURIComponent(data.ticket)}`)
        } else {
          openSSE('')
        }
      })
      .catch(() => openSSE(''))
  } else {
    openSSE('')
  }
}

function disconnect() {
  connecting = false
  if (reconnectTimer) {
    clearTimeout(reconnectTimer)
    reconnectTimer = null
  }
  if (eventSource.value) {
    eventSource.value.close()
    eventSource.value = null
    connected.value = false
  }
}

function addESListener(es: EventSource, type: string) {
  es.addEventListener(type, ((e: MessageEvent) => {
    let data: any
    try {
      const parsed = JSON.parse(e.data)
      data = parsed.payload ?? parsed
    } catch {
      data = e.data
    }
    const cbs = listeners.get(type)
    if (cbs) {
      cbs.forEach((cb) => {
        cb(data)
      })
    }
  }) as EventListener)
}

function on(eventType: string, callback: EventCallback) {
  let cbs = listeners.get(eventType)
  if (!cbs) {
    cbs = new Set()
    listeners.set(eventType, cbs)
    // Add listener to existing EventSource if connected
    if (eventSource.value) {
      addESListener(eventSource.value, eventType)
    }
  }
  cbs.add(callback)
}

function off(eventType: string, callback: EventCallback) {
  const cbs = listeners.get(eventType)
  if (cbs) {
    cbs.delete(callback)
    if (cbs.size === 0) {
      listeners.delete(eventType)
    }
  }
}

export function useEventStream() {
  subscribers++
  if (!eventSource.value && !connecting) {
    connect()
  }

  onUnmounted(() => {
    subscribers--
    if (subscribers <= 0) {
      disconnect()
      subscribers = 0
    }
  })

  return {
    connected,
    on,
    off,
  }
}
