import { openTaskByLink } from '../fixtures/open-task'
import { openRpc } from '../fixtures/rpc'
import { expect, test, waitForRpc } from '../fixtures/test'

test('J19/A-02 the header terminal button opens exactly one Terminal tab and the shell works', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j19-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'slow', prompt: 'hello' })
  await openTaskByLink(app, t, title)
  const tabs = page.getByTestId('workspace-tab-terminal')
  await expect(tabs).toHaveCount(0)

  await page.getByTestId('app-header-terminal').click()
  await expect(tabs).toHaveCount(1)
  await expect(tabs.first()).toContainText('Terminal')
  const rows = page.locator('.xterm-rows:visible').first()
  await expect(rows).toContainText(/[%$#]\s*$/m, { timeout: 15_000 })

  // a second click focuses the existing tab, it does not open another
  await page.getByTestId('app-header-terminal').click()
  await expect(tabs).toHaveCount(1)

  await page.locator('.xterm-helper-textarea, [aria-label="Terminal input"]').first().focus()
  await page.keyboard.type('echo hi', { delay: 25 })
  await page.keyboard.press('Enter')
  // innerText keeps the line breaks that toContainText would collapse: the output must sit on its own line
  await expect.poll(() => rows.innerText()).toMatch(/^hi\s*$/m)
  await expect(tabs).toHaveCount(1)
})

test('J24/A-03 the diff chip above the composer opens the Diff tab', async ({ app }) => {
  const { page } = app
  // idle-done is seeded with uncommitted changes (README.md edited, src/notes.txt untracked); this only reads them
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  const chip = page.getByTestId('composer-diff-stat').first()
  await expect(chip).toHaveAttribute('aria-label', /^Open diff/)
  await chip.click()
  await expect(page.getByTestId('workspace-tab-diff')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('diff-viewer-pane')).toBeVisible()
  const list = page.getByTestId('diff-file-list')
  await expect(list).toContainText('README.md')
  await expect(list).toContainText('notes.txt')
})

test('J22/A-05 the Files tab lists folders first, then files, and hides .git', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j22-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'slow', prompt: 'hello' })
  await app.openApp(`#/workspace/${app.daemon.ids.workspaces.alpha}/task/${t.id}/files`)
  const pane = page.getByTestId('file-explorer-pane')
  await expect(pane.getByTestId('file-row').first()).toBeVisible()
  const top = pane.locator('[data-testid=dir-row], [data-testid=file-row]')
  const rows = await top.evaluateAll((els) =>
    els
      .map((e) => ({ kind: e.getAttribute('data-testid'), path: e.getAttribute('data-path') ?? '' }))
      .filter((r) => !r.path.includes('/')),
  )
  const names = rows.map((r) => r.path)
  expect(names).toContain('src')
  expect(names).toContain('README.md')
  expect(names).not.toContain('.git')
  const firstFile = rows.findIndex((r) => r.kind === 'file-row')
  const lastDir = rows.map((r) => r.kind).lastIndexOf('dir-row')
  expect(lastDir).toBeGreaterThanOrEqual(0)
  expect(firstFile).toBeGreaterThan(lastDir)
})

test('J39/B-01 archiving the selected task moves the selection off it', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j39-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const marker = `archive-me-${title}`
  const t = await app.createTask(title, { scenario: 'slow', prompt: marker })
  await openTaskByLink(app, t, title)
  await expect(page.getByTestId('chat-stop-button')).toHaveCount(0)
  await expect(page.getByTestId('app-main-content')).toContainText(marker)
  // the daemon refuses to archive a task with a running run: wait for its own view of the run, not just the UI's
  await expect
    .poll(async () => {
      const rpc = await openRpc(app.daemon.url)
      try {
        const r = (await rpc.call('run.list', { taskId: t.id }))[0] ?? {}
        return String(r.status ?? r.Status ?? '')
      } finally {
        rpc.close()
      }
    })
    .toBe('done')
  const hash = await page.evaluate(() => location.hash)

  await app.openDrawer()
  const item = page.locator(`[data-testid=sidebar-task-item][data-task-id="${t.id}"]`)
  await item.hover()
  await item.getByTestId('sidebar-task-actions-trigger').click()
  await page.getByTestId('sidebar-task-archive-action').click()
  const archived = waitForRpc(page, 'task.archive')
  await page.getByTestId('dialog-archive-task-confirm').click()
  expect((await archived).error).toBeUndefined()

  await expect(item).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => location.hash)).not.toBe(hash)
  await expect(page.getByTestId('app-header-title').filter({ hasText: title })).toHaveCount(0)
  // the selection moved to another task (or the home screen): the archived task's chat (and with it its composer) is not in the document
  await expect(page.getByTestId('app-main-content')).not.toContainText(marker)
})
