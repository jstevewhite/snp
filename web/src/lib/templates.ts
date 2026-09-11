// Template placeholders: {{name}} and {{name|default}}.
// Variable names match [A-Za-z_][A-Za-z0-9_]* (spec §4 "Templates").
// The server treats bodies as opaque text; parsing lives here.

export interface TemplateVar {
  name: string
  /** Default text after '|'; null when the placeholder has no default. */
  defaultValue: string | null
}

const NAME_RE = /^[A-Za-z_][A-Za-z0-9_]*$/

interface Placeholder {
  /** Index of the '}}' that closes the placeholder. */
  close: number
  /** null when the placeholder is not a valid variable (bad name). */
  name: string | null
  defaultValue: string | null
}

// Scan from `open` (index of a '{{') to the first '}}'. Returns null when
// the placeholder is unclosed; otherwise the parsed (possibly invalid)
// placeholder and its closing index.
function findPlaceholder(body: string, open: number): Placeholder | null {
  const close = body.indexOf('}}', open + 2)
  if (close === -1) return null
  const inner = body.slice(open + 2, close)
  const pipe = inner.indexOf('|')
  const rawName = (pipe === -1 ? inner : inner.slice(0, pipe)).trim()
  if (!NAME_RE.test(rawName)) return { close, name: null, defaultValue: null }
  let defaultValue: string | null = null
  if (pipe !== -1) {
    const d = inner.slice(pipe + 1).trim()
    defaultValue = d === '' ? null : d
  }
  return { close, name: rawName, defaultValue }
}

/**
 * List the distinct template variables in a body, in order of first
 * appearance. Invalid placeholders (bad name, unclosed) are ignored.
 */
export function extractTemplateVars(body: string): TemplateVar[] {
  const vars: TemplateVar[] = []
  const seen = new Set<string>()
  let i = 0
  for (;;) {
    const open = body.indexOf('{{', i)
    if (open === -1) break
    const ph = findPlaceholder(body, open)
    if (ph === null) break // unclosed; the rest is literal
    if (ph.name !== null && !seen.has(ph.name)) {
      seen.add(ph.name)
      vars.push({ name: ph.name, defaultValue: ph.defaultValue })
    }
    // A valid placeholder consumes through its closing '}}'. An invalid
    // one consumes only the opening '{{', so a valid placeholder nested
    // inside it (e.g. `{{oops {{real}}`) is still found.
    i = ph.name !== null ? ph.close + 2 : open + 2
  }
  return vars
}

export function hasTemplateVars(body: string): boolean {
  return extractTemplateVars(body).length > 0
}

/**
 * Fill {{name}} / {{name|default}} placeholders with values.
 * A value that is missing or blank falls back to the default; a variable
 * without a default renders as an empty string (spec §4). Invalid
 * placeholders are left as literal text.
 */
export function renderTemplate(body: string, values: Record<string, string>): string {
  let out = ''
  let i = 0
  for (;;) {
    const open = body.indexOf('{{', i)
    if (open === -1) {
      out += body.slice(i)
      break
    }
    out += body.slice(i, open)
    const ph = findPlaceholder(body, open)
    if (ph === null) {
      out += body.slice(open) // unclosed; the rest is literal
      break
    }
    if (ph.name !== null) {
      let value = values[ph.name] ?? ''
      if (value === '' && ph.defaultValue !== null) value = ph.defaultValue
      out += value
      i = ph.close + 2
    } else {
      // Invalid placeholder: keep the literal '{{' and rescan just after
      // it, so a valid placeholder nested inside is still rendered.
      out += '{{'
      i = open + 2
    }
  }
  return out
}

/**
 * Render the body for the live preview in the variables panel. Like
 * renderTemplate, a value falls back to the placeholder's default; the
 * difference is that a variable with no value and no default is left as
 * its literal {{name}} placeholder, so unfilled variables stay visible
 * instead of silently blanking out. Invalid placeholders are left as
 * literal text, as in renderTemplate.
 */
export function previewTemplate(body: string, values: Record<string, string>): string {
  let out = ''
  let i = 0
  for (;;) {
    const open = body.indexOf('{{', i)
    if (open === -1) {
      out += body.slice(i)
      break
    }
    out += body.slice(i, open)
    const ph = findPlaceholder(body, open)
    if (ph === null) {
      out += body.slice(open) // unclosed; the rest is literal
      break
    }
    if (ph.name !== null) {
      let value = values[ph.name] ?? ''
      if (value === '' && ph.defaultValue !== null) value = ph.defaultValue
      if (value === '') {
        // No value and no default: keep the original placeholder text so
        // the unfilled variable is visible in the preview.
        out += body.slice(open, ph.close + 2)
      } else {
        out += value
      }
      i = ph.close + 2
    } else {
      // Invalid placeholder: keep the literal '{{' and rescan just after
      // it, so a valid placeholder nested inside is still rendered.
      out += '{{'
      i = open + 2
    }
  }
  return out
}
