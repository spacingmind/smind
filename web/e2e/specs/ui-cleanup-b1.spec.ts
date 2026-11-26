import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { expect, test } from '../fixtures/test'

// UI cleanup B1 (docs/plans/active/ui-cleanup-b1.md H2) on the built UI: the
// redundant visual shortcut surfaces are gone while the shortcuts behind them
// keep working. The palette check runs on the web build, where the desktop
// entries (peer A's palette source) must never register. Frames only when
// UICLEAN_FRAMES_DIR is set, one per project, like native-pass.spec.ts.
async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.UICLEAN_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

test('UC-01 the header has no keyboard icon', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await expect(page.getByTestId('app-header')).toBeVisible()
  await expect(page.getByTestId('app-header-help')).toHaveCount(0)
  await frame(page, testInfo, 'uc01-header')
})

test('UC-02 the sidebar toolbar has no plus but keeps search and filter', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await app.openDrawer()
  await expect(page.getByTestId('sidebar-search-toggle').first()).toBeVisible()
  await expect(page.getByTestId('sidebar-automation-filter').first()).toBeVisible()
  await expect(page.getByTestId('sidebar-new-workspace-button')).toHaveCount(0)
  await frame(page, testInfo, 'uc02-sidebar-toolbar')
})

test('UC-03 the composer has no focus hint', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await expect(page.getByTestId('composer-focus-hint')).toHaveCount(0)
  await frame(page, testInfo, 'uc03-composer')
})

test('UC-04 the palette on web lists no desktop entries', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await page.keyboard.press('ControlOrMeta+k')
  await expect(page.getByRole('dialog', { name: 'Command palette' })).toBeVisible()
  await page.getByTestId('command-palette-input').fill('Reload')
  await expect(page.getByRole('option', { name: /^Reload/ })).toHaveCount(0)
  await frame(page, testInfo, 'uc04-palette')
})
