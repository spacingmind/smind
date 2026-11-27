import { execFileSync } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { type App, expect, test } from '../fixtures/test'
import { openRpc } from '../fixtures/rpc'

// C1 (docs/plans/active/create-project-c1.md): the "Add project" dialog on the real daemon the harness
// starts. Every case drives the dialog the way a person reaches it -- the sidebar footer's "Add workspace"
// button at desktop sizes, the `smind:new-workspace` window event at 390 where the sidebar is a closed
// sheet -- and reads the daemon's own state back over a second WebSocket (the way the fixtures' seed
// talks to it) where the assertion is about the daemon, not the DOM. Frames only when C1_FRAMES_DIR is set.

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

async function shot(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.C1_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await settled(page)
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
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

test('UC-C1-01 opens from the sidebar with the host chip and the live method', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await openDialog(app)

  await expect(page.getByTestId('add-project-host-chip')).toHaveText(/^This server/)
  await expect(page.getByTestId('add-project-method-open')).toHaveAttribute('aria-pressed', 'true')
  for (const id of ['add-project-method-clone', 'add-project-method-new', 'add-project-method-session']) {
    const card = page.getByTestId(id)
    await expect(card).toHaveAttribute('aria-disabled', 'true')
    await expect(card).toContainText('Coming soon')
  }
  await shot(page, testInfo, 'UC-C1-01')
})

test('UC-C1-02 a real git repo is added and the done state offers Start a task', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const repo = gitRepo(path.join(daemon.home, 'fresh-repo'))
  await app.openApp()
  await openDialog(app)

  await typePath(page, repo)
  await expect(page.getByTestId('add-project-path-status')).toContainText('Git repository.')
  await shot(page, testInfo, 'UC-C1-02-git')

  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-done')).toContainText('fresh-repo is ready.')
  await shot(page, testInfo, 'UC-C1-02-done')

  // Start a task opens the existing New task dialog for that workspace, and the Add project dialog is gone.
  await page.getByTestId('add-project-start-task').click()
  const newTask = page.getByRole('dialog', { name: 'New task' })
  await expect(newTask).toBeVisible()
  await expect(newTask).toContainText('A task worktree in fresh-repo.')
  await expect(page.getByTestId('add-project-dialog')).toHaveCount(0)
  await page.getByTestId('dialog-new-task-cancel').click()
  await expect(page.getByTestId('dialog-new-task-title-input')).toHaveCount(0)

  // The project is in the sidebar (the done state already selected and scrolled it into view).
  await expect(page.getByTestId('sidebar-workspace-row').filter({ hasText: 'fresh-repo' })).toBeVisible()
})

test('UC-C1-03 recents show after creating and a removed project stays', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const repo = gitRepo(path.join(daemon.home, 'removed-repo'))
  await app.openApp()
  await openDialog(app)

  await typePath(page, repo)
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-done')).toContainText('removed-repo is ready.')
  await page.getByTestId('add-project-done-close').click()
  await expect(page.getByTestId('add-project-dialog')).toHaveCount(0)

  // The project is removed from the daemon over a second WebSocket (no spec deletes through the UI yet).
  const made = (await daemonWorkspaces(daemon.url)).find((w) => w.Path === repo)
  expect(made).toBeDefined()
  await deleteWorkspace(daemon.url, made!.ID)

  // The removed project stays in recents (C1 D4): the row appears once the tree has dropped it.
  await openDialog(app)
  const row = page.getByTestId('add-project-recent-0')
  await expect(row).toContainText('removed-repo')
  await expect(row).toContainText(repo)
  await shot(page, testInfo, 'UC-C1-03-recents')

  // The x removes the recents entry alone: the folder is still on disk.
  await page.getByTestId('add-project-recent-remove-0').click()
  await expect(page.getByTestId('add-project-recents')).toHaveCount(0)
  expect(fs.existsSync(repo)).toBe(true)
})

