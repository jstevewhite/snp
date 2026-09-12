/**
 * Keep an installed PWA — or any long-lived tab — from running a stale app
 * shell.
 *
 * The service worker precaches `index.html` and the hashed bundles, and a
 * browser only looks for a new worker on a navigation (or when something
 * calls `register()` / `update()`). A window that is simply left open, or a
 * PWA resumed from the background, can therefore keep serving a bundle from
 * an old deploy almost indefinitely. That failure is invisible from the
 * server side — the log shows ordinary successful requests — and reads as
 * "the app is broken" rather than "the app is old", so it is worth a few
 * lines to prevent.
 *
 * Two jobs:
 *
 *  - re-check for an update whenever the window is focused or shown again;
 *  - reload once when a new worker takes control, because
 *    `registerType: 'autoUpdate'` skips waiting and claims clients, but the
 *    already-loaded page keeps executing its old script until it reloads.
 *
 * `reload` is injectable so the reload can be asserted without jsdom
 * navigation.
 */
export function watchServiceWorkerUpdates(
  reload: () => void = () => window.location.reload(),
): void {
  if (typeof navigator === 'undefined') return
  const sw = navigator.serviceWorker
  if (sw === undefined || typeof sw.addEventListener !== 'function') return

  // A first install also fires controllerchange, because the generated
  // worker claims clients; reloading then would be a pointless flash on the
  // very first visit, so only react when a worker was already in charge.
  const hadController = sw.controller !== null
  let reloading = false

  sw.addEventListener('controllerchange', () => {
    if (!hadController || reloading) return
    reloading = true
    reload()
  })

  const check = (): void => {
    // Failures are swallowed on purpose: an update check while offline is
    // normal and not worth surfacing.
    void sw
      .getRegistration()
      .then((reg) => reg?.update())
      .catch(() => {})
  }

  window.addEventListener('focus', check)
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') check()
  })
  // A tab restored from the back/forward cache never navigated, so nothing
  // has asked the browser for a new worker yet.
  check()
}
