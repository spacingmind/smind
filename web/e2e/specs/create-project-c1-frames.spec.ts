import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { type App, expect, test } from '../fixtures/test'
import { openRpc } from '../fixtures/rpc'

// C1 frames (docs/plans/active/create-project-c1.md, "Frames"): one gallery shot per dialog state, light and
// dark, at 1280x800 and at 800x520 (the full-bleed sheet). Frames are written only when C1_FRAMES_DIR is set;
// without it every test stops right after the trivial "the dialog opens" assertion, so the plain suite stays
// cheap. The real daemon the harness starts drives every state except the two named stubs: fleet.host.list is
// answered locally for the host-rows frame, and workspace.create's answer is held back for the "Adding…" frame
// and forwarded to the daemon after the shot. The full behaviour behind each state is create-project-c1.spec.ts's
// job (UC-C1-01..08); this file only frames.

const GIT_ENV = { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null', GIT_CONFIG_NOSYSTEM: '1' }

/** A fresh git repo on disk: `git init` + one empty initial commit, exactly what "Open a folder" wants. */
function gitRepo(dir: string): string {
  fs.mkdirSync(dir, { recursive: true })
  execFileSync('git', ['-C', dir, 'init', '-q'], { env: GIT_ENV })
  execFileSync('git', ['-C', dir, '-c', 'user.name=t', '-c', 'user.email=t@t', 'commit', '--allow-empty', '-q', '-m', 'init'], { env: GIT_ENV })
  return dir
}

/** Opens the dialog the way a person does: the sidebar footer at desktop sizes, the window event at 390. */
async function openDialog(app: App) {
  const { page } = app
  await app.openDrawer()
  if (app.narrow) {
    // At 390 the sidebar is a closed sheet; every entry point lands on this same event.
    await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:new-workspace')))
  } else {
    await page.getByTestId('sidebar-footer-add-workspace').click()
  }
  await expect(page.getByTestId('add-project-dialog')).toBeVisible()
}

/** Types a path and leaves the input, so the folder check judges it from the parent listing (C1 D3). */
async function typePath(page: Page, value: string) {
  const input = page.getByTestId('add-project-path-input')
  await input.fill(value)
  await input.blur()
}

async function settled(page: Page) {
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
}

/** Frames start from the top: a state that loaded late can leave a dialog scroller part-way down (both dialogs count). */
async function fromTheTop(page: Page) {
  await page.evaluate(() =>
    document.querySelectorAll('[data-slot="dialog-content"] *').forEach((el) => { if (el.scrollTop > 0) el.scrollTop = 0 })
  )
}

/** One state, two frames: 1280x800 first, then the same state resized to 800x520 (the sheet), each settled and from the top. */
async function shot(page: Page, testInfo: TestInfo, state: string) {
  const dir = process.env.C1_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  const size = page.viewportSize()!
  await page.setViewportSize({ width: 1280, height: 800 })
  await settled(page)
  await fromTheTop(page)
  await page.screenshot({ path: path.join(dir, `${state}-${testInfo.project.name}-1280x800.png`) })
  await page.setViewportSize({ width: 800, height: 520 })
  await settled(page)
  await fromTheTop(page)
  await page.screenshot({ path: path.join(dir, `${state}-${testInfo.project.name}-800x520.png`) })
  await page.setViewportSize(size)
  await settled(page)
}

interface WorkspaceRow {
  ID: number
  Path: string
  Title: string
}

/** The daemon's own workspace list, over a second WebSocket (fixtures/rpc.ts, the seed's transport). */
async function daemonWorkspaces(url: string): Promise<WorkspaceRow[]> {
  const rpc = await openRpc(url)
  try {
    return await rpc.call<WorkspaceRow[]>('workspace.list')
  } finally {
    rpc.close()
  }
}

async function deleteWorkspace(url: string, id: number) {
  const rpc = await openRpc(url)
  try {
    await rpc.call('workspace.delete', { id })
  } finally {
    rpc.close()
  }
}

/** One approved fleet host, the wire shape useFleetHosts reads; only hostId/alias/label/state reach the Where step. */
const BUILD_BOX = {
  hostId: 'hbuildbox',
  alias: 'build-box',
  label: 'build-box',
  state: 'approved',
  pendingApproval: false,
  link: 'active',
  outboundEnabled: true,
  inbound: { enabled: false, grant: {} },
  createdAt: '2026-11-27T10:00:00Z',
}

/** Answers fleet.host.list locally (the one stubbed input of the host-rows frame); everything else reaches the daemon. */
async function stubFleetHosts(page: Page) {
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return server.send(m)
      }
      if (msg.method === 'fleet.host.list') return ws.send(JSON.stringify({ id: msg.id, result: { off: false, hosts: [BUILD_BOX] } }))
      server.send(m)
    })
  })
}

