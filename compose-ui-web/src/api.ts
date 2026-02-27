import type { ActionResult, ComposeFile, ContainerItem, ImageDeleteResult, ImageItem, Project } from './types'

const API_BASE = (import.meta.env.VITE_API_BASE as string | undefined) ?? '/api/v1'
const AUTH_STORAGE_KEY = 'compose_ui_basic_auth'

type ActionBody = { action: 'stop' | 'restart' | 'delete' | 'start' }

type BasicAuth = { user: string; pass: string }

export class AuthRequiredError extends Error {
  reason: 'missing' | 'invalid'

  constructor(reason: 'missing' | 'invalid', message: string) {
    super(message)
    this.name = 'AuthRequiredError'
    this.reason = reason
  }
}

let cachedAuth: BasicAuth | null = loadAuthFromStorage()

function loadAuthFromStorage(): BasicAuth | null {
  try {
    const raw = window.localStorage.getItem(AUTH_STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<BasicAuth>
    if (!parsed.user || !parsed.pass) return null
    return { user: parsed.user, pass: parsed.pass }
  } catch {
    return null
  }
}

function saveAuthToStorage(auth: BasicAuth) {
  try {
    window.localStorage.setItem(AUTH_STORAGE_KEY, JSON.stringify(auth))
  } catch {
    // ignore storage failure
  }
}

function clearAuth() {
  cachedAuth = null
  try {
    window.localStorage.removeItem(AUTH_STORAGE_KEY)
  } catch {
    // ignore storage failure
  }
}

function buildAuthHeader(auth: BasicAuth) {
  const token = btoa(`${auth.user}:${auth.pass}`)
  return `Basic ${token}`
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const auth = cachedAuth
  if (!auth) {
    throw new AuthRequiredError('missing', '请先登录')
  }

  const res = await fetch(`${API_BASE}${path}`, {
    headers: { 'Content-Type': 'application/json', Authorization: buildAuthHeader(auth), ...(init?.headers ?? {}) },
    ...init,
  })
  if (res.status === 401) {
    clearAuth()
    throw new AuthRequiredError('invalid', '认证失败，请重新登录')
  }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw new Error(data?.message ?? `请求失败: ${res.status}`)
  }
  return data as T
}

export function hasBasicAuth(): boolean {
  return !!cachedAuth
}

export function getBasicAuthUser(): string {
  return cachedAuth?.user ?? ''
}

export function logoutBasicAuth() {
  clearAuth()
}

export async function loginBasicAuth(user: string, pass: string): Promise<void> {
  const auth = { user: user.trim(), pass }
  if (!auth.user || !auth.pass) {
    throw new Error('请输入用户名和密码')
  }
  cachedAuth = auth
  saveAuthToStorage(auth)
  try {
    await request<{ items?: Project[] }>('/projects')
  } catch (e) {
    clearAuth()
    if (e instanceof AuthRequiredError && e.reason === 'invalid') {
      throw new Error('用户名或密码错误')
    }
    throw e
  }
}

export async function listProjects(): Promise<Project[]> {
  const data = await request<{ items?: Project[] }>('/projects')
  return Array.isArray(data?.items) ? data.items : []
}

export function getComposeFile(projectId: string): Promise<ComposeFile> {
  return request<ComposeFile>(`/projects/${projectId}/compose-file`)
}

export function saveComposeFile(projectId: string, content: string, expectedMtime: number): Promise<ComposeFile> {
  return request<ComposeFile>(`/projects/${projectId}/compose-file`, {
    method: 'PUT',
    body: JSON.stringify({ content, expectedMtime }),
  })
}

export function redeploy(projectId: string): Promise<ActionResult> {
  return request<ActionResult>(`/projects/${projectId}/redeploy`, { method: 'POST' })
}

