/** Focus containment/restoration shared by dialogs, the palette and drawer. */
const stack: HTMLElement[] = []

export function modal(node: HTMLElement, enabled = true): { update(enabled: boolean): void; destroy(): void } {
  let stop: (() => void) | undefined
  function update(active: boolean): void {
    if (active === Boolean(stop)) return
    if (!active) { stop?.(); stop = undefined; return }
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    stack.push(node)
    const top = () => stack.at(-1) === node
    const controls = () => [...node.querySelectorAll<HTMLElement>(
      'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]',
    )].filter((el) => !el.closest('[hidden], [inert]'))
    const initial = () => node.querySelector<HTMLElement>('[data-modal-initial]') ?? controls().at(-1) ?? node
    queueMicrotask(() => { if (node.isConnected && top()) initial().focus() })
    function keydown(e: KeyboardEvent): void {
      if (e.key !== 'Tab' || !top()) return
      const items = controls()
      const first = items[0] ?? node
      const last = items.at(-1) ?? node
      if (!node.contains(document.activeElement)) {
        e.preventDefault(); (e.shiftKey ? last : first).focus()
      } else if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus()
      }
    }
    function focusin(e: FocusEvent): void {
      if (top() && !node.contains(e.target as Node)) initial().focus()
    }
    document.addEventListener('keydown', keydown)
    document.addEventListener('focusin', focusin)
    stop = () => {
      stack.splice(stack.indexOf(node), 1)
      document.removeEventListener('keydown', keydown)
      document.removeEventListener('focusin', focusin)
      queueMicrotask(() => {
        const current = stack.at(-1)
        if (opener?.isConnected && !opener.closest('[inert], [hidden]') && (!current || current.contains(opener))) opener.focus()
      })
    }
  }
  update(enabled)
  return { update, destroy() { update(false) } }
}
