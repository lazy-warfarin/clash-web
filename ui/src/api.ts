export class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const method = (init?.method || 'GET').toUpperCase()
  const csrf = document.cookie.split('; ').find(item => item.startsWith('clash_web_csrf='))?.split('=').slice(1).join('=')
  const response = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    ...init,
    headers: {
      ...(init?.body ? { 'Content-Type': 'application/json' } : {}),
      ...(method !== 'GET' && method !== 'HEAD' && csrf ? { 'X-CSRF-Token': decodeURIComponent(csrf) } : {}),
      ...init?.headers,
    },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }))
    throw new APIError(response.status, body.error || response.statusText)
  }
  if (response.status === 204) return undefined as T
  return response.json()
}

export type Me = { username: string; mustChangePassword: boolean; version: string }
export type Status = {
  appVersion: string
  webListen?: string
  coreOnline: boolean
  helperOnline: boolean
  core?: { version?: string }
  helper?: { running?: boolean; pid?: number; startedAt?: string; lastExit?: string }
  config?: Record<string, any>
}
export type Profile = {
  id: number
  name: string
  source: string
  url?: string
  active: boolean
  updatedAt: string
  content?: string
}

export const json = (method: string, body?: unknown): RequestInit => ({
  method,
  body: body === undefined ? undefined : JSON.stringify(body),
})

export function wsURL(topic: string) {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${scheme}//${location.host}/api/v1/ws/${topic}`
}
