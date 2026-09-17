/**
 * Clipboard access with a result the caller has to act on.
 *
 * `navigator.clipboard` is exposed only in a secure context. The Master serves the UI over
 * plain HTTP by design — TLS for browsers is terminated by a reverse proxy — so on a LAN
 * deployment the API is absent and the copy cannot succeed. Returning `false` instead of
 * throwing or quietly doing nothing is the whole point: the caller must say so, and the
 * user falls back to selecting the text by hand (#103).
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  // Present but still rejectable — a denied permission or a non-user-initiated call throws.
  if (!navigator.clipboard?.writeText) return false
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
