// Wire types matching the Go server (internal/store, internal/server).
// Timestamps are RFC3339 UTC, second precision, always 'Z'.

export interface Snippet {
  id: string
  title: string
  /**
   * null in list/sync responses for sensitive snippets; the body is only
   * fetched via GET /api/snippets/{id} on reveal and never stored locally.
   */
  body: string | null
  language: string
  notes: string
  folder_id: string | null
  tags: string[]
  is_sensitive: boolean
  /** True when the body contains {{var}} / {{var|default}} placeholders. */
  uses_variables: boolean
  /**
   * Per-variable default values (spec §4). null for sensitive snippets in
   * list/sync responses, like the body; omitted/absent decodes to no
   * defaults.
   */
  var_defaults?: Record<string, string> | null
  created_at: string
  updated_at: string
}

/** Request body for POST/PUT /api/snippets. id/timestamps are server-assigned. */
export interface SnippetInput {
  title: string
  body: string
  language: string
  notes: string
  folder_id: string | null
  tags: string[]
  is_sensitive: boolean
  /** True when the body contains {{var}} / {{var|default}} placeholders. */
  uses_variables: boolean
  /** Per-variable default values; omitted means the server stores an empty map. */
  var_defaults?: Record<string, string>
}

export interface Folder {
  id: string
  parent_id: string | null
  name: string
  created_at: string
  updated_at: string
  deleted_at?: string
}

export interface TagCount {
  name: string
  count: number
}

/** Soft-deleted row as returned by /api/sync. */
export interface Tombstone {
  id: string
  deleted_at: string
}

export interface SyncResponse {
  /** Captured before the read; use as the next `since`. */
  server_time: string
  folders: Array<Folder | Tombstone>
  snippets: Array<Snippet | Tombstone>
}

export interface Identity {
  login: string
  display_name: string
}

export interface SnippetSearchParams {
  /** May contain tag:x / lang:y tokens mixed with FTS terms. */
  q?: string
  /** Repeated tag= params; ANDed with tag: tokens inside q. */
  tag?: string[]
  /** Repeated lang= params; ANDed with lang: tokens inside q. */
  lang?: string[]
  folder?: string
  limit?: number
  offset?: number
}

/**
 * Error for API calls. status 0 means a network failure (server
 * unreachable / offline); other values are the HTTP status.
 */
export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }

  get isNetworkError(): boolean {
    return this.status === 0
  }
}
