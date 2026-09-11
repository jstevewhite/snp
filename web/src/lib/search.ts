import MiniSearch from 'minisearch'
import { parseQuery } from './query'
import type { Snippet } from './types'

/**
 * Offline search index over the locally cached snippets (spec §6),
 * mirroring the server's search semantics (spec §5, internal/store/search.go):
 *
 * - `tag:x` / `lang:x` tokens are filters, not FTS terms: tags are
 *   ANDed (the snippet must carry every tag — the server's
 *   `AND EXISTS`), langs are ORed within the list (`language IN`).
 * - The remaining tokens are ORed full-text terms over title (boost
 *   10), notes, body, and tags — the same fields FTS5 indexes.
 *   (MiniSearch's default combineWith is OR, like FTS5.)
 * - No remaining terms → every snippet matching the filters, ordered
 *   `updated_at DESC, id DESC` (the server's non-FTS ordering).
 * - With FTS terms → relevance order (the server uses bm25 with the
 *   title weighted highest).
 *
 * MiniSearch tokenizes like FTS5's unicode61 for ordinary text, but it
 * does not support FTS5's phrase/AND/NOT/NEAR operators or the
 * quoted-phrase fallback. That is an accepted difference for offline
 * mode: the same query *syntax* works both ways (spec §6), and offline
 * search is lenient where the server would return 400.
 *
 * Note: this targets minisearch v7, whose API is `add`/`remove`/
 * `replace` (not the v4 `addDocument`/`removeDocument`), with `fields`
 * as a plain string list and per-field boosting via the `boost`
 * search option.
 */

interface Doc {
  id: string
  title: string
  notes: string
  body: string
  tags: string
}

function toDoc(s: Snippet): Doc {
  return {
    id: s.id,
    title: s.title,
    notes: s.notes,
    // Sensitive bodies are null locally (never cached), so they cannot
    // match — consistent with the server, which never indexes them.
    body: s.body ?? '',
    // Space-joined so the default tokenizer yields one term per tag.
    tags: s.tags.join(' '),
  }
}

export class SnippetIndex {
  private ms: MiniSearch<Doc>
  private docs = new Map<string, Snippet>()

  constructor(initial: Snippet[] = []) {
    this.ms = new MiniSearch<Doc>({
      idField: 'id',
      fields: ['title', 'notes', 'body', 'tags'],
      // Mirrors the server: exact terms only (no fuzzy/prefix), title
      // weighted highest. These are the defaults for every search.
      searchOptions: { boost: { title: 10 }, fuzzy: 0, prefix: false },
    })
    for (const s of initial) this.upsert(s)
  }

  get size(): number {
    return this.docs.size
  }

  /** Adds or updates a snippet (idempotent upsert). */
  upsert(s: Snippet): void {
    const doc = toDoc(s)
    if (this.docs.has(s.id)) {
      // replace = discard + add with the same id (minisearch v7).
      this.ms.replace(doc)
    } else {
      this.ms.add(doc)
    }
    this.docs.set(s.id, s)
  }

  /** Removes a snippet; a no-op for unknown ids (idempotent). */
  remove(id: string): void {
    const s = this.docs.get(id)
    if (s !== undefined) {
      this.docs.delete(id)
      // remove() requires the document exactly as it was indexed.
      this.ms.remove(toDoc(s))
    }
  }

  /**
   * Search with the spec §5 query syntax. Returns matching snippets in
   * relevance order when FTS terms remain, otherwise in
   * `updated_at DESC, id DESC` order.
   */
  search(q: string): Snippet[] {
    const parsed = parseQuery(q)
    let base: Snippet[]
    if (parsed.text !== '') {
      // Keep MiniSearch's relevance order.
      base = this.ms
        .search(parsed.text)
        .map((r) => this.docs.get(r.id))
        .filter((s): s is Snippet => s !== undefined)
    } else {
      base = [...this.docs.values()]
    }
    if (parsed.tags.length > 0) {
      base = base.filter((s) => parsed.tags.every((t) => s.tags.includes(t)))
    }
    if (parsed.langs.length > 0) {
      base = base.filter((s) => parsed.langs.includes(s.language))
    }
    if (parsed.text === '') base.sort(byUpdatedThenIdDesc)
    return base
  }
}

function byUpdatedThenIdDesc(a: Snippet, b: Snippet): number {
  if (a.updated_at !== b.updated_at) return a.updated_at < b.updated_at ? 1 : -1
  return a.id < b.id ? 1 : -1
}
