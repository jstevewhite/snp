import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnippetDetail from './SnippetDetail.svelte'
import type { Snippet } from './types'

function snippet(p: Partial<Snippet> = {}): Snippet {
  return {
    id: 's1',
    title: 'Caddyfile',
    body: 'http://localhost:8080',
    language: 'go',
    notes: 'keep in sync with the repo',
    folder_id: 'f1',
    tags: ['ops'],
    is_sensitive: false,
    uses_variables: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...p,
  }
}

// Every render must supply the required callbacks; no-op by default.
const noop = () => {}

function renderDetail(
  p: Partial<Snippet> = {},
  extra: {
    body?: string | null
    folderName?: string | null
    offline?: boolean
    oncopy?: (text: string) => void
    onsavedefaults?: (defaults: Record<string, string>) => Promise<boolean>
    onreveal?: () => void
    onpin?: () => void
  } = {},
) {
  return render(SnippetDetail, {
    snippet: snippet(p),
    body: extra.body ?? null,
    folderName: extra.folderName ?? null,
    offline: extra.offline ?? false,
    oncopy: extra.oncopy ?? noop,
    onedit: noop,
    onremove: noop,
    onreveal: extra.onreveal ?? noop,
    onpin: extra.onpin ?? noop,
    onsavedefaults: extra.onsavedefaults ?? (async () => true),
  })
}

afterEach(() => {
  cleanup()
  // The transient-result cases opt into fake timers; always restore.
  vi.useRealTimers()
})

/** The Save defaults button; inert while it has nothing to save. */
function saveDefaultsButton(): HTMLButtonElement {
  return screen.getByText('Save defaults').closest('button') as HTMLButtonElement
}

