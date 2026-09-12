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
 * - The remaining terms are ANDed, and each is a *prefix* query: a snippet
 *   must have every term as the start of some token in title (boost 10),
 *   notes, body, or tags — the columns FTS5 indexes. The server builds
 *   `"term"*` per term and FTS5 ANDs them, so both engines match the same
 *   set (spec §6). MiniSearch defaults to OR, so `combineWith: 'AND'` is
 *   set explicitly.
 * - No remaining terms → every snippet matching the filters, ordered
 *   `updated_at DESC, id DESC` (the server's non-FTS ordering).
 * - With terms → relevance order (the server uses bm25 with the title
 *   weighted highest). Ranking is per-engine, so the two *orders* can
 *   differ even though the matching set does not.
 *
 * Neither engine supports FTS5 operators: the server quotes every term,
 * so `AND`, `OR`, `NOT`, `NEAR` and `title:x` are literal words. One rule
 * is left — prefix, case-insensitive, all terms — and that is what keeps
 * online and offline agreeing. An older version of this file claimed
 * MiniSearch ORed "like FTS5"; FTS5 ANDs, and that mismatch is what the
 * shared fixture in search.test.ts now guards against.
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
      // Mirrors the server: every term is a prefix query (`"term"*` on the
      // server), terms are ANDed, and the title is weighted highest.
      searchOptions: { boost: { title: 10 }, fuzzy: 0, prefix: true, combineWith: 'AND' },
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
    if (parsed.terms.length > 0) {
      // Keep MiniSearch's relevance order. The terms are re-joined and
      // MiniSearch tokenizes them with the same rules it indexed with
      // (the server sees the same single spaces between terms).
      base = this.ms
        .search(parsed.terms.join(' '))
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
    if (parsed.terms.length === 0) base.sort(byUpdatedThenIdDesc)
    return base
  }
}

function byUpdatedThenIdDesc(a: Snippet, b: Snippet): number {
  if (a.updated_at !== b.updated_at) return a.updated_at < b.updated_at ? 1 : -1
  return a.id < b.id ? 1 : -1
}
