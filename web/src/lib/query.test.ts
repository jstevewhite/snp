import { describe, expect, it } from 'vitest'
import { parseQuery } from './query'

describe('parseQuery', () => {
  it('returns empty parts for an empty query', () => {
    expect(parseQuery('')).toEqual({ terms: [], tags: [], langs: [] })
  })

  it('treats plain words as search terms, in order', () => {
    expect(parseQuery('restart caddy')).toEqual({
      terms: ['restart', 'caddy'],
      tags: [],
      langs: [],
    })
  })

  it('extracts tag and lang filters mixed with terms', () => {
    expect(parseQuery('tag:ops lang:go caddy')).toEqual({
      terms: ['caddy'],
      tags: ['ops'],
      langs: ['go'],
    })
  })

  it('supports repeatable filters (ANDed server-side)', () => {
    expect(parseQuery('tag:a tag:b lang:go lang:python')).toEqual({
      terms: [],
      tags: ['a', 'b'],
      langs: ['go', 'python'],
    })
  })

  it('collapses runs of whitespace like Go strings.Fields', () => {
    expect(parseQuery('  a\t\tb  ')).toEqual({ terms: ['a', 'b'], tags: [], langs: [] })
  })

  it('a bare tag: with an empty value is a term, not a filter', () => {
    // The server regex is ^tag:(.+)$ — it requires at least one char.
    expect(parseQuery('tag:')).toEqual({ terms: ['tag:'], tags: [], langs: [] })
  })

  it('a token like tags:x is a term: the colon must follow "tag" exactly', () => {
    expect(parseQuery('tags:x')).toEqual({ terms: ['tags:x'], tags: [], langs: [] })
  })

  it('filters can appear in any order', () => {
    expect(parseQuery('caddy tag:ops restart lang:go')).toEqual({
      terms: ['caddy', 'restart'],
      tags: ['ops'],
      langs: ['go'],
    })
  })

  it('keeps filter values verbatim (no trimming, no case folding)', () => {
    expect(parseQuery('tag:Ops lang:GO')).toEqual({
      terms: [],
      tags: ['Ops'],
      langs: ['GO'],
    })
  })
})
