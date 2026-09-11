// Client-side mirror of store.ParseQuery (internal/store/search.go).
// The same syntax must work online (server FTS5) and offline (local index),
// so the tokenization rules are copied, not approximated.

export interface ParsedQuery {
  /** FTS terms joined by single spaces; '' when no terms. */
  text: string
  tags: string[]
  langs: string[]
}

// Mirror the server regexes exactly: `^tag:(.+)$` requires the literal
// prefix `tag:` and at least one character after the colon, so a bare
// `tag:` is an FTS term, not a filter. (Tokens like `tags:x` don't match
// either — the colon must come right after `tag`.)
const tagFilterRe = /^tag:(.+)$/
const langFilterRe = /^lang:(.+)$/

export function parseQuery(q: string): ParsedQuery {
  const text: string[] = []
  const tags: string[] = []
  const langs: string[] = []
  for (const tok of q.split(/\s+/)) {
    if (tok === '') continue
    const tag = tok.match(tagFilterRe)
    if (tag) {
      tags.push(tag[1])
      continue
    }
    const lang = tok.match(langFilterRe)
    if (lang) {
      langs.push(lang[1])
      continue
    }
    text.push(tok)
  }
  return { text: text.join(' '), tags, langs }
}
