import { mount } from 'svelte'
import './app.css'
import App from './App.svelte'
import { applyTextScale, applyTheme, loadSettings } from './lib/settings'
import { watchServiceWorkerUpdates } from './lib/sw'

// Apply the saved appearance (theme + text size) before first paint so
// the shell mounts already themed, in the browser and the desktop
// window alike.
const appearance = loadSettings()
applyTheme(appearance.theme)
applyTextScale(appearance.textScale)

// In the wails window, forward page errors and unhandled rejections to
// the Go log so a blank window's cause shows up under --debug. The
// wails runtime object is NOT injected when our own middleware serves
// index.html, so diagnostics go through the bound bridge method
// (window.go.desktop.App.Log) instead; in a browser this is inert.
function pageLog(message: string): void {
  const w = window as unknown as {
    go?: { desktop?: { App?: { Log?: (level: string, message: string) => void } } }
    runtime?: { LogDebug?: (message: string) => void }
  }
  w.go?.desktop?.App?.Log?.('debug', message)
  w.runtime?.LogDebug?.(message)
}

pageLog('snp shell loaded')

window.addEventListener('error', (event) => {
  const detail =
    event.message || (event.error instanceof Error ? event.error.message : String(event.error))
  pageLog(`page error: ${detail}`)
})
window.addEventListener('unhandledrejection', (event) => {
  const reason = event.reason
  const detail = reason instanceof Error ? (reason.stack ?? reason.message) : String(reason)
  pageLog(`unhandled rejection: ${detail}`)
})

// A browser or installed PWA can be running a bundle older than the one the
// server now serves, and nothing on the server side can tell: the requests
// all succeed. Re-check for a new service worker whenever the window comes
// back, and reload once when one takes over (lib/sw.ts). Inert in the wails
// window, which registers no worker.
watchServiceWorkerUpdates()

const app = mount(App, {
  target: document.getElementById('app')!,
})

// Render probe: log whether the DOM actually mounted and what the
// computed styles are, via the bridge (see pageLog). Plain timers —
// NOT requestAnimationFrame — because a webview that is not compositing
// may never fire rAF. Runs early (t0+1.2s) and again late (t0+30s, by
// which time the first sync has populated the cache).
function renderProbe(label: string): void {
  const appEl = document.getElementById('app')
  const rootEl = appEl?.firstElementChild
  const cs = rootEl ? window.getComputedStyle(rootEl) : null
  pageLog(
    `${label}: inner=${window.innerWidth}x${window.innerHeight} ` +
      `appChildren=${appEl?.childElementCount} ` +
      `bodyChildren=${document.body.childElementCount} ` +
      `bodyTextLen=${document.body.innerText.length} ` +
      `appBox=${appEl?.offsetWidth}x${appEl?.offsetHeight} ` +
      `visible=${document.visibilityState} focus=${document.hasFocus()} ` +
      `bg=${cs?.backgroundColor} color=${cs?.color} fontSize=${cs?.fontSize}`,
  )
}
setTimeout(() => renderProbe('render probe early'), 1200)
setTimeout(() => renderProbe('render probe late'), 30000)

export default app
