/**
 * Helpers for turning Connect-protocol error responses into text fit for the UI.
 *
 * A Connect error body is JSON of the shape `{"code":"...","message":"..."}`, but
 * the same call sites also see plain-text bodies and thrown `Error`s, so each
 * helper degrades to returning something displayable rather than throwing.
 */

/** Unwrap a Connect-protocol JSON error body into a human-readable message. */
export function apiError(text: string): string {
  try {
    const j = JSON.parse(text)
    if (j && typeof j.message === 'string' && j.message) return j.message
  } catch { /* plain text response */ }
  return text || 'Request failed'
}

/** Best-effort message for an unknown thrown value. */
export function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/**
 * Unwrap an error that has already been stringified via `String(err)`, so the
 * JSON body is prefixed with the `Error: ` (or similar) marker that `toString()`
 * adds. Falls back to the raw string with a leading `Error: ` stripped.
 */
export function parseConnectError(raw: string): string {
  try {
    const obj = JSON.parse(raw.replace(/^.*?: /, ''))
    return obj.message ?? raw
  } catch {
    return raw.replace(/^Error: /, '')
  }
}
