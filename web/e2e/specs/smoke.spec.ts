import { expect, test, waitForRpc } from '../fixtures/test'

test('J04/J06/J07 smoke: workspaces, new task with the UAT agent, prompt, timeline', async ({ app }) => {
  const { page } = app
  await app.openApp()

  // J04: the seeded workspaces are listed (the drawer holds the sidebar at 390)
  await app.openDrawer()
  const sidebar = page.getByTestId('sidebar-quick-actions').first()
  await expect(sidebar).toBeVisible()
  for (const ws of ['alpha', 'beta', 'gamma']) {
    await expect(page.getByText(ws, { exact: true }).first()).toBeVisible()
  }

  // J06: New task through the dialog (the quick action targets the selected task's workspace)
  await app.openTask('idle-done')
  await app.openDrawer()
  await page.getByTestId('sidebar-quick-new-task').click()
  const title = `smoke-${test.info().project.name}`
  await page.getByTestId('dialog-new-task-title-input').fill(title)
  await expect(page.getByRole('dialog').last()).toContainText('in alpha')
  await page.getByTestId('dialog-new-task-submit').click()
  await expect(page.getByTestId('app-header-title').filter({ hasText: title }).first()).toBeVisible()

  // J07: pick the UAT Agent (the default provider must never be sent with), then send
  const chip = page.getByTestId('composer-model-picker').first()
  if (!/uat/i.test(((await chip.innerText()) + ' ' + ((await chip.getAttribute('aria-label')) ?? '')))) {
    await chip.click()
    await page.getByTestId('model-picker-back').click({ timeout: 1500 }).catch(() => {})
    await page.getByTestId('model-provider-uat').click()
    await page.getByTestId('model-row-uat:').click()
    await page.keyboard.press('Escape').catch(() => {})
    await expect(page.getByTestId('model-picker-popover').or(page.getByTestId('model-picker-sheet')).first()).toBeHidden()
  }
  await expect(chip).toHaveText(/uat/i)
  const prompt = page.locator('[aria-label="Prompt"]').first()
  await prompt.fill('Fix the retry backoff in src/routing/retry.go')
  const started = waitForRpc(page, 'run.start')
  await page.getByTestId('chat-send-button').or(page.getByTestId('home-send-button')).first().click()
  await started
  // the run ends: Send is back and Stop is gone
  await expect(page.getByTestId('chat-send-button')).toBeVisible({ timeout: 30_000 })
  await expect(page.getByTestId('chat-stop-button')).toHaveCount(0)

  const main = page.getByTestId('app-main-content')
  await expect(main).toContainText(/retry backoff/i)
  await expect(main.locator('table').first()).toBeVisible()
  await expect(main.locator('pre').first()).toBeVisible()
  await expect(page.getByTestId('tool-call-toggle')).toHaveCount(5)
  await expect(main).toContainText(/failure|failed/i)
})
