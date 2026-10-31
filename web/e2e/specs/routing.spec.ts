import { refuseWebSocket } from '../fixtures/peer-sonnet'
import { expect, test } from '../fixtures/test'

test('J47 unknown task id shows Task not found (warm), then back/forward re-select tasks (B-02)', async ({ app }) => {
  const { page, daemon } = app
  const T = daemon.ids.tasks as Record<string, number>
  const hdr = page.getByTestId('app-header-title')
  const go = (hash: string) => page.evaluate((h) => (location.hash = h), hash)

  const hashHas = (id: number) => expect.poll(() => page.evaluate(() => location.hash)).toContain(`task/${id}/`)

  await app.openApp()
  // Select by clicking, like a user: the app mirrors each selection into the hash (one history entry each).
  // Assigning location.hash by hand right after a selection races that mirror write and can be overwritten by it.
  for (const name of ['idle-done', 'errored', 'long-timeline']) {
    await app.openTask(name)
    await hashHas(T[name])
  }

  await page.goBack()
  await expect(hdr.filter({ hasText: /^errored$/ })).toBeVisible()
  await hashHas(T.errored)
  await page.goBack()
  await expect(hdr.filter({ hasText: /^idle-done$/ })).toBeVisible()
  await page.goForward()
  await expect(hdr.filter({ hasText: /^errored$/ })).toBeVisible()
  expect(await page.evaluate(() => location.hash)).toContain(`task/${T.errored}`)

  // warm bad id: the previous task must not stay on screen and the hash is cleared
  await go('#/workspace/1/task/99999/chat/1')
  const nf = page.getByTestId('task-not-found')
  await expect(nf).toBeVisible()
  await expect(nf).toContainText('Task 99999 not found')
  await expect(page.getByRole('button', { name: /back to home/i })).toBeVisible()
  await expect(hdr.filter({ hasText: /^errored$/ })).toHaveCount(0)
  await expect.poll(() => page.evaluate(() => location.hash)).not.toContain('99999')
  await page.getByRole('button', { name: /back to home/i }).first().click()
  await expect(page.getByTestId('home-landing')).toBeVisible()
})

test('J47 cold load of an unknown task id shows Task not found with Back to home (B-02)', async ({ app }) => {
  const { page } = app
  await app.openApp('#/workspace/1/task/99999/chat/1')
  const nf = page.getByTestId('task-not-found')
  await expect(nf).toBeVisible()
  await expect(nf).toContainText('Task 99999 not found')
  await expect.poll(() => page.evaluate(() => location.hash)).not.toContain('99999')
  await page.getByRole('button', { name: /back to home/i }).first().click()
  await expect(page.getByTestId('home-landing')).toBeVisible()
})

test('J51 websocket refused at load shows Retry; un-refusing then Retry loads workspaces without a reload (B-03)', async ({ app }) => {
  const { page, daemon } = app
  await refuseWebSocket(page)
  await page.goto(`${daemon.url}/`, { waitUntil: 'load' })
  const toggle = page.getByRole('button', { name: 'Toggle Sidebar' }).first()
  const cant = page.getByText("Can't reach the daemon").first()
  if (app.narrow) await toggle.click()
  await expect(cant).toBeVisible({ timeout: 20_000 })
  await expect(page.getByText('Loading workspaces…')).toHaveCount(0)

  // the visible banner text never carries the access token
  const body = await page.locator('body').innerText()
  expect(body).not.toMatch(/token=(?!REDACTED)/)
  const tokenParam = await page.evaluate(() => (location.href.match(/[?&]token=([^&#]+)/) ?? [])[1] ?? '')
  if (tokenParam) expect(body).not.toContain(tokenParam)
  const bannerText = await page.locator('body').evaluate((b) => [...b.querySelectorAll('[role=alert], [role=status]')].map((e) => (e as HTMLElement).innerText).join('\n'))
  expect(bannerText).not.toMatch(/token=(?!REDACTED)/)

  await page.evaluate(() => ((window as any).__refuse = false))
  await page.getByRole('button', { name: /retry/i }).first().click()
  await expect(page.getByTestId('sidebar-workspace-row').first()).toBeVisible({ timeout: 20_000 })
  await expect(page.getByText("Can't reach the daemon")).toHaveCount(0)
})
