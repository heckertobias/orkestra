/**
 * Clipboard access that also works outside a secure context.
 *
 * The Master serves the UI over plain HTTP by design — TLS for browsers is terminated by a
 * reverse proxy — so a LAN deployment like `http://master:8080` is a documented, supported
 * mode. Outside a secure context `navigator.clipboard` is `undefined`, which used to make
 * every copy button silently do nothing (#103). Hence the legacy fallback, and hence a
 * boolean result: the caller must be able to tell the user when the copy did not happen.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  // Present but still rejectable — a denied permission or a non-user-initiated call throws.
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      /* fall through to the legacy path */
    }
  }
  return legacyCopy(text)
}

/**
 * Pre-Clipboard-API copy: select the text in an off-screen textarea and let the browser's
 * own copy command take it. `execCommand` is deprecated but remains the only synchronous
 * copy path available on plain HTTP.
 */
function legacyCopy(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  // Off-screen but still rendered and selectable: `display: none` or `visibility: hidden`
  // would make the selection — and therefore the copy — fail. `position: fixed` keeps the
  // page from scrolling to it.
  ta.style.position = 'fixed'
  ta.style.top = '0'
  ta.style.left = '0'
  ta.style.opacity = '0'
  ta.style.pointerEvents = 'none'
  document.body.appendChild(ta)
  try {
    ta.select()
    ta.setSelectionRange(0, text.length)
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    ta.remove()
  }
}
