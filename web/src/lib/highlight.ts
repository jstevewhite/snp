// Read-view syntax highlighting (spec §6). highlight.js core plus a
// curated set of grammars, each loaded on demand so a given page only
// pays for the languages it actually shows. An empty or unknown
// `language` falls back to plain text (the caller renders the raw body).
import hljs from 'highlight.js/lib/core'
import type { LanguageFn } from 'highlight.js'
import DOMPurify from 'dompurify'

/** Free-text language field → highlight.js grammar id. */
const ALIASES: Record<string, string> = {
  bash: 'bash',
  sh: 'bash',
  shell: 'bash',
  zsh: 'bash',
  javascript: 'javascript',
  js: 'javascript',
  node: 'javascript',
  typescript: 'typescript',
  ts: 'typescript',
  python: 'python',
  py: 'python',
  go: 'go',
  golang: 'go',
  json: 'json',
  yaml: 'yaml',
  yml: 'yaml',
  toml: 'ini',
  ini: 'ini',
  sql: 'sql',
  html: 'xml',
  xml: 'xml',
  css: 'css',
  markdown: 'markdown',
  md: 'markdown',
  c: 'c',
  cpp: 'cpp',
  'c++': 'cpp',
  rust: 'rust',
  rs: 'rust',
  java: 'java',
  php: 'php',
  ruby: 'ruby',
  rb: 'ruby',
  diff: 'diff',
  dockerfile: 'dockerfile',
  makefile: 'makefile',
  make: 'makefile',
  nginx: 'nginx',
}

// Static loader map: Vite can analyse these and code-split each grammar.
const LOADERS: Record<string, () => Promise<{ default: LanguageFn }>> = {
  bash: () => import('highlight.js/lib/languages/bash'),
  javascript: () => import('highlight.js/lib/languages/javascript'),
  typescript: () => import('highlight.js/lib/languages/typescript'),
  python: () => import('highlight.js/lib/languages/python'),
  go: () => import('highlight.js/lib/languages/go'),
  json: () => import('highlight.js/lib/languages/json'),
  yaml: () => import('highlight.js/lib/languages/yaml'),
  ini: () => import('highlight.js/lib/languages/ini'),
  sql: () => import('highlight.js/lib/languages/sql'),
  xml: () => import('highlight.js/lib/languages/xml'),
  css: () => import('highlight.js/lib/languages/css'),
  markdown: () => import('highlight.js/lib/languages/markdown'),
  c: () => import('highlight.js/lib/languages/c'),
  cpp: () => import('highlight.js/lib/languages/cpp'),
  rust: () => import('highlight.js/lib/languages/rust'),
  java: () => import('highlight.js/lib/languages/java'),
  php: () => import('highlight.js/lib/languages/php'),
  ruby: () => import('highlight.js/lib/languages/ruby'),
  diff: () => import('highlight.js/lib/languages/diff'),
  dockerfile: () => import('highlight.js/lib/languages/dockerfile'),
  makefile: () => import('highlight.js/lib/languages/makefile'),
  nginx: () => import('highlight.js/lib/languages/nginx'),
}

const registered = new Set<string>()

async function ensureLanguage(id: string): Promise<boolean> {
  if (registered.has(id)) return true
  const load = LOADERS[id]
  if (load === undefined) return false
  try {
    const mod = await load()
    hljs.registerLanguage(id, mod.default)
    registered.add(id)
    return true
  } catch {
    return false
  }
}

/**
 * Highlight `code` for the given free-text language field. Resolves to
 * sanitized HTML (highlight.js escapes the source; DOMPurify is a
 * second boundary), or null when the language is empty/unknown — the
 * caller then renders the plain body.
 */
export async function highlightBody(code: string, language: string): Promise<string | null> {
  if (code === '') return null
  const raw = language.trim().toLowerCase()
  if (raw === '') return null
  const id = ALIASES[raw] ?? raw
  if (!(await ensureLanguage(id))) return null
  try {
    const html = hljs.highlight(code, { language: id, ignoreIllegals: true }).value
    return DOMPurify.sanitize(html, { ALLOWED_TAGS: ['span'], ALLOWED_ATTR: ['class'] })
  } catch {
    return null
  }
}
