import { type Page } from '@playwright/test'
import { parkMouse } from '../fixtures/specs1'
import { expect, test } from '../fixtures/test'

const PROMPT = 'Update src/app.go to use the new value'

async function unseen(page: Page): Promise<number> {
  const el = page.getByTestId('inbox-unseen')
  if (!(await el.count())) return 0
  return parseInt((await el.innerText()).replace(/\D/g, ''), 10)
}

test('J10 approving a permission request lets the run continue and finish', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j10-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  await app.createTask(title, { scenario: 'permission', prompt: PROMPT })
  await app.openApp()
  await app.openTask(title)
  await parkMouse(page)
  const card = page.getByTestId('permission-card')
  await expect(card).toBeVisible()
  await expect(page.getByTestId('app-main-content')).toContainText('src/app.go')
  await page.getByTestId('chat-permission-option-0').click()
  const main = page.getByTestId('app-main-content')
  await expect(main).toContainText('Permission answer: allow_once')
  await expect(main).toContainText('Done.')
  await expect(card).toHaveCount(0)
  await expect(page.getByTestId('chat-stop-button')).toHaveCount(0)
  await expect(page.getByTestId('chat-send-button')).toBeVisible()
})

test('J11 denying a permission request ends the run without a stuck spinner', async ({ app }, testInfo) => {
  const { page } = app
  const title = `j11-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  await app.createTask(title, { scenario: 'permission', prompt: PROMPT })
  await app.openApp()
  await app.openTask(title)
  await parkMouse(page)
  await expect(page.getByTestId('permission-card')).toBeVisible()
  await page.getByTestId('chat-permission-option-2').click()
  await expect(page.getByTestId('app-main-content')).toContainText('Permission answer: reject_once')
  await expect(page.getByTestId('permission-card')).toHaveCount(0)
  await expect(page.getByTestId('chat-stop-button')).toHaveCount(0)
  await expect(page.locator('[data-testid=running-dot], [data-testid=composer-working-chip], [data-testid=run-wait]')).toHaveCount(0)
  await expect(page.getByTestId('chat-send-button')).toBeVisible()
})

test('J12/A-08 the Inbox lists pending permission rows with Open task and the badge drops after answering', async ({ app }, testInfo) => {
  const { page } = app
  const a = `j12a-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  const b = `j12b-${testInfo.project.name}-${testInfo.repeatEachIndex}`
  await app.createTask(a, { scenario: 'permission', prompt: `${PROMPT} A` })
  await app.createTask(b, { scenario: 'permission', prompt: `${PROMPT} B` })
  await app.openApp()
  await app.openDrawer()
  const inboxBtn = page.getByTestId('sidebar-footer-inbox')
  // seeded needs-permission tasks are already pending: assert on the delta, not an absolute
  await expect(inboxBtn).toContainText(/\d/)
  await inboxBtn.click()
  await expect(page.getByTestId('inbox-screen')).toBeVisible()
  await expect(page.getByTestId('inbox-unseen')).toContainText(/\d+ unseen/)
  const before = await unseen(page)
  expect(before).toBeGreaterThanOrEqual(4)

  const rowA = page.locator('[data-testid^=inbox-row-]').filter({ hasText: a }).first()
  await expect(rowA).toBeVisible()
  await expect(page.locator('[data-testid^=inbox-row-]').filter({ hasText: b }).first()).toBeVisible()

  // answer A inline from the inbox: the badge drops by one
  await rowA.locator('[data-testid^=inbox-review-]').click()
  await page.getByTestId('chat-permission-option-0').click()
  await expect.poll(() => unseen(page)).toBe(before - 1)

  // B: Open task opens that task and closes the inbox
  const rowB = page.locator('[data-testid^=inbox-row-]').filter({ hasText: b }).first()
  await expect(rowB).toBeVisible()
  await rowB.locator('[data-testid^=inbox-open-]').click()
  await expect(page.getByTestId('app-header-title').filter({ hasText: b }).first()).toBeVisible()
  await expect(page.getByTestId('inbox-screen')).toBeHidden()
})
