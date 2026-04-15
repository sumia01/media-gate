import { ref, onMounted, onUnmounted } from 'vue'
import client from '@/api/client'
import { useEventStream } from './useEventStream'

const updateEnabled = ref(false)
const updateAvailable = ref(false)
const latestVersion = ref('')
const releaseNotes = ref('')
const publishedAt = ref('')
const currentVersion = ref('')
const checking = ref(false)
const applying = ref(false)

let initialized = false

async function fetchStatus() {
  const { data } = await client.GET('/update/status')
  if (data) {
    updateEnabled.value = data.enabled
    currentVersion.value = data.currentVersion
    updateAvailable.value = data.updateAvailable ?? false
    latestVersion.value = data.latestVersion ?? ''
    releaseNotes.value = data.releaseNotes ?? ''
    publishedAt.value = data.publishedAt ?? ''
  }
}

async function checkNow() {
  checking.value = true
  try {
    const { data } = await client.POST('/update/check')
    if (data) {
      updateEnabled.value = data.enabled
      updateAvailable.value = data.available
      currentVersion.value = data.currentVersion
      latestVersion.value = data.latestVersion ?? ''
      releaseNotes.value = data.releaseNotes ?? ''
      publishedAt.value = data.publishedAt ?? ''
    }
  } finally {
    checking.value = false
  }
}

async function applyUpdate() {
  applying.value = true
  try {
    await client.POST('/update/apply')
    // If successful, the server will restart and we'll lose connection.
    // After a brief delay, start polling for the server to come back.
    setTimeout(() => {
      pollForRestart()
    }, 2000)
  } catch {
    applying.value = false
  }
}

function pollForRestart() {
  const interval = setInterval(async () => {
    try {
      const resp = await fetch('/api/v1/health')
      if (resp.ok) {
        clearInterval(interval)
        window.location.reload()
      }
    } catch {
      // Server still restarting, keep polling.
    }
  }, 2000)

  // Give up after 60 seconds.
  setTimeout(() => {
    clearInterval(interval)
    applying.value = false
  }, 60000)
}

export function useUpdateCheck() {
  if (!initialized) {
    initialized = true
    fetchStatus()
  }

  const { on, off } = useEventStream()

  const handleUpdate = (data: any) => {
    updateAvailable.value = true
    latestVersion.value = data.newVersion ?? ''
    releaseNotes.value = data.releaseNotes ?? ''
    publishedAt.value = data.publishedAt ?? ''
    if (data.currentVersion) {
      currentVersion.value = data.currentVersion
    }
  }

  onMounted(() => {
    on('app.update_available', handleUpdate)
  })

  onUnmounted(() => {
    off('app.update_available', handleUpdate)
  })

  return {
    updateEnabled,
    updateAvailable,
    latestVersion,
    releaseNotes,
    publishedAt,
    currentVersion,
    checking,
    applying,
    checkNow,
    applyUpdate,
  }
}
