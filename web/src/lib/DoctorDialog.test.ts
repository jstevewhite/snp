import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import DoctorDialog from './DoctorDialog.svelte'
import * as api from './api'

function check(
  name: string,
  status: api.DoctorStatus,
  repairable = true,
  detail = '',
): api.DoctorCheck {
  return { name, status, repairable, detail }
}

function report(checks: api.DoctorCheck[], healthy = false): api.DoctorReport {
  return {
    healthy,
    checked_at: '2026-09-25T00:00:00Z',
    schema_version: 5,
    binary_schema: 5,
    counts: { snippets: 4, trashed: 1, folders: 2, tags: 5, revisions: 3 },
    checks,
  }
}

const REBUILT: api.DoctorRepairResult = {
  cleared_sensitive_mirrors: 0,
  resynced_tag_mirrors: 0,
  rebuilt_fts: true,
}

function setup(props: { online?: boolean; saving?: boolean } = {}) {
  const onclose = vi.fn()
  const view = render(DoctorDialog, { online: true, saving: false, onclose, ...props })
  return { ...view, onclose }
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe('DoctorDialog', () => {
  it('reports a healthy library without offering a repair', async () => {
    vi.spyOn(api, 'doctor').mockResolvedValue({
      report: report(
        [check('fts', 'ok', true, 'integrity-check ok'), check('key', 'ok', false, '32 bytes')],
        true,
      ),
    })
    setup()

    await screen.findByText(/No problems found/)
    expect(screen.getByText(/4 snippets \(1 trashed\)/)).toBeDefined()
    expect(screen.queryByRole('button', { name: /^Repair/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Check again' })).toBeDefined()
  })

  it('names what a repair will touch, then reports what it changed', async () => {
    const before = report([
      check('fts', 'error', true, 'indexed terms do not match the content table'),
      check('fts_count', 'error', true, 'snippets=4 but the index holds 3 rows'),
      check('tags', 'warn', true, '1 snippet disagrees with snippet_tags'),
      check('key', 'warn', false, 'file mode is 0644, expected 0600'),
    ])
    const after = report(
      [check('fts', 'ok'), check('fts_count', 'ok'), check('tags', 'ok'), check('key', 'warn', false)],
      true,
    )
    vi.spyOn(api, 'doctor').mockResolvedValue({ report: before })
    const repair = vi.spyOn(api, 'repairDoctor').mockResolvedValue({
      report: before,
      repair: { cleared_sensitive_mirrors: 1, resynced_tag_mirrors: 2, rebuilt_fts: true },
      after,
    })
    setup()

    // The button names the failing checks it can fix; the non-repairable key
    // warning is not offered.
    const button = await screen.findByRole('button', {
      name: 'Repair fts, fts_count, tags',
    })
    await fireEvent.click(button)

    expect(repair).toHaveBeenCalledTimes(1)
    await screen.findByText(/2 → 0 errors/)
    expect(screen.getByText(/cleared 1 leaked sensitive value\(s\)/)).toBeDefined()
    expect(screen.getByText(/resynced 2 tag mirror\(s\)/)).toBeDefined()
    expect(screen.getByText(/rebuilt the search index/)).toBeDefined()
    expect(screen.getByText(/No problems found/)).toBeDefined()
  })

  it('reports findings it cannot repair, without offering a repair', async () => {
    vi.spyOn(api, 'doctor').mockResolvedValue({
      report: report([check('orphans', 'error', false, '2 live rows point at a deleted parent')]),
    })
    setup()

    await screen.findByText(/Nothing here can be repaired automatically/)
    expect(screen.queryByRole('button', { name: /^Repair/ })).toBeNull()
    expect(screen.getByText(/Problems found/)).toBeDefined()
  })

  it('does not check, or offer anything, while offline', async () => {
    const doctor = vi.spyOn(api, 'doctor')
    setup({ online: false })

    await screen.findByText(/Reconnect to check the library/)
    expect(doctor).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: /^Repair/ })).toBeNull()
  })

  it('disables the repair while one is in flight', async () => {
    vi.spyOn(api, 'doctor').mockResolvedValue({ report: report([check('fts', 'error', true)]) })
    let settle!: (value: api.DoctorResponse) => void
    vi.spyOn(api, 'repairDoctor').mockReturnValue(
      new Promise((done) => {
        settle = done
      }),
    )
    setup()

    await fireEvent.click(await screen.findByRole('button', { name: /^Repair fts/ }))
    const running = await screen.findByRole('button', { name: 'Repairing…' })
    expect((running as HTMLButtonElement).disabled).toBe(true)

    settle({ report: report([check('fts', 'ok')], true), repair: REBUILT, after: report([check('fts', 'ok')], true) })
    await screen.findByText(/rebuilt the search index/)
  })

  it('shows a repair failure without losing the report', async () => {
    vi.spyOn(api, 'doctor').mockResolvedValue({ report: report([check('fts', 'error', true)]) })
    vi.spyOn(api, 'repairDoctor').mockRejectedValue(
      new Error('A repair is already running. Try again shortly.'),
    )
    setup()

    await fireEvent.click(await screen.findByRole('button', { name: /^Repair fts/ }))
    expect((await screen.findByRole('alert')).textContent).toContain('A repair is already running.')
    // The report survives the failure, so the checks are still readable.
    expect(screen.getByText(/Problems found/)).toBeDefined()
    expect(screen.getByRole('button', { name: /^Repair fts/ })).toBeDefined()
  })

  it('closes from the Close control', async () => {
    vi.spyOn(api, 'doctor').mockResolvedValue({ report: report([], true) })
    const { onclose } = setup()

    await fireEvent.click(screen.getByRole('button', { name: 'Close health check' }))
    expect(onclose).toHaveBeenCalledTimes(1)
  })
})
