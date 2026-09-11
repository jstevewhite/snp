// JSON API client. The SPA is same-origin in production (embedded in the
// Go binary); in dev, Vite proxies /api to the snp dev server
// (see vite.config.ts, VITE_API_PROXY). In the wails desktop app there
// is no HTTP listener: every call is served in-process through the
// window.go.desktop.App bridge (spec §12) instead of fetch.

import {
  type Folder,
  type Identity,
  type Snippet,
  type SnippetInput,
  type SnippetSearchParams,
  type SyncResponse,
  type TagCount,
  type VersionInfo,
  ApiError,
} from './types'

// --- AI generation (spec §13) ---

export interface AIStatus {
  enabled: boolean
  model: string
}

export interface AIGenerateInput {
  prompt: string
  language?: string
  /** Shape of the requested snippet (spec §13); omitted = command. */
  kind?: AIKind
}

/** What the model should produce: one command, a multi-line script, or a
 * function definition (spec §13). */
export type AIKind = 'command' | 'script' | 'function'

export interface AIGenerateResult {
  title: string
  language: string
  body: string
  /** Plain-text explanation of the snippet, for the Notes field. */
  notes?: string
  uses_variables: boolean
}
import { resolveDesktopApp } from './desktop'

const API = '/api'

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
}

interface PlainResponse {
  status: number
  ok: boolean
  bodyText: string
}

/**
 * One HTTP-shaped exchange. In the desktop app the exchange runs
 * against the in-process bridge; otherwise it is a plain fetch. Either
 * way the response is reduced to {status, ok, bodyText} so the two
 * transports share one parse/error path.
 */
async function httpExchange(
  method: string,
  path: string,
  body: string | undefined,
): Promise<PlainResponse> {
  const url = API + path // the full "/api/..." request path for both transports
  const bridge = await resolveDesktopApp()
  if (bridge !== undefined) {
    try {
      const r = await bridge.CallAPI(method, url, body ?? '')
      return { status: r.status, ok: r.status >= 200 && r.status < 300, bodyText: r.body }
    } catch (err) {
      const detail = err instanceof Error ? err.message : String(err)
      throw new ApiError(0, `desktop error: ${detail}`)
    }
  }
  const init: RequestInit = { method }
  // The server rejects every state-changing request (POST/PUT/DELETE,
  // spec §3 CSRF rule) without Content-Type: application/json — 415
  // otherwise. DELETE carries no body, so the header must be set
  // regardless of whether a body is present.
  if (method === 'POST' || method === 'PUT' || method === 'DELETE' || body !== undefined) {
    if (body !== undefined) init.body = body
    init.headers = { 'Content-Type': 'application/json' }
  }
  let res: Response
  try {
    res = await fetch(url, init)
  } catch (err) {
    const detail = err instanceof Error ? err.message : String(err)
    throw new ApiError(0, `network error: ${detail}`)
  }
  return { status: res.status, ok: res.ok, bodyText: await res.text() }
}

/** Extract the server's {"error": msg} message, when present. */
function errorMessage(status: number, bodyText: string): string {
  try {
    const j = JSON.parse(bodyText) as { error?: unknown }
    if (typeof j.error === 'string' && j.error !== '') return j.error
  } catch {
    // Non-JSON error body.
  }
  return `HTTP ${status}`
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const body = opts.body === undefined ? undefined : JSON.stringify(opts.body)
  const res = await httpExchange(method, path, body)
  if (!res.ok) throw new ApiError(res.status, errorMessage(res.status, res.bodyText))
  if (res.status === 204 || res.bodyText === '') return undefined as T
  return JSON.parse(res.bodyText) as T
}

export function me(): Promise<Identity> {
  return request<Identity>('/me')
}

/** GET /api/version: the release version stamped into the running binary. */
export function version(): Promise<VersionInfo> {
  return request<VersionInfo>('/version')
}

