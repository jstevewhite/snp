// Desktop bridge (spec §12). In the wails desktop app there is no HTTP
// listener: the SPA's API calls are served in-process by the Go handler
// through the bound window.go.desktop.App.CallAPI method (see
// internal/desktop). This module detects that mode and routes the
// fetch-based api client through the bridge.
//
// Detection: the desktop asset server stamps the shell with
// window.__SNP_DESKTOP__ = true (cmd/snp-desktop injectDesktopMarker).
// In a plain browser the marker is absent and this module is inert, so
// the online/offline and PWA behavior is unchanged.

export interface DesktopCallResult {
  status: number
  contentType: string
  body: string
}

export interface DesktopApp {
  CallAPI(method: string, path: string, body: string): Promise<DesktopCallResult>
  OpenImport?(): Promise<{ name: string; text: string } | null>
  SaveEncryptedData?(kind: 'export' | 'backup', password: string): Promise<boolean>
  SaveData?(kind: 'export' | 'backup'): Promise<boolean>
}

interface DesktopWindow {
  __SNP_DESKTOP__?: boolean
  go?: { desktop?: { App?: DesktopApp } }
}

function win(): DesktopWindow | undefined {
  return typeof window === 'undefined' ? undefined : (window as unknown as DesktopWindow)
}

/** True when the page runs inside the wails desktop shell. */
export function isDesktop(): boolean {
  const w = win()
  return w?.__SNP_DESKTOP__ === true
}

/** The bridge object, when the wails runtime has bound it yet. */
export function desktopApp(): DesktopApp | undefined {
  return win()?.go?.desktop?.App
}

/**
 * Resolve the bridge. The wails runtime populates window.go during
 * startup, so a desktop call that arrives just before that (e.g. the
 * first sync) waits briefly instead of falling through to fetch, which
 * would 404 against the asset server. In a browser the marker is
 * absent and this resolves immediately to undefined.
 */
export async function resolveDesktopApp(timeoutMs = 2000): Promise<DesktopApp | undefined> {
  const existing = desktopApp()
  if (existing !== undefined || !isDesktop()) return existing
  const deadline = Date.now() + timeoutMs
  for (;;) {
    const app = desktopApp()
    if (app !== undefined) return app
    if (Date.now() >= deadline) return undefined
    await new Promise((r) => setTimeout(r, 50))
  }
}

/** One bridge call; rejects on transport-level failure. */
export async function desktopCall(
  app: DesktopApp,
  method: string,
  path: string,
  body: string,
): Promise<DesktopCallResult> {
  return app.CallAPI(method, path, body)
}

interface CloseGuard {
  Ready(): Promise<void>
  ConfirmClose(): Promise<void>
}
function closeGuard(): CloseGuard | undefined {
  return (window as unknown as { go?: { main?: { CloseGuard?: CloseGuard } } }).go?.main?.CloseGuard
}

/** Native close requests use the same in-app dirty-form guard as navigation. */
export function registerDesktopClose(onclose: () => void): () => void {
  let active = true
  window.addEventListener('snp:close-request', onclose)
  if (isDesktop()) void resolveDesktopApp().then(() => {
    if (active) void closeGuard()?.Ready()
  })
  return () => {
    active = false
    window.removeEventListener('snp:close-request', onclose)
  }
}

export async function closeDesktop(): Promise<void> {
  await closeGuard()?.ConfirmClose()
}