/** Holds every workspace.create request back from the daemon until release() forwards it, so "Adding…" stays on screen. */
async function holdWorkspaceCreate(page: Page) {
  const held: string[] = []
  let forward: (m: string) => void = () => {}
  await page.routeWebSocket(/\/ws/, (ws) => {
    const server = ws.connectToServer()
    forward = (m) => server.send(m)
    server.onMessage((m) => ws.send(m))
    ws.onMessage((m) => {
      let msg: { id?: string | number; method?: string }
      try {
        msg = JSON.parse(String(m))
      } catch {
        return forward(String(m))
      }
      if (msg.method === 'workspace.create') {
        held.push(String(m))
        return
      }
      forward(String(m))
    })
  })
  return { held: () => held.length, release: () => { for (const m of held.splice(0)) forward(m) } }
}

/** Removes the workspace a frame test created, so the next test's daemon is back to the seed (frames share a worker). */
async function cleanupWorkspace(daemon: { url: string; home: string }, repo: string) {
  const made = (await daemonWorkspaces(daemon.url)).find((w) => w.Path === repo)
  if (made) await deleteWorkspace(daemon.url, made.ID)
}

test('one-host: the dialog just opened with the host chip and the empty path', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  await expect(page.getByTestId('add-project-host-chip')).toHaveText(/^On: /)
  await expect(page.getByTestId('add-project-method-open')).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByTestId('add-project-path-input')).toHaveValue('')
  await shot(page, testInfo, 'one-host')
})

test('open-folder-empty: the Open a folder step with an empty path', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  // The spec's frame list names "the modal with one host" and "Open a folder empty" apart (it was written against a
  // paged design); D1 stacked the three steps into one view, so this frame is the same resting dialog shot under its
  // own name: the open-folder step is exactly what the freshly opened dialog shows.
  await expect(page.getByTestId('add-project-path-input')).toHaveValue('')
  await expect(page.getByTestId('add-project-path-status')).toHaveText('')
  await expect(page.getByTestId('add-project-submit')).toHaveText('Add project')
  await shot(page, testInfo, 'open-folder-empty')
})

test('fleet-rows: the current host plus a disabled fleet row (stubbed)', async ({ app }, testInfo) => {
  const { page } = app
  await stubFleetHosts(page)
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  const host = await page.evaluate(() => window.location.host)
  await expect(page.getByTestId('add-project-host-chip')).toHaveCount(0)
  await expect(page.getByTestId('add-project-host-current')).toContainText(host)
  await expect(page.getByTestId('add-project-host-current')).toContainText('This connection')
  const fleet = page.getByTestId('add-project-host-fleet-hbuildbox')
  await expect(fleet).toHaveAttribute('aria-disabled', 'true')
  await expect(fleet).toContainText('build-box')
  await expect(fleet).toContainText('Not available yet')
  await shot(page, testInfo, 'fleet-rows-stub')
})

test('browse-chooser: Browse opens the folder chooser with the Home chip', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  await page.getByTestId('add-project-browse-button').click()
  await expect(page.getByRole('dialog', { name: 'Choose a folder' })).toBeVisible()
  await expect(page.getByTestId('folder-picker-home')).toBeVisible()
  // The listing is the daemon user's home (repos/ is one of its few sub-directories), not the "…" placeholder.
  await expect(page.getByTestId('folder-row').filter({ hasText: 'repos' })).toBeVisible()
  await shot(page, testInfo, 'browse-chooser')
})

