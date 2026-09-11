/**
 * Clipboard writes with a fallback for engines that lack the async API.
 *
 * `navigator.clipboard.writeText` is the modern path and what browsers
 * use. It is missing in insecure contexts and some webviews — the wails
 * desktop shell's WKWebView among them — so fall back to a hidden
 * textarea plus `document.execCommand('copy')`, which those engines
 * still implement. `execCommand` is deprecated, but it is the only
 * fallback available in a webview and it is confined to this module.
 *
 * A rejection means the text did not reach the clipboard, so callers can
 * tell the user their copy failed instead of silently doing nothing.
 * Reading the clipboard back to verify is deliberately not attempted:
 * that requires a separate permission and would fail on the path we are
 * trying to support.
 */
export async function writeClipboard(text: string): Promise<void> {
  const clip = typeof navigator === 'undefined' ? undefined : navigator.clipboard
  if (clip !== undefined && typeof clip.writeText === 'function') {
    try {
      await clip.writeText(text)
      return
    } catch {
      // Permission denied or the API is present but unusable: the legacy
      // path below is the last resort, so fall through rather than fail.
    }
  }
  if (legacyCopy(text)) return
  throw new Error('Could not copy to the clipboard')
}

/**
 * Hidden-textarea copy for engines without the async clipboard API.
 * Returns false when nothing is available (jsdom, or a browser that has
 * dropped execCommand), never throws.
 */
function legacyCopy(text: string): boolean {
  if (typeof document === 'undefined' || typeof document.body === 'undefined') return false
  const el = document.createElement('textarea')
  el.value = text
  el.setAttribute('readonly', '')
  // Kept off-screen rather than display:none: a textarea that is not
  // rendered cannot be selected, and select() is what the copy reads.
  el.style.position = 'fixed'
  el.style.top = '-1000px'
  el.style.opacity = '0'
  document.body.appendChild(el)
  try {
    el.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    el.remove()
  }
}
