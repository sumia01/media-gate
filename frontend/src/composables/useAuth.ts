import { ref, computed } from 'vue'
import client from '@/api/client'
import type { components } from '@/api/schema'

type UserProfile = components['schemas']['UserProfile']

const accessToken = ref<string | null>(null)
const currentUser = ref<UserProfile | null>(null)

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
    refresh,
    logout,
    fetchProfile,
    getAccessToken,
  }
}
