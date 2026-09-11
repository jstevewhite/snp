<script module lang="ts">
  /** How long the transient result label stays before reverting. */
  export const COPY_FLASH_MS = 1500
</script>

<script lang="ts">
  let {
    text,
    label = 'Copy',
    small = false,
    disabled = false,
    oncopy,
  }: {
    /** The text handed to the clipboard via oncopy. */
    text: string
    /** Resting label; the transient result replaces it briefly. */
    label?: string
    /** Compact variant for the labeled code-box headers. */
    small?: boolean
    disabled?: boolean
    /**
     * Performs the clipboard write. The returned promise settles the
     * button's feedback: a rejection reads "Copy failed" so a blocked or
     * unsupported clipboard is visible instead of silent.
     */
    oncopy: (text: string) => void | Promise<void>
  } = $props()

  type Status = 'idle' | 'copied' | 'failed'
  let status = $state<Status>('idle')
  let timer: ReturnType<typeof setTimeout> | undefined

  async function copy(): Promise<void> {
    if (disabled) return
    try {
      await oncopy(text)
      status = 'copied'
    } catch {
      status = 'failed'
    }
    clearTimeout(timer)
    timer = setTimeout(() => (status = 'idle'), COPY_FLASH_MS)
  }

  // Drop a pending revert when the button unmounts (selection change, or
  // leaving edit mode) so the timer cannot outlive it.
  $effect(() => () => clearTimeout(timer))
</script>

<button
  type="button"
  class="copy-button"
  class:small
  class:copied={status === 'copied'}
  class:failed={status === 'failed'}
  aria-live="polite"
  {disabled}
  onclick={() => void copy()}
>
  {status === 'copied' ? 'Copied.' : status === 'failed' ? 'Copy failed' : label}
</button>
