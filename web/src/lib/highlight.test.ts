import { describe, expect, it } from 'vitest'
import { highlightBody } from './highlight'

describe('highlightBody', () => {
  it('highlights a known language', async () => {
    const html = await highlightBody('package main', 'go')
    expect(html).not.toBeNull()
    expect(html).toContain('hljs-keyword')
  })

  it('resolves common aliases', async () => {
    expect(await highlightBody('echo hi', 'shell')).toContain('hljs-built_in')
    expect(await highlightBody('print(1)', 'py')).toContain('hljs-built_in')
  })

  it('falls back to plain text for empty or unknown languages', async () => {
    expect(await highlightBody('x', '')).toBeNull()
    expect(await highlightBody('x', 'brainfuck')).toBeNull()
    expect(await highlightBody('', 'go')).toBeNull()
  })

  it('escapes HTML in the source (no raw script tag)', async () => {
    const html = await highlightBody('<script>alert(1)</script>', 'html')
    expect(html).not.toBeNull()
    // No raw tag survives; the angle brackets are escaped (hljs may
    // split the escaped text across token spans, so don't assume
    // contiguity).
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;')
  })
})
