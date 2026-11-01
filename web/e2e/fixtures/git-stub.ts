import type { Page } from '@playwright/test'

export type DiffStat = { files: number; insertions: number; deletions: number }

/**
 * Pins the diff stat the sidebar reads for some tasks (task id -> diff). The real counts come from the daemon's git
 * checkout state, which differs between machines (a CI checkout had 0 deletions), so a contrast scan that must see
 * "+N -N", "+N -0" and "+0 -N" cannot depend on them. Rewrites `task.gitList` / `task.git` results and the `task.git`
 * push event; everything else goes to the real daemon untouched.
 */
export async function stubTaskDiffs(page: Page, diffs: Record<number, DiffStat>): Promise<void> {
  const pin = (git: any) => {
    const d = git && diffs[git.taskId]
    if (d) git.diff = { ...d }
    return git
  }
  await page.routeWebSocket(/\/ws/, (sock) => {
    const server = sock.connectToServer()
    const pending = new Map<string | number, string>()
    sock.onMessage((raw) => {
      try {
        const m = JSON.parse(String(raw))
        if (m.id && (m.method === 'task.gitList' || m.method === 'task.git')) pending.set(m.id, m.method)
      } catch {
        /* not JSON: forward as is */
      }
      server.send(raw)
    })
    server.onMessage((raw) => {
      let m: any
      try {
        m = JSON.parse(String(raw))
      } catch {
        return sock.send(raw)
      }
      const method = m.id !== undefined ? pending.get(m.id) : undefined
      if (method && m.result) {
        pending.delete(m.id)
        if (method === 'task.gitList') for (const item of m.result.items ?? []) pin(item)
        else pin(m.result)
        return sock.send(JSON.stringify(m))
      }
      if (m.event?.topic === 'task.git') pin(m.event.payload?.git)
      sock.send(JSON.stringify(m))
    })
  })
}
