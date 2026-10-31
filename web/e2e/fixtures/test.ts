import { EventEmitter } from 'node:events'
import fs from 'node:fs'
import path from 'node:path'
import { type Page, test as base, expect } from '@playwright/test'
import { type Daemon, startDaemon } from './daemon'
import { openRpc } from './rpc'
import { seedDaemon } from './seed'

export { expect }

export type App = {
  page: Page
  daemon: Daemon
  narrow: boolean
  openApp(hash?: string): Promise<void>
  /** Opens the sidebar drawer at narrow widths (no-op on desktop). */
  openDrawer(): Promise<void>
  openTask(title: string): Promise<void>
  /** Creates a task through RPC with the fake agent scenario written before any prompt. Returns its id and worktree. */
  createTask(title: string, opts?: { workspace?: 'alpha' | 'beta' | 'gamma'; scenario?: string; prompt?: string }): Promise<{ id: number; worktree: string; runId?: string }>
}

const rpcBus = new WeakMap<Page, EventEmitter>()

// Tracks every wsapi request/response pair on the page; waitForRpc() listens on it.
function trackRpc(page: Page): void {
  const bus = new EventEmitter()
  rpcBus.set(page, bus)
  page.on('websocket', (ws) => {
    if (!ws.url().includes('/ws')) return
    const sent = new Map<string, string>()
    ws.on('framesent', (f) => {
      try {
        const m = JSON.parse(String(f.payload))
        if (m.id && m.method) sent.set(m.id, m.method)
      } catch {
        /* not json */
      }
    })
    ws.on('framereceived', (f) => {
      try {
        const m = JSON.parse(String(f.payload))
        const method = m.id && sent.get(m.id)
        if (method && !m.event) bus.emit(method, m)
      } catch {
        /* not json */
      }
    })
  })
}

/** Resolves with the next response frame for `method` (never a sleep). Call it BEFORE the action that triggers the request. */
export function waitForRpc(page: Page, method: string): Promise<{ result?: unknown; error?: { message: string } }> {
  const bus = rpcBus.get(page)
  if (!bus) throw new Error('waitForRpc: page was not created by the app fixture')
  return new Promise((resolve) => bus.once(method, resolve))
}

export const test = base.extend<{ app: App }, { daemon: Daemon }>({
  daemon: [
    // eslint-disable-next-line no-empty-pattern
    async ({}, use) => {
      const d = await startDaemon()
      try {
        await seedDaemon(d)
        await use(d)
      } finally {
        await d.stop()
      }
    },
    { scope: 'worker', timeout: 120_000 },
  ],

  app: async ({ page, daemon, context }, use, testInfo) => {
    const scheme = testInfo.project.use.colorScheme === 'dark' ? 'dark' : 'light'
    const narrow = (testInfo.project.use.viewport?.width ?? 1280) < 768
    await context.addInitScript((t) => {
      try {
        localStorage.setItem('smind:theme', t)
      } catch {
        /* storage unavailable */
      }
    }, scheme)

    trackRpc(page)
    const problems: string[] = []
    const note = (kind: string, msg: string) => problems.push(`[${new Date().toISOString()}] ${kind} ${msg}`)
    page.on('console', (m) => m.type() === 'error' && note('console.error', m.text()))
    page.on('pageerror', (e) => note('pageerror', e.stack || e.message))
    page.on('requestfailed', (r) => {
      const err = r.failure()?.errorText ?? ''
      if (!/ERR_ABORTED/.test(err)) note('requestfailed', `${r.method()} ${r.url()} ${err}`)
    })
    page.on('response', (r) => r.status() >= 400 && note(`http${r.status()}`, `${r.request().method()} ${r.url()}`))

    const app: App = {
      page,
      daemon,
      narrow,
      openApp: async (hash = '') => {
        await page.goto(`${daemon.url}/${hash}`, { waitUntil: 'load' })
        await expect(page.locator('header, [data-testid="sidebar-footer"], [data-testid="settings-screen"]').first()).toBeAttached()
      },
      openDrawer: async () => {
        if (!narrow) return
        const quick = page.getByTestId('sidebar-quick-actions').first()
        if (await quick.isVisible()) return
        await page.getByRole('button', { name: 'Toggle Sidebar' }).first().click()
        await expect(quick).toBeVisible()
      },
      openTask: async (title) => {
        await app.openDrawer()
        const row = page
          .getByTestId('sidebar-task-row')
          .filter({ has: page.getByTestId('task-title').filter({ hasText: new RegExp(`^${title}$`) }) })
          .first()
        await row.click()
        await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()
        // the narrow drawer closes itself on selection; wait for it so a following openDrawer() does not race the close animation
        if (narrow) await expect(page.getByTestId('sidebar-quick-actions')).toHaveCount(0)
      },
      createTask: async (title, opts = {}) => {
        const rpc = await openRpc(daemon.url)
        try {
          const t = await rpc.call('task.create', { workspaceId: daemon.ids.workspaces[opts.workspace ?? 'alpha'], title })
          fs.writeFileSync(path.join(t.WorktreePath, 'scenario'), `${opts.scenario ?? 'default'}\n`)
          let runId: string | undefined
          if (opts.prompt) runId = (await rpc.call('run.start', { taskId: t.ID, provider: 'uat', prompt: opts.prompt })).runId
          return { id: t.ID, worktree: t.WorktreePath, runId }
        } finally {
          rpc.close()
        }
      },
    }

    await use(app)

    if (problems.length) {
      await testInfo.attach('browser-problems.log', { body: problems.join('\n') + '\n', contentType: 'text/plain' })
    }
  },
})
