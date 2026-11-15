import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// DT-UI B 2b: Settings, Inbox and Automations are drawn inside the shell, beside the sidebar, on desktop widths, so the
// sidebar stays and the macOS traffic lights (over the sidebar header) never sit over page content. Below the mobile
// breakpoint they still cover the window. Frames are written only when PAGE_FRAMES_DIR is set.

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.PAGE_FRAMES_DIR
  if (!dir) return
  // Narrow: the drawer sheet is closing under the page; frame the settled page.
  if ((testInfo.project.use.viewport?.width ?? 1280) < 768) await expect(page.getByTestId('sidebar-quick-actions')).toBeHidden()
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

const narrow = (testInfo: TestInfo) => (testInfo.project.use.viewport?.width ?? 1280) < 768

test('PG-01 Settings opens beside the sidebar, which stays visible and usable', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-settings-button').click()
  const settings = page.getByTestId('settings-screen')
  await expect(settings).toBeVisible()
  if (narrow(testInfo)) {
    await expect(page.getByTestId('app-shell')).toHaveAttribute('inert', '')
  } else {
    const sidebar = page.getByTestId('sidebar-content')
    await expect(sidebar).toBeVisible()
    const [s, p] = await Promise.all([sidebar.boundingBox(), settings.boundingBox()])
    expect(s && p && p.x >= s.x + s.width - 1).toBe(true) // the page starts where the sidebar ends
    await page.getByTestId('sidebar-task-item').first().click()
    await expect(settings).toHaveCount(0)
  }
})

test('PG-02 the Settings frame, light and dark', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-settings-button').click()
  await expect(page.getByTestId('settings-screen')).toBeVisible()
  await frame(page, testInfo, 'settings')
})

test('PG-03 Inbox opens beside the sidebar', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-footer-inbox').click()
  await expect(page.getByTestId('inbox-screen')).toBeVisible()
  if (!narrow(testInfo)) await expect(page.getByTestId('sidebar-content')).toBeVisible()
  await frame(page, testInfo, 'inbox')
})

test('PG-04 Automations opens beside the sidebar', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-quick-automations').click()
  await expect(page.getByTestId('automations-screen')).toBeVisible()
  if (!narrow(testInfo)) await expect(page.getByTestId('sidebar-content')).toBeVisible()
  await frame(page, testInfo, 'automations')
})
