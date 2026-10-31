import { type Page } from '@playwright/test'
import { openRpc } from '../fixtures/rpc'
import { openTaskByLink } from '../fixtures/open-task'
import { type App, expect, test } from '../fixtures/test'

async function assertUat(page: Page): Promise<void> {
  await expect(page.getByTestId('composer-model-picker').first()).toHaveText(/uat/i)
}

async function queue(page: Page, text: string, n: number): Promise<void> {
  const ta = page.locator('[aria-label="Prompt"]').first()
  await ta.click()
  await ta.fill(text)
  await page.getByTestId('chat-send-button').click()
  await expect(page.getByTestId('composer-queued-item')).toHaveCount(n)
}

async function openHanging(app: App, title: string): Promise<void> {
  const t = await app.createTask(title, { scenario: 'hang', prompt: 'Start the long job' })
  await openTaskByLink(app, t, title)
  await expect(app.page.getByTestId('chat-stop-button')).toBeVisible()
}

test('J13 two prompts queued while a run hangs show as queued chips in order', async ({ app }, testInfo) => {
  const { page } = app
  await openHanging(app, `j13-${testInfo.project.name}-${testInfo.repeatEachIndex}`)
  await assertUat(page)
  await queue(page, 'Queued A: update docs', 1)
  await queue(page, 'Queued B: run tests', 2)
  const items = page.getByTestId('composer-queued-item')
  await expect(items.nth(0)).toContainText('Queued A')
  await expect(items.nth(1)).toContainText('Queued B')
})

test('J14 cancelling one queued prompt leaves the other', async ({ app }, testInfo) => {
  const { page } = app
  await openHanging(app, `j14-${testInfo.project.name}-${testInfo.repeatEachIndex}`)
  await assertUat(page)
  await queue(page, 'Queued A: update docs', 1)
  await queue(page, 'Queued B: run tests', 2)
  await page.getByRole('button', { name: /Cancel queued prompt: Queued A/ }).click()
  const items = page.getByTestId('composer-queued-item')
  await expect(items).toHaveCount(1)
  await expect(items.first()).toContainText('Queued B')
})

test('J15 stopping a hanging run leaves it stopped and the composer usable', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j15-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'hang', prompt: 'Start the long job' })
  await openTaskByLink(app, t, title)
  await expect(page.getByTestId('chat-stop-button')).toBeVisible()
  await page.getByTestId('chat-stop-button').click()
  await expect(page.getByTestId('chat-stop-button')).toHaveCount(0)
  // the run's own state (the header status lives in a hover card): stopped, not running or errored
  await expect
    .poll(async () => {
      const rpc = await openRpc(app.daemon.url)
      try {
        const r = (await rpc.call('run.list', { taskId: t.id }))[0]
        return String(r.status ?? r.Status)
      } finally {
        rpc.close()
      }
    })
    .toMatch(/^(stopped|cancel+ed)$/i)
  await expect(page.locator('[data-testid=running-dot], [data-testid=composer-working-chip], [data-testid=run-working-elapsed]')).toHaveCount(0)
  const ta = page.locator('[aria-label="Prompt"]').first()
  await ta.click()
  await ta.fill('after stop')
  await expect(page.getByTestId('chat-send-button')).toBeEnabled()
})

test('J45 the app reconnects after a daemon restart without a reload and keeps a finished timeline', async ({ app }, testInfo) => {
  const { page, daemon } = app
  const title = `j45-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const t = await app.createTask(title, { scenario: 'slow', prompt: 'Summarize the repo layout' })
  await openTaskByLink(app, t, title)
  const main = page.getByTestId('app-main-content')
  await expect(main).toContainText('Chunk 5 of 5.')
  await expect(page.getByTestId('chat-send-button')).toBeVisible()
  await page.evaluate(() => {
    ;(window as unknown as { __noReload: string }).__noReload = 'alive'
  })
  const status = page.getByTestId('app-connection-status')
  await expect(status).toHaveAttribute('data-connection', 'connected')

  const restarted = daemon.restart()
  await expect(status).not.toHaveAttribute('data-connection', 'connected')
  await restarted
  await expect(status).toHaveAttribute('data-connection', 'connected', { timeout: 30_000 })
  expect(await page.evaluate(() => (window as unknown as { __noReload?: string }).__noReload)).toBe('alive')
  await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()
  await expect(main).toContainText('Summarize the repo layout')
  await expect(main).toContainText('Chunk 5 of 5.')
})

test('J46 reloading during a hanging run keeps the task selected and restores the run state', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j46-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  await openHanging(app, title)
  const hash = await page.evaluate(() => location.hash)
  expect(hash).not.toBe('')
  await page.reload({ waitUntil: 'load' })
  await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()
  expect(await page.evaluate(() => location.hash)).toBe(hash)
  await expect(page.getByTestId('app-main-content')).toContainText('Start the long job')
  await expect(page.getByTestId('chat-stop-button')).toBeVisible()
})
