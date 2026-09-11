// Markdown rendering for the Notes field (spec §6). Notes are edited as
// plain Markdown text and rendered, sanitized, in the snippet view.
// DOMPurify is the XSS boundary: model- or user-supplied notes are never
// trusted as HTML.
import { marked } from 'marked'
import DOMPurify from 'dompurify'

/** Render Markdown text to sanitized HTML. Empty input yields ''. */
export function renderMarkdown(text: string): string {
  if (text.trim() === '') return ''
  const html = marked.parse(text, { breaks: true }) as string
  return DOMPurify.sanitize(html)
}
