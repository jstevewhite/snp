/**
 * Connectivity tracking (spec §8): the browser's navigator.onLine plus
 * API fetch outcomes. navigator.onLine alone is not enough — the tailnet
 * can be down while the browser still reports online, and a fetch
 * failure is the first signal of trouble.
 *
 * Plain TypeScript (no Svelte runes) so it is unit-testable; the UI
 * layer subscribes via `subscribe` inside a `$effect`.
 */
export class OnlineTracker {
  private browserOnline: boolean
  private lastFetchFailed = false
  private listeners = new Set<(online: boolean) => void>()

  constructor() {
    this.browserOnline =
      typeof navigator === 'undefined' ? true : navigator.onLine
  }

  /** True when the browser is online AND the last fetch did not fail. */
  get online(): boolean {
    return this.browserOnline && !this.lastFetchFailed
  }

  /** Call after a successful API response. */
  fetchSucceeded(): void {
    if (this.lastFetchFailed) {
      this.lastFetchFailed = false
      this.emit()
    }
  }

  /** Call on a network-level API failure (ApiError with status 0). */
  fetchFailed(): void {
    if (!this.lastFetchFailed) {
      this.lastFetchFailed = true
      this.emit()
    }
  }

  /**
   * Track the browser's online/offline events. Call from an `$effect`
   * and return the unsubscribe as the effect's cleanup.
   */
  trackBrowser(): () => void {
    const on = () => {
      this.browserOnline = true
      this.emit()
    }
    const off = () => {
      this.browserOnline = false
      this.emit()
    }
    window.addEventListener('online', on)
    window.addEventListener('offline', off)
    return () => {
      window.removeEventListener('online', on)
      window.removeEventListener('offline', off)
    }
  }

  /**
   * Subscribe to changes of `online`; called immediately with the
   * current value. Returns an unsubscribe.
   */
  subscribe(fn: (online: boolean) => void): () => void {
    this.listeners.add(fn)
    fn(this.online)
    return () => {
      this.listeners.delete(fn)
    }
  }

  private emit(): void {
    for (const fn of [...this.listeners]) fn(this.online)
  }
}
