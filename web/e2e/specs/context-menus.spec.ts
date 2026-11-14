import fs from 'node:fs'
import path from 'node:path'
import { type Locator, type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { expect, test } from '../fixtures/test'

// DT2 (a): the context menus are drawn from menu models (lib/menu-models.tsx). In the browser build, which is what this suite
// runs, they are the in-page Radix menus, unchanged: this pins their items and takes the frames. The native menus exist only in
// the desktop app (see docs/plans/active/desktop-native-feel.md). Frames are written only when MENU_FRAMES_DIR is set.

/** Waits for the menu's enter animation to finish so the frame shows the settled menu. */
async function settled(menu: Locator) {
  await menu.evaluate((el) => Promise.all(el.getAnimations().map((a) => a.finished)))
}

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.MENU_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

test('CM-01 a task row opens its context menu with pin, mark unread and archive', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  const row = page.getByTestId('sidebar-task-item').first()
  await expect(row).toBeVisible()
  await row.click({ button: 'right' })
  const menu = page.getByTestId('sidebar-task-context-menu')
  await expect(menu).toBeVisible()
  await expect(menu.getByTestId('sidebar-task-pin-action')).toBeVisible()
  await expect(menu.getByTestId('sidebar-task-mark-unread-action')).toBeVisible()
  await expect(menu.getByTestId('sidebar-task-archive-action')).toBeVisible()
  await settled(menu)
  await frame(page, testInfo, 'a-task-row-menu')
  await page.keyboard.press('Escape')
  await expect(menu).toHaveCount(0)
})

test('CM-02 a workspace row opens its context menu', async ({ app }, testInfo) => {
  const { page } = app
  await app.openApp()
  await app.openDrawer()
  await page.getByTestId('sidebar-workspace-row').first().click({ button: 'right' })
  const menu = page.getByTestId('sidebar-workspace-context-menu')
  await expect(menu).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Add task' })).toBeVisible()
  await expect(menu.getByRole('menuitem', { name: 'Delete workspace' })).toBeVisible()
  await settled(menu)
  await frame(page, testInfo, 'b-workspace-row-menu')
})

test('CM-03 a chat tab opens its context menu: Close others is off with no other tab, Rename and Archive chat are offered', async ({ app }, testInfo) => {
  const { page } = app
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  const tab = page.getByTestId('workspace-tab-chat').first()
  await expect(tab).toBeVisible()
  await tab.click({ button: 'right' })
  const menu = page.getByTestId('workspace-tab-context-menu')
  await expect(menu).toBeVisible()
  await expect(menu.getByTestId('workspace-tab-menu-close-others')).toHaveAttribute('data-disabled', '')
  await expect(menu.getByTestId('workspace-tab-menu-rename')).toBeVisible()
  await expect(menu.getByTestId('workspace-tab-menu-archive-chat')).toBeVisible()
  await settled(menu)
  await frame(page, testInfo, 'c-tab-menu')
  await page.keyboard.press('Escape')
})
