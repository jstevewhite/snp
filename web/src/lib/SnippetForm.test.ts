import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SnippetForm from './SnippetForm.svelte'
import * as api from './api'
import { ApiError } from './types'
import type { Folder, Snippet, SnippetInput } from './types'

// The Ask-AI control queries the server for its status; default to
// disabled so existing tests are unaffected, and override per test.
vi.mock('./api', () => ({
  aiStatus: vi.fn(async () => ({ enabled: false, model: '' })),
  generateSnippet: vi.fn(async () => ({
    title: '',
    language: '',
    body: '',
    uses_variables: false,
  })),
  suggestTags: vi.fn(async () => ({ tags: [] })),
  explainSnippet: vi.fn(async () => ({ notes: '' })),
}))
const mockedApi = vi.mocked(api)

function folder(id: string, name: string): Folder {
  return {
    id,
    name,
    parent_id: null,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

function snippet(p: Partial<Snippet> = {}): Snippet {
  return {
    id: 's1',
    title: 'Old title',
    body: 'old body',
    language: 'go',
    notes: 'old notes',
    folder_id: 'f1',
    tags: ['a'],
    is_sensitive: false,
    uses_variables: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z',
    ...p,
  }
}

afterEach(cleanup)

describe('SnippetForm', () => {
  it('submits a create payload with parsed tags and folder', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: null,
      folders: [folder('f1', 'Ops')],
      defaultFolderId: 'f1',
      onsave,
      oncancel: () => {},
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'Caddyfile' },
    })
    await fireEvent.input(screen.getByLabelText('Language'), {
      target: { value: 'go' },
    })
    await fireEvent.input(screen.getByLabelText('Tags'), {
      target: { value: 'ops, caddy , ' },
    })
    await fireEvent.change(screen.getByLabelText('Folder'), {
      target: { value: 'f1' },
    })
    await fireEvent.input(screen.getByLabelText('Body'), {
      target: { value: 'http://' },
    })
    await fireEvent.click(screen.getByText('Create'))
    expect(onsave).toHaveBeenCalledWith({
      title: 'Caddyfile',
      body: 'http://',
      language: 'go',
      notes: '',
      folder_id: 'f1',
      tags: ['ops', 'caddy'],
      is_sensitive: false,
      uses_variables: false,
      var_defaults: {},
    })
  })

  it('omits the folder when "(none)" is selected', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: null,
      folders: [folder('f1', 'Ops')],
      defaultFolderId: 'f1',
      onsave,
      oncancel: () => {},
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'X' },
    })
    await fireEvent.change(screen.getByLabelText('Folder'), {
      target: { value: '' },
    })
    await fireEvent.click(screen.getByText('Create'))
    expect(onsave.mock.calls[0][0].folder_id).toBeNull()
  })

  it('flags sensitive snippets', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: null,
      folders: [],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'API key' },
    })
    await fireEvent.click(screen.getByLabelText(/Sensitive/))
    await fireEvent.click(screen.getByText('Create'))
    expect(onsave.mock.calls[0][0].is_sensitive).toBe(true)
  })

  it('prefills from the initial snippet and submits save', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: snippet(),
      folders: [folder('f1', 'Ops')],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe(
      'Old title',
    )
    await fireEvent.click(screen.getByText('Save'))
    expect(onsave).toHaveBeenCalledWith({
      title: 'Old title',
      body: 'old body',
      language: 'go',
      notes: 'old notes',
      folder_id: 'f1',
      tags: ['a'],
      is_sensitive: false,
      uses_variables: false,
      var_defaults: {},
    } satisfies SnippetInput)
  })

  it('carries saved defaults forward when the body keeps the variable', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: snippet({
        body: 'curl {{host}}',
        uses_variables: true,
        var_defaults: { host: 'example.com', stale: 'x' },
      }),
      folders: [],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    const box = screen.getByLabelText(/Template/) as HTMLInputElement
    await vi.waitFor(() => expect(box.checked).toBe(true))
    await fireEvent.click(screen.getByText('Save'))
    // The live default is carried; the stale key (not in the body) is
    // pruned on save.
    expect(onsave.mock.calls[0][0].var_defaults).toEqual({ host: 'example.com' })
  })

  it('prunes all saved defaults when the body stops using variables', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: snippet({
        body: 'curl {{host}}',
        uses_variables: true,
        var_defaults: { host: 'example.com' },
      }),
      folders: [],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    await fireEvent.input(screen.getByLabelText('Body'), {
      target: { value: 'plain text' },
    })
    const box = screen.getByLabelText(/Template/) as HTMLInputElement
    await vi.waitFor(() => expect(box.checked).toBe(false))
    await fireEvent.click(screen.getByText('Save'))
    expect(onsave.mock.calls[0][0].var_defaults).toEqual({})
  })

  it('emits cancel', async () => {
    const oncancel = vi.fn()
    render(SnippetForm, {
      initial: null,
      folders: [],
      defaultFolderId: null,
      onsave: () => {},
      oncancel,
    })
    await fireEvent.click(screen.getByText('Cancel'))
    expect(oncancel).toHaveBeenCalled()
  })

  it('auto-checks the template flag when the body contains placeholders', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: null,
      folders: [],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    await fireEvent.input(screen.getByLabelText('Title'), {
      target: { value: 'X' },
    })
    await fireEvent.input(screen.getByLabelText('Body'), {
      target: { value: 'curl {{host}}' },
    })
    const box = screen.getByLabelText(/Template/) as HTMLInputElement
    await vi.waitFor(() => expect(box.checked).toBe(true))
    await fireEvent.click(screen.getByText('Create'))
    expect(onsave.mock.calls[0][0].uses_variables).toBe(true)
  })

  it('unchecks the template flag when the body no longer contains placeholders', async () => {
    const onsave = vi.fn()
    render(SnippetForm, {
      initial: snippet({ body: 'curl {{host}}', uses_variables: true }),
      folders: [],
      defaultFolderId: null,
      onsave,
      oncancel: () => {},
    })
    const box = screen.getByLabelText(/Template/) as HTMLInputElement
    await vi.waitFor(() => expect(box.checked).toBe(true))
    await fireEvent.input(screen.getByLabelText('Body'), {
      target: { value: 'plain text' },
    })
    await vi.waitFor(() => expect(box.checked).toBe(false))
    await fireEvent.click(screen.getByText('Save'))
    expect(onsave.mock.calls[0][0].uses_variables).toBe(false)
  })

  it('Ask-AI fills the form for review when generation succeeds', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'test-model' })
    mockedApi.generateSnippet.mockResolvedValue({
      title: 'Copy a file home',
      language: 'bash',
      body: 'cp -rf {{file}} ~/',
      notes: 'Recursively copies the file into your home directory.',
      uses_variables: true,
    })
    const onsave = vi.fn()
    render(SnippetForm, { initial: null, folders: [], onsave, oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Ask AI…')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('AI prompt'), {
      target: { value: 'give me a command to copy a file to my home dir' },
    })
    await fireEvent.click(screen.getByText('Generate'))
    await waitFor(() =>
      expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe(
        'Copy a file home',
      ),
    )
    expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe(
      'cp -rf {{file}} ~/',
    )
    expect((screen.getByLabelText('Language') as HTMLInputElement).value).toBe('bash')
    expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe(
      'Recursively copies the file into your home directory.',
    )
    expect(mockedApi.generateSnippet).toHaveBeenCalledWith({
      prompt: 'give me a command to copy a file to my home dir',
      language: undefined,
      kind: 'command',
    })
    // A templatized body flips uses_variables on, exactly like typing
    // {{vars}} by hand (spec §4), and the hint makes that state visible
    // so it is not mistaken for "unchecked".
    const template = screen.getByRole('checkbox', {
      name: /body contains/,
    }) as HTMLInputElement
    await waitFor(() => expect(template.checked).toBe(true))
    expect(screen.getByText('Detected variables: file')).toBeDefined()
    // Saving (without touching the checkbox) persists the template flag.
    await fireEvent.click(screen.getByText('Create'))
    await waitFor(() => expect(onsave).toHaveBeenCalled())
    expect(onsave.mock.calls[0][0].uses_variables).toBe(true)
    expect(screen.getByText('Generated — review and save.')).toBeDefined()
  })

  it('Ask-AI Script mode asks for a script and keeps its newlines', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'test-model' })
    const script = '#!/usr/bin/env bash\nset -euo pipefail\ncp -rf "$1" ~/'
    mockedApi.generateSnippet.mockResolvedValue({
      title: 'Back up a file',
      language: 'bash',
      body: script,
      notes: 'Run it as ./backup.sh <file>.',
      uses_variables: false,
    })
    render(SnippetForm, { initial: null, folders: [], onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Ask AI…')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('AI prompt'), {
      target: { value: 'a script that copies a file into my home dir' },
    })
    await fireEvent.change(screen.getByLabelText('AI output type'), {
      target: { value: 'script' },
    })
    await fireEvent.click(screen.getByText('Generate'))
    await waitFor(() =>
      expect((screen.getByLabelText('Body') as HTMLTextAreaElement).value).toBe(script),
    )
    expect(mockedApi.generateSnippet).toHaveBeenCalledWith({
      prompt: 'a script that copies a file into my home dir',
      language: undefined,
      kind: 'script',
    })
    expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe(
      'Run it as ./backup.sh <file>.',
    )
  })

  it('Ask-AI surfaces errors inline and leaves the form untouched', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'test-model' })
    mockedApi.generateSnippet.mockRejectedValue(new ApiError(502, 'AI provider error'))
    render(SnippetForm, { initial: null, folders: [], onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Ask AI…')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('AI prompt'), {
      target: { value: 'something' },
    })
    await fireEvent.click(screen.getByText('Generate'))
    await waitFor(() => expect(screen.getByText('AI provider error')).toBeDefined())
    expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('')
    expect(screen.queryByText('Generated — review and save.')).toBeNull()
  })

  it('Suggest tags merges the model tags into the Tags field', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.suggestTags.mockResolvedValue({ tags: ['python', 'network'] })
    const onsave = vi.fn()
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave, oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Suggest tags')).toBeDefined())
    const btn = screen.getByText('Suggest tags').closest('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    await fireEvent.input(screen.getByLabelText('Tags'), { target: { value: 'ops' } })
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'python -m http.server' } })
    await fireEvent.click(screen.getByText('Suggest tags'))
    await waitFor(() =>
      expect((screen.getByLabelText('Tags') as HTMLInputElement).value).toBe(
        'ops, python, network',
      ),
    )
    expect(mockedApi.suggestTags).toHaveBeenCalledWith({
      body: 'python -m http.server',
      title: undefined,
      language: undefined,
    })
  })

  it('Suggest tags does not duplicate tags already present', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.suggestTags.mockResolvedValue({ tags: ['Python', 'ops'] })
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Suggest tags')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Tags'), { target: { value: 'ops' } })
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'x' } })
    await fireEvent.click(screen.getByText('Suggest tags'))
    await waitFor(() =>
      expect((screen.getByLabelText('Tags') as HTMLInputElement).value).toBe('ops, python'),
    )
  })

  it('Suggest tags surfaces errors inline', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.suggestTags.mockRejectedValue(new ApiError(502, 'AI provider error'))
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Suggest tags')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'x' } })
    await fireEvent.click(screen.getByText('Suggest tags'))
    await waitFor(() => expect(screen.getByText('AI provider error')).toBeDefined())
  })

  it('Explain replaces Notes with the model explanation', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.explainSnippet.mockResolvedValue({
      notes: 'Recursively copies the file and overwrites the destination.',
    })
    const onsave = vi.fn()
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave, oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Explain')).toBeDefined())
    const btn = screen.getByText('Explain').closest('button') as HTMLButtonElement
    expect(btn.disabled).toBe(true)
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'cp -rf {{file}} ~/' } })
    await fireEvent.input(screen.getByLabelText('Notes'), { target: { value: 'existing note' } })
    await fireEvent.click(screen.getByText('Explain'))
    await waitFor(() =>
      expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe(
        'Recursively copies the file and overwrites the destination.',
      ),
    )
    expect(mockedApi.explainSnippet).toHaveBeenCalledWith('cp -rf {{file}} ~/')
  })

  it('Undo puts back the notes Explain replaced', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.explainSnippet.mockResolvedValue({ notes: 'replacement' })
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Explain')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'x' } })
    await fireEvent.input(screen.getByLabelText('Notes'), { target: { value: 'mine' } })

    // No overwrite has happened yet, so there is nothing to undo.
    expect(screen.queryByText('Undo')).toBeNull()

    await fireEvent.click(screen.getByText('Explain'))
    await waitFor(() => expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe('replacement'))

    await fireEvent.click(screen.getByText('Undo'))
    expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe('mine')
    // Used once, then gone — it is not a general undo stack.
    expect(screen.queryByText('Undo')).toBeNull()
  })

  it('typing in Notes after Explain drops the undo affordance', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.explainSnippet.mockResolvedValue({ notes: 'replacement' })
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Explain')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'x' } })
    await fireEvent.input(screen.getByLabelText('Notes'), { target: { value: 'mine' } })
    await fireEvent.click(screen.getByText('Explain'))
    await waitFor(() => expect(screen.getByText('Undo')).toBeDefined())

    // Editing by hand gives up the undo point rather than letting Undo
    // silently discard what was just typed.
    await fireEvent.input(screen.getByLabelText('Notes'), { target: { value: 'edited' } })
    await waitFor(() => expect(screen.queryByText('Undo')).toBeNull())
    expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe('edited')
  })

  it('Explain surfaces errors inline and leaves Notes untouched', async () => {
    mockedApi.aiStatus.mockResolvedValue({ enabled: true, model: 'm' })
    mockedApi.explainSnippet.mockRejectedValue(new ApiError(502, 'AI provider error'))
    render(SnippetForm, { initial: null, folders: [], defaultFolderId: null, onsave: vi.fn(), oncancel: () => {} })
    await waitFor(() => expect(screen.getByText('Explain')).toBeDefined())
    await fireEvent.input(screen.getByLabelText('Body'), { target: { value: 'x' } })
    await fireEvent.input(screen.getByLabelText('Notes'), { target: { value: 'keep me' } })
    await fireEvent.click(screen.getByText('Explain'))
    await waitFor(() => expect(screen.getByText('AI provider error')).toBeDefined())
    expect((screen.getByLabelText('Notes') as HTMLTextAreaElement).value).toBe('keep me')
    // A failed run must not offer an undo point for a write that never
    // happened.
    expect(screen.queryByText('Undo')).toBeNull()
  })
})