test('non-git-hint: a plain folder is said before submit', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  const folder = path.join(daemon.home, 'frames-plain')
  fs.mkdirSync(folder, { recursive: true })
  await typePath(page, folder)
  await expect(page.getByTestId('add-project-hint-not-git')).toContainText('Not a git repository')
  await expect(page.getByTestId('add-project-submit')).toBeDisabled()
  await shot(page, testInfo, 'non-git-hint')
})

test('duplicate: the path of an existing workspace says Already a project', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  // beta is one of the harness's seeded workspaces.
  await typePath(page, daemon.repos.beta)
  await expect(page.getByTestId('add-project-hint-duplicate')).toContainText('Already a project: beta.')
  await expect(page.getByTestId('add-project-submit')).toHaveText('Open project')
  await shot(page, testInfo, 'duplicate')
})

test('error: the daemon error is a sentence and the dialog stays', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  // A path whose last segment starts with "." cannot be judged from the parent listing (dot entries are
  // hidden), so it reaches the daemon, which refuses it.
  const hidden = path.join(daemon.home, '.nope')
  await typePath(page, hidden)
  await expect(page.getByTestId('add-project-submit')).toBeEnabled()
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-error')).toContainText('no such file or directory')
  await expect(page.getByTestId('add-project-dialog')).toBeVisible()
  await shot(page, testInfo, 'error')
})

test('recents: a removed project stays in Recent', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  const repo = gitRepo(path.join(daemon.home, 'frames-recent'))
  await typePath(page, repo)
  await expect(page.getByTestId('add-project-path-status')).toContainText('Git repository.')
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-done')).toContainText('frames-recent is ready.')
  await page.getByTestId('add-project-done-close').click()
  await expect(page.getByTestId('add-project-dialog')).toHaveCount(0)

  // The workspace is removed the way UC-C1-03 does: over a second WebSocket, not through the UI.
  const made = (await daemonWorkspaces(daemon.url)).find((w) => w.Path === repo)
  expect(made).toBeDefined()
  await deleteWorkspace(daemon.url, made!.ID)
  await expect(page.getByTestId('sidebar-workspace-row').filter({ hasText: 'frames-recent' })).toHaveCount(0)

  // Reopened, the removed project is the one entry in Recent (C1 D4).
  await openDialog(app)
  const row = page.getByTestId('add-project-recent-0')
  await expect(row).toContainText('frames-recent')
  await expect(row).toContainText(repo)
  await shot(page, testInfo, 'recents')
})

test('creating: Adding… while workspace.create is held (stubbed)', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const hold = await holdWorkspaceCreate(page)
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  const repo = gitRepo(path.join(daemon.home, 'frames-creating'))
  await typePath(page, repo)
  await expect(page.getByTestId('add-project-path-status')).toContainText('Git repository.')
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-submit')).toHaveText('Adding…')
  // The request has reached the hold (never the daemon) before the frame is taken.
  await expect.poll(() => hold.held()).toBe(1)
  await shot(page, testInfo, 'creating')

  // Forwarded after the shot, the held create completes: the frame showed a real in-flight submit, not a dead one.
  hold.release()
  await expect(page.getByTestId('add-project-done')).toContainText('frames-creating is ready.')
  await cleanupWorkspace(daemon, repo)
})

test('after-create: the done view with Start a task', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)
  if (!process.env.C1_FRAMES_DIR) return
  const repo = gitRepo(path.join(daemon.home, 'frames-after'))
  await typePath(page, repo)
  await expect(page.getByTestId('add-project-path-status')).toContainText('Git repository.')
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-done')).toContainText('frames-after is ready.')
  await expect(page.getByTestId('add-project-start-task')).toHaveText('Start a task')
  await shot(page, testInfo, 'after-create')
  await cleanupWorkspace(daemon, repo)
})
