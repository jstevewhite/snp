/**
 * Wake-up triggers (spec §6): mobile OSes suspend background apps, so the
 * 5-minute timer alone is not enough. Call `fn` when the page becomes
 * visible again or the window regains focus.
 *
 * Plain TypeScript (no Svelte runes) so it is unit-testable; the UI
 * layer calls it from an `$effect`.
 */
export function onWake(fn: () => void): () => void {
  const onVisibility = (): void => {
    if (document.visibilityState === 'visible') fn()
  }
  const onFocus = (): void => {
    fn()
  }
  document.addEventListener('visibilitychange', onVisibility)
  window.addEventListener('focus', onFocus)
  return () => {
    document.removeEventListener('visibilitychange', onVisibility)
    window.removeEventListener('focus', onFocus)
  }
}
