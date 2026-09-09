const STORAGE_KEY = 'dreadgoad.console.auth-token'
const WS_PROTOCOL = 'dreadgoad.auth'
const WS_TOKEN_PROTOCOL_PREFIX = 'dreadgoad.token.'

let memoryToken: string | null | undefined

export function parseAuthFragment(hash: string): {
  token: string | null
  remainingHash: string
} {
  const params = new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : hash)
  const token = params.get('token')?.trim() || null
  params.delete('token')
  const remaining = params.toString()
  return { token, remainingHash: remaining ? `#${remaining}` : '' }
}

/** Capture the launch token once, then remove it from the visible browser URL. */
export function authToken(): string | null {
  if (memoryToken !== undefined) return memoryToken
  if (typeof window === 'undefined') {
    memoryToken = null
    return memoryToken
  }

  const parsed = parseAuthFragment(window.location.hash)
  if (parsed.token) {
    memoryToken = parsed.token
    try {
      window.sessionStorage.setItem(STORAGE_KEY, parsed.token)
    } catch {
      // The in-memory token still keeps this page usable.
    }
    window.history.replaceState(
      window.history.state,
      '',
      `${window.location.pathname}${window.location.search}${parsed.remainingHash}`,
    )
    return memoryToken
  }

  try {
    memoryToken = window.sessionStorage.getItem(STORAGE_KEY)
  } catch {
    memoryToken = null
  }
  return memoryToken
}

export function authenticatedHeaders(
  initial?: HeadersInit,
  token: string | null = authToken(),
): Headers {
  const headers = new Headers(initial)
  if (token) headers.set('Authorization', `Bearer ${token}`)
  return headers
}

export function authenticatedFetch(
  input: RequestInfo | URL,
  init: RequestInit = {},
): Promise<Response> {
  return fetch(input, { ...init, headers: authenticatedHeaders(init.headers) })
}

export function websocketProtocols(
  token: string | null = authToken(),
): string[] | undefined {
  return token ? [WS_PROTOCOL, `${WS_TOKEN_PROTOCOL_PREFIX}${token}`] : undefined
}