test('UC-C1-04 non-git folder shows the hint before submit', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const folder = path.join(daemon.home, 'plain-folder')
  fs.mkdirSync(folder, { recursive: true })
  await app.openApp()
  await openDialog(app)

  await typePath(page, folder)
  const hint = page.getByTestId('add-project-hint-not-git')
  await expect(hint).toBeVisible()
  await expect(hint).toContainText('Not a git repository')
  await shot(page, testInfo, 'UC-C1-04-hint')
  // A known non-repo never reaches the daemon, and there is no git-init action (that is C2).
  await expect(page.getByTestId('add-project-submit')).toBeDisabled()
  await expect(page.getByRole('button', { name: /git init/i })).toHaveCount(0)
})

test('UC-C1-05 a duplicate path opens the existing project', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)

  // beta is one of the harness's seeded workspaces.
  await typePath(page, daemon.repos.beta)
  await expect(page.getByTestId('add-project-hint-duplicate')).toContainText('Already a project: beta.')
  await shot(page, testInfo, 'UC-C1-05-duplicate')
  await expect(page.getByTestId('add-project-submit')).toHaveText('Open project')
  await page.getByTestId('add-project-submit').click()

  await expect(page.getByTestId('add-project-done')).toContainText('beta is already a project.')
  // Find-or-switch sent no create: still exactly one workspace on that path.
  expect((await daemonWorkspaces(daemon.url)).filter((w) => w.Path === daemon.repos.beta)).toHaveLength(1)
})

test('UC-C1-06 tilde is expanded', async ({ app }, testInfo) => {
  const { page, daemon } = app
  // The harness daemon runs with HOME = its temp home, so `~` expands there (C1 D3).
  const repo = gitRepo(path.join(daemon.home, 'tilde-repo'))
  await app.openApp()
  await openDialog(app)

  await typePath(page, '~/tilde-repo')
  await expect(page.getByTestId('add-project-path-status')).toContainText('Git repository.')
  await page.getByTestId('add-project-submit').click()
  await expect(page.getByTestId('add-project-done')).toContainText('tilde-repo is ready.')

  // The daemon never sees the `~`: the created workspace carries the absolute path.
  const made = (await daemonWorkspaces(daemon.url)).find((w) => w.Path === repo)
  expect(made).toBeDefined()
  await page.getByTestId('add-project-done-close').click()
  await expect(page.getByTestId('sidebar-workspace-row').filter({ hasText: 'tilde-repo' })).toBeVisible()
})

test('UC-C1-07 a daemon error is a sentence and the dialog stays', async ({ app }, testInfo) => {
  const { page, daemon } = app
  await app.openApp()
  await openDialog(app)

  // A path whose last segment starts with "." cannot be judged from the parent listing (dot entries are
  // hidden), so it reaches the daemon, which refuses it.
  const hidden = path.join(daemon.home, '.nope')
  await typePath(page, hidden)
  await expect(page.getByTestId('add-project-submit')).toBeEnabled()
  await page.getByTestId('add-project-submit').click()

  const error = page.getByTestId('add-project-error')
  await expect(error).toBeVisible()
  await expect(error).toContainText('no such file or directory')
  await expect(error).not.toContainText('workspace.create')
  await shot(page, testInfo, 'UC-C1-07-error')
  // The dialog stays open with the typed path kept, ready to fix and retry.
  await expect(page.getByTestId('add-project-dialog')).toBeVisible()
  await expect(page.getByTestId('add-project-path-input')).toHaveValue(hidden)
})

test('UC-C1-08 the small window is the full-bleed sheet', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await page.setViewportSize({ width: 800, height: 520 })
  await openDialog(app)

  const dialog = page.getByTestId('add-project-dialog')
  await expect(dialog).toHaveAttribute('data-sheet', 'true')
  // The dialog animates in (zoom-in-95); measure the sheet at rest, not mid-animation.
  await settled(page)
  const box = await dialog.boundingBox()
  expect(box).not.toBeNull()
  expect(Math.abs(box!.width - 800)).toBeLessThanOrEqual(2)
  expect(Math.abs(box!.height - 520)).toBeLessThanOrEqual(2)
  expect(Math.abs(box!.x)).toBeLessThanOrEqual(2)
  expect(Math.abs(box!.y)).toBeLessThanOrEqual(2)
  await shot(page, testInfo, 'UC-C1-08-sheet')
})
