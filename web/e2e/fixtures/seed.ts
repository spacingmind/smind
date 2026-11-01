import fs from 'node:fs'
import path from 'node:path'
import type { Daemon } from './daemon'
import { openRpc, until } from './rpc'

export type Seeded = {
  workspaces: { alpha: number; beta: number; gamma: number }
  tasks: Record<string, number>
  runs: Record<string, string>
  permissionRequests: Record<string, string>
  queue: string[]
}

// Same state as the phase 1 harness seed. The scenario file goes into the task worktree BEFORE the prompt.
export async function seedDaemon(d: Daemon): Promise<Seeded> {
  const c = await openRpc(d.url)
  try {
    const ids: Seeded = { workspaces: { alpha: 0, beta: 0, gamma: 0 }, tasks: {}, runs: {}, permissionRequests: {}, queue: [] }
    for (const n of ['alpha', 'beta', 'gamma'] as const) {
      const w = await c.call('workspace.create', { path: d.repos[n], title: n, routingPolicy: 'worktree' })
      ids.workspaces[n] = w.ID
    }
    const A = ids.workspaces.alpha
    const mk = async (title: string, extra: Record<string, unknown> = {}) => {
      const t = await c.call('task.create', { workspaceId: A, title, ...extra })
      ids.tasks[title] = t.ID
      return t
    }
    const scen = (t: any, s: string) => fs.writeFileSync(path.join(t.WorktreePath, 'scenario'), s + '\n')
    const send = (t: any, prompt: string, extra: Record<string, unknown> = {}) => c.call('run.start', { taskId: t.ID, provider: 'uat', prompt, ...extra })
    const events = async (runId: string): Promise<any[]> => (await c.call('run.logs', { runId })).events || []
    const finished = (t: any, status = 'done') =>
      until(async () => {
        const r = (await c.call('run.list', { taskId: t.ID }))[0]
        return r && (r.status || r.Status) === status ? r : null
      }, `${t.Title} ${status}`)

    let t = await mk('idle-done')
    scen(t, 'default')
    let r = await send(t, 'Fix the retry backoff in src/routing/retry.go')
    ids.runs['idle-done'] = r.runId
    await finished(t)
    fs.appendFileSync(path.join(t.WorktreePath, 'README.md'), '\n## Notes\n\nedited by the idle-done task.\n')
    fs.writeFileSync(path.join(t.WorktreePath, 'src', 'notes.txt'), 'untracked note\n')

    t = await mk('busy-hang')
    scen(t, 'hang')
    r = await send(t, 'Start the long migration')
    ids.runs['busy-hang'] = r.runId
    await until(async () => (await events(r.runId)).length, 'busy-hang first chunk')
    const q1 = await send(t, 'Queued prompt one: then update the docs', { whenBusy: 'queue' })
    const q2 = await send(t, 'Queued prompt two: and run the full test suite', { whenBusy: 'queue' })
    if (!q1.queued || !q2.queued) throw new Error('seed: busy-hang prompts did not queue')
    ids.queue = [q1.queueItemId, q2.queueItemId]

    for (const title of ['needs-permission', 'needs-permission-2']) {
      t = await mk(title)
      scen(t, 'permission')
      r = await send(t, 'Update src/app.go to use the new value')
      ids.runs[title] = r.runId
      const req = await until(async () => (await events(r.runId)).find((e) => e.type === 'permission_request'), `${title} permission_request`)
      ids.permissionRequests[title] = req.requestId
    }

    t = await mk('errored')
    scen(t, 'error')
    r = await send(t, 'This will fail')
    ids.runs.errored = r.runId
    await finished(t, 'error')

    t = await mk('long-timeline')
    scen(t, 'long')
    r = await send(t, 'Write the full guide')
    ids.runs['long-timeline'] = r.runId
    await finished(t)

    t = await mk('archived-one')
    scen(t, 'default')
    r = await send(t, 'One small change')
    ids.runs['archived-one'] = r.runId
    await finished(t)
    await c.call('task.archive', { id: t.ID, force: true })

    const lead = await mk('lead-orchestrator')
    scen(lead, 'default')
    r = await send(lead, 'Split the work across two peers')
    ids.runs['lead-orchestrator'] = r.runId
    await finished(lead)
    for (const [title, s] of [['peer-one-routing', 'default'], ['peer-two-docs', 'slow']] as const) {
      const p = await mk(title, { parentTaskId: lead.ID })
      scen(p, s)
      const pr = await send(p, `Peer job: ${title}`)
      ids.runs[title] = pr.runId
      await finished(p)
    }

    const bt = await c.call('task.create', { workspaceId: ids.workspaces.beta, title: 'beta-first' })
    ids.tasks['beta-first'] = bt.ID
    d.ids = ids
    return ids
  } finally {
    c.close()
  }
}
