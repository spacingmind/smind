import fs from 'node:fs'
import path from 'node:path'
import { type Locator, type Page, type TestInfo } from '@playwright/test'
import { openTaskByLink } from '../fixtures/open-task'
import { expect, test } from '../fixtures/test'

// DT-UI B native pass. The browser build never sets `html[data-desktop-os]` (the desktop shell does), so these tests set it
// themselves to exercise desktop-chrome.css on the real built UI: the inactive-window styling and the reduced-motion
// fallback. "windows" is used so the macOS-only transparent body does not apply. Frames only when NATIVE_FRAMES_DIR is set.

async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.NATIVE_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

/** The longest transition on the element, in ms. */
const transitionMs = (loc: Locator) =>
  loc.evaluate((el) =>
    Math.max(...getComputedStyle(el).transitionDuration.split(',').map((part) => parseFloat(part) * (part.trim().endsWith('ms') ? 1 : 1000))),
  )

const setWindowActive = (page: Page, active: boolean) =>
  page.evaluate((a) => {
    document.documentElement.dataset.desktopOs = 'windows'
    document.documentElement.dataset.windowActive = a ? 'true' : 'false'
  }, active)

test('NP-01 an inactive window dims the sidebar selection and icons; an active one does not', async ({ app }, testInfo) => {
  const { page } = app
  // The desktop window is never narrower than 800 px, so the drawer layout has no inactive state to style.
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'desktop widths only')
  await openTaskByLink(app, { id: app.daemon.ids.tasks['idle-done'] }, 'idle-done')
  await app.openDrawer()
  const row = page.getByTestId('sidebar-task-item').first()
  await expect(row).toBeVisible()
  const sidebarIcon = page.locator('[data-slot="sidebar-inner"] svg').first()

  await setWindowActive(page, true)
  await expect(sidebarIcon).toHaveCSS('opacity', '1')
  await frame(page, testInfo, 'active')

  await setWindowActive(page, false)
  await expect(sidebarIcon).toHaveCSS('opacity', '0.6')
  const inactiveSelected = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--selected').trim())
  await setWindowActive(page, true)
  const activeSelected = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--selected').trim())
  expect(inactiveSelected).not.toBe(activeSelected)
  // ...and the selected row itself is painted with it: its background changes between the two states.
  const selectedRow = page.locator('[data-slot="sidebar-inner"] .bg-selected:has([aria-current="true"])').first()
  await expect(selectedRow).toBeVisible()
  const bg = () => selectedRow.evaluate((el) => getComputedStyle(el).backgroundColor)
  const activeBg = await bg()
  await setWindowActive(page, false)
  const inactiveBg = await bg()
  expect(inactiveBg).not.toBe(activeBg)
  await setWindowActive(page, false)
  await frame(page, testInfo, 'inactive')
})

test('NP-02 reduced motion removes transitions in the desktop build', async ({ app }, testInfo) => {
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'the sidebar panel exists at desktop widths only')
  const { page } = app
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await app.openApp()
  await setWindowActive(page, true)
  const panel = page.locator('[data-slot="sidebar-panel"]').first()
  expect(await transitionMs(panel)).toBeLessThan(1)
  const slow = await page.evaluate(() => {
    let longest = 0
    for (const el of document.querySelectorAll('*')) {
      if (el.classList.contains('animate-spin')) continue
      for (const part of getComputedStyle(el).transitionDuration.split(',')) {
        const t = parseFloat(part) * (part.trim().endsWith('ms') ? 1 : 1000)
        if (t > longest) longest = t
      }
    }
    return longest
  })
  expect(slow).toBeLessThan(1)
})

test('NP-03 without the reduced-motion setting the sidebar still animates', async ({ app }, testInfo) => {
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'the sidebar panel exists at desktop widths only')
  const { page } = app
  await app.openApp()
  await setWindowActive(page, true)
  const panel = page.locator('[data-slot="sidebar-panel"]').first()
  expect(await transitionMs(panel)).toBeGreaterThanOrEqual(100)
})
