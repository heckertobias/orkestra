// REST/Connect JSON fetch helpers.
// Generated TypeScript clients (web/gen/) are built by CI via `buf generate`
// and not committed. Until M2 integration, we use direct fetch with the Connect JSON protocol.

export const BASE = window.location.origin

export async function connectPost<T>(procedure: string, body: unknown): Promise<T> {
  const res = await fetch(`${BASE}/${procedure}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    throw new Error(`${procedure}: HTTP ${res.status} ${text}`)
  }
  return res.json() as Promise<T>
}