describe('SnippetDetail', () => {
  it('renders title, body, meta and notes', () => {
    renderDetail()
    expect(screen.getByText('Caddyfile')).toBeDefined()
    expect(screen.getByText('http://localhost:8080')).toBeDefined()
    expect(screen.getByText('#ops')).toBeDefined()
    expect(screen.getByText('keep in sync with the repo')).toBeDefined()
  })

  it('highlights the body for a known language', async () => {
    const { container } = renderDetail({ language: 'go', body: 'package main' })
    await waitFor(() => expect(container.querySelector('.hljs-keyword')).toBeTruthy())
  })

  it('renders the body as plain text for an unknown language', async () => {
    const { container } = renderDetail({ language: 'brainfuck', body: '+++.' })
    expect(screen.getByText('+++.')).toBeDefined()
    expect(container.querySelector('.hljs')).toBeNull()
  })

  it('caps a long body at ten lines and toggles Show all / Show less', async () => {
    const body = Array.from({ length: 15 }, (_, i) => `line ${i + 1}`).join('\n')
    const { container } = renderDetail({ language: 'brainfuck', body })
    const pre = container.querySelector('pre.body') as HTMLPreElement
    expect(pre.classList.contains('clamped')).toBe(true)
    const showAll = screen.getByText('Show all')
    expect(showAll.getAttribute('aria-expanded')).toBe('false')
    await fireEvent.click(showAll)
    expect(pre.classList.contains('clamped')).toBe(false)
    expect(screen.getByText('Show less')).toBeDefined()
    await fireEvent.click(screen.getByText('Show less'))
    expect(pre.classList.contains('clamped')).toBe(true)
    expect(screen.getByText('Show all')).toBeDefined()
  })

  it('caps the Rendered preview too, independently of the body', async () => {
    const body = Array.from({ length: 15 }, (_, i) => `echo {{v}} ${i + 1}`).join('\n')
    const { container } = renderDetail({ language: 'brainfuck', body, uses_variables: true })
    const bodyPre = container.querySelector('.box > pre.body') as HTMLPreElement
    const renderedPre = container.querySelector('.vars .preview') as HTMLPreElement
    expect(bodyPre.classList.contains('clamped')).toBe(true)
    expect(renderedPre.classList.contains('clamped')).toBe(true)

    // Expanding the Rendered preview must not expand the body box.
    const vars = container.querySelector('.vars') as HTMLElement
    await fireEvent.click(within(vars).getByText('Show all'))
    expect(renderedPre.classList.contains('clamped')).toBe(false)
    expect(bodyPre.classList.contains('clamped')).toBe(true)
    expect(within(vars).getByText('Show less')).toBeDefined()

    // The body box keeps its own toggle.
    const bodyBox = container.querySelector('.box') as HTMLElement
    await fireEvent.click(within(bodyBox).getByText('Show all'))
    expect(bodyPre.classList.contains('clamped')).toBe(false)
  })

  it('does not cap ten lines, even with a trailing newline', () => {
    const body = Array.from({ length: 10 }, (_, i) => `line ${i + 1}`).join('\n') + '\n'
    const { container } = renderDetail({ language: 'brainfuck', body })
    expect(container.querySelector('pre.body')?.classList.contains('clamped')).toBe(false)
    expect(screen.queryByText('Show all')).toBeNull()
  })

  it('offers no Show all for a hidden (sensitive, unrevealed) body', () => {
    const { container } = renderDetail({ is_sensitive: true, body: null, language: 'bash' })
    expect(container.querySelector('pre.body')).toBeNull()
    expect(screen.queryByText('Show all')).toBeNull()
  })

  it('renders notes as sanitized markdown', () => {
    renderDetail({
      notes: '**bold** and `code`\n- a\n- b',
    })
    expect(screen.getByText('bold').tagName).toBe('STRONG')
    expect(screen.getByText('code').tagName).toBe('CODE')
    expect(screen.getByText('a').closest('ul')).toBeTruthy()
  })

  it('emits copy, edit and remove', async () => {
    const oncopy = vi.fn()
    const onedit = vi.fn()
    const onremove = vi.fn()
    render(SnippetDetail, {
      snippet: snippet(),
      body: null,
      folderName: null,
      oncopy,
      onedit,
      onremove,
      onreveal: noop,
      onpin: noop,
      onsavedefaults: async () => true,
    })
    await fireEvent.click(screen.getByText('Copy snippet'))
    expect(oncopy).toHaveBeenCalled()
    await fireEvent.click(screen.getByText('Edit'))
    expect(onedit).toHaveBeenCalled()
    await fireEvent.click(screen.getByLabelText('Delete snippet'))
    expect(onremove).toHaveBeenCalled()
  })

  it('places the edit action above the notes', () => {
    const { container } = renderDetail()
    const actions = container.querySelector('.actions') as HTMLElement
    const notes = container.querySelector('.notes') as HTMLElement
    expect(actions).not.toBeNull()
    expect(notes).not.toBeNull()
    // Actions precede Notes in document order.
    expect(
      actions.compareDocumentPosition(notes) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('hides a sensitive body until revealed', async () => {
    const onreveal = vi.fn()
    renderDetail({ body: null, is_sensitive: true }, { onreveal })
    expect(screen.getByText('Show body')).toBeDefined()
    const copyBtn = screen.getByText('Copy snippet').closest('button') as HTMLButtonElement
    expect(copyBtn.disabled).toBe(true)
    await fireEvent.click(screen.getByText('Show body'))
    expect(onreveal).toHaveBeenCalled()
  })

  it('shows the revealed body of a sensitive snippet', () => {
    renderDetail({ body: null, is_sensitive: true }, { body: 'secret stuff' })
    expect(screen.getByText('secret stuff')).toBeDefined()
    expect(screen.queryByText('Show body')).toBeNull()
  })

  it('shows the variables panel for a template snippet', () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: true })
    expect(screen.getByText('Variables')).toBeDefined()
    expect(screen.getByLabelText('host')).toBeDefined()
  })

  it('hides the variables panel when uses_variables is false', () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: false })
    expect(screen.queryByText('Variables')).toBeNull()
  })

  it('labels the template and rendered boxes of a template snippet', () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: true })
    expect(screen.getByText('Template')).toBeDefined()
    expect(screen.getByText('Rendered')).toBeDefined()
  })

  it('does not label the body of a non-template snippet', () => {
    renderDetail({ body: 'echo hi', uses_variables: false })
    expect(screen.queryByText('Template')).toBeNull()
    expect(screen.queryByText('Rendered')).toBeNull()
  })

  it('copies the raw placeholders from the Template box', async () => {
    const oncopy = vi.fn()
    renderDetail(
      { body: 'curl {{host|example.com}}', uses_variables: true },
      { oncopy },
    )
    await fireEvent.click(screen.getByText('Copy template'))
    expect(oncopy).toHaveBeenCalledWith('curl {{host|example.com}}')
  })

  it('copies the filled-in command from the Rendered box', async () => {
    const oncopy = vi.fn()
    renderDetail(
      { body: 'curl {{host|example.com}}', uses_variables: true },
      { oncopy },
    )
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(oncopy).toHaveBeenCalledWith('curl example.com')
  })

  it('offers Copy snippet, not the box actions, for a non-template snippet', () => {
    renderDetail({ body: 'echo hi', uses_variables: false })
    expect(screen.getByText('Copy snippet')).toBeDefined()
    expect(screen.queryByText('Copy template')).toBeNull()
    expect(screen.queryByText('Copy rendered')).toBeNull()
  })

  it('shows a pin control reflecting the pinned state', async () => {
    const onpin = vi.fn()
    const { container } = renderDetail({ pinned: true }, { onpin })
    const pin = container.querySelector('.detail .pin') as HTMLButtonElement
    expect(pin.getAttribute('aria-pressed')).toBe('true')
    expect(pin.classList.contains('pinned')).toBe(true)
    expect(pin.getAttribute('aria-label')).toBe('Remove from favorites')
    await fireEvent.click(pin)
    expect(onpin).toHaveBeenCalled()
  })

  it('offers to pin an unpinned snippet, disabled offline', () => {
    const { container } = renderDetail({}, { offline: true })
    const pin = container.querySelector('.detail .pin') as HTMLButtonElement
    expect(pin.getAttribute('aria-pressed')).toBe('false')
    expect(pin.getAttribute('aria-label')).toBe('Add to favorites')
    expect(pin.disabled).toBe(true)
  })

  it('shows a decorative icon beside the language and the folder', () => {
    const { container } = renderDetail({}, { folderName: 'dev' })
    const langIcon = container.querySelector('.lang svg.icon')
    const folderIcon = container.querySelector('.folder svg.icon')
    expect(langIcon).not.toBeNull()
    expect(folderIcon).not.toBeNull()
    // Decorative: hidden from assistive tech, so the badge text is just the value.
    expect(langIcon?.getAttribute('aria-hidden')).toBe('true')
    expect(folderIcon?.getAttribute('aria-hidden')).toBe('true')
    expect(container.querySelector('.lang')?.textContent?.trim()).toBe('go')
    expect(container.querySelector('.folder')?.textContent?.trim()).toBe('dev')
  })

  it('simplifies the updated time and keeps the precise one in a tooltip', () => {
    const { container } = renderDetail()
    const when = container.querySelector('.detail .when') as HTMLElement
    expect(when.textContent).toContain('Updated ')
    // The raw RFC3339 value is not user-visible...
    expect(when.textContent).not.toContain('2026-01-02T00:00:00Z')
    // ...but is recoverable from the tooltip.
    expect(when.getAttribute('title')).toContain('2026')
  })

  it('separates the tag chips from the language and folder', () => {
    const { container } = renderDetail({ tags: ['ops', 'dev'] }, { folderName: 'dev' })
    const tags = container.querySelector('.tags') as HTMLElement
    expect(tags.classList.contains('divided')).toBe(true)
    expect(tags.querySelectorAll('.tag').length).toBe(2)
  })

  it('drops the tag divider when no language or folder precedes the tags', () => {
    const { container } = renderDetail({ language: '', tags: ['ops'] })
    expect((container.querySelector('.tags') as HTMLElement).classList.contains('divided')).toBe(
      false,
    )
  })

  it('puts the delete control in the title row, not the footer', () => {
    const { container } = renderDetail()
    const row = container.querySelector('.detail .title-row') as HTMLElement
    expect(row.querySelector('h1')?.textContent).toBe('Caddyfile')
    const trash = row.querySelector('button.trash') as HTMLButtonElement
    expect(trash).not.toBeNull()
    expect(trash.getAttribute('aria-label')).toBe('Delete snippet')
    expect(trash.disabled).toBe(false)
    // The old text Delete button is gone from the footer actions.
    expect(container.querySelector('.actions .danger')).toBeNull()
  })

  it('disables the delete control offline', () => {
    const { container } = renderDetail({}, { offline: true })
    expect((container.querySelector('button.trash') as HTMLButtonElement).disabled).toBe(
      true,
    )
  })

  it('moves the updated timestamp below the notes', () => {
    const { container } = renderDetail()
    const when = container.querySelector('.when') as HTMLElement
    const notes = container.querySelector('.notes') as HTMLElement
    expect(when.textContent).toContain('Updated')
    expect(
      notes.compareDocumentPosition(when) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()
  })

  it('shows the default value as the input placeholder', () => {
    renderDetail({ body: 'curl {{host|example.com}}', uses_variables: true })
    expect((screen.getByLabelText('host') as HTMLInputElement).placeholder).toBe(
      'example.com',
    )
  })

  it('updates the live preview as values are entered', async () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: true })
    expect(
      screen.getByText('curl {{host}}', { selector: 'pre.preview' }),
    ).toBeDefined()
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'example.com' },
    })
    expect(
      screen.getByText('curl example.com', { selector: 'pre.preview' }),
    ).toBeDefined()
  })

  it('keeps unfilled variables visible in the preview', () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: true })
    const preview = document.querySelector('pre.preview') as HTMLElement
    expect(preview.textContent).toBe('curl {{host}}')
  })

  it('copies the rendered body, falling back to the default', async () => {
    const oncopy = vi.fn()
    renderDetail({ body: 'curl {{host|example.com}}', uses_variables: true }, { oncopy })
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(oncopy).toHaveBeenCalledWith('curl example.com')
  })

  it('copies with entered values and blanks vars without a default', async () => {
    const oncopy = vi.fn()
    renderDetail(
      { body: 'curl {{host}} -H {{token}}', uses_variables: true },
      { oncopy },
    )
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'example.com' },
    })
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(oncopy).toHaveBeenCalledWith('curl example.com -H ')
  })

  it('pre-fills the inputs from saved defaults', () => {
    renderDetail({
      body: 'curl {{host}} {{port}}',
      uses_variables: true,
      var_defaults: { host: 'example.com' },
    })
    expect((screen.getByLabelText('host') as HTMLInputElement).value).toBe(
      'example.com',
    )
    // Unsaved vars stay empty; their value is not invented.
    expect((screen.getByLabelText('port') as HTMLInputElement).value).toBe('')
  })

  it('copies with saved defaults when inputs stay blank', async () => {
    const oncopy = vi.fn()
    renderDetail(
      {
        body: 'curl {{host}}',
        uses_variables: true,
        var_defaults: { host: 'example.com' },
      },
      { oncopy },
    )
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(oncopy).toHaveBeenCalledWith('curl example.com')
  })

  it('lets a saved default win over the inline default', async () => {
    const oncopy = vi.fn()
    renderDetail(
      {
        body: 'curl {{host|fallback.com}}',
        uses_variables: true,
        var_defaults: { host: 'example.com' },
      },
      { oncopy },
    )
    await fireEvent.click(screen.getByText('Copy rendered'))
    expect(oncopy).toHaveBeenCalledWith('curl example.com')
  })

  it('ignores saved defaults for variables no longer in the body', async () => {
    const onsavedefaults = vi.fn().mockResolvedValue(true)
    renderDetail(
      {
        body: 'curl {{host}}',
        uses_variables: true,
        var_defaults: { gone: 'x', host: 'example.com' },
      },
      { onsavedefaults },
    )
    // The stale key is not pre-filled, and it is not part of what a save
    // would write either — so with nothing else changed there is nothing
    // to save.
    expect(screen.queryByLabelText('gone')).toBeNull()
    expect(saveDefaultsButton().disabled).toBe(true)
    // Changing the value it does own re-enables the button, and saving
    // still prunes the stale key.
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'new.dev' },
    })
    expect(saveDefaultsButton().disabled).toBe(false)
    await fireEvent.click(screen.getByText('Save defaults'))
    expect(onsavedefaults).toHaveBeenCalledWith({ host: 'new.dev' })
  })

  it('saving defaults prunes blanks: a cleared input clears that default', async () => {
    const onsavedefaults = vi.fn().mockResolvedValue(true)
    renderDetail(
      {
        body: 'curl {{host}} {{port}}',
        uses_variables: true,
        var_defaults: { host: 'example.com', port: '8080' },
      },
      { onsavedefaults },
    )
    await fireEvent.input(screen.getByLabelText('port'), {
      target: { value: '' },
    })
    await fireEvent.click(screen.getByText('Save defaults'))
    expect(onsavedefaults).toHaveBeenCalledWith({ host: 'example.com' })
  })

  it('saving defaults keeps typed values', async () => {
    const onsavedefaults = vi.fn().mockResolvedValue(true)
    renderDetail({ body: 'curl {{host}}', uses_variables: true }, { onsavedefaults })
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'type.dev' },
    })
    await fireEvent.click(screen.getByText('Save defaults'))
    expect(onsavedefaults).toHaveBeenCalledWith({ host: 'type.dev' })
  })

  it('keeps Save defaults inert until a value differs from what is stored', async () => {
    renderDetail({
      body: 'curl {{host}}',
      uses_variables: true,
      var_defaults: { host: 'example.com' },
    })
    expect(saveDefaultsButton().disabled).toBe(true)

    // Retyping the same value is not a change.
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'example.com' },
    })
    expect(saveDefaultsButton().disabled).toBe(true)

    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'example.org' },
    })
    expect(saveDefaultsButton().disabled).toBe(false)

    // Back to the stored value: nothing to save again.
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'example.com' },
    })
    expect(saveDefaultsButton().disabled).toBe(true)
  })

  it('confirms a successful save, then goes inert again', async () => {
    vi.useFakeTimers()
    const onsavedefaults = vi.fn().mockResolvedValue(true)
    renderDetail({ body: 'curl {{host}}', uses_variables: true }, { onsavedefaults })
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'type.dev' },
    })
    await fireEvent.click(screen.getByText('Save defaults'))
    await vi.advanceTimersByTimeAsync(0)
    expect(screen.getByText('Defaults saved')).toBeDefined()
    // The saved value is now the baseline, so there is nothing left to save.
    expect(saveDefaultsButton().disabled).toBe(true)

    await vi.advanceTimersByTimeAsync(1500)
    expect(screen.queryByText('Defaults saved')).toBeNull()
    vi.useRealTimers()
  })

  it('reports a failed save and keeps offering it', async () => {
    vi.useFakeTimers()
    const onsavedefaults = vi.fn().mockResolvedValue(false)
    renderDetail({ body: 'curl {{host}}', uses_variables: true }, { onsavedefaults })
    await fireEvent.input(screen.getByLabelText('host'), {
      target: { value: 'type.dev' },
    })
    await fireEvent.click(screen.getByText('Save defaults'))
    await vi.advanceTimersByTimeAsync(0)
    expect(screen.getByText('Save failed')).toBeDefined()
    // The baseline did not move, so the same save is still on offer.
    expect(saveDefaultsButton().disabled).toBe(false)
    vi.useRealTimers()
  })

  it('disables saving defaults when offline', () => {
    renderDetail({ body: 'curl {{host}}', uses_variables: true }, { offline: true })
    const btn = screen.getByText('Save defaults').closest('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })
})
