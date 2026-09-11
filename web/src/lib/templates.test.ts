import { describe, expect, it } from 'vitest'
import {
  extractTemplateVars,
  hasTemplateVars,
  previewTemplate,
  renderTemplate,
} from './templates'

describe('extractTemplateVars', () => {
  it('finds simple variables', () => {
    expect(extractTemplateVars('echo {{name}}')).toEqual([
      { name: 'name', defaultValue: null },
    ])
  })

  it('finds variables with defaults', () => {
    expect(extractTemplateVars('curl {{url|http://localhost}}')).toEqual([
      { name: 'url', defaultValue: 'http://localhost' },
    ])
  })

  it('trims whitespace inside placeholders', () => {
    expect(extractTemplateVars('{{ name | spaced }}')).toEqual([
      { name: 'name', defaultValue: 'spaced' },
    ])
  })

  it('dedupes repeated variables; the first occurrence wins', () => {
    expect(extractTemplateVars('{{a|1}} ... {{a|2}}')).toEqual([
      { name: 'a', defaultValue: '1' },
    ])
  })

  it('ignores invalid variable names', () => {
    expect(extractTemplateVars('{{1abc}} {{foo bar}} {{-x}}')).toEqual([])
  })

  it('ignores unclosed placeholders', () => {
    expect(extractTemplateVars('x {{name')).toEqual([])
  })

  it('an empty default means no default', () => {
    expect(extractTemplateVars('{{a|}}')).toEqual([
      { name: 'a', defaultValue: null },
    ])
  })

  it('handles defaults containing a single brace (the first }} closes the placeholder)', () => {
    expect(extractTemplateVars('{{k|{braced}}}')).toEqual([
      { name: 'k', defaultValue: '{braced' },
    ])
  })

  it('preserves order of first appearance', () => {
    expect(extractTemplateVars('{{b}} {{a}} {{b}}')).toEqual([
      { name: 'b', defaultValue: null },
      { name: 'a', defaultValue: null },
    ])
  })
})

describe('hasTemplateVars', () => {
  it('is false for plain text', () => {
    expect(hasTemplateVars('sudo reboot')).toBe(false)
  })

  it('is true when a variable is present', () => {
    expect(hasTemplateVars('hello {{who}}')).toBe(true)
  })

  it('is false when only invalid placeholders are present', () => {
    expect(hasTemplateVars('{{1abc}}')).toBe(false)
  })
})

describe('renderTemplate', () => {
  it('fills provided values', () => {
    expect(renderTemplate('echo {{name}}', { name: 'world' })).toBe('echo world')
  })

  it('falls back to the default when the value is blank', () => {
    expect(renderTemplate('curl {{url|http://x}}', { url: '' })).toBe('curl http://x')
  })

  it('falls back to the default when the value is missing', () => {
    expect(renderTemplate('curl {{url|http://x}}', {})).toBe('curl http://x')
  })

  it('renders an empty string for a missing value without a default', () => {
    expect(renderTemplate('a {{name}} b', {})).toBe('a  b')
  })

  it('leaves invalid placeholders as literal text', () => {
    expect(renderTemplate('{{1abc}}', { '1abc': 'x' })).toBe('{{1abc}}')
  })

  it('preserves surrounding text and renders multiple vars', () => {
    expect(renderTemplate('{{a}}-{{b}}-{{c}}', { a: '1', b: '2', c: '3' })).toBe(
      '1-2-3',
    )
  })

  it('renders defaults containing a single brace', () => {
    expect(renderTemplate('x {{k|{braced}}} y', {})).toBe('x {braced} y')
  })

  it('leaves an unclosed placeholder as literal', () => {
    expect(renderTemplate('a {{b c', {})).toBe('a {{b c')
  })

  it('leaves braces that are not placeholders alone', () => {
    expect(renderTemplate('json: {"a": 1}', {})).toBe('json: {"a": 1}')
  })

  it('does not touch text after an unclosed placeholder', () => {
    expect(renderTemplate('{{oops then {{real}}', { real: 'v' })).toBe(
      '{{oops then v',
    )
  })

  it('is a no-op for bodies without placeholders', () => {
    expect(renderTemplate('sudo reboot', { x: 'y' })).toBe('sudo reboot')
  })
})

describe('previewTemplate', () => {
  it('fills provided values', () => {
    expect(previewTemplate('echo {{name}}', { name: 'world' })).toBe('echo world')
  })

  it('falls back to the default when the value is blank', () => {
    expect(previewTemplate('curl {{url|http://x}}', { url: '' })).toBe('curl http://x')
  })

  it('falls back to the default when the value is missing', () => {
    expect(previewTemplate('curl {{url|http://x}}', {})).toBe('curl http://x')
  })

  it('keeps the literal placeholder for a missing value without a default', () => {
    expect(previewTemplate('a {{name}} b', {})).toBe('a {{name}} b')
  })

  it('keeps the literal placeholder for a blank value without a default', () => {
    expect(previewTemplate('a {{name}} b', { name: '' })).toBe('a {{name}} b')
  })

  it('renders a mix of filled and unfilled variables', () => {
    expect(previewTemplate('{{a}}-{{b}}-{{c}}', { a: '1' })).toBe('1-{{b}}-{{c}}')
  })

  it('leaves invalid placeholders as literal text', () => {
    expect(previewTemplate('{{1abc}}', { '1abc': 'x' })).toBe('{{1abc}}')
  })

  it('leaves an unclosed placeholder as literal', () => {
    expect(previewTemplate('a {{b c', {})).toBe('a {{b c')
  })

  it('is a no-op for bodies without placeholders', () => {
    expect(previewTemplate('sudo reboot', { x: 'y' })).toBe('sudo reboot')
  })
})
