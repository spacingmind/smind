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

test('PG-01 Settings opens as a modal over the sidebar and workspace, which stay visible', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-settings-button').click()
  const settings = page.getByTestId('settings-dialog')
  await expect(settings).toBeVisible()
  const box = await settings.boundingBox()
  const vp = page.viewportSize()
  expect(box && vp && box.x >= 0 && box.y >= 0 && box.x + box.width <= vp.width && box.y + box.height <= vp.height, 'the dialog is within the viewport').toBe(true)
  // Narrow: the sidebar is a drawer that closes behind the dialog, so only the dialog is asserted there.
  if (!narrow(testInfo)) {
    await expect(page.getByTestId('sidebar-content')).toBeVisible()
    const content = page.getByTestId('content-region')
    await expect(content).toBeVisible()
    await expect(content).not.toHaveAttribute('inert', '')
    // Clicking the sidebar while Settings is open is blocked on purpose now: a modal blocks interaction outside it.
  }
})

test('PG-02 the Settings frame, light and dark', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-settings-button').click()
  await expect(page.getByTestId('settings-dialog')).toBeVisible()
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
