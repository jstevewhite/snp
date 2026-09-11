import { describe, expect, it } from 'vitest'
import { renderMarkdown } from './markdown'

describe('renderMarkdown', () => {
  it('renders basic inline and block markdown', () => {
    expect(renderMarkdown('**bold** and *em*')).toContain('<strong>bold</strong>')
    expect(renderMarkdown('- one\n- two')).toContain('<ul>')
    expect(renderMarkdown('')).toBe('')
    expect(renderMarkdown('   ')).toBe('')
  })

  it('converts single newlines to line breaks (breaks: true)', () => {
    expect(renderMarkdown('line one\nline two')).toContain('<br>')
  })

  it('strips script tags and event handlers (XSS boundary)', () => {
    const out = renderMarkdown('hello <script>alert(1)</script> <img src=x onerror=alert(2)>')
    expect(out).not.toContain('<script')
    expect(out).not.toContain('onerror')
    expect(out).toContain('hello')
  })

  it('renders inline code', () => {
    expect(renderMarkdown('run `make test`')).toContain('<code>make test</code>')
  })
})
