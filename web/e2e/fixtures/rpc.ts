// Minimal wsapi JSON-RPC client over node's global WebSocket (the same wire the web UI speaks).
export type Rpc = { call<T = any>(method: string, params?: Record<string, unknown>): Promise<T>; close(): void }

export async function openRpc(url: string): Promise<Rpc> {
  const res = await fetch(`${url}/api/token`)
  if (!res.ok) throw new Error(`GET /api/token: ${res.status}`)
  const { token } = (await res.json()) as { token: string }
  const ws = new WebSocket(`${url.replace(/^http/, 'ws')}/ws?token=${encodeURIComponent(token)}`)
  await new Promise<void>((resolve, reject) => {
    ws.onopen = () => resolve()
    ws.onerror = () => reject(new Error('e2e rpc: websocket connect failed'))
  })
  let n = 0
  const pending = new Map<string, { resolve: (v: any) => void; reject: (e: Error) => void }>()
  ws.onmessage = (e) => {
    const m = JSON.parse(String(e.data))
    if (m.id && pending.has(m.id) && !m.event) {
      const p = pending.get(m.id)!
      pending.delete(m.id)
      if (m.error) p.reject(new Error(`${m.method ?? 'rpc'}: ${m.error.message}`))
      else p.resolve(m.result)
    }
  }
  return {
    close: () => ws.close(),
    call: (method, params = {}) =>
      new Promise((resolve, reject) => {
        const id = `r${++n}`
        pending.set(id, { resolve, reject: (e) => reject(new Error(`${method}: ${e.message.replace(/^[^:]*: /, '')}`)) })
        ws.send(JSON.stringify({ id, method, params }))
      }),
  }
}

export async function until<T>(fn: () => Promise<T | null | undefined | false>, what: string, ms = 15_000): Promise<T> {
  const t0 = Date.now()
  for (;;) {
    const v = await fn()
    if (v) return v
    if (Date.now() - t0 > ms) throw new Error(`timeout waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 150))
  }
}