export function serviceAction(serviceId: string, action: ActionBody['action']): Promise<ActionResult> {
  return request<ActionResult>(`/services/${serviceId}/action`, {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export function projectAction(projectId: string, action: ActionBody['action']): Promise<ActionResult> {
  return request<ActionResult>(`/projects/${projectId}/action`, {
    method: 'POST',
    body: JSON.stringify({ action }),
  })
}

export async function getLogs(containerId: string, tail = 200): Promise<string> {
  const data = await request<{ content: string }>(`/containers/${containerId}/logs?tail=${tail}&follow=false`)
  return data.content
}

export async function listContainers(keyword = ''): Promise<ContainerItem[]> {
  const query = new URLSearchParams()
  if (keyword.trim()) query.set('keyword', keyword.trim())
  const path = query.toString() ? `/containers?${query.toString()}` : '/containers'
  const data = await request<{ items?: ContainerItem[] }>(path)
  return Array.isArray(data?.items) ? data.items : []
}

export async function listImages(keyword = '', used: 'all' | 'used' | 'unused' = 'all'): Promise<ImageItem[]> {
  const query = new URLSearchParams()
  if (keyword.trim()) query.set('keyword', keyword.trim())
  if (used !== 'all') query.set('used', used)
  const path = query.toString() ? `/images?${query.toString()}` : '/images'
  const data = await request<{ items?: ImageItem[] }>(path)
  return Array.isArray(data?.items) ? data.items : []
}

export async function deleteImages(imageIds: string[], force = false): Promise<ImageDeleteResult[]> {
  const data = await request<{ items?: ImageDeleteResult[] }>('/images/delete', {
    method: 'POST',
    body: JSON.stringify({ imageIds, force }),
  })
  return Array.isArray(data?.items) ? data.items : []
}

function getAuthOrThrow(): BasicAuth {
  const auth = cachedAuth
  if (!auth) {
    throw new AuthRequiredError('missing', '请先登录')
  }
  return auth
}

function handleSSEAuthStatus(status: number) {
  if (status === 401) {
    clearAuth()
    throw new AuthRequiredError('invalid', '认证失败，请重新登录')
  }
}

async function streamSSE(path: string, onEvent: (event: string, data: string) => void, signal?: AbortSignal): Promise<void> {
  const auth = getAuthOrThrow()
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'GET',
    headers: {
      Accept: 'text/event-stream',
      Authorization: buildAuthHeader(auth),
    },
    signal,
  })
  handleSSEAuthStatus(res.status)
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new Error(text || `请求失败: ${res.status}`)
  }
  if (!res.body) {
    throw new Error('SSE 响应为空')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let eventName = 'message'
  let dataLines: string[] = []

  const emit = () => {
    if (dataLines.length === 0) return
    onEvent(eventName, dataLines.join('\n'))
    eventName = 'message'
    dataLines = []
  }

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const chunks = buffer.split(/\r?\n/)
    buffer = chunks.pop() ?? ''
    for (const line of chunks) {
      if (line === '') {
        emit()
        continue
      }
      if (line.startsWith(':')) continue
      if (line.startsWith('event:')) {
        eventName = line.slice(6).trim() || 'message'
        continue
      }
      if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart())
      }
    }
  }
  if (buffer.trim() !== '') {
    const line = buffer.trim()
    if (line.startsWith('data:')) {
      dataLines.push(line.slice(5).trimStart())
    }
  }
  emit()
}

export async function streamContainerLogs(
  containerId: string,
  onEvent: (event: string, data: string) => void,
  signal?: AbortSignal
): Promise<void> {
  await streamSSE(`/containers/${containerId}/logs/stream`, onEvent, signal)
}

export async function streamProjectAction(
  projectId: string,
  action: 'start' | 'stop' | 'redeploy',
  onEvent: (event: string, data: string) => void,
  signal?: AbortSignal
): Promise<void> {
  const q = new URLSearchParams({ action }).toString()
  await streamSSE(`/projects/${projectId}/action-stream?${q}`, onEvent, signal)
}
