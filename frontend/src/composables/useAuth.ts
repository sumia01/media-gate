import { computed, ref } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type UserProfile = components['schemas']['UserProfile']

export interface SetupStatus {
  needsSetup: boolean
  onboardingCompleted: boolean
  onboardingStep: number
}

const accessToken = ref<string | null>(null)
const currentUser = ref<UserProfile | null>(null)
const setupStatusCache = ref<SetupStatus | null>(null)

const isAuthenticated = computed(() => !!accessToken.value && !!currentUser.value)

async function login(email: string, password: string, rememberMe: boolean = false): Promise<void> {
  const res = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, rememberMe }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new Error(body?.message || 'Login failed')
  }
  const data = await res.json()
  accessToken.value = data.accessToken
  currentUser.value = data.user
}

async function setup(email: string, password: string): Promise<void> {
  const res = await fetch('/api/v1/auth/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    credentials: 'include',
    body: JSON.stringify({ email, password }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => null)
    throw new Error(body?.message || 'Setup failed')
  }
  const data = await res.json()
  accessToken.value = data.accessToken
  currentUser.value = data.user
  // Invalidate cached status so the router re-checks.
  setupStatusCache.value = null
}

async function getSetupStatus(): Promise<SetupStatus> {
  // Return cached result if onboarding is already completed.
  if (setupStatusCache.value?.onboardingCompleted) {
    return setupStatusCache.value
  }
  const res = await fetch('/api/v1/setup/status')
  if (!res.ok) throw new Error('Failed to check setup status')
  const status: SetupStatus = await res.json()
  setupStatusCache.value = status
  return status
}

function clearSetupStatusCache(): void {
  setupStatusCache.value = null
}

async function refresh(): Promise<boolean> {
  try {
    const res = await fetch('/api/v1/auth/refresh', {
      method: 'POST',
      credentials: 'include',
    })
    if (!res.ok) return false
    const data = await res.json()
    accessToken.value = data.accessToken
    return true
  } catch {
    return false
  }
}

async function logout(): Promise<void> {
  try {
    await fetch('/api/v1/auth/logout', {
      method: 'POST',
      credentials: 'include',
    })
  } catch {
    // Ignore errors — clear state regardless.
  }
  accessToken.value = null
  currentUser.value = null
}

async function fetchProfile(): Promise<void> {
  const { data, error } = await client.GET('/auth/me')
  if (error) throw new Error('Failed to fetch profile')
  currentUser.value = data as UserProfile
}

function getAccessToken(): string | null {
  return accessToken.value
}

export function useAuth() {
  return {
    accessToken,
    currentUser,
    isAuthenticated,
    login,
    setup,
    getSetupStatus,
    clearSetupStatusCache,
    refresh,
    logout,
    fetchProfile,
    getAccessToken,
  }
}
