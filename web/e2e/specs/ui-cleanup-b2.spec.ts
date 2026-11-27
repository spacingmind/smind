import fs from 'node:fs'
import path from 'node:path'
import { type Page, type TestInfo } from '@playwright/test'
import { expect, test } from '../fixtures/test'

// UI cleanup B2 (docs/plans/active/ui-cleanup-b2.md): D1 (one content column, so no tab shifts the
// content left or right) and D2 (the Agents / Safety & usage nav groups), measured on the running UI.
// Frames only when UICLEAN2_FRAMES_DIR is set: one per tab, <index>-<id>-<project>.png.

// Below 768px the Settings nav is a list page that replaces the detail page (no settings-page-title,
// a different content wrapper), so both checks run on the two-column layout only.
const wideOnly = (testInfo: TestInfo) =>
  test.skip((testInfo.project.use.viewport?.width ?? 1280) < 768, 'the two-column Settings layout exists at >=768px; at 390 the nav list replaces the page')

const settingsNav = (page: Page) => page.getByRole('navigation', { name: 'Settings sections' })

async function openSettings(page: Page) {
  await page.evaluate(() => window.dispatchEvent(new CustomEvent('smind:open-settings', { detail: { sectionId: 'general' } })))
  await expect(page.getByTestId('settings-screen').first()).toBeVisible()
}

// A frame of the settled tab: finite animations (screen and section entrances) finish first so the
// screenshot is not mid-transition.
async function frame(page: Page, testInfo: TestInfo, name: string) {
  const dir = process.env.UICLEAN2_FRAMES_DIR
  if (!dir) return
  fs.mkdirSync(dir, { recursive: true })
  await page.evaluate(() =>
    Promise.all(
      document
        .getAnimations()
        .filter((a) => a.effect?.getComputedTiming().iterations !== Infinity)
        .map((a) => a.finished.catch(() => undefined)),
    ),
  )
  await page.screenshot({ path: path.join(dir, `${name}-${testInfo.project.name}.png`) })
}

test('UC2-01 the content column starts at the same x on every tab', async ({ app }, testInfo) => {
  wideOnly(testInfo)
  const { page } = app
  await app.openApp()
  await openSettings(page)
  const nav = settingsNav(page)
  await expect(nav).toBeVisible()
  const ids = await nav.locator('[data-testid^="settings-nav-"]').evaluateAll((els) =>
    els.map((e) => (e.getAttribute('data-testid') ?? '').replace('settings-nav-', '')).filter((s) => s && s !== 'back'),
  )
  expect(ids.length, 'settings sections found in the nav').toBeGreaterThan(0)

  const title = page.getByTestId('settings-page-title')
  // The title's parent is the content column itself (settings-content's one child, the mx-auto
  // max-w-3xl div), so its box is the column's box.
  const column = title.locator('xpath=..')
  const xs: Record<string, number> = {}
  const widths: Record<string, number> = {}
  let index = 0
  for (const id of ids) {
    const row = page.locator(`[data-testid="settings-nav-${id}"]`)
    await row.click()
    // aria-current flips in the same React commit that swaps the page content (the title element
    // itself is reused between tabs), so this is the wait for the tab to really be active.
    await expect(row).toHaveAttribute('aria-current', 'true')
    await expect(title).toBeVisible()
    const titleBox = await title.boundingBox()
    const columnBox = await column.boundingBox()
    xs[id] = Math.round(titleBox?.x ?? -1)
    widths[id] = Math.round(columnBox?.width ?? -1)
    await frame(page, testInfo, `${String(index++).padStart(2, '0')}-${id}`)
  }
  expect(new Set(Object.values(xs)).size, `title x per tab (id -> x): ${JSON.stringify(xs)}`).toBe(1)
  expect(new Set(Object.values(widths)).size, `content column width per tab (id -> width): ${JSON.stringify(widths)}`).toBe(1)
})

test('UC2-02 the nav has the Agents and Safety & usage groups', async ({ app }, testInfo) => {
  wideOnly(testInfo)
  const { page } = app
  await app.openApp()
  await openSettings(page)
  const nav = settingsNav(page)
  await expect(nav).toBeVisible()
  await expect(nav).toContainText('Agents')
  await expect(nav).toContainText('Safety & usage')
  await expect(nav).not.toContainText('Profiles & providers')
  const order = await nav.locator('[data-testid^="settings-nav-"]').evaluateAll((els) => els.map((e) => e.getAttribute('data-testid') ?? ''))
  const at = (id: string) => order.indexOf(`settings-nav-${id}`)
  const groups: Array<[string, string[]]> = [
    ['Agents', ['agents', 'providers', 'mcp-servers']],
    ['Safety & usage', ['permission-policy', 'saved-grants', 'browser', 'usage']],
  ]
  for (const [group, ids] of groups) {
    const positions = ids.map(at)
    const list = order.join(' > ')
    for (let j = 0; j < ids.length; j++) expect(positions[j], `settings-nav-${ids[j]} exists in the nav (${list})`).toBeGreaterThan(-1)
    for (let j = 1; j < ids.length; j++)
      expect(positions[j], `${group}: settings-nav-${ids[j - 1]} comes before settings-nav-${ids[j]} (${list})`).toBeGreaterThan(positions[j - 1])
  }
})