export function listSnippets(p: SnippetSearchParams = {}): Promise<Snippet[]> {
  const usp = new URLSearchParams()
  if (p.q) usp.set('q', p.q)
  for (const t of p.tag ?? []) usp.append('tag', t)
  for (const l of p.lang ?? []) usp.append('lang', l)
  if (p.folder) usp.set('folder', p.folder)
  if (p.limit !== undefined) usp.set('limit', String(p.limit))
  if (p.offset !== undefined) usp.set('offset', String(p.offset))
  const qs = usp.toString()
  return request<Snippet[]>(qs ? `/snippets?${qs}` : '/snippets')
}

export function createSnippet(input: SnippetInput): Promise<Snippet> {
  return request<Snippet>('/snippets', { method: 'POST', body: input })
}

export function getSnippet(id: string): Promise<Snippet> {
  return request<Snippet>(`/snippets/${encodeURIComponent(id)}`)
}

export function updateSnippet(id: string, input: SnippetInput): Promise<Snippet> {
  return request<Snippet>(`/snippets/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: input,
  })
}

export function deleteSnippet(id: string): Promise<void> {
  return request<void>(`/snippets/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

/** Decrypted body as text/plain; for `curl | sh` and copy. */
export async function getRaw(id: string): Promise<string> {
  const res = await httpExchange(
    'GET',
    `/snippets/${encodeURIComponent(id)}/raw`,
    undefined,
  )
  if (!res.ok) throw new ApiError(res.status, `raw fetch failed: ${res.status}`)
  return res.bodyText
}

export function listFolders(): Promise<Folder[]> {
  return request<Folder[]>('/folders')
}

export function createFolder(
  name: string,
  parent_id: string | null = null,
): Promise<Folder> {
  return request<Folder>('/folders', { method: 'POST', body: { name, parent_id } })
}

/** Rename and/or move; omitted fields are left unchanged. */
export function updateFolder(
  id: string,
  patch: { name?: string; parent_id?: string | null },
): Promise<Folder> {
  return request<Folder>(`/folders/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: patch,
  })
}

export function deleteFolder(id: string): Promise<void> {
  return request<void>(`/folders/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function listTags(): Promise<TagCount[]> {
  return request<TagCount[]>('/tags')
}

/** since omitted → full sync. server_time from the response is the next since. */
export function sync(since?: string): Promise<SyncResponse> {
  return request<SyncResponse>(
    since ? `/sync?since=${encodeURIComponent(since)}` : '/sync',
  )
}

/** Result of applying the bundled starter pack (spec §5). */
export interface SeedResult {
  created: number
  updated: number
}

/**
 * Apply the bundled starter snippets (spec §5). Explicit only: the server
 * never seeds by itself, because an import merge clears deleted_at and
 * would resurrect snippets the user had deleted.
 */
export function seedStarter(): Promise<SeedResult> {
  return request<SeedResult>('/seed', { method: 'POST' })
}

/** Whether AI snippet generation is configured on the server. */
export function aiStatus(): Promise<AIStatus> {
  return request<AIStatus>('/ai/status')
}

/**
 * One-shot AI generation (no chat history): returns snippet fields to
 * review and save through the normal snippet endpoints.
 */
export function generateSnippet(input: AIGenerateInput): Promise<AIGenerateResult> {
  return request<AIGenerateResult>('/ai/generate', { method: 'POST', body: input })
}

export interface AITagSuggestInput {
  body: string
  title?: string
  language?: string
}

export interface AITagSuggestResult {
  tags: string[]
}

/** Suggest 2-3 relevant tags for the snippet (reusing existing tags). */
export function suggestTags(input: AITagSuggestInput): Promise<AITagSuggestResult> {
  return request<AITagSuggestResult>('/ai/tags', { method: 'POST', body: input })
}

export interface AIExplainResult {
  notes: string
}

/** One-shot explanation of a command, for the Notes field (spec §13). */
export function explainSnippet(body: string): Promise<AIExplainResult> {
  return request<AIExplainResult>('/ai/explain', { method: 'POST', body: { body } })
}
