import createClient from 'openapi-fetch'
import type { paths } from './schema'

let getToken: (() => string | null) | null = null
let refreshToken: (() => Promise<boolean>) | null = null
let onAuthFailure: (() => void) | null = null

export function configureAuth(
  tokenGetter: () => string | null,
  tokenRefresher: () => Promise<boolean>,
  failureHandler: () => void,
) {
  getToken = tokenGetter
  refreshToken = tokenRefresher
  onAuthFailure = failureHandler
}

let isRefreshing = false
let refreshPromise: Promise<boolean> | null = null

const authFetch: typeof fetch = async (input, init) => {
  const token = getToken?.()
  const headers = new Headers(init?.headers)
  if (token) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  let res = await fetch(input, { ...init, headers })

  if (res.status === 401 && refreshToken) {
    if (!isRefreshing) {
      isRefreshing = true
      refreshPromise = refreshToken()
    }
    const ok = await refreshPromise!
    isRefreshing = false
    refreshPromise = null

    if (ok) {
      const newToken = getToken?.()
      if (newToken) {
        headers.set('Authorization', `Bearer ${newToken}`)
      }
      res = await fetch(input, { ...init, headers })
    }

    if (res.status === 401) {
      onAuthFailure?.()
    }
  }

  return res
}

const client = createClient<paths>({ baseUrl: '/api/v1', fetch: authFetch })

export { authFetch }
export default client
